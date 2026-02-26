// claid is the clai background daemon that handles shell integration requests.
// It is spawned automatically when needed and exits after an idle timeout.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/runger/clai/internal/claude"
	"github.com/runger/clai/internal/config"
	"github.com/runger/clai/internal/daemon"
	"github.com/runger/clai/internal/storage"
	suggestdb "github.com/runger/clai/internal/suggestions/db"
	"github.com/runger/clai/internal/suggestions/feedback"
	"github.com/runger/clai/internal/suggestions/maintenance"
)

// claudeLLM adapts claude.QueryWithContext to the daemon.LLMQuerier interface.
type claudeLLM struct{}

func (c *claudeLLM) Query(ctx context.Context, prompt string) (string, error) {
	return claude.QueryFast(ctx, prompt)
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
	cfgObj, cfgErr := config.Load()
	if cfgErr != nil {
		logger.Warn("failed to load config, using defaults", "error", cfgErr)
	}

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

	var feedbackStore *feedback.Store
	var maintenanceRunner *maintenance.Runner
	if v2db != nil {
		feedbackStore = feedback.NewStore(v2db.DB(), feedback.DefaultConfig(), logger)
		mcfg := maintenance.Config{
			Interval:      5 * time.Minute,
			RetentionDays: 90,
			DBPath:        v2db.Path(),
			Logger:        logger,
		}
		if cfgObj != nil {
			if ms := cfgObj.Suggestions.MaintenanceIntervalMs; ms > 0 {
				mcfg.Interval = time.Duration(ms) * time.Millisecond
			}
			if days := cfgObj.Suggestions.RetentionDays; days > 0 {
				mcfg.RetentionDays = days
			}
		}
		maintenanceRunner = maintenance.NewRunner(v2db.DB(), mcfg)
	}

	// Create server config
	cfg := &daemon.ServerConfig{
		Store:             store,
		V2DB:              v2db,
		Paths:             paths,
		Logger:            logger,
		LLM:               &claudeLLM{},
		FeedbackStore:     feedbackStore,
		MaintenanceRunner: maintenanceRunner,
	}

	// Run the daemon (blocks until shutdown)
	return daemon.Run(ctx, cfg)
}
