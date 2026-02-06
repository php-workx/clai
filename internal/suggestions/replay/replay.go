// Package replay provides deterministic replay validation for the suggestions engine.
// It replays sanitized session recordings through the suggestion engine and validates
// that scoring output is deterministic and matches expected top-k rankings.
//
// Per spec Section 14.9: maintain a replay corpus of sanitized command sessions with
// expected top-k suggestions per step. Replay runner executes with fixed clock and
// fixed random seed for deterministic comparisons.
package replay

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/runger/clai/internal/suggestions/db"
	"github.com/runger/clai/internal/suggestions/event"
	"github.com/runger/clai/internal/suggestions/ingest"
	"github.com/runger/clai/internal/suggestions/normalize"
	"github.com/runger/clai/internal/suggestions/score"
	"github.com/runger/clai/internal/suggestions/suggest"
)

// Session represents a sanitized command session recording for replay.
type Session struct {
	// ID is a human-readable identifier for the session.
	ID string

	// Commands is the ordered sequence of commands in this session.
	Commands []Command

	// Expected defines the expected top-k suggestions at specific points in the session.
	Expected []ExpectedTopK
}

// Command represents a single command in a replay session.
type Command struct {
	// CmdNorm is the normalized command string (also used as CmdRaw for replay).
	CmdNorm string

	// CmdRaw is the raw command string. If empty, CmdNorm is used.
	CmdRaw string

	// ExitCode is the exit status of the command.
	ExitCode int

	// TimestampMs is the command timestamp in milliseconds.
	TimestampMs int64

	// CWD is the current working directory when the command was executed.
	CWD string
}

// ExpectedTopK defines the expected top-k suggestions after a specific command.
type ExpectedTopK struct {
	// AfterCommand is the index into the session's Commands slice.
	AfterCommand int

	// TopK is the expected suggestion order (cmd_norm values).
	TopK []string
}

// DiffResult captures the difference between expected and actual top-k at one step.
type DiffResult struct {
	// StepIndex is the index into ExpectedTopK that this diff corresponds to.
	StepIndex int

	// AfterCommand is the command index this expectation was checked after.
	AfterCommand int

	// Expected is the expected top-k list.
	Expected []string

	// Got is the actual top-k list from the scorer.
	Got []string

	// Mismatches lists the specific differences found.
	Mismatches []Mismatch
}

// Mismatch describes a single difference between expected and actual top-k.
type Mismatch struct {
	// Position is the index in the top-k list where the mismatch occurs.
	Position int

	// Expected is what was expected at this position (empty for "extra" type).
	Expected string

	// Got is what was actually at this position (empty for "missing" type).
	Got string

	// Type classifies the mismatch: "missing", "extra", or "reordered".
	Type string
}

// RunnerConfig configures the replay runner.
type RunnerConfig struct {
	// BaseTimestampMs is the starting timestamp for the deterministic clock.
	// Default: 1000.
	BaseTimestampMs int64

	// TimestampIncrementMs is the fixed increment per command for the deterministic clock.
	// Default: 1000.
	TimestampIncrementMs int64

	// SessionID is the fixed session ID used during replay.
	// Default: "replay-session".
	SessionID string

	// RepoKey is the repository key used during replay.
	// Default: "/replay/repo".
	RepoKey string

	// CWD is the default working directory used during replay.
	// Default: "/replay/workdir".
	CWD string

	// TopK is the number of suggestions to request.
	// Default: suggest.DefaultTopK (3).
	TopK int
}

// DefaultRunnerConfig returns a RunnerConfig with deterministic defaults.
func DefaultRunnerConfig() RunnerConfig {
	return RunnerConfig{
		BaseTimestampMs:      1000,
		TimestampIncrementMs: 1000,
		SessionID:            "replay-session",
		RepoKey:              "/replay/repo",
		CWD:                  "/replay/workdir",
		TopK:                 suggest.DefaultTopK,
	}
}

// Runner replays sanitized command sessions through the suggestion engine
// and compares actual output against expected top-k rankings.
type Runner struct {
	cfg RunnerConfig
}

