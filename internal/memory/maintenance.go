package memory

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"
)

// DuplicatePair identifies two live memories whose embeddings are at least as
// similar as the requested threshold and that one recall can return together.
type DuplicatePair struct {
	A          string
	B          string
	Similarity float64
}

// RevisionCount is the number of earlier versions behind one live memory.
type RevisionCount struct {
	ID        string
	Revisions int
}

// MaintenanceStore is the read-only persistence needed by the offline
// maintenance report. It is deliberately separate from Store: nothing here is
// reachable from the request path.
type MaintenanceStore interface {
	DuplicatePairs(ctx context.Context, threshold float64, sharedScopes []string, limit int) ([]DuplicatePair, error)
	SupersedeChains(ctx context.Context, minRevisions, limit int) ([]RevisionCount, error)
	EmbeddingModelCounts(ctx context.Context) (map[string]int64, error)
	LiveByIDs(ctx context.Context, ids []string) ([]Memory, error)
}

type MaintenanceRequest struct {
	// DuplicateThreshold is the minimum cosine similarity for a reported pair.
	DuplicateThreshold float64
	// MinRevisions is the minimum number of earlier versions for a reported chain.
	MinRevisions int
	// MaxPairs bounds the duplicate pairs considered, most similar first.
	MaxPairs int
	// MaxChains bounds the reported supersede chains, longest first.
	MaxChains int
	// IncludeContent adds memory summaries and content to the report.
	IncludeContent bool
}

