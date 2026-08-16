package server

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/kilo666mj/wayminder/internal/config"
	"github.com/kilo666mj/wayminder/internal/memory"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const version = "0.1.0"

type principalContextKey struct{}

type rememberInput struct {
	Content string   `json:"content" jsonschema:"Durable knowledge to store"`
	Summary string   `json:"summary,omitempty" jsonschema:"Optional short summary"`
	Kind    string   `json:"kind,omitempty" jsonschema:"fact, decision, preference, procedure, or reference"`
	Scope   string   `json:"scope,omitempty" jsonschema:"Narrowest applicable scope; defaults to global"`
	Tags    []string `json:"tags,omitempty" jsonschema:"Optional searchable labels"`
}

type recallInput struct {
	Query string `json:"query" jsonschema:"Natural-language question or search"`
	Scope string `json:"scope,omitempty" jsonschema:"Current context, such as repo:wayminder"`
	Kind  string `json:"kind,omitempty" jsonschema:"Optional memory kind filter"`
	Limit int    `json:"limit,omitempty" jsonschema:"Maximum results, default 5 and maximum 50"`
}

type listInput struct {
	Scope  string `json:"scope,omitempty" jsonschema:"Current context, such as repo:wayminder"`
	Kind   string `json:"kind,omitempty" jsonschema:"Optional memory kind filter"`
	Limit  int    `json:"limit,omitempty" jsonschema:"Maximum results, default 50 and maximum 100"`
	Before string `json:"before,omitempty" jsonschema:"ULID cursor from the previous page"`
}

type supersedeInput struct {
	ID      string   `json:"id" jsonschema:"Memory ULID to replace"`
	Content string   `json:"content" jsonschema:"Corrected durable knowledge"`
	Summary string   `json:"summary,omitempty" jsonschema:"Replacement summary; inherited when omitted"`
	Kind    string   `json:"kind,omitempty" jsonschema:"Replacement kind; inherited when omitted"`
	Scope   string   `json:"scope,omitempty" jsonschema:"Replacement scope; inherited when omitted"`
	Tags    []string `json:"tags,omitempty" jsonschema:"Replacement tags; inherited when omitted"`
}

type idInput struct {
	ID string `json:"id" jsonschema:"Memory ULID"`
}

type memoriesOutput struct {
	Memories []memory.Memory `json:"memories"`
}

type memoryOutput struct {
	Memory memory.Memory `json:"memory"`
}

type statusOutput struct {
	Status             string       `json:"status"`
	Version            string       `json:"version"`
	EmbeddingModel     string       `json:"embedding_model"`
	EmbeddingDimension int          `json:"embedding_dimension"`
	Stats              memory.Stats `json:"stats"`
}

func NewHandler(cfg config.Config, service *memory.Service, logger *slog.Logger) http.Handler {
	instructions := buildInstructions(context.Background(), service)
	mcpServer := mcp.NewServer(
		&mcp.Implementation{Name: "wayminder", Version: version},
		&mcp.ServerOptions{Instructions: instructions, Logger: logger},
	)

	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "remember",
		Description: "Store verified knowledge that will remain useful after this task. Use the narrowest correct scope. Never store secrets, credentials, transient logs, or speculation.",
	}, func(ctx context.Context, request *mcp.CallToolRequest, input rememberInput) (*mcp.CallToolResult, memory.RememberResult, error) {
		result, err := service.Remember(ctx, memory.RememberRequest{
			Content: input.Content, Summary: input.Summary, Kind: input.Kind, Scope: input.Scope, Tags: input.Tags,
		}, principal(ctx, request))
		return nil, result, err
	})

	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "recall",
		Description: "Search durable memory before relying on assumptions. Recall searches global and personal memory plus the requested scope. " + instructions,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input recallInput) (*mcp.CallToolResult, memoriesOutput, error) {
		items, err := service.Recall(ctx, memory.RecallRequest{Query: input.Query, Scope: input.Scope, Kind: input.Kind, Limit: input.Limit})
		return nil, memoriesOutput{Memories: items}, err
	})

	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "list_memories",
		Description: "List recent live memories visible in a scope. Use the final returned ULID as the before cursor.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input listInput) (*mcp.CallToolResult, memoriesOutput, error) {
		items, err := service.List(ctx, memory.ListRequest{Scope: input.Scope, Kind: input.Kind, Limit: input.Limit, Before: input.Before})
		return nil, memoriesOutput{Memories: items}, err
	})

	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "supersede",
		Description: "Replace incorrect or stale knowledge while preserving its version history. Omitted metadata is inherited from the old memory.",
	}, func(ctx context.Context, request *mcp.CallToolRequest, input supersedeInput) (*mcp.CallToolResult, memoryOutput, error) {
		item, err := service.Supersede(ctx, input.ID, memory.RememberRequest{
			Content: input.Content, Summary: input.Summary, Kind: input.Kind, Scope: input.Scope, Tags: input.Tags,
		}, principal(ctx, request))
		return nil, memoryOutput{Memory: item}, err
	})

	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "forget",
		Description: "Soft-delete a memory that should no longer appear in normal recall.",
	}, func(ctx context.Context, request *mcp.CallToolRequest, input idInput) (*mcp.CallToolResult, memoryOutput, error) {
		item, err := service.Forget(ctx, input.ID, principal(ctx, request))
		return nil, memoryOutput{Memory: item}, err
	})

	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "status",
		Description: "Report Wayminder health metadata and live memory counts.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, statusOutput, error) {
		stats, err := service.Stats(ctx)
		model, dimension := service.EmbeddingInfo()
		return nil, statusOutput{
			Status: "ok", Version: version, EmbeddingModel: model,
			EmbeddingDimension: dimension, Stats: stats,
		}, err
	})

	mcpHandler := mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return mcpServer },
		&mcp.StreamableHTTPOptions{
			Stateless: true, JSONResponse: true, MaxRequestBodyBytes: int64(cfg.MaxMemoryBytes + 64*1024), Logger: logger,
		},
	)
	protectedMCP := authMiddleware(cfg.AuthToken, cfg.AuthClients, cfg.AllowInsecure,
		newClientRateLimiter(cfg.RateLimitPerMinute, cfg.RateLimitBurst), mcpHandler)

	mux := http.NewServeMux()
	mux.Handle("/mcp", protectedMCP)
	mux.Handle("/mcp/", protectedMCP)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "service": "wayminder", "version": version})
	})
	mux.Handle("GET /readyz", readinessHandler(logger, func(ctx context.Context) error {
		return service.Ready(ctx)
	}))
	return loggingMiddleware(logger, hostMiddleware(cfg.AllowedHosts, cfg.AllowInsecure, mux))
}

