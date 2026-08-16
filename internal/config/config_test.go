package config

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadRequiresToken(t *testing.T) {
	t.Setenv("WAYMINDER_AUTH_TOKEN", "")
	t.Setenv("WAYMINDER_ALLOW_INSECURE", "false")
	if _, err := Load(); err == nil {
		t.Fatal("Load() succeeded without an auth token")
	}
}

func TestLoadAcceptsClientRegistryWithoutLegacyToken(t *testing.T) {
	tokenHash := sha256.Sum256([]byte("client token"))
	path := filepath.Join(t.TempDir(), "clients.json")
	data := fmt.Sprintf(`{"clients":[{"id":"client-a","token_sha256":"%x"}]}`, tokenHash)
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WAYMINDER_AUTH_TOKEN", "")
	t.Setenv("WAYMINDER_CLIENTS_FILE", path)
	t.Setenv("WAYMINDER_ALLOW_INSECURE", "false")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cfg.AuthClients) != 1 || cfg.AuthClients[0].ID != "client-a" {
		t.Fatalf("unexpected clients: %#v", cfg.AuthClients)
	}
}

func TestLoadRejectsInvalidClientRegistry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "clients.json")
	if err := os.WriteFile(path, []byte(`{"clients":[{"id":"bad id","token_sha256":"nope"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WAYMINDER_AUTH_TOKEN", "")
	t.Setenv("WAYMINDER_CLIENTS_FILE", path)
	if _, err := Load(); err == nil {
		t.Fatal("Load() accepted an invalid client registry")
	}
}

func TestLoadDefaults(t *testing.T) {
	t.Setenv("WAYMINDER_AUTH_TOKEN", "0123456789abcdef0123456789abcdef")
	t.Setenv("WAYMINDER_ALLOW_INSECURE", "false")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.EmbeddingModel != "nomic-embed-text" || cfg.EmbeddingDimension != 768 {
		t.Fatalf("unexpected embedding config: %s/%d", cfg.EmbeddingModel, cfg.EmbeddingDimension)
	}
	if len(cfg.AllowedHosts) == 0 {
		t.Fatal("expected default allowed hosts")
	}
	if cfg.RateLimitPerMinute != 120 || cfg.RateLimitBurst != 30 {
		t.Fatalf("unexpected rate limits: %d/%d", cfg.RateLimitPerMinute, cfg.RateLimitBurst)
	}
}

func TestLoadRejectsInvalidRateLimits(t *testing.T) {
	t.Setenv("WAYMINDER_AUTH_TOKEN", strings.Repeat("x", 32))
	t.Setenv("WAYMINDER_RATE_LIMIT_PER_MINUTE", "0")
	if _, err := Load(); err == nil {
		t.Fatal("Load() accepted a non-positive rate limit")
	}
}

func TestLoadRejectsWildcardHostInSecureMode(t *testing.T) {
	t.Setenv("WAYMINDER_AUTH_TOKEN", strings.Repeat("x", 32))
	t.Setenv("WAYMINDER_ALLOWED_HOSTS", "*")

	if _, err := Load(); err == nil {
		t.Fatal("Load() accepted wildcard host in secure mode")
	}
}