// MemoryRef describes a memory in a report. Summary and content are present
// only when the operator asked for them.
type MemoryRef struct {
	ID          string    `json:"id"`
	Scope       string    `json:"scope"`
	Kind        string    `json:"kind"`
	AuthorAgent string    `json:"author_agent,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	Summary     string    `json:"summary,omitempty"`
	Content     string    `json:"content,omitempty"`
}

type DuplicateCluster struct {
	Members       []MemoryRef `json:"members"`
	MaxSimilarity float64     `json:"max_similarity"`
	MinSimilarity float64     `json:"min_similarity"`
}

type SupersedeChain struct {
	Memory    MemoryRef `json:"memory"`
	Revisions int       `json:"revisions"`
}

type MaintenanceReport struct {
	GeneratedAt        time.Time          `json:"generated_at"`
	EmbeddingModel     string             `json:"embedding_model"`
	DuplicateThreshold float64            `json:"duplicate_threshold"`
	DuplicatePairs     int                `json:"duplicate_pairs"`
	PairsTruncated     bool               `json:"pairs_truncated"`
	DuplicateClusters  []DuplicateCluster `json:"duplicate_clusters"`
	MinRevisions       int                `json:"min_revisions"`
	SupersedeChains    []SupersedeChain   `json:"supersede_chains"`
	ModelMismatches    map[string]int64   `json:"embedding_model_mismatches"`
}

// NewMemoryRef reduces a memory to its report form.
func NewMemoryRef(item Memory, includeContent bool) MemoryRef {
	ref := MemoryRef{
		ID: item.ID, Scope: item.Scope, Kind: item.Kind,
		AuthorAgent: item.AuthorAgent, CreatedAt: item.CreatedAt,
	}
	if includeContent {
		ref.Summary, ref.Content = item.Summary, item.Content
	}
	return ref
}

// BuildMaintenanceReport reads the store and reports consolidation candidates.
// It never mutates memories: acting on the report is a separate, audited
// supersede or forget.
func BuildMaintenanceReport(ctx context.Context, store MaintenanceStore, embeddingModel string, request MaintenanceRequest) (MaintenanceReport, error) {
	if request.DuplicateThreshold <= 0 || request.DuplicateThreshold > 1 {
		return MaintenanceReport{}, errors.New("duplicate threshold must be in (0, 1]")
	}
	if request.MinRevisions < 1 {
		return MaintenanceReport{}, errors.New("minimum revisions must be at least 1")
	}
	if request.MaxPairs < 1 || request.MaxChains < 1 {
		return MaintenanceReport{}, errors.New("pair and chain limits must be at least 1")
	}
	report := MaintenanceReport{
		GeneratedAt: time.Now().UTC(), EmbeddingModel: embeddingModel,
		DuplicateThreshold: request.DuplicateThreshold, MinRevisions: request.MinRevisions,
		DuplicateClusters: []DuplicateCluster{}, SupersedeChains: []SupersedeChain{},
		ModelMismatches: map[string]int64{},
	}

	pairs, err := store.DuplicatePairs(ctx, request.DuplicateThreshold, []string{"global", "personal"}, request.MaxPairs+1)
	if err != nil {
		return MaintenanceReport{}, fmt.Errorf("find duplicate pairs: %w", err)
	}
	if len(pairs) > request.MaxPairs {
		pairs, report.PairsTruncated = pairs[:request.MaxPairs], true
	}
	report.DuplicatePairs = len(pairs)

	chains, err := store.SupersedeChains(ctx, request.MinRevisions, request.MaxChains)
	if err != nil {
		return MaintenanceReport{}, fmt.Errorf("find supersede chains: %w", err)
	}

	ids := make([]string, 0, 2*len(pairs)+len(chains))
	for _, pair := range pairs {
		ids = append(ids, pair.A, pair.B)
	}
	for _, chain := range chains {
		ids = append(ids, chain.ID)
	}
	refs := map[string]MemoryRef{}
	if len(ids) > 0 {
		items, err := store.LiveByIDs(ctx, ids)
		if err != nil {
			return MaintenanceReport{}, fmt.Errorf("load reported memories: %w", err)
		}
		for _, item := range items {
			refs[item.ID] = NewMemoryRef(item, request.IncludeContent)
		}
	}

	report.DuplicateClusters = clusterPairs(pairs, refs)
	for _, chain := range chains {
		// A memory superseded or forgotten since the chain query is no longer a
		// candidate.
		if ref, ok := refs[chain.ID]; ok {
			report.SupersedeChains = append(report.SupersedeChains, SupersedeChain{Memory: ref, Revisions: chain.Revisions})
		}
	}

	counts, err := store.EmbeddingModelCounts(ctx)
	if err != nil {
		return MaintenanceReport{}, fmt.Errorf("count embedding models: %w", err)
	}
	for model, count := range counts {
		if model != embeddingModel {
			report.ModelMismatches[model] = count
		}
	}
	return report, nil
}

// clusterPairs groups pairs into connected components, so three memories that
// restate one fact are reported once rather than as three pairs.
func clusterPairs(pairs []DuplicatePair, refs map[string]MemoryRef) []DuplicateCluster {
	parent := map[string]string{}
	var find func(string) string
	find = func(id string) string {
		if parent[id] == id {
			return id
		}
		parent[id] = find(parent[id])
		return parent[id]
	}
	live := make([]DuplicatePair, 0, len(pairs))
	for _, pair := range pairs {
		if _, ok := refs[pair.A]; !ok {
			continue
		}
		if _, ok := refs[pair.B]; !ok {
			continue
		}
		live = append(live, pair)
		for _, id := range []string{pair.A, pair.B} {
			if _, ok := parent[id]; !ok {
				parent[id] = id
			}
		}
		parent[find(pair.A)] = find(pair.B)
	}

	byRoot := map[string]*DuplicateCluster{}
	for _, pair := range live {
		root := find(pair.A)
		cluster, ok := byRoot[root]
		if !ok {
			cluster = &DuplicateCluster{MaxSimilarity: pair.Similarity, MinSimilarity: pair.Similarity}
			byRoot[root] = cluster
		}
		cluster.MaxSimilarity = max(cluster.MaxSimilarity, pair.Similarity)
		cluster.MinSimilarity = min(cluster.MinSimilarity, pair.Similarity)
	}
	for id := range parent {
		cluster := byRoot[find(id)]
		cluster.Members = append(cluster.Members, refs[id])
	}

	clusters := make([]DuplicateCluster, 0, len(byRoot))
	for _, cluster := range byRoot {
		sort.Slice(cluster.Members, func(i, j int) bool { return cluster.Members[i].ID < cluster.Members[j].ID })
		clusters = append(clusters, *cluster)
	}
	sort.Slice(clusters, func(i, j int) bool {
		if clusters[i].MaxSimilarity != clusters[j].MaxSimilarity {
			return clusters[i].MaxSimilarity > clusters[j].MaxSimilarity
		}
		return clusters[i].Members[0].ID < clusters[j].Members[0].ID
	})
	return clusters
}