func readinessHandler(logger *slog.Logger, ready func(context.Context) error) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		if err := ready(ctx); err != nil {
			logger.Error("readiness check failed")
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	})
}

func buildInstructions(ctx context.Context, service *memory.Service) string {
	stats, err := service.Stats(ctx)
	if err != nil {
		return "Wayminder stores durable cross-session knowledge. Recall before assuming; remember only verified, durable facts; use narrow scopes; never store secrets."
	}
	return fmt.Sprintf(
		"Wayminder currently contains %d live memories across %d scopes. Recall before assuming; remember only verified, durable facts; use narrow scopes; never store secrets.",
		stats.Live, len(stats.ByScope),
	)
}

func principal(ctx context.Context, request *mcp.CallToolRequest) memory.Principal {
	result, _ := ctx.Value(principalContextKey{}).(memory.Principal)
	if request.Extra != nil {
		result.Source = strings.TrimSpace(request.Extra.Header.Get("X-Wayminder-Source"))
	}
	if result.Agent == "" && request.Extra != nil {
		result.Agent = strings.TrimSpace(request.Extra.Header.Get("X-Wayminder-Agent"))
	}
	if result.Agent == "" {
		if client := request.ClientInfo(); client != nil {
			result.Agent = client.Name
		}
	}
	return result
}

func authMiddleware(legacyToken string, clients []config.AuthClient, allowInsecure bool, limiter *clientRateLimiter, next http.Handler) http.Handler {
	type credential struct {
		id   string
		hash [sha256.Size]byte
	}
	credentials := make([]credential, 0, len(clients)+1)
	for _, client := range clients {
		var hash [sha256.Size]byte
		decoded, _ := hex.DecodeString(client.TokenSHA256)
		copy(hash[:], decoded)
		credentials = append(credentials, credential{id: client.ID, hash: hash})
	}
	if legacyToken != "" {
		credentials = append(credentials, credential{id: "legacy", hash: sha256.Sum256([]byte(legacyToken))})
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if allowInsecure && len(credentials) == 0 {
			next.ServeHTTP(w, r)
			return
		}
		const prefix = "Bearer "
		header := r.Header.Get("Authorization")
		if len(r.Header.Values("Authorization")) != 1 || !strings.HasPrefix(header, prefix) ||
			strings.TrimSpace(strings.TrimPrefix(header, prefix)) == "" ||
			strings.ContainsAny(strings.TrimPrefix(header, prefix), " \t\r\n") {
			w.Header().Set("WWW-Authenticate", `Bearer realm="wayminder"`)
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid or missing bearer token"})
			return
		}
		presented := sha256.Sum256([]byte(strings.TrimSpace(strings.TrimPrefix(header, prefix))))
		clientID := ""
		for _, candidate := range credentials {
			if subtle.ConstantTimeCompare(presented[:], candidate.hash[:]) == 1 {
				clientID = candidate.id
			}
		}
		if clientID == "" {
			w.Header().Set("WWW-Authenticate", `Bearer realm="wayminder"`)
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid or missing bearer token"})
			return
		}
		if !limiter.Allow(clientID, time.Now()) {
			w.Header().Set("Retry-After", "60")
			writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "rate limit exceeded"})
			return
		}
		ctx := context.WithValue(r.Context(), principalContextKey{}, memory.Principal{Agent: clientID})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func hostMiddleware(allowed []string, allowInsecure bool, next http.Handler) http.Handler {
	set := make(map[string]bool, len(allowed))
	for _, host := range allowed {
		set[strings.ToLower(host)] = true
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := strings.ToLower(r.Host)
		if parsed, _, err := net.SplitHostPort(host); err == nil {
			host = parsed
		}
		if !set[host] && !(allowInsecure && set["*"]) {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "host is not allowed"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

type responseRecorder struct {
	http.ResponseWriter
	status int
}

func (r *responseRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func loggingMiddleware(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		recorder := &responseRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(recorder, r)
		logger.Info("http request", "method", r.Method, "path", r.URL.Path, "status", recorder.status, "duration_ms", time.Since(started).Milliseconds())
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
