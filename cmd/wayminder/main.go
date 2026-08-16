package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/kilo666mj/wayminder/internal/config"
	"github.com/kilo666mj/wayminder/internal/embed"
	"github.com/kilo666mj/wayminder/internal/memory"
	"github.com/kilo666mj/wayminder/internal/server"
	"github.com/kilo666mj/wayminder/internal/store"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg, err := config.Load()
	if err != nil {
		logger.Error("invalid configuration", "error", err)
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	database, err := store.Open(ctx, cfg.DatabaseURL, cfg.EmbeddingDimension)
	cancel()
	if err != nil {
		logger.Error("database startup failed", "error", err)
		os.Exit(1)
	}
	defer database.Close()

	embedder := embed.NewOllama(cfg.OllamaURL, cfg.EmbeddingModel, cfg.EmbeddingDimension, cfg.RequestTimeout)
	service := memory.NewService(database, embedder, cfg.DedupThreshold, cfg.MaxMemoryBytes)
	httpServer := &http.Server{
		Addr: cfg.ListenAddress, Handler: server.NewHandler(cfg, service, logger),
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: cfg.RequestTimeout,
		WriteTimeout: cfg.RequestTimeout + 5*time.Second, IdleTimeout: 2 * time.Minute,
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		logger.Info("wayminder listening", "address", cfg.ListenAddress, "embedding_model", cfg.EmbeddingModel, "embedding_dimension", cfg.EmbeddingDimension)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("HTTP server failed", "error", err)
			os.Exit(1)
		}
	}()
	<-stop
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown failed", "error", err)
	}
}
