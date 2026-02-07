package daemon

import (
	"database/sql"
	"log/slog"

	"github.com/runger/clai/internal/suggestions/discovery"
	"github.com/runger/clai/internal/suggestions/dismissal"
	"github.com/runger/clai/internal/suggestions/recovery"
	"github.com/runger/clai/internal/suggestions/score"
	suggest2 "github.com/runger/clai/internal/suggestions/suggest"
	"github.com/runger/clai/internal/suggestions/workflow"
)

// initV2Scorer creates a V2 Scorer with all available dependencies.
// Dependencies that fail to initialize are left nil; the Scorer handles nil
// stores gracefully by skipping those scoring features. This allows partial
// operation even when V1-schema stores are not compatible with the V2 database.
func initV2Scorer(db *sql.DB, logger *slog.Logger) *suggest2.Scorer {
	var deps suggest2.ScorerDependencies
	deps.DB = db

	if fs, err := score.NewFrequencyStore(db, score.DefaultFrequencyOptions()); err != nil {
		logger.Warn("v2 scorer: frequency store unavailable", "error", err)
	} else {
		deps.FreqStore = fs
	}

	if ts, err := score.NewTransitionStore(db); err != nil {
		logger.Warn("v2 scorer: transition store unavailable", "error", err)
	} else {
		deps.TransitionStore = ts
	}

	deps.PipelineStore = score.NewPipelineStore(db)

	if ds, err := discovery.NewService(db, discovery.DefaultOptions()); err != nil {
		logger.Warn("v2 scorer: discovery service unavailable", "error", err)
	} else {
		deps.DiscoveryService = ds
	}

	deps.WorkflowTracker = workflow.NewTracker(nil, workflow.DefaultTrackerConfig())

	deps.DismissalStore = dismissal.NewStore(db, dismissal.DefaultConfig(), logger)

	if re, err := recovery.NewEngine(db, nil, nil, recovery.DefaultEngineConfig()); err != nil {
		logger.Warn("v2 scorer: recovery engine unavailable", "error", err)
	} else {
		deps.RecoveryEngine = re
	}

	scorer, err := suggest2.NewScorer(deps, suggest2.DefaultScorerConfig())
	if err != nil {
		logger.Warn("v2 scorer: failed to create scorer", "error", err)
		return nil
	}

	logger.Info("v2 scorer initialized")
	return scorer
}
