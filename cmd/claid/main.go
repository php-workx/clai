// claid is the clai background daemon that handles shell integration requests.
// It is spawned automatically when needed and exits after an idle timeout.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/runger/clai/internal/claude"
	"github.com/runger/clai/internal/config"
	"github.com/runger/clai/internal/daemon"
	"github.com/runger/clai/internal/storage"
)

// claudeLLM adapts claude.QueryWithContext to the daemon.LLMQuerier interface.
type claudeLLM struct{}

func (c *claudeLLM) Query(ctx context.Context, prompt string) (string, error) {
	return claude.QueryWithContext(ctx, prompt)
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "claid: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	// Set up logging
	logHandler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})
	logger := slog.New(logHandler)

	// Load configuration
	paths := config.DefaultPaths()

	// Ensure directories exist
	if err := paths.EnsureDirectories(); err != nil {
		return fmt.Errorf("failed to create directories: %w", err)
	}

	// Open database
	store, err := storage.NewSQLiteStore(paths.DatabaseFile())
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer store.Close()

	// Create server config
	cfg := &daemon.ServerConfig{
		Store:  store,
		Paths:  paths,
		Logger: logger,
		LLM:    &claudeLLM{},
	}

	// Run the daemon (blocks until shutdown)
	return daemon.Run(context.Background(), cfg)
}
