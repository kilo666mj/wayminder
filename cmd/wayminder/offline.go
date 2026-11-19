package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/kilo666mj/wayminder/internal/config"
	"github.com/kilo666mj/wayminder/internal/embed"
	"github.com/kilo666mj/wayminder/internal/memory"
	"github.com/kilo666mj/wayminder/internal/probe"
	"github.com/kilo666mj/wayminder/internal/store"
)

// runOffline runs a maintenance command against the database and exits. The
// JSON report goes to stdout; progress and errors go to stderr so that the
// report can be redirected to a file on its own.
func runOffline(command string, args []string) int {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	var err error
	switch command {
	case "maintain":
		err = runMaintain(ctx, args, os.Stdout)
	case "probe":
		err = runProbe(ctx, args, os.Stdout, logger)
	}
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	if err != nil {
		logger.Error(command+" failed", "error", err)
		return 1
	}
	return 0
}

func openOffline(ctx context.Context) (config.Config, *store.Postgres, error) {
	cfg, err := config.LoadOffline()
	if err != nil {
		return config.Config{}, nil, fmt.Errorf("invalid configuration: %w", err)
	}
	openCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	database, err := store.Open(openCtx, cfg.DatabaseURL, cfg.EmbeddingDimension)
	if err != nil {
		return config.Config{}, nil, fmt.Errorf("database startup failed: %w", err)
	}
	return cfg, database, nil
}

func runMaintain(ctx context.Context, args []string, output io.Writer) error {
	flags := flag.NewFlagSet("wayminder maintain", flag.ContinueOnError)
	request := memory.MaintenanceRequest{}
	flags.Float64Var(&request.DuplicateThreshold, "threshold", 0.85, "minimum cosine similarity for a probable duplicate pair")
	flags.IntVar(&request.MinRevisions, "min-revisions", 3, "minimum earlier versions for a repeatedly superseded memory")
	flags.IntVar(&request.MaxPairs, "max-pairs", 500, "maximum duplicate pairs to consider, most similar first")
	flags.IntVar(&request.MaxChains, "max-chains", 100, "maximum supersede chains to report, longest first")
	flags.BoolVar(&request.IncludeContent, "content", false, "include memory summaries and content in the report")
	if err := flags.Parse(args); err != nil {
		return err
	}
	cfg, database, err := openOffline(ctx)
	if err != nil {
		return err
	}
	defer database.Close()
	report, err := memory.BuildMaintenanceReport(ctx, database, cfg.EmbeddingModel, request)
	if err != nil {
		return err
	}
	return writeReport(output, report)
}

func runProbe(ctx context.Context, args []string, output io.Writer, logger *slog.Logger) error {
	flags := flag.NewFlagSet("wayminder probe", flag.ContinueOnError)
	options := probe.Options{}
	model := flags.String("model", strings.TrimSpace(os.Getenv("WAYMINDER_PROBE_MODEL")), "generative Ollama model that writes the questions (WAYMINDER_PROBE_MODEL)")
	generatorURL := flags.String("ollama-url", strings.TrimSpace(os.Getenv("WAYMINDER_PROBE_OLLAMA_URL")), "Ollama base URL for the generative model; defaults to WAYMINDER_OLLAMA_URL (WAYMINDER_PROBE_OLLAMA_URL)")
	timeout := flags.Duration("timeout", 2*time.Minute, "time allowed for generating one memory's questions")
	flags.StringVar(&options.Scope, "scope", "", "probe one exact scope instead of sampling every scope")
	flags.StringVar(&options.Seed, "seed", "wayminder", "sample seed; the same seed probes the same memories")
	flags.IntVar(&options.Memories, "memories", 50, "number of memories to sample")
	flags.IntVar(&options.Questions, "questions", 3, "questions generated per memory")
	flags.IntVar(&options.K, "k", 5, "recall limit; a probe hits when its memory is in the top k")
	flags.BoolVar(&options.IncludeContent, "content", false, "include memory summaries and generated questions in the report")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *model == "" {
		return errors.New("a generative model is required: pass -model or set WAYMINDER_PROBE_MODEL")
	}
	cfg, database, err := openOffline(ctx)
	if err != nil {
		return err
	}
	defer database.Close()
	if *generatorURL == "" {
		*generatorURL = cfg.OllamaURL
	}
	embedder := embed.NewOllama(cfg.OllamaURL, cfg.EmbeddingModel, cfg.EmbeddingDimension, cfg.RequestTimeout)
	service := memory.NewService(database, embedder, cfg.DedupThreshold, cfg.MaxMemoryBytes)
	options.Progress = func(done, total int) {
		if done%10 == 0 || done == total {
			logger.Info("probe progress", "done", done, "total", total)
		}
	}
	report, err := probe.Run(ctx, database, probe.NewOllama(*generatorURL, *model, *timeout), service, options)
	if err != nil {
		return err
	}
	return writeReport(output, report)
}

func writeReport(output io.Writer, report any) error {
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	return encoder.Encode(report)
}
