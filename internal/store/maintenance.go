package store

import (
	"context"

	"github.com/kilo666mj/wayminder/internal/memory"
)

// DuplicatePairs compares every pair of live memories that one recall can
// return together: the same scope, or either side in a shared scope. The
// comparison is exact and quadratic in the number of live memories, which is
// acceptable only because it runs offline.
func (p *Postgres) DuplicatePairs(ctx context.Context, threshold float64, sharedScopes []string, limit int) ([]memory.DuplicatePair, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT a.id, b.id, 1 - (a.embedding <=> b.embedding) AS similarity
		FROM memories a
		JOIN memories b ON a.id < b.id
		  AND (a.scope = b.scope OR a.scope = ANY($2::text[]) OR b.scope = ANY($2::text[]))
		WHERE a.deleted_at IS NULL AND b.deleted_at IS NULL
		  AND 1 - (a.embedding <=> b.embedding) >= $1
		ORDER BY similarity DESC, a.id, b.id
		LIMIT $3`, threshold, sharedScopes, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []memory.DuplicatePair
	for rows.Next() {
		var pair memory.DuplicatePair
		if err := rows.Scan(&pair.A, &pair.B, &pair.Similarity); err != nil {
			return nil, err
		}
		result = append(result, pair)
	}
	return result, rows.Err()
}

// SupersedeChains returns live memories with at least minRevisions earlier
// versions, longest history first.
func (p *Postgres) SupersedeChains(ctx context.Context, minRevisions, limit int) ([]memory.RevisionCount, error) {
	rows, err := p.pool.Query(ctx, `
		WITH RECURSIVE chain AS (
			SELECT id AS head, supersedes_id AS previous, 1 AS revisions
			FROM memories
			WHERE deleted_at IS NULL AND supersedes_id IS NOT NULL
			UNION ALL
			SELECT chain.head, m.supersedes_id, chain.revisions + 1
			FROM chain
			JOIN memories m ON m.id = chain.previous
			WHERE m.supersedes_id IS NOT NULL
		)
		SELECT head, max(revisions) AS revisions
		FROM chain
		GROUP BY head
		HAVING max(revisions) >= $1
		ORDER BY revisions DESC, head
		LIMIT $2`, minRevisions, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []memory.RevisionCount
	for rows.Next() {
		var item memory.RevisionCount
		if err := rows.Scan(&item.ID, &item.Revisions); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (p *Postgres) EmbeddingModelCounts(ctx context.Context) (map[string]int64, error) {
	counts := map[string]int64{}
	if err := p.countBy(ctx, "embedding_model", counts); err != nil {
		return nil, err
	}
	return counts, nil
}

func (p *Postgres) LiveByIDs(ctx context.Context, ids []string) ([]memory.Memory, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT `+memoryColumns+`
		FROM memories
		WHERE deleted_at IS NULL AND id = ANY($1::text[])`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []memory.Memory
	for rows.Next() {
		item, err := scanMemory(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

// Sample returns up to limit live memories in an order fixed by seed, so a
// probe run can be repeated against the same memories. An empty scope samples
// every scope; otherwise the scope is matched exactly.
func (p *Postgres) Sample(ctx context.Context, scope, seed string, limit int) ([]memory.Memory, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT `+memoryColumns+`
		FROM memories
		WHERE deleted_at IS NULL AND ($1 = '' OR scope = $1)
		ORDER BY md5(id || $2), id
		LIMIT $3`, scope, seed, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []memory.Memory
	for rows.Next() {
		item, err := scanMemory(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}
