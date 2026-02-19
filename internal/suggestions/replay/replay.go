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

	// CWD is the current working directory when the command was executed.
	CWD string

	// TimestampMs is the command timestamp in milliseconds.
	TimestampMs int64

	// ExitCode is the exit status of the command.
	ExitCode int
}

// ExpectedTopK defines the expected top-k suggestions after a specific command.
type ExpectedTopK struct {
	TopK         []string
	AfterCommand int
}

// DiffResult captures the difference between expected and actual top-k at one step.
type DiffResult struct {
	// Expected is the expected top-k list.
	Expected []string

	// Got is the actual top-k list from the scorer.
	Got []string

	// Mismatches lists the specific differences found.
	Mismatches []Mismatch

	// StepIndex is the index into ExpectedTopK that this diff corresponds to.
	StepIndex int

	// AfterCommand is the command index this expectation was checked after.
	AfterCommand int
}

// Mismatch describes a single difference between expected and actual top-k.
type Mismatch struct {
	// Expected is what was expected at this position (empty for "extra" type).
	Expected string

	// Got is what was actually at this position (empty for "missing" type).
	Got string

	// Type classifies the mismatch: "missing", "extra", or "reordered".
	Type string

	// Position is the index in the top-k list where the mismatch occurs.
	Position int
}

// RunnerConfig configures the replay runner.
type RunnerConfig struct {
	// SessionID is the fixed session ID used during replay.
	// Default: "replay-session".
	SessionID string

	// RepoKey is the repository key used during replay.
	// Default: "/replay/repo".
	RepoKey string

	// CWD is the default working directory used during replay.
	// Default: "/replay/workdir".
	CWD string

	// BaseTimestampMs is the starting timestamp for the deterministic clock.
	// Default: 1000.
	BaseTimestampMs int64

	// TimestampIncrementMs is the fixed increment per command for the deterministic clock.
	// Default: 1000.
	TimestampIncrementMs int64

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

	runtime, cleanup, err := r.setupReplayRuntime(ctx, tmpDir, session)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	var diffs []DiffResult
	var prevTemplateID string

	for cmdIdx, cmd := range session.Commands {
		stepDiffs, nextTemplateID, stepErr := r.replayCommandStep(ctx, runtime, session, cmdIdx, cmd, prevTemplateID)
		if stepErr != nil {
			return nil, stepErr
		}
		prevTemplateID = nextTemplateID
		diffs = append(diffs, stepDiffs...)
	}

	return diffs, nil
}

type replayRuntime struct {
	sqlDB          *sql.DB
	freqStore      *score.FrequencyStore
	transStore     *score.TransitionStore
	scorer         *suggest.Scorer
	expectationMap map[int][]int
}

func (r *Runner) setupReplayRuntime(ctx context.Context, tmpDir string, session Session) (*replayRuntime, func(), error) {
	d, err := openReplayDB(tmpDir)
	if err != nil {
		return nil, nil, err
	}
	cleanup := func() { d.Close() }
	sqlDB := d.DB()
	if err = createScorerTables(ctx, sqlDB); err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("create scorer tables: %w", err)
	}
	if err = insertReplaySession(ctx, sqlDB, r.cfg.SessionID, r.cfg.BaseTimestampMs); err != nil {
		cleanup()
		return nil, nil, err
	}
	freqStore, err := score.NewFrequencyStore(sqlDB, score.DefaultFrequencyOptions())
	if err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("create frequency store: %w", err)
	}
	transStore, err := score.NewTransitionStore(sqlDB)
	if err != nil {
		freqStore.Close()
		cleanup()
		return nil, nil, fmt.Errorf("create transition store: %w", err)
	}
	scorer, err := suggest.NewScorer(&suggest.ScorerDependencies{
		DB:              sqlDB,
		FreqStore:       freqStore,
		TransitionStore: transStore,
	}, &suggest.ScorerConfig{
		Weights:    suggest.DefaultWeights(),
		Amplifiers: suggest.DefaultAmplifierConfig(),
		TopK:       r.cfg.TopK,
	})
	if err != nil {
		transStore.Close()
		freqStore.Close()
		cleanup()
		return nil, nil, fmt.Errorf("create scorer: %w", err)
	}
	finalCleanup := func() {
		transStore.Close()
		freqStore.Close()
		cleanup()
	}
	return &replayRuntime{
		sqlDB:          sqlDB,
		freqStore:      freqStore,
		transStore:     transStore,
		scorer:         scorer,
		expectationMap: buildExpectationMap(session.Expected),
	}, finalCleanup, nil
}

