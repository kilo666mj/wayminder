package memory

import "time"

type Memory struct {
	ID             string     `json:"id"`
	Content        string     `json:"content"`
	Summary        string     `json:"summary,omitempty"`
	Kind           string     `json:"kind"`
	Scope          string     `json:"scope"`
	Tags           []string   `json:"tags,omitempty"`
	EmbeddingModel string     `json:"embedding_model"`
	AuthorAgent    string     `json:"author_agent,omitempty"`
	Source         string     `json:"source,omitempty"`
	SupersedesID   string     `json:"supersedes_id,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	DeletedAt      *time.Time `json:"deleted_at,omitempty"`
	Similarity     float64    `json:"similarity,omitempty"`
	RankScore      float64    `json:"rank_score,omitempty"`
}

type Principal struct {
	Agent  string
	Source string
}

type RememberRequest struct {
	Content string
	Summary string
	Kind    string
	Scope   string
	Tags    []string
}

type RememberResult struct {
	Memory Memory `json:"memory"`
	Action string `json:"action"`
}

type RecallRequest struct {
	Query string
	Scope string
	Kind  string
	Limit int
}

type ListRequest struct {
	Scope  string
	Kind   string
	Limit  int
	Before string
}

type Stats struct {
	Live       int64            `json:"live"`
	Deleted    int64            `json:"deleted"`
	Superseded int64            `json:"superseded"`
	ByScope    map[string]int64 `json:"by_scope"`
	ByKind     map[string]int64 `json:"by_kind"`
}
