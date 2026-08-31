package server

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/kilo666mj/mcpkit/mcpkittest"
	"github.com/kilo666mj/wayminder/internal/config"
	"github.com/kilo666mj/wayminder/internal/memory"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type mcpTestStore struct{}

func (mcpTestStore) Get(context.Context, string, bool) (memory.Memory, error) {
	return memory.Memory{}, nil
}
func (mcpTestStore) FindDuplicate(context.Context, []float32, string, float64) (*memory.Memory, error) {
	return nil, nil
}
func (mcpTestStore) Insert(_ context.Context, item memory.Memory, _ []float32) (memory.Memory, error) {
	return item, nil
}
func (mcpTestStore) Replace(_ context.Context, _ string, item memory.Memory, _ []float32) (memory.Memory, error) {
	return item, nil
}
func (mcpTestStore) Recall(context.Context, []float32, string, []string, string, int) ([]memory.Memory, error) {
	return nil, nil
}
func (mcpTestStore) List(context.Context, []string, string, int, string) ([]memory.Memory, error) {
	return nil, nil
}
func (mcpTestStore) Forget(context.Context, string, memory.Principal) (memory.Memory, error) {
	return memory.Memory{}, nil
}
func (mcpTestStore) Stats(context.Context) (memory.Stats, error) {
	return memory.Stats{Live: 2, ByScope: map[string]int64{"global": 2}}, nil
}
func (mcpTestStore) Ping(context.Context) error { return nil }

type mcpTestEmbedder struct{}

func (mcpTestEmbedder) Embed(context.Context, []string) ([][]float32, error) {
	return [][]float32{{1, 2, 3}}, nil
}
func (mcpTestEmbedder) Ping(context.Context) error { return nil }
func (mcpTestEmbedder) Dimension() int             { return 3 }
func (mcpTestEmbedder) Model() string              { return "test" }

func testService() *memory.Service {
	return memory.NewService(mcpTestStore{}, mcpTestEmbedder{}, .92, 1024)
}

func TestMCPServerPublishesStableAnnotatedToolSurface(t *testing.T) {
	session := mcpkittest.Connect(t, newMCPServer(testService(), slog.New(slog.NewTextHandler(io.Discard, nil))))
	listed, err := session.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"forget", "list_memories", "recall", "remember", "status", "supersede"}
	got := make([]string, 0, len(listed.Tools))
	for _, tool := range listed.Tools {
		got = append(got, tool.Name)
		if tool.Annotations == nil {
			t.Errorf("tool %q has no safety annotations", tool.Name)
			continue
		}
	}
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Fatalf("tool names = %v, want %v", got, want)
	}
	for _, tool := range listed.Tools {
		if tool.Annotations == nil {
			continue
		}
		readOnly := slices.Contains([]string{"list_memories", "recall", "status"}, tool.Name)
		destructive := slices.Contains([]string{"forget", "supersede"}, tool.Name)
		if tool.Annotations.ReadOnlyHint != readOnly || *tool.Annotations.DestructiveHint != destructive || *tool.Annotations.OpenWorldHint {
			t.Errorf("tool %q annotations = %+v", tool.Name, tool.Annotations)
		}
	}
}

func TestHandlerRetainsAuthenticationAndRequestLimit(t *testing.T) {
	cfg := config.Config{
		AuthToken: "0123456789abcdef0123456789abcdef", AllowedHosts: []string{"wayminder"},
		MaxMemoryBytes: 8, RateLimitPerMinute: 1000, RateLimitBurst: 100,
	}
	handler, err := NewHandler(cfg, testService(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "http://wayminder/mcp", strings.NewReader(strings.Repeat("x", cfg.MaxMemoryBytes+64*1024+1)))
	request.Header.Set("Authorization", "Bearer "+cfg.AuthToken)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized authenticated MCP request status = %d, want 413; body=%q", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodPost, "http://wayminder/mcp", strings.NewReader(`{}`))
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated MCP request status = %d, want 401", response.Code)
	}

	request = httptest.NewRequest(http.MethodPost, "http://wayminder/mcp", strings.NewReader(`{}`))
	request.Header.Set("Authorization", "Bearer "+cfg.AuthToken)
	request.Header.Set("Origin", "https://attacker.example")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("cross-origin MCP request status = %d, want 403", response.Code)
	}
}

