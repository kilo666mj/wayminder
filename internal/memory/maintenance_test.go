package memory

import (
	"context"
	"reflect"
	"testing"
)

type fakeMaintenanceStore struct {
	pairs     []DuplicatePair
	chains    []RevisionCount
	models    map[string]int64
	live      []Memory
	pairLimit int
	shared    []string
}

func (f *fakeMaintenanceStore) DuplicatePairs(_ context.Context, _ float64, shared []string, limit int) ([]DuplicatePair, error) {
	f.pairLimit, f.shared = limit, shared
	if len(f.pairs) > limit {
		return f.pairs[:limit], nil
	}
	return f.pairs, nil
}

func (f *fakeMaintenanceStore) SupersedeChains(context.Context, int, int) ([]RevisionCount, error) {
	return f.chains, nil
}

func (f *fakeMaintenanceStore) EmbeddingModelCounts(context.Context) (map[string]int64, error) {
	return f.models, nil
}

func (f *fakeMaintenanceStore) LiveByIDs(_ context.Context, ids []string) ([]Memory, error) {
	wanted := map[string]bool{}
	for _, id := range ids {
		wanted[id] = true
	}
	var result []Memory
	for _, item := range f.live {
		if wanted[item.ID] {
			result = append(result, item)
		}
	}
	return result, nil
}

func liveMemories(ids ...string) []Memory {
	items := make([]Memory, 0, len(ids))
	for _, id := range ids {
		items = append(items, Memory{ID: id, Scope: "repo:example", Kind: "fact", Summary: "summary " + id, Content: "content " + id})
	}
	return items
}

func memberIDs(cluster DuplicateCluster) []string {
	ids := make([]string, 0, len(cluster.Members))
	for _, member := range cluster.Members {
		ids = append(ids, member.ID)
	}
	return ids
}

func defaultMaintenanceRequest() MaintenanceRequest {
	return MaintenanceRequest{DuplicateThreshold: 0.85, MinRevisions: 3, MaxPairs: 10, MaxChains: 10}
}

func TestMaintenanceReportClustersConnectedPairs(t *testing.T) {
	store := &fakeMaintenanceStore{
		pairs: []DuplicatePair{
			{A: "d", B: "e", Similarity: 0.97},
			{A: "a", B: "b", Similarity: 0.95},
			{A: "b", B: "c", Similarity: 0.88},
		},
		live: liveMemories("a", "b", "c", "d", "e"),
	}
	report, err := BuildMaintenanceReport(context.Background(), store, "model", defaultMaintenanceRequest())
	if err != nil {
		t.Fatalf("BuildMaintenanceReport() error = %v", err)
	}
	if len(report.DuplicateClusters) != 2 || report.DuplicatePairs != 3 || report.PairsTruncated {
		t.Fatalf("unexpected clusters: %#v", report)
	}
	first, second := report.DuplicateClusters[0], report.DuplicateClusters[1]
	if !reflect.DeepEqual(memberIDs(first), []string{"d", "e"}) || first.MaxSimilarity != 0.97 {
		t.Fatalf("unexpected first cluster: %#v", first)
	}
	if !reflect.DeepEqual(memberIDs(second), []string{"a", "b", "c"}) || second.MaxSimilarity != 0.95 || second.MinSimilarity != 0.88 {
		t.Fatalf("unexpected second cluster: %#v", second)
	}
	if !reflect.DeepEqual(store.shared, []string{"global", "personal"}) {
		t.Fatalf("shared scopes = %v", store.shared)
	}
}