func insertReplaySession(ctx context.Context, sqlDB *sql.DB, sessionID string, startedAtMs int64) error {
	_, err := sqlDB.ExecContext(ctx, `
		INSERT INTO session (id, shell, started_at_ms) VALUES (?, 'zsh', ?)
	`, sessionID, startedAtMs)
	if err != nil {
		return fmt.Errorf("insert session: %w", err)
	}
	return nil
}

func buildExpectationMap(expected []ExpectedTopK) map[int][]int {
	expectationMap := make(map[int][]int)
	for i, exp := range expected {
		expectationMap[exp.AfterCommand] = append(expectationMap[exp.AfterCommand], i)
	}
	return expectationMap
}

func (r *Runner) replayCommandStep(
	ctx context.Context,
	runtime *replayRuntime,
	session Session,
	cmdIdx int,
	cmd Command,
	prevTemplateID string,
) ([]DiffResult, string, error) {
	tsMs := r.resolveReplayTimestamp(cmdIdx, cmd)
	cwd := r.resolveReplayCWD(cmd)
	cmdRaw := resolveReplayCmdRaw(cmd)
	preNorm, err := r.ingestReplayCommand(ctx, runtime, &replayIngestInput{
		session:        session,
		cmdIdx:         cmdIdx,
		cmd:            cmd,
		prevTemplateID: prevTemplateID,
		tsMs:           tsMs,
		cwd:            cwd,
		cmdRaw:         cmdRaw,
	})
	if err != nil {
		return nil, "", err
	}
	stepDiffs, err := r.evaluateReplayExpectations(ctx, runtime, session, cmdIdx, preNorm.CmdNorm, cwd, tsMs)
	if err != nil {
		return nil, "", err
	}
	return stepDiffs, preNorm.CmdNorm, nil
}

func (r *Runner) resolveReplayTimestamp(cmdIdx int, cmd Command) int64 {
	if cmd.TimestampMs != 0 {
		return cmd.TimestampMs
	}
	return r.cfg.BaseTimestampMs + int64(cmdIdx)*r.cfg.TimestampIncrementMs
}

func (r *Runner) resolveReplayCWD(cmd Command) string {
	if cmd.CWD != "" {
		return cmd.CWD
	}
	return r.cfg.CWD
}

func resolveReplayCmdRaw(cmd Command) string {
	if cmd.CmdRaw != "" {
		return cmd.CmdRaw
	}
	return cmd.CmdNorm
}

type replayIngestInput struct {
	prevTemplateID string
	cwd            string
	cmdRaw         string
	session        Session
	cmd            Command
	tsMs           int64
	cmdIdx         int
}

func (r *Runner) ingestReplayCommand(
	ctx context.Context,
	runtime *replayRuntime,
	input *replayIngestInput,
) (normalize.PreNormResult, error) {
	ev := &event.CommandEvent{
		Version:   event.EventVersion,
		Type:      event.EventTypeCommandEnd,
		TS:        input.tsMs,
		SessionID: r.cfg.SessionID,
		Shell:     event.ShellZsh,
		Cwd:       input.cwd,
		CmdRaw:    input.cmdRaw,
		ExitCode:  input.cmd.ExitCode,
	}
	prevExitCode, prevFailed := previousReplayStatus(input.session.Commands, input.cmdIdx)
	wctx := ingest.PrepareWriteContext(ev, r.cfg.RepoKey, "", input.prevTemplateID, prevExitCode, prevFailed, nil)
	if _, err := ingest.WritePath(ctx, runtime.sqlDB, wctx, &ingest.WritePathConfig{}); err != nil {
		return normalize.PreNormResult{}, fmt.Errorf("write path for command %d (%q): %w", input.cmdIdx, input.cmd.CmdNorm, err)
	}
	preNorm := normalize.PreNormalize(input.cmdRaw, normalize.PreNormConfig{})
	if err := runtime.freqStore.Update(ctx, score.ScopeGlobal, preNorm.CmdNorm, input.tsMs); err != nil {
		return normalize.PreNormResult{}, fmt.Errorf("update global frequency for command %d: %w", input.cmdIdx, err)
	}
	if err := runtime.freqStore.Update(ctx, r.cfg.RepoKey, preNorm.CmdNorm, input.tsMs); err != nil {
		return normalize.PreNormResult{}, fmt.Errorf("update repo frequency for command %d: %w", input.cmdIdx, err)
	}
	if input.prevTemplateID != "" {
		if err := runtime.transStore.RecordTransition(ctx, score.ScopeGlobal, input.prevTemplateID, preNorm.CmdNorm, input.tsMs); err != nil {
			return normalize.PreNormResult{}, fmt.Errorf("record global transition for command %d: %w", input.cmdIdx, err)
		}
		if err := runtime.transStore.RecordTransition(ctx, r.cfg.RepoKey, input.prevTemplateID, preNorm.CmdNorm, input.tsMs); err != nil {
			return normalize.PreNormResult{}, fmt.Errorf("record repo transition for command %d: %w", input.cmdIdx, err)
		}
	}
	return preNorm, nil
}