// NewRunner creates a replay runner with deterministic configuration.
func NewRunner(cfg RunnerConfig) *Runner {
	if cfg.BaseTimestampMs <= 0 {
		cfg.BaseTimestampMs = 1000
	}
	if cfg.TimestampIncrementMs <= 0 {
		cfg.TimestampIncrementMs = 1000
	}
	if cfg.SessionID == "" {
		cfg.SessionID = "replay-session"
	}
	if cfg.RepoKey == "" {
		cfg.RepoKey = "/replay/repo"
	}
	if cfg.CWD == "" {
		cfg.CWD = "/replay/workdir"
	}
	if cfg.TopK <= 0 {
		cfg.TopK = suggest.DefaultTopK
	}
	return &Runner{cfg: cfg}
}

// openReplayDB creates and opens a fresh temporary database for replay.
// Each replay gets an isolated database to ensure determinism.
// It opens a V2 database via db.Open and then adds V1-compatible tables
// needed by the scorer's FrequencyStore and TransitionStore.
func openReplayDB(tmpDir string) (*db.DB, error) {
	dbPath := filepath.Join(tmpDir, "replay.db")
	d, err := db.Open(context.Background(), db.Options{
		Path:     dbPath,
		SkipLock: true,
	})
	if err != nil {
		return nil, fmt.Errorf("open replay database: %w", err)
	}
	return d, nil
}