func TestAuthMiddleware(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	handler := authMiddleware("0123456789abcdef0123456789abcdef", nil, false, newClientRateLimiter(1000, 100), next)
	for _, test := range []struct {
		name, authorization string
		want                int
	}{
		{"missing", "", http.StatusUnauthorized},
		{"wrong", "Bearer wrong", http.StatusUnauthorized},
		{"empty", "Bearer ", http.StatusUnauthorized},
		{"embedded whitespace", "Bearer two tokens", http.StatusUnauthorized},
		{"wrong scheme", "Basic 0123456789abcdef0123456789abcdef", http.StatusUnauthorized},
		{"valid", "Bearer 0123456789abcdef0123456789abcdef", http.StatusNoContent},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "http://wayminder/mcp", nil)
			request.Header.Set("Authorization", test.authorization)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.want {
				t.Fatalf("status = %d, want %d", response.Code, test.want)
			}
		})
	}
}

func TestAuthMiddlewareRejectsMultipleAuthorizationHeaders(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	handler := authMiddleware("0123456789abcdef0123456789abcdef", nil, false, newClientRateLimiter(1000, 100), next)
	request := httptest.NewRequest(http.MethodPost, "http://wayminder/mcp", nil)
	request.Header.Add("Authorization", "Bearer 0123456789abcdef0123456789abcdef")
	request.Header.Add("Authorization", "Bearer attacker")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}

func TestAuthMiddlewareRateLimitsPerClient(t *testing.T) {
	tokenA, tokenB := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	hashA, hashB := sha256.Sum256([]byte(tokenA)), sha256.Sum256([]byte(tokenB))
	clients := []config.AuthClient{
		{ID: "a", TokenSHA256: fmt.Sprintf("%x", hashA)},
		{ID: "b", TokenSHA256: fmt.Sprintf("%x", hashB)},
	}
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	handler := authMiddleware("", clients, false, newClientRateLimiter(1, 1), next)
	call := func(token string) int {
		request := httptest.NewRequest(http.MethodPost, "http://wayminder/mcp", nil)
		request.Header.Set("Authorization", "Bearer "+token)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response.Code
	}
	if got := call(tokenA); got != http.StatusNoContent {
		t.Fatalf("first client A status = %d", got)
	}
	if got := call(tokenA); got != http.StatusTooManyRequests {
		t.Fatalf("second client A status = %d", got)
	}
	if got := call(tokenB); got != http.StatusNoContent {
		t.Fatalf("first client B status = %d", got)
	}
}

func TestAuthMiddlewareSetsRegisteredClientIdentity(t *testing.T) {
	token := "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
	hash := sha256.Sum256([]byte(token))
	var got memory.Principal
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, _ = r.Context().Value(principalContextKey{}).(memory.Principal)
		w.WriteHeader(http.StatusNoContent)
	})
	handler := authMiddleware("", []config.AuthClient{{ID: "client-a", TokenSHA256: fmt.Sprintf("%x", hash)}}, false, newClientRateLimiter(1000, 100), next)
	request := httptest.NewRequest(http.MethodPost, "http://wayminder/mcp", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("X-Wayminder-Agent", "forged")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent || got.Agent != "client-a" {
		t.Fatalf("status/principal = %d/%q, want 204/client-a", response.Code, got.Agent)
	}
}

func TestPrincipalPreservesAuthenticatedIdentity(t *testing.T) {
	ctx := context.WithValue(context.Background(), principalContextKey{}, memory.Principal{Agent: "claude-home"})
	request := &mcp.CallToolRequest{}
	if got := principal(ctx, request); got.Agent != "claude-home" {
		t.Fatalf("principal agent = %q", got.Agent)
	}
}

func TestHostMiddleware(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	handler := hostMiddleware([]string{"wayminder.example.com"}, false, next)
	request := httptest.NewRequest(http.MethodGet, "http://evil.example/healthz", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusForbidden)
	}
}

func TestReadinessHandlerDoesNotExposeInternalError(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := readinessHandler(logger, func(context.Context) error {
		return fmt.Errorf("password authentication failed for database secret-host")
	})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://wayminder/readyz", nil))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d", response.Code)
	}
	if body := response.Body.String(); body != "{\"status\":\"unavailable\"}\n" {
		t.Fatalf("readiness body leaked details: %q", body)
	}
}