func previousReplayStatus(commands []Command, idx int) (int, bool) {
	if idx <= 0 {
		return 0, false
	}
	prevExitCode := commands[idx-1].ExitCode
	return prevExitCode, prevExitCode != 0
}

func (r *Runner) evaluateReplayExpectations(
	ctx context.Context,
	runtime *replayRuntime,
	session Session,
	cmdIdx int,
	lastCmd string,
	cwd string,
	tsMs int64,
) ([]DiffResult, error) {
	expIndices, hasExpectation := runtime.expectationMap[cmdIdx]
	if !hasExpectation {
		return nil, nil
	}
	suggestions, err := runtime.scorer.Suggest(ctx, &suggest.SuggestContext{
		SessionID: r.cfg.SessionID,
		RepoKey:   r.cfg.RepoKey,
		LastCmd:   lastCmd,
		Cwd:       cwd,
		NowMs:     tsMs,
	})
	if err != nil {
		return nil, fmt.Errorf("suggest after command %d: %w", cmdIdx, err)
	}
	got := make([]string, len(suggestions))
	for i := range suggestions {
		got[i] = suggestions[i].Command
	}
	diffs := make([]DiffResult, 0, len(expIndices))
	for _, expIdx := range expIndices {
		exp := session.Expected[expIdx]
		mismatches := computeMismatches(exp.TopK, got)
		if len(mismatches) == 0 {
			continue
		}
		diffs = append(diffs, DiffResult{
			StepIndex:    expIdx,
			AfterCommand: cmdIdx,
			Expected:     exp.TopK,
			Got:          got,
			Mismatches:   mismatches,
		})
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

	gotSet := toPositionSet(got)
	expectedSet := toPositionSet(expected)

	for i := 0; i < maxSliceLen(expected, got); i++ {
		expVal := valueAt(expected, i)
		gotVal := valueAt(got, i)
		if expVal == gotVal {
			continue
		}
		mismatch, ok := mismatchAtPosition(i, expVal, gotVal, gotSet, expectedSet)
		if ok {
			mismatches = append(mismatches, mismatch)
		}
	}

	return mismatches
}

func toPositionSet(values []string) map[string]int {
	set := make(map[string]int, len(values))
	for i, value := range values {
		set[value] = i
	}
	return set
}

func maxSliceLen(a, b []string) int {
	if len(a) > len(b) {
		return len(a)
	}
	return len(b)
}

func valueAt(values []string, index int) string {
	if index >= 0 && index < len(values) {
		return values[index]
	}
	return ""
}

func mismatchAtPosition(position int, expectedVal, gotVal string, gotSet, expectedSet map[string]int) (Mismatch, bool) {
	switch {
	case expectedVal != "" && gotVal == "":
		return newMismatch(position, expectedVal, "", "missing"), true
	case expectedVal == "" && gotVal != "":
		if _, inExpected := expectedSet[gotVal]; !inExpected {
			return newMismatch(position, "", gotVal, "extra"), true
		}
		return Mismatch{}, false
	case expectedVal != "" && gotVal != "":
		if _, inGot := gotSet[expectedVal]; inGot {
			return newMismatch(position, expectedVal, gotVal, "reordered"), true
		}
		return newMismatch(position, expectedVal, gotVal, "missing"), true
	default:
		return Mismatch{}, false
	}
}

func newMismatch(position int, expected, got, mismatchType string) Mismatch {
	return Mismatch{
		Position: position,
		Expected: expected,
		Got:      got,
		Type:     mismatchType,
	}
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
