// claid is the clai background daemon that handles shell integration requests.
// It is spawned automatically when needed and exits after an idle timeout.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/runger/clai/internal/claude"
	"github.com/runger/clai/internal/config"
	"github.com/runger/clai/internal/daemon"
	suggestdb "github.com/runger/clai/internal/suggestions/db"
	"github.com/runger/clai/internal/suggestions/feedback"
	"github.com/runger/clai/internal/suggestions/maintenance"
	"github.com/runger/clai/internal/suggestions/ops"
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

	// Open V2 suggestions database (the single unified database)
	ctx := context.Background()
	v2db, err := suggestdb.Open(ctx, suggestdb.Options{})
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer v2db.Close()

	// One-time V1 data migration (non-fatal)
	home, _ := os.UserHomeDir()
	if home != "" {
		v1Path := filepath.Join(home, ".clai", "state.db")
		if migErr := ops.MigrateV1Data(ctx, v2db, v1Path, logger); migErr != nil {
			logger.Warn("V1 data migration failed (non-fatal)", "error", migErr)
		}
	}

	feedbackStore := feedback.NewStore(v2db.DB(), feedback.DefaultConfig(), logger)
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
	maintenanceRunner := maintenance.NewRunner(v2db.DB(), mcfg)

	// Create server config
	cfg := &daemon.ServerConfig{
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