func TestMaintenanceReportOmitsContentUnlessRequested(t *testing.T) {
	store := &fakeMaintenanceStore{
		pairs:  []DuplicatePair{{A: "a", B: "b", Similarity: 0.9}},
		chains: []RevisionCount{{ID: "a", Revisions: 4}},
		live:   liveMemories("a", "b"),
	}
	request := defaultMaintenanceRequest()
	report, err := BuildMaintenanceReport(context.Background(), store, "model", request)
	if err != nil {
		t.Fatalf("BuildMaintenanceReport() error = %v", err)
	}
	for _, member := range append(report.DuplicateClusters[0].Members, report.SupersedeChains[0].Memory) {
		if member.Content != "" || member.Summary != "" {
			t.Fatalf("report leaked content without -content: %#v", member)
		}
	}
	request.IncludeContent = true
	report, err = BuildMaintenanceReport(context.Background(), store, "model", request)
	if err != nil {
		t.Fatalf("BuildMaintenanceReport() error = %v", err)
	}
	if got := report.DuplicateClusters[0].Members[0]; got.Content != "content a" || got.Summary != "summary a" {
		t.Fatalf("content missing when requested: %#v", got)
	}
}

func TestMaintenanceReportMarksTruncatedPairs(t *testing.T) {
	store := &fakeMaintenanceStore{
		pairs: []DuplicatePair{{A: "a", B: "b", Similarity: 0.99}, {A: "c", B: "d", Similarity: 0.9}},
		live:  liveMemories("a", "b", "c", "d"),
	}
	request := defaultMaintenanceRequest()
	request.MaxPairs = 1
	report, err := BuildMaintenanceReport(context.Background(), store, "model", request)
	if err != nil {
		t.Fatalf("BuildMaintenanceReport() error = %v", err)
	}
	if store.pairLimit != 2 || !report.PairsTruncated || report.DuplicatePairs != 1 || len(report.DuplicateClusters) != 1 {
		t.Fatalf("unexpected truncation: limit=%d report=%#v", store.pairLimit, report)
	}
}

func TestMaintenanceReportSkipsMemoriesNoLongerLive(t *testing.T) {
	store := &fakeMaintenanceStore{
		pairs:  []DuplicatePair{{A: "a", B: "gone", Similarity: 0.99}},
		chains: []RevisionCount{{ID: "gone", Revisions: 5}, {ID: "a", Revisions: 3}},
		live:   liveMemories("a"),
	}
	report, err := BuildMaintenanceReport(context.Background(), store, "model", defaultMaintenanceRequest())
	if err != nil {
		t.Fatalf("BuildMaintenanceReport() error = %v", err)
	}
	if len(report.DuplicateClusters) != 0 {
		t.Fatalf("cluster kept a memory that is no longer live: %#v", report.DuplicateClusters)
	}
	if len(report.SupersedeChains) != 1 || report.SupersedeChains[0].Memory.ID != "a" {
		t.Fatalf("unexpected chains: %#v", report.SupersedeChains)
	}
}

func TestMaintenanceReportListsOnlyOtherEmbeddingModels(t *testing.T) {
	store := &fakeMaintenanceStore{models: map[string]int64{"model": 40, "older-model": 2}}
	report, err := BuildMaintenanceReport(context.Background(), store, "model", defaultMaintenanceRequest())
	if err != nil {
		t.Fatalf("BuildMaintenanceReport() error = %v", err)
	}
	if !reflect.DeepEqual(report.ModelMismatches, map[string]int64{"older-model": 2}) {
		t.Fatalf("ModelMismatches = %v", report.ModelMismatches)
	}
}

func TestMaintenanceReportRejectsInvalidRequest(t *testing.T) {
	for _, request := range []MaintenanceRequest{
		{DuplicateThreshold: 0, MinRevisions: 3, MaxPairs: 10, MaxChains: 10},
		{DuplicateThreshold: 1.5, MinRevisions: 3, MaxPairs: 10, MaxChains: 10},
		{DuplicateThreshold: 0.85, MinRevisions: 0, MaxPairs: 10, MaxChains: 10},
		{DuplicateThreshold: 0.85, MinRevisions: 3, MaxPairs: 0, MaxChains: 10},
	} {
		if _, err := BuildMaintenanceReport(context.Background(), &fakeMaintenanceStore{}, "model", request); err == nil {
			t.Fatalf("BuildMaintenanceReport() accepted %#v", request)
		}
	}
}
