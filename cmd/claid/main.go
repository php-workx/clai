// claid is the clai background daemon that handles shell integration requests.
// It is spawned automatically when needed and exits after an idle timeout.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/runger/clai/internal/config"
	"github.com/runger/clai/internal/daemon"
	"github.com/runger/clai/internal/storage"
	suggestdb "github.com/runger/clai/internal/suggestions/db"
)

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

	// Open V2 suggestions database (graceful degradation if unavailable)
	ctx := context.Background()
	v2db, err := suggestdb.Open(ctx, suggestdb.Options{})
	if err != nil {
		logger.Warn("V2 suggestions database unavailable, continuing with V1 only", "error", err)
		// v2db stays nil — graceful degradation
	}
	if v2db != nil {
		defer v2db.Close()
	}

	// Create server config
	cfg := &daemon.ServerConfig{
		Store:  store,
		V2DB:   v2db,
		Paths:  paths,
		Logger: logger,
	}

	// Run the daemon (blocks until shutdown)
	return daemon.Run(ctx, cfg)
}
