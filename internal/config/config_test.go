package config

import (
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
}

func TestLoadRejectsWildcardHostInSecureMode(t *testing.T) {
	t.Setenv("WAYMINDER_AUTH_TOKEN", strings.Repeat("x", 32))
	t.Setenv("WAYMINDER_ALLOWED_HOSTS", "*")

	if _, err := Load(); err == nil {
		t.Fatal("Load() accepted wildcard host in secure mode")
	}
}
