package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	ListenAddress      string
	DatabaseURL        string
	AuthToken          string
	AllowInsecure      bool
	AllowedHosts       []string
	OllamaURL          string
	EmbeddingModel     string
	EmbeddingDimension int
	DedupThreshold     float64
	RequestTimeout     time.Duration
	MaxMemoryBytes     int
}

func Load() (Config, error) {
	cfg := Config{
		ListenAddress:      env("WAYMINDER_LISTEN_ADDRESS", ":8080"),
		DatabaseURL:        env("WAYMINDER_DATABASE_URL", "postgres://wayminder_app:wayminder@localhost:5432/wayminder?sslmode=disable"),
		AuthToken:          strings.TrimSpace(os.Getenv("WAYMINDER_AUTH_TOKEN")),
		AllowInsecure:      envBool("WAYMINDER_ALLOW_INSECURE", false),
		AllowedHosts:       splitCSV(env("WAYMINDER_ALLOWED_HOSTS", "localhost,127.0.0.1,deployment-host,deployment-host.example.com")),
		OllamaURL:          strings.TrimRight(env("WAYMINDER_OLLAMA_URL", "http://localhost:11434"), "/"),
		EmbeddingModel:     env("WAYMINDER_EMBED_MODEL", "nomic-embed-text"),
		EmbeddingDimension: envInt("WAYMINDER_EMBED_DIMENSION", 768),
		DedupThreshold:     envFloat("WAYMINDER_DEDUP_THRESHOLD", 0.92),
		RequestTimeout:     envDuration("WAYMINDER_REQUEST_TIMEOUT", 20*time.Second),
		MaxMemoryBytes:     envInt("WAYMINDER_MAX_MEMORY_BYTES", 16*1024),
	}
	if cfg.AuthToken == "" && !cfg.AllowInsecure {
		return Config{}, fmt.Errorf("WAYMINDER_AUTH_TOKEN is required unless WAYMINDER_ALLOW_INSECURE=true")
	}
	if len(cfg.AuthToken) > 0 && len(cfg.AuthToken) < 32 {
		return Config{}, fmt.Errorf("WAYMINDER_AUTH_TOKEN must be at least 32 characters")
	}
	if cfg.EmbeddingDimension <= 0 {
		return Config{}, fmt.Errorf("WAYMINDER_EMBED_DIMENSION must be positive")
	}
	if cfg.DedupThreshold <= 0 || cfg.DedupThreshold > 1 {
		return Config{}, fmt.Errorf("WAYMINDER_DEDUP_THRESHOLD must be in (0, 1]")
	}
	if cfg.MaxMemoryBytes < 256 {
		return Config{}, fmt.Errorf("WAYMINDER_MAX_MEMORY_BYTES must be at least 256")
	}
	if len(cfg.AllowedHosts) == 0 && !cfg.AllowInsecure {
		return Config{}, fmt.Errorf("WAYMINDER_ALLOWED_HOSTS must not be empty")
	}
	for _, host := range cfg.AllowedHosts {
		if host == "*" && !cfg.AllowInsecure {
			return Config{}, fmt.Errorf("WAYMINDER_ALLOWED_HOSTS may contain * only when WAYMINDER_ALLOW_INSECURE=true")
		}
	}
	return cfg, nil
}

func env(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func envBool(name string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func envInt(name string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func envFloat(name string, fallback float64) float64 {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return fallback
	}
	return parsed
}

func envDuration(name string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func splitCSV(value string) []string {
	var result []string
	for _, item := range strings.Split(value, ",") {
		if item = strings.ToLower(strings.TrimSpace(item)); item != "" {
			result = append(result, item)
		}
	}
	return result
}