// Replay replays a single session through the suggestion engine and returns any diffs.
// An empty DiffResult slice means the replay matched all expectations.
func (r *Runner) Replay(ctx context.Context, tmpDir string, session Session) ([]DiffResult, error) {
	if len(session.Commands) == 0 {
		return nil, nil
	}

	// Open a fresh database for this replay
	d, err := openReplayDB(tmpDir)
	if err != nil {
		return nil, err
	}
	defer d.Close()

	sqlDB := d.DB()

	// Create the V1 tables needed by the scorer's FrequencyStore and TransitionStore.
	// The V2 schema uses different table names (command_stat, transition_stat) but the
	// scorer dependencies use the V1-era tables (command_score, transition).
	if err := createScorerTables(ctx, sqlDB); err != nil {
		return nil, fmt.Errorf("create scorer tables: %w", err)
	}

	// Create the session row
	_, err = sqlDB.ExecContext(ctx, `
		INSERT INTO session (id, shell, started_at_ms) VALUES (?, 'zsh', ?)
	`, r.cfg.SessionID, r.cfg.BaseTimestampMs)
	if err != nil {
		return nil, fmt.Errorf("insert session: %w", err)
	}

	// Build expectation lookup: afterCommand -> []ExpectedTopK index
	expectationMap := make(map[int][]int)
	for i, exp := range session.Expected {
		expectationMap[exp.AfterCommand] = append(expectationMap[exp.AfterCommand], i)
	}

	// Create scorer dependencies
	freqStore, err := score.NewFrequencyStore(sqlDB, score.DefaultFrequencyOptions())
	if err != nil {
		return nil, fmt.Errorf("create frequency store: %w", err)
	}
	defer freqStore.Close()

	transStore, err := score.NewTransitionStore(sqlDB)
	if err != nil {
		return nil, fmt.Errorf("create transition store: %w", err)
	}
	defer transStore.Close()

	scorerCfg := suggest.ScorerConfig{
		Weights:    suggest.DefaultWeights(),
		Amplifiers: suggest.DefaultAmplifierConfig(),
		TopK:       r.cfg.TopK,
	}

	scorer, err := suggest.NewScorer(suggest.ScorerDependencies{
		DB:              sqlDB,
		FreqStore:       freqStore,
		TransitionStore: transStore,
	}, scorerCfg)
	if err != nil {
		return nil, fmt.Errorf("create scorer: %w", err)
	}

	var diffs []DiffResult
	var prevTemplateID string

	// Replay each command
	for cmdIdx, cmd := range session.Commands {
		// Resolve timestamps deterministically
		tsMs := cmd.TimestampMs
		if tsMs == 0 {
			tsMs = r.cfg.BaseTimestampMs + int64(cmdIdx)*r.cfg.TimestampIncrementMs
		}

		// Resolve CWD
		cwd := cmd.CWD
		if cwd == "" {
			cwd = r.cfg.CWD
		}

		// Resolve raw command
		cmdRaw := cmd.CmdRaw
		if cmdRaw == "" {
			cmdRaw = cmd.CmdNorm
		}

		// Ingest the command through the write path
		ev := &event.CommandEvent{
			Version:   event.EventVersion,
			Type:      event.EventTypeCommandEnd,
			Ts:        tsMs,
			SessionID: r.cfg.SessionID,
			Shell:     event.ShellZsh,
			Cwd:       cwd,
			CmdRaw:    cmdRaw,
			ExitCode:  cmd.ExitCode,
		}

		prevExitCode := 0
		prevFailed := false
		if cmdIdx > 0 {
			prevExitCode = session.Commands[cmdIdx-1].ExitCode
			prevFailed = prevExitCode != 0
		}

		wctx := ingest.PrepareWriteContext(ev, r.cfg.RepoKey, "", prevTemplateID, prevExitCode, prevFailed, nil)

		_, err := ingest.WritePath(ctx, sqlDB, wctx, ingest.WritePathConfig{})
		if err != nil {
			return nil, fmt.Errorf("write path for command %d (%q): %w", cmdIdx, cmd.CmdNorm, err)
		}

		// Also update the frequency and transition stores directly
		// (the V1 stores used by the scorer use different tables than the V2 write path)
		preNorm := normalize.PreNormalize(cmdRaw, normalize.PreNormConfig{})

		if err := freqStore.Update(ctx, score.ScopeGlobal, preNorm.CmdNorm, tsMs); err != nil {
			return nil, fmt.Errorf("update global frequency for command %d: %w", cmdIdx, err)
		}
		if err := freqStore.Update(ctx, r.cfg.RepoKey, preNorm.CmdNorm, tsMs); err != nil {
			return nil, fmt.Errorf("update repo frequency for command %d: %w", cmdIdx, err)
		}

		if prevTemplateID != "" {
			if err := transStore.RecordTransition(ctx, score.ScopeGlobal, prevTemplateID, preNorm.CmdNorm, tsMs); err != nil {
				return nil, fmt.Errorf("record global transition for command %d: %w", cmdIdx, err)
			}
			if err := transStore.RecordTransition(ctx, r.cfg.RepoKey, prevTemplateID, preNorm.CmdNorm, tsMs); err != nil {
				return nil, fmt.Errorf("record repo transition for command %d: %w", cmdIdx, err)
			}
		}

		// Update prevTemplateID for next iteration
		prevTemplateID = preNorm.CmdNorm

		// Check if there are expectations after this command
		expIndices, hasExpectation := expectationMap[cmdIdx]
		if !hasExpectation {
			continue
		}

		// Query suggestions
		suggestCtx := suggest.SuggestContext{
			SessionID: r.cfg.SessionID,
			RepoKey:   r.cfg.RepoKey,
			LastCmd:   preNorm.CmdNorm,
			Cwd:       cwd,
			NowMs:     tsMs,
		}

		suggestions, err := scorer.Suggest(ctx, suggestCtx)
		if err != nil {
			return nil, fmt.Errorf("suggest after command %d: %w", cmdIdx, err)
		}

		// Extract actual top-k
		got := make([]string, len(suggestions))
		for i, s := range suggestions {
			got[i] = s.Command
		}

		// Compare against each expectation for this command
		for _, expIdx := range expIndices {
			exp := session.Expected[expIdx]
			mismatches := computeMismatches(exp.TopK, got)
			if len(mismatches) > 0 {
				diffs = append(diffs, DiffResult{
					StepIndex:    expIdx,
					AfterCommand: cmdIdx,
					Expected:     exp.TopK,
					Got:          got,
					Mismatches:   mismatches,
				})
			}
		}
	}

	return diffs, nil
}

