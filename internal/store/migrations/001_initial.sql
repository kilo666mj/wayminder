CREATE TABLE memories (
    id text PRIMARY KEY,
    content text NOT NULL,
    summary text NOT NULL DEFAULT '',
    kind text NOT NULL CHECK (kind IN ('fact', 'decision', 'preference', 'procedure', 'reference')),
    scope text NOT NULL,
    tags text[] NOT NULL DEFAULT '{}',
    embedding vector(768) NOT NULL,
    embedding_model text NOT NULL,
    author_agent text NOT NULL DEFAULT '',
    source text NOT NULL DEFAULT '',
    supersedes_id text REFERENCES memories(id),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    deleted_at timestamptz
);

CREATE INDEX memories_embedding_hnsw_idx
    ON memories USING hnsw (embedding vector_cosine_ops);
CREATE INDEX memories_live_scope_idx
    ON memories (scope, kind, id DESC) WHERE deleted_at IS NULL;
CREATE INDEX memories_tags_idx ON memories USING gin (tags);
CREATE INDEX memories_search_idx ON memories USING gin (
    to_tsvector('simple'::regconfig, coalesce(content, '') || ' ' || coalesce(summary, ''))
);
CREATE INDEX memories_supersedes_idx ON memories (supersedes_id)
    WHERE supersedes_id IS NOT NULL;
