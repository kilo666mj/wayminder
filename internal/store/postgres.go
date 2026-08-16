package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kilo666mj/wayminder/internal/memory"
	"github.com/pgvector/pgvector-go"
	pgxvec "github.com/pgvector/pgvector-go/pgx"
)

type Postgres struct{ pool *pgxpool.Pool }

func Open(ctx context.Context, databaseURL string, expectedDimension int) (*Postgres, error) {
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database URL: %w", err)
	}
	config.MaxConns = 10
	config.MinConns = 1
	config.MaxConnLifetime = time.Hour
	config.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		return pgxvec.RegisterTypes(ctx, conn)
	}
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	if err := migrate(ctx, pool); err != nil {
		pool.Close()
		return nil, err
	}
	var vectorType string
	if err := pool.QueryRow(ctx, `
		SELECT format_type(a.atttypid, a.atttypmod)
		FROM pg_attribute a
		JOIN pg_class c ON c.oid = a.attrelid
		WHERE c.relname = 'memories' AND a.attname = 'embedding' AND NOT a.attisdropped
	`).Scan(&vectorType); err != nil {
		pool.Close()
		return nil, fmt.Errorf("read embedding column type: %w", err)
	}
	expected := fmt.Sprintf("vector(%d)", expectedDimension)
	if vectorType != expected {
		pool.Close()
		return nil, fmt.Errorf("database embedding column is %s, configured dimension requires %s", vectorType, expected)
	}
	return &Postgres{pool: pool}, nil
}

func (p *Postgres) Close()                         { p.pool.Close() }
func (p *Postgres) Ping(ctx context.Context) error { return p.pool.Ping(ctx) }

const memoryColumns = `
	id, content, summary, kind, scope, tags, embedding_model,
	author_agent, source, coalesce(supersedes_id, ''), created_at, updated_at, deleted_at`

const qualifiedMemoryColumns = `
	m.id, m.content, m.summary, m.kind, m.scope, m.tags, m.embedding_model,
	m.author_agent, m.source, coalesce(m.supersedes_id, ''), m.created_at, m.updated_at, m.deleted_at`

type scanner interface{ Scan(...any) error }

func scanMemory(row scanner, extra ...any) (memory.Memory, error) {
	var item memory.Memory
	dest := []any{
		&item.ID, &item.Content, &item.Summary, &item.Kind, &item.Scope, &item.Tags,
		&item.EmbeddingModel, &item.AuthorAgent, &item.Source, &item.SupersedesID,
		&item.CreatedAt, &item.UpdatedAt, &item.DeletedAt,
	}
	dest = append(dest, extra...)
	err := row.Scan(dest...)
	return item, err
}

func (p *Postgres) Get(ctx context.Context, id string, includeDeleted bool) (memory.Memory, error) {
	row := p.pool.QueryRow(ctx, `
		SELECT `+memoryColumns+`
		FROM memories
		WHERE id = $1 AND ($2 OR deleted_at IS NULL)`, id, includeDeleted)
	item, err := scanMemory(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return memory.Memory{}, fmt.Errorf("memory %s not found", id)
	}
	return item, err
}

func (p *Postgres) FindDuplicate(ctx context.Context, embedding []float32, scope string, threshold float64) (*memory.Memory, error) {
	row := p.pool.QueryRow(ctx, `
		SELECT `+memoryColumns+`, 1 - (embedding <=> $1) AS similarity
		FROM memories
		WHERE deleted_at IS NULL AND scope = $2 AND 1 - (embedding <=> $1) >= $3
		ORDER BY embedding <=> $1
		LIMIT 1`, pgvector.NewVector(embedding), scope, threshold)
	var similarity float64
	item, err := scanMemory(row, &similarity)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	item.Similarity = similarity
	return &item, nil
}

func (p *Postgres) Insert(ctx context.Context, item memory.Memory, embedding []float32) (memory.Memory, error) {
	row := p.pool.QueryRow(ctx, `
		INSERT INTO memories (
			id, content, summary, kind, scope, tags, embedding, embedding_model,
			author_agent, source, supersedes_id, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,nullif($11,''),$12,$13)
		RETURNING `+memoryColumns,
		item.ID, item.Content, item.Summary, item.Kind, item.Scope, item.Tags,
		pgvector.NewVector(embedding), item.EmbeddingModel, item.AuthorAgent, item.Source,
		item.SupersedesID, item.CreatedAt, item.UpdatedAt)
	return scanMemory(row)
}

