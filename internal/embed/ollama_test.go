package embed

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestOllamaEmbed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/embed" {
			http.NotFound(w, r)
			return
		}
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request["model"] != "test-model" {
			t.Fatalf("model = %v", request["model"])
		}
		if request["keep_alive"] != float64(-1) {
			t.Fatalf("keep_alive = %v", request["keep_alive"])
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"embeddings": [][]float32{{1, 2, 3}}})
	}))
	defer server.Close()
	embedder := NewOllama(server.URL, "test-model", 3, time.Second)
	vectors, err := embedder.Embed(context.Background(), []string{"remember this"})
	if err != nil {
		t.Fatalf("Embed() error = %v", err)
	}
	if len(vectors) != 1 || len(vectors[0]) != 3 {
		t.Fatalf("unexpected embeddings: %#v", vectors)
	}
}

func TestOllamaRejectsWrongDimension(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"embeddings": [][]float32{{1, 2}}})
	}))
	defer server.Close()
	embedder := NewOllama(server.URL, "test", 3, time.Second)
	if _, err := embedder.Embed(context.Background(), []string{"x"}); err == nil {
		t.Fatal("Embed() accepted a vector with the wrong dimension")
	}
}
