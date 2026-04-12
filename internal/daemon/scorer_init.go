package daemon

import (
	"context"
	"database/sql"
	"io"
	"log/slog"

	"github.com/runger/clai/internal/suggestions/discover"
	"github.com/runger/clai/internal/suggestions/discovery"
	"github.com/runger/clai/internal/suggestions/dismissal"
	"github.com/runger/clai/internal/suggestions/recovery"
	"github.com/runger/clai/internal/suggestions/score"
	suggest2 "github.com/runger/clai/internal/suggestions/suggest"
	"github.com/runger/clai/internal/suggestions/workflow"
)

// initScorer creates a Scorer with all available dependencies.
// Dependencies that fail to initialize are left nil; the Scorer handles nil
// stores gracefully by skipping those scoring features. This allows partial
// operation even when some stores are not compatible with the database.
func initScorer(db *sql.DB, logger *slog.Logger) *suggest2.Scorer {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}

	var deps suggest2.ScorerDependencies
	deps.DB = db

	deps.PipelineStore = score.NewPipelineStore(db)

	if ds, err := discovery.NewService(db, discovery.DefaultOptions()); err != nil {
		logger.Warn("scorer: discovery service unavailable", "error", err)
	} else {
		deps.DiscoveryService = ds
	}

	deps.DiscoverEngine = discover.NewEngine()

	patterns, err := workflow.LoadPromotedPatterns(context.Background(), db, workflow.DefaultMinerConfig().MinOccurrences)
	if err != nil {
		logger.Warn("scorer: workflow pattern load failed", "error", err)
	}
	deps.WorkflowTracker = workflow.NewTracker(patterns, workflow.DefaultTrackerConfig())

	deps.DismissalStore = dismissal.NewStore(db, dismissal.DefaultConfig(), logger)

	re, recErr := recovery.NewEngine(db, nil, nil, recovery.DefaultEngineConfig())
	if recErr != nil {
		logger.Warn("scorer: recovery engine unavailable", "error", recErr)
	} else {
		deps.RecoveryEngine = re
	}

	scorer, err := suggest2.NewScorer(&deps, suggest2.DefaultScorerConfig())
	if err != nil {
		logger.Warn("scorer: failed to create scorer", "error", err)
		return nil
	}

	logger.Info("scorer initialized")
	return scorer
}