func (p *Postgres) Replace(ctx context.Context, oldID string, replacement memory.Memory, embedding []float32) (memory.Memory, error) {
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return memory.Memory{}, err
	}
	defer tx.Rollback(ctx)
	var lockedID string
	if err := tx.QueryRow(ctx,
		"SELECT id FROM memories WHERE id = $1 AND deleted_at IS NULL FOR UPDATE", oldID,
	).Scan(&lockedID); errors.Is(err, pgx.ErrNoRows) {
		return memory.Memory{}, fmt.Errorf("live memory %s not found", oldID)
	} else if err != nil {
		return memory.Memory{}, err
	}
	now := time.Now().UTC()
	if _, err := tx.Exec(ctx, "UPDATE memories SET deleted_at = $2, updated_at = $2 WHERE id = $1", oldID, now); err != nil {
		return memory.Memory{}, err
	}
	row := tx.QueryRow(ctx, `
		INSERT INTO memories (
			id, content, summary, kind, scope, tags, embedding, embedding_model,
			author_agent, source, supersedes_id, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
		RETURNING `+memoryColumns,
		replacement.ID, replacement.Content, replacement.Summary, replacement.Kind,
		replacement.Scope, replacement.Tags, pgvector.NewVector(embedding),
		replacement.EmbeddingModel, replacement.AuthorAgent, replacement.Source,
		oldID, replacement.CreatedAt, replacement.UpdatedAt)
	stored, err := scanMemory(row)
	if err != nil {
		return memory.Memory{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return memory.Memory{}, err
	}
	return stored, nil
}

func (p *Postgres) Recall(ctx context.Context, embedding []float32, query string, scopes []string, kind string, limit int) ([]memory.Memory, error) {
	candidates := limit * 4
	if candidates < 20 {
		candidates = 20
	}
	rows, err := p.pool.Query(ctx, `
		WITH semantic AS (
			SELECT id, row_number() OVER (ORDER BY embedding <=> $1) AS rank
			FROM memories
			WHERE deleted_at IS NULL AND scope = ANY($2::text[])
			  AND ($3 = '' OR kind = $3)
			ORDER BY embedding <=> $1
			LIMIT $4
		), lexical AS (
			SELECT id, row_number() OVER (
				ORDER BY ts_rank_cd(
					to_tsvector('simple', coalesce(content, '') || ' ' || coalesce(summary, '')),
					websearch_to_tsquery('simple', $5)
				) DESC
			) AS rank
			FROM memories
			WHERE deleted_at IS NULL AND scope = ANY($2::text[])
			  AND ($3 = '' OR kind = $3)
			  AND to_tsvector('simple', coalesce(content, '') || ' ' || coalesce(summary, ''))
			      @@ websearch_to_tsquery('simple', $5)
			LIMIT $4
		), fused AS (
			SELECT id, sum(score) AS score FROM (
				SELECT id, 1.0 / (60 + rank) AS score FROM semantic
				UNION ALL
				SELECT id, 1.0 / (60 + rank) AS score FROM lexical
			) ranks GROUP BY id
		)
		SELECT `+qualifiedMemoryColumns+`,
		       greatest(0, 1 - (m.embedding <=> $1)) AS similarity,
		       fused.score
		FROM fused
		JOIN memories m ON m.id = fused.id
		ORDER BY fused.score DESC, similarity DESC
		LIMIT $6`, pgvector.NewVector(embedding), scopes, kind, candidates, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []memory.Memory
	for rows.Next() {
		var similarity, score float64
		item, err := scanMemory(rows, &similarity, &score)
		if err != nil {
			return nil, err
		}
		item.Similarity, item.RankScore = similarity, score
		result = append(result, item)
	}
	return result, rows.Err()
}

func (p *Postgres) List(ctx context.Context, scopes []string, kind string, limit int, before string) ([]memory.Memory, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT `+memoryColumns+`
		FROM memories
		WHERE deleted_at IS NULL AND scope = ANY($1::text[])
		  AND ($2 = '' OR kind = $2)
		  AND ($3 = '' OR id < $3)
		ORDER BY id DESC
		LIMIT $4`, scopes, kind, before, limit)
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

func (p *Postgres) Forget(ctx context.Context, id string) (memory.Memory, error) {
	row := p.pool.QueryRow(ctx, `
		UPDATE memories SET deleted_at = now(), updated_at = now()
		WHERE id = $1 AND deleted_at IS NULL
		RETURNING `+memoryColumns, id)
	item, err := scanMemory(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return memory.Memory{}, fmt.Errorf("live memory %s not found", id)
	}
	return item, err
}

func (p *Postgres) Stats(ctx context.Context) (memory.Stats, error) {
	stats := memory.Stats{ByScope: map[string]int64{}, ByKind: map[string]int64{}}
	if err := p.pool.QueryRow(ctx, `
		SELECT
			count(*) FILTER (WHERE deleted_at IS NULL),
			count(*) FILTER (WHERE deleted_at IS NOT NULL AND NOT EXISTS (
				SELECT 1 FROM memories n WHERE n.supersedes_id = memories.id
			)),
			count(*) FILTER (WHERE EXISTS (
				SELECT 1 FROM memories n WHERE n.supersedes_id = memories.id
			))
		FROM memories`).Scan(&stats.Live, &stats.Deleted, &stats.Superseded); err != nil {
		return stats, err
	}
	if err := p.countBy(ctx, "scope", stats.ByScope); err != nil {
		return stats, err
	}
	if err := p.countBy(ctx, "kind", stats.ByKind); err != nil {
		return stats, err
	}
	return stats, nil
}

func (p *Postgres) countBy(ctx context.Context, column string, target map[string]int64) error {
	rows, err := p.pool.Query(ctx, "SELECT "+column+", count(*) FROM memories WHERE deleted_at IS NULL GROUP BY "+column)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var key string
		var count int64
		if err := rows.Scan(&key, &count); err != nil {
			return err
		}
		target[key] = count
	}
	return rows.Err()
}