// ReplayAll replays all sessions and returns a map of session ID to diff results.
// Sessions with no diffs have an empty slice in the result map.
func (r *Runner) ReplayAll(ctx context.Context, tmpDir string, sessions []Session) (map[string][]DiffResult, error) {
	results := make(map[string][]DiffResult, len(sessions))

	for i, session := range sessions {
		// Each session gets its own subdirectory for database isolation
		sessionDir := filepath.Join(tmpDir, fmt.Sprintf("session-%d", i))
		diffs, err := r.Replay(ctx, sessionDir, session)
		if err != nil {
			return nil, fmt.Errorf("replay session %q: %w", session.ID, err)
		}
		results[session.ID] = diffs
	}

	return results, nil
}

// createScorerTables creates the V1-era tables that the scorer's FrequencyStore
// and TransitionStore depend on. The V2 schema (from db.Open) uses different table
// names (command_stat, transition_stat), but the scorer stores use command_score
// and transition tables. Additionally, TransitionStore.getPrevStmt references
// command_event.ts (V1 column name) which is ts_ms in V2, so we create a view
// that provides backward compatibility.
func createScorerTables(ctx context.Context, sqlDB *sql.DB) error {
	_, err := sqlDB.ExecContext(ctx, `
		-- V1 command_score table used by score.FrequencyStore
		CREATE TABLE IF NOT EXISTS command_score (
			scope    TEXT NOT NULL,
			cmd_norm TEXT NOT NULL,
			score    REAL NOT NULL,
			last_ts  INTEGER NOT NULL,
			PRIMARY KEY(scope, cmd_norm)
		);
		CREATE INDEX IF NOT EXISTS idx_command_score_scope
			ON command_score(scope, score DESC);

		-- V1 transition table used by score.TransitionStore
		CREATE TABLE IF NOT EXISTS transition (
			scope     TEXT NOT NULL,
			prev_norm TEXT NOT NULL,
			next_norm TEXT NOT NULL,
			count     INTEGER NOT NULL,
			last_ts   INTEGER NOT NULL,
			PRIMARY KEY(scope, prev_norm, next_norm)
		);
		CREATE INDEX IF NOT EXISTS idx_transition_prev
			ON transition(scope, prev_norm);

		-- V1 project_task table used by discovery.Service
		CREATE TABLE IF NOT EXISTS project_task (
			repo_key      TEXT NOT NULL,
			kind          TEXT NOT NULL,
			name          TEXT NOT NULL,
			command       TEXT NOT NULL,
			description   TEXT,
			discovered_ts INTEGER NOT NULL,
			PRIMARY KEY(repo_key, kind, name)
		);
	`)
	if err != nil {
		return err
	}

	// The TransitionStore.getPrevStmt queries command_event with column 'ts',
	// but V2 schema uses 'ts_ms'. Rename V2 table and create a V1-compatible
	// view with the aliased column name.
	_, err = sqlDB.ExecContext(ctx, `ALTER TABLE command_event RENAME TO command_event_v2`)
	if err != nil {
		return fmt.Errorf("rename command_event: %w", err)
	}

	// Drop V2 triggers referencing old table name before creating the view
	_, _ = sqlDB.ExecContext(ctx, `DROP TRIGGER IF EXISTS command_event_ai`)
	_, _ = sqlDB.ExecContext(ctx, `DROP TRIGGER IF EXISTS command_event_ad`)

	// Create a view that provides both V1 (ts) and V2 (ts_ms) column names
	_, err = sqlDB.ExecContext(ctx, `
		CREATE VIEW command_event AS
		SELECT
			id, session_id, ts_ms, ts_ms AS ts, cwd, repo_key, branch,
			cmd_raw, cmd_norm, cmd_truncated, template_id,
			exit_code, duration_ms, ephemeral
		FROM command_event_v2
	`)
	if err != nil {
		return fmt.Errorf("create command_event view: %w", err)
	}

	// Create an INSTEAD OF INSERT trigger so INSERTs into the view work
	_, err = sqlDB.ExecContext(ctx, `
		CREATE TRIGGER command_event_insert INSTEAD OF INSERT ON command_event
		BEGIN
			INSERT INTO command_event_v2 (
				session_id, ts_ms, cwd, repo_key, branch,
				cmd_raw, cmd_norm, cmd_truncated, template_id,
				exit_code, duration_ms, ephemeral
			) VALUES (
				NEW.session_id, NEW.ts_ms, NEW.cwd, NEW.repo_key, NEW.branch,
				NEW.cmd_raw, NEW.cmd_norm, NEW.cmd_truncated, NEW.template_id,
				NEW.exit_code, NEW.duration_ms, NEW.ephemeral
			);
		END
	`)
	if err != nil {
		return fmt.Errorf("create command_event insert trigger: %w", err)
	}

	return nil
}

