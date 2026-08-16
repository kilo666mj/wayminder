package memory

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/kilo666mj/wayminder/internal/embed"
	"github.com/oklog/ulid/v2"
)

var (
	validScope     = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._:/-]{0,127}$`)
	secretPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)-----BEGIN (?:RSA |EC |OPENSSH )?PRIVATE KEY-----`),
		regexp.MustCompile(`(?i)(?:api[_-]?key|access[_-]?token|client[_-]?secret|password)\s*[:=]\s*[^\s]{8,}`),
		regexp.MustCompile(`\bgh[oprsu]_[A-Za-z0-9_]{20,}\b`),
		regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{20,}\b`),
	}
	validKinds = map[string]bool{
		"fact": true, "decision": true, "preference": true, "procedure": true, "reference": true,
	}
)

type Store interface {
	Get(context.Context, string, bool) (Memory, error)
	FindDuplicate(context.Context, []float32, string, float64) (*Memory, error)
	Insert(context.Context, Memory, []float32) (Memory, error)
	Replace(context.Context, string, Memory, []float32) (Memory, error)
	Recall(context.Context, []float32, string, []string, string, int) ([]Memory, error)
	List(context.Context, []string, string, int, string) ([]Memory, error)
	Forget(context.Context, string) (Memory, error)
	Stats(context.Context) (Stats, error)
	Ping(context.Context) error
}

type Service struct {
	store          Store
	embedder       embed.Embedder
	dedupThreshold float64
	maxMemoryBytes int
	mu             sync.Mutex
	entropy        *ulid.MonotonicEntropy
}

func NewService(store Store, embedder embed.Embedder, dedupThreshold float64, maxMemoryBytes int) *Service {
	return &Service{
		store: store, embedder: embedder, dedupThreshold: dedupThreshold,
		maxMemoryBytes: maxMemoryBytes, entropy: ulid.Monotonic(rand.Reader, 0),
	}
}

func (s *Service) Remember(ctx context.Context, request RememberRequest, principal Principal) (RememberResult, error) {
	request.Content = strings.TrimSpace(request.Content)
	request.Summary = strings.TrimSpace(request.Summary)
	request.Kind = defaultString(strings.ToLower(strings.TrimSpace(request.Kind)), "reference")
	request.Scope = defaultString(strings.TrimSpace(request.Scope), "global")
	request.Tags = normalizeTags(request.Tags)
	if err := s.validateWrite(request); err != nil {
		return RememberResult{}, err
	}
	vectors, err := s.embedder.Embed(ctx, []string{request.Content})
	if err != nil {
		return RememberResult{}, fmt.Errorf("embed memory: %w", err)
	}
	duplicate, err := s.store.FindDuplicate(ctx, vectors[0], request.Scope, s.dedupThreshold)
	if err != nil {
		return RememberResult{}, fmt.Errorf("find duplicate: %w", err)
	}
	if duplicate != nil && normalizeContent(duplicate.Content) == normalizeContent(request.Content) {
		return RememberResult{Memory: *duplicate, Action: "existing"}, nil
	}
	now := time.Now().UTC()
	item := Memory{
		ID: s.newID(now), Content: request.Content, Summary: request.Summary,
		Kind: request.Kind, Scope: request.Scope, Tags: request.Tags,
		EmbeddingModel: s.embedder.Model(), AuthorAgent: cleanLabel(principal.Agent, 128),
		Source: cleanLabel(principal.Source, 256), CreatedAt: now, UpdatedAt: now,
	}
	if duplicate != nil {
		item.SupersedesID = duplicate.ID
		stored, err := s.store.Replace(ctx, duplicate.ID, item, vectors[0])
		if err != nil {
			return RememberResult{}, fmt.Errorf("supersede duplicate: %w", err)
		}
		return RememberResult{Memory: stored, Action: "superseded"}, nil
	}
	stored, err := s.store.Insert(ctx, item, vectors[0])
	if err != nil {
		return RememberResult{}, fmt.Errorf("insert memory: %w", err)
	}
	return RememberResult{Memory: stored, Action: "created"}, nil
}

func (s *Service) Recall(ctx context.Context, request RecallRequest) ([]Memory, error) {
	request.Query = strings.TrimSpace(request.Query)
	request.Scope = defaultString(strings.TrimSpace(request.Scope), "global")
	request.Kind = strings.ToLower(strings.TrimSpace(request.Kind))
	request.Limit = boundedLimit(request.Limit)
	if request.Query == "" {
		return nil, errors.New("query is required")
	}
	if err := validateScope(request.Scope); err != nil {
		return nil, err
	}
	if request.Kind != "" && !validKinds[request.Kind] {
		return nil, fmt.Errorf("invalid kind %q", request.Kind)
	}
	vectors, err := s.embedder.Embed(ctx, []string{request.Query})
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}
	return s.store.Recall(ctx, vectors[0], request.Query, EffectiveScopes(request.Scope), request.Kind, request.Limit)
}

func (s *Service) List(ctx context.Context, request ListRequest) ([]Memory, error) {
	request.Scope = defaultString(strings.TrimSpace(request.Scope), "global")
	request.Kind = strings.ToLower(strings.TrimSpace(request.Kind))
	request.Limit = boundedListLimit(request.Limit)
	if err := validateScope(request.Scope); err != nil {
		return nil, err
	}
	if request.Kind != "" && !validKinds[request.Kind] {
		return nil, fmt.Errorf("invalid kind %q", request.Kind)
	}
	return s.store.List(ctx, EffectiveScopes(request.Scope), request.Kind, request.Limit, request.Before)
}

func (s *Service) Supersede(ctx context.Context, id string, request RememberRequest, principal Principal) (Memory, error) {
	if _, err := ulid.ParseStrict(id); err != nil {
		return Memory{}, errors.New("invalid memory id")
	}
	existing, err := s.store.Get(ctx, id, false)
	if err != nil {
		return Memory{}, err
	}
	request.Content = strings.TrimSpace(request.Content)
	request.Summary = defaultString(strings.TrimSpace(request.Summary), existing.Summary)
	request.Kind = defaultString(strings.ToLower(strings.TrimSpace(request.Kind)), existing.Kind)
	request.Scope = defaultString(strings.TrimSpace(request.Scope), existing.Scope)
	if request.Tags == nil {
		request.Tags = existing.Tags
	}
	request.Tags = normalizeTags(request.Tags)
	if err := s.validateWrite(request); err != nil {
		return Memory{}, err
	}
	vectors, err := s.embedder.Embed(ctx, []string{request.Content})
	if err != nil {
		return Memory{}, fmt.Errorf("embed memory: %w", err)
	}
	now := time.Now().UTC()
	replacement := Memory{
		ID: s.newID(now), Content: request.Content, Summary: request.Summary,
		Kind: request.Kind, Scope: request.Scope, Tags: request.Tags,
		EmbeddingModel: s.embedder.Model(), AuthorAgent: cleanLabel(principal.Agent, 128),
		Source: cleanLabel(principal.Source, 256), SupersedesID: id, CreatedAt: now, UpdatedAt: now,
	}
	return s.store.Replace(ctx, id, replacement, vectors[0])
}

func (s *Service) Forget(ctx context.Context, id string) (Memory, error) {
	if _, err := ulid.ParseStrict(id); err != nil {
		return Memory{}, errors.New("invalid memory id")
	}
	return s.store.Forget(ctx, id)
}

func (s *Service) Stats(ctx context.Context) (Stats, error) { return s.store.Stats(ctx) }

func (s *Service) Ready(ctx context.Context) error {
	if err := s.store.Ping(ctx); err != nil {
		return fmt.Errorf("database: %w", err)
	}
	if err := s.embedder.Ping(ctx); err != nil {
		return fmt.Errorf("embedding provider: %w", err)
	}
	return nil
}

func (s *Service) EmbeddingInfo() (string, int) { return s.embedder.Model(), s.embedder.Dimension() }

func EffectiveScopes(scope string) []string {
	set := map[string]bool{"global": true, "personal": true, scope: true}
	result := make([]string, 0, len(set))
	for item := range set {
		result = append(result, item)
	}
	sort.Strings(result)
	return result
}

func (s *Service) validateWrite(request RememberRequest) error {
	if request.Content == "" {
		return errors.New("content is required")
	}
	if len([]byte(request.Content)) > s.maxMemoryBytes {
		return fmt.Errorf("content exceeds %d bytes", s.maxMemoryBytes)
	}
	if !validKinds[request.Kind] {
		return fmt.Errorf("invalid kind %q", request.Kind)
	}
	if err := validateScope(request.Scope); err != nil {
		return err
	}
	for _, pattern := range secretPatterns {
		if pattern.MatchString(request.Content) {
			return errors.New("content appears to contain a secret; memory was not stored")
		}
	}
	return nil
}

func validateScope(scope string) error {
	if !validScope.MatchString(scope) {
		return errors.New("scope must be 1-128 characters using letters, numbers, dot, underscore, colon, slash, or dash")
	}
	return nil
}

func normalizeTags(tags []string) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, len(tags))
	for _, tag := range tags {
		tag = strings.ToLower(strings.TrimSpace(tag))
		if tag == "" || len(tag) > 64 || seen[tag] {
			continue
		}
		seen[tag] = true
		result = append(result, tag)
		if len(result) == 20 {
			break
		}
	}
	sort.Strings(result)
	return result
}

func (s *Service) newID(now time.Time) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return ulid.MustNew(ulid.Timestamp(now), s.entropy).String()
}

func defaultString(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func boundedLimit(value int) int {
	if value <= 0 {
		return 5
	}
	if value > 50 {
		return 50
	}
	return value
}

func boundedListLimit(value int) int {
	if value <= 0 {
		return 50
	}
	if value > 100 {
		return 100
	}
	return value
}

func cleanLabel(value string, limit int) string {
	value = strings.TrimSpace(value)
	if len(value) > limit {
		return value[:limit]
	}
	return value
}

func normalizeContent(value string) string {
	return strings.Join(strings.Fields(strings.ToLower(value)), " ")
}
