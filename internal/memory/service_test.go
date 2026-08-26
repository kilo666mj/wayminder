package memory

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

type fakeEmbedder struct{ vector []float32 }

func (f fakeEmbedder) Embed(context.Context, []string) ([][]float32, error) {
	return [][]float32{f.vector}, nil
}
func (fakeEmbedder) Ping(context.Context) error { return nil }
func (fakeEmbedder) Dimension() int             { return 3 }
func (fakeEmbedder) Model() string              { return "fake" }

type fakeStore struct {
	duplicate *Memory
	existing  Memory
	inserted  Memory
	replaced  Memory
}

func (f *fakeStore) Get(context.Context, string, bool) (Memory, error) { return f.existing, nil }
func (f *fakeStore) FindDuplicate(context.Context, []float32, string, float64) (*Memory, error) {
	return f.duplicate, nil
}
func (f *fakeStore) Insert(_ context.Context, m Memory, _ []float32) (Memory, error) {
	f.inserted = m
	return m, nil
}
func (f *fakeStore) Replace(_ context.Context, _ string, m Memory, _ []float32) (Memory, error) {
	f.replaced = m
	return m, nil
}
func (*fakeStore) Recall(context.Context, []float32, string, []string, string, int) ([]Memory, error) {
	return nil, nil
}
func (*fakeStore) List(context.Context, []string, string, int, string) ([]Memory, error) {
	return nil, nil
}
func (*fakeStore) Forget(context.Context, string, Principal) (Memory, error) {
	return Memory{}, errors.New("not implemented")
}
func (*fakeStore) Stats(context.Context) (Stats, error) { return Stats{}, nil }
func (*fakeStore) Ping(context.Context) error           { return nil }

func TestEffectiveScopes(t *testing.T) {
	got := EffectiveScopes("repo:wayminder")
	want := []string{"global", "personal", "repo:wayminder"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("EffectiveScopes() = %v, want %v", got, want)
	}
}

func TestRememberCreatesMemory(t *testing.T) {
	store := &fakeStore{}
	service := NewService(store, fakeEmbedder{[]float32{1, 2, 3}}, .92, 1024)
	result, err := service.Remember(context.Background(), RememberRequest{Content: "Postgres runs on app-host", Scope: "host:app-host"}, Principal{Agent: "codex"})
	if err != nil {
		t.Fatalf("Remember() error = %v", err)
	}
	if result.Action != "created" || result.Memory.Kind != "reference" || result.Memory.AuthorAgent != "codex" {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestRememberRejectsSecrets(t *testing.T) {
	service := NewService(&fakeStore{}, fakeEmbedder{}, .92, 1024)
	_, err := service.Remember(context.Background(), RememberRequest{Content: "api_key=1234567890abcdef", Scope: "global"}, Principal{})
	if err == nil {
		t.Fatal("Remember() accepted a likely secret")
	}
}

func TestRememberRejectsSecretsInEveryStoredUserField(t *testing.T) {
	service := NewService(&fakeStore{}, fakeEmbedder{}, .92, 1024)
	tests := []RememberRequest{
		{Content: "safe", Summary: "password=1234567890abcdef"},
		{Content: "safe", Tags: []string{"api_key=1234567890abcdef"}},
	}
	for _, request := range tests {
		if _, err := service.Remember(context.Background(), request, Principal{}); err == nil {
			t.Fatalf("Remember() accepted likely secret in %#v", request)
		}
	}
	if _, err := service.Remember(context.Background(), RememberRequest{Content: "safe"}, Principal{Source: "access_token=1234567890abcdef"}); err == nil {
		t.Fatal("Remember() accepted likely secret in provenance")
	}
}

func TestRememberReturnsExactDuplicate(t *testing.T) {
	existing := &Memory{ID: "01J00000000000000000000000", Content: "Use PostgreSQL"}
	store := &fakeStore{duplicate: existing}
	service := NewService(store, fakeEmbedder{[]float32{1, 2, 3}}, .92, 1024)
	result, err := service.Remember(context.Background(), RememberRequest{Content: "  use   postgresql ", Scope: "global"}, Principal{})
	if err != nil {
		t.Fatalf("Remember() error = %v", err)
	}
	if result.Action != "existing" || store.inserted.ID != "" || store.replaced.ID != "" {
		t.Fatalf("unexpected duplicate behavior: %#v", result)
	}
}