// computeMismatches compares expected vs actual top-k and returns mismatches.
func computeMismatches(expected, got []string) []Mismatch {
	var mismatches []Mismatch

	// Build lookup sets
	gotSet := make(map[string]int) // value -> position
	for i, g := range got {
		gotSet[g] = i
	}
	expectedSet := make(map[string]int) // value -> position
	for i, e := range expected {
		expectedSet[e] = i
	}

	// Check each expected entry
	maxLen := len(expected)
	if len(got) > maxLen {
		maxLen = len(got)
	}

	for i := 0; i < maxLen; i++ {
		var expVal, gotVal string
		if i < len(expected) {
			expVal = expected[i]
		}
		if i < len(got) {
			gotVal = got[i]
		}

		if expVal == gotVal {
			continue
		}

		if expVal != "" && gotVal == "" {
			// Expected something but got nothing at this position
			mismatches = append(mismatches, Mismatch{
				Position: i,
				Expected: expVal,
				Got:      "",
				Type:     "missing",
			})
		} else if expVal == "" && gotVal != "" {
			// Got something unexpected at this position
			if _, inExpected := expectedSet[gotVal]; !inExpected {
				mismatches = append(mismatches, Mismatch{
					Position: i,
					Expected: "",
					Got:      gotVal,
					Type:     "extra",
				})
			}
		} else {
			// Both have values but they differ
			if _, inGot := gotSet[expVal]; inGot {
				// Expected value exists but at wrong position
				mismatches = append(mismatches, Mismatch{
					Position: i,
					Expected: expVal,
					Got:      gotVal,
					Type:     "reordered",
				})
			} else {
				// Expected value is completely missing
				mismatches = append(mismatches, Mismatch{
					Position: i,
					Expected: expVal,
					Got:      gotVal,
					Type:     "missing",
				})
			}
		}
	}

	return mismatches
}

// FormatDiffs produces human-readable diff output showing expected vs actual top-k rankings.
func FormatDiffs(sessionID string, diffs []DiffResult) string {
	if len(diffs) == 0 {
		return fmt.Sprintf("Session %q: all expectations matched.\n", sessionID)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Session %q: %d diff(s) found:\n", sessionID, len(diffs))

	for _, diff := range diffs {
		fmt.Fprintf(&b, "\n  Step %d (after command #%d):\n", diff.StepIndex, diff.AfterCommand)
		fmt.Fprintf(&b, "    Expected: %s\n", formatTopK(diff.Expected))
		fmt.Fprintf(&b, "    Got:      %s\n", formatTopK(diff.Got))

		for _, m := range diff.Mismatches {
			switch m.Type {
			case "missing":
				fmt.Fprintf(&b, "    [%d] MISSING: expected %q, got %q\n", m.Position, m.Expected, m.Got)
			case "extra":
				fmt.Fprintf(&b, "    [%d] EXTRA: unexpected %q\n", m.Position, m.Got)
			case "reordered":
				fmt.Fprintf(&b, "    [%d] REORDERED: expected %q, got %q\n", m.Position, m.Expected, m.Got)
			}
		}
	}

	return b.String()
}

// FormatAllDiffs produces human-readable output for multiple sessions.
func FormatAllDiffs(results map[string][]DiffResult) string {
	var b strings.Builder

	for sessionID, diffs := range results {
		b.WriteString(FormatDiffs(sessionID, diffs))
	}

	return b.String()
}

// formatTopK formats a top-k list for display.
func formatTopK(topK []string) string {
	if len(topK) == 0 {
		return "(empty)"
	}

	parts := make([]string, len(topK))
	for i, cmd := range topK {
		parts[i] = fmt.Sprintf("%d:%q", i+1, cmd)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}
