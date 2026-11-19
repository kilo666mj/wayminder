package store

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/kilo666mj/wayminder/internal/memory"
	"github.com/oklog/ulid/v2"
)

const integrationDimension = 768

// openIntegration connects to a disposable pgvector database. The tests are
// skipped unless WAYMINDER_TEST_DATABASE_URL names one.
func openIntegration(t *testing.T) *Postgres {
	t.Helper()
	url := os.Getenv("WAYMINDER_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("WAYMINDER_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	database, err := Open(ctx, url, integrationDimension)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(database.Close)
	return database
}

// vector returns a unit-length-agnostic test embedding with the given
// components set and every other component zero.
func vector(components map[int]float32) []float32 {
	result := make([]float32, integrationDimension)
	for index, value := range components {
		result[index] = value
	}
	return result
}

func insertTestMemory(t *testing.T, database *Postgres, scope, model string, embedding []float32) memory.Memory {
	t.Helper()
	now := time.Now().UTC()
	item, err := database.Insert(context.Background(), memory.Memory{
		ID: ulid.Make().String(), Content: "content", Kind: "fact", Scope: scope,
		Tags: []string{}, EmbeddingModel: model, CreatedAt: now, UpdatedAt: now,
	}, embedding)
	if err != nil {
		t.Fatalf("Insert() error = %v", err)
	}
	return item
}

func TestMaintenanceQueries(t *testing.T) {
	database := openIntegration(t)
	ctx := context.Background()
	run := ulid.Make().String()
	scopeA, scopeB, shared := "test:a-"+run, "test:b-"+run, "test:shared-"+run
	model := "model-" + run
	// Each run uses its own pair of axes so that reruns against the same
	// database do not produce duplicates of earlier runs.
	axis := int(ulid.Make().Time() % 300)
	base := vector(map[int]float32{axis: 1})
	near := vector(map[int]float32{axis: 1, axis + 300: 0.1})
	other := vector(map[int]float32{axis + 400: 1})

	first := insertTestMemory(t, database, scopeA, model, base)
	second := insertTestMemory(t, database, scopeA, model, near)
	otherScope := insertTestMemory(t, database, scopeB, model, base)
	inShared := insertTestMemory(t, database, shared, model, near)
	forgotten := insertTestMemory(t, database, scopeA, model, base)
	if _, err := database.Forget(ctx, forgotten.ID, memory.Principal{}); err != nil {
		t.Fatalf("Forget() error = %v", err)
	}

	head := insertTestMemory(t, database, scopeA, "older-"+run, other)
	for range 3 {
		now := time.Now().UTC()
		replacement := memory.Memory{
			ID: ulid.Make().String(), Content: "content", Kind: "fact", Scope: scopeA,
			Tags: []string{}, EmbeddingModel: "older-" + run, SupersedesID: head.ID, CreatedAt: now, UpdatedAt: now,
		}
		var err error
		if head, err = database.Replace(ctx, head.ID, replacement, other); err != nil {
			t.Fatalf("Replace() error = %v", err)
		}
	}

	mine := map[string]bool{first.ID: true, second.ID: true, otherScope.ID: true, inShared.ID: true, forgotten.ID: true, head.ID: true}
	pairs, err := database.DuplicatePairs(ctx, 0.9, []string{shared}, 100000)
	if err != nil {
		t.Fatalf("DuplicatePairs() error = %v", err)
	}
	found := map[[2]string]float64{}
	for _, pair := range pairs {
		if mine[pair.A] && mine[pair.B] {
			if pair.A >= pair.B {
				t.Fatalf("pair is not ordered: %#v", pair)
			}
			found[[2]string{pair.A, pair.B}] = pair.Similarity
		}
	}
	key := func(a, b string) [2]string {
		if a > b {
			a, b = b, a
		}
		return [2]string{a, b}
	}
	// Same scope, and every memory against the shared scope.
	for _, want := range [][2]string{key(first.ID, second.ID), key(first.ID, inShared.ID), key(second.ID, inShared.ID), key(otherScope.ID, inShared.ID)} {
		if similarity, ok := found[want]; !ok || similarity < 0.9 || similarity > 1.0001 {
			t.Fatalf("missing pair %v (similarity %v) in %v", want, similarity, found)
		}
	}
	// Two unshared scopes are never recalled together, and forgotten or
	// dissimilar memories are not candidates.
	if len(found) != 4 {
		t.Fatalf("unexpected pairs: %v", found)
	}

	chains, err := database.SupersedeChains(ctx, 3, 100000)
	if err != nil {
		t.Fatalf("SupersedeChains() error = %v", err)
	}
	var revisions int
	for _, chain := range chains {
		if chain.ID == head.ID {
			revisions = chain.Revisions
		}
	}
	if revisions != 3 {
		t.Fatalf("head revisions = %d, want 3 (chains %v)", revisions, chains)
	}
	if longer, err := database.SupersedeChains(ctx, 4, 100000); err != nil {
		t.Fatalf("SupersedeChains() error = %v", err)
	} else {
		for _, chain := range longer {
			if chain.ID == head.ID {
				t.Fatalf("head reported with minimum 4 revisions: %#v", chain)
			}
		}
	}

	counts, err := database.EmbeddingModelCounts(ctx)
	if err != nil {
		t.Fatalf("EmbeddingModelCounts() error = %v", err)
	}
	if counts[model] != 4 || counts["older-"+run] != 1 {
		t.Fatalf("unexpected model counts for this run: %v", counts)
	}

	live, err := database.LiveByIDs(ctx, []string{first.ID, forgotten.ID, head.ID})
	if err != nil {
		t.Fatalf("LiveByIDs() error = %v", err)
	}
	if len(live) != 2 {
		t.Fatalf("LiveByIDs() returned %d memories, want the 2 live ones", len(live))
	}

	sample, err := database.Sample(ctx, scopeA, "seed", 10)
	if err != nil {
		t.Fatalf("Sample() error = %v", err)
	}
	again, err := database.Sample(ctx, scopeA, "seed", 10)
	if err != nil {
		t.Fatalf("Sample() error = %v", err)
	}
	if len(sample) != 3 || len(again) != 3 {
		t.Fatalf("Sample() returned %d and %d memories, want 3 live in scope", len(sample), len(again))
	}
	for index := range sample {
		if sample[index].ID != again[index].ID || sample[index].Scope != scopeA {
			t.Fatalf("Sample() is not repeatable or left the scope: %v vs %v", sample[index], again[index])
		}
	}
	if limited, err := database.Sample(ctx, "", "seed", 2); err != nil || len(limited) != 2 {
		t.Fatalf("Sample() across scopes = %d memories, error %v", len(limited), err)
	}
}
