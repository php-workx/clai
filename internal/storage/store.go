// Package storage provides SQLite-based persistent storage for clai.
// It handles sessions, commands, and AI response caching.
package storage

import (
	"context"

	"github.com/runger/clai/internal/history"
)

// Store defines the interface for all storage operations.
// The daemon is the single writer; clai-shim never opens the DB directly.
type Store interface {
	// Sessions
	CreateSession(ctx context.Context, s *Session) error
	EndSession(ctx context.Context, sessionID string, endTime int64) error
	GetSession(ctx context.Context, sessionID string) (*Session, error)
	GetSessionByPrefix(ctx context.Context, prefix string) (*Session, error)

	// Commands
	CreateCommand(ctx context.Context, c *Command) error
	UpdateCommandEnd(ctx context.Context, commandID string, exitCode int, endTime, duration int64) error
	QueryCommands(ctx context.Context, q CommandQuery) ([]Command, error)
	QueryHistoryCommands(ctx context.Context, q CommandQuery) ([]HistoryRow, error)

	// AI Cache
	GetCached(ctx context.Context, key string) (*CacheEntry, error)
	SetCached(ctx context.Context, entry *CacheEntry) error
	PruneExpiredCache(ctx context.Context) (int64, error)

	// History Import
	HasImportedHistory(ctx context.Context, shell string) (bool, error)
	ImportHistory(ctx context.Context, entries []history.ImportEntry, shell string) (int, error)

	// Workflow methods
	CreateWorkflowRun(ctx context.Context, run *WorkflowRun) error
	UpdateWorkflowRun(ctx context.Context, runID string, status string, endedAt int64, durationMs int64) error
	GetWorkflowRun(ctx context.Context, runID string) (*WorkflowRun, error)
	QueryWorkflowRuns(ctx context.Context, q WorkflowRunQuery) ([]WorkflowRun, error)
	CreateWorkflowStep(ctx context.Context, step *WorkflowStep) error
	UpdateWorkflowStep(ctx context.Context, update *WorkflowStepUpdate) error
	GetWorkflowStep(ctx context.Context, runID, stepID, matrixKey string) (*WorkflowStep, error)
	CreateWorkflowAnalysis(ctx context.Context, analysis *WorkflowAnalysis) error
	GetWorkflowAnalyses(ctx context.Context, runID, stepID, matrixKey string) ([]WorkflowAnalysisRecord, error)

	// Lifecycle
	Close() error
}

// Session represents a shell session.
type Session struct {
	SessionID       string
	StartedAtUnixMs int64
	EndedAtUnixMs   *int64
	Shell           string
	OS              string
	Hostname        string
	Username        string
	InitialCWD      string
}

// Command represents a command executed in a session.
type Command struct {
	ID            int64
	CommandID     string
	SessionID     string
	TsStartUnixMs int64
	TsEndUnixMs   *int64
	DurationMs    *int64
	CWD           string
	Command       string
	CommandNorm   string
	CommandHash   string
	ExitCode      *int
	IsSuccess     *bool // nil = unknown (treated as success), false = failure, true = success

	// Git context (captured at command start)
	GitBranch   *string
	GitRepoName *string
	GitRepoRoot *string

	// Sequence tracking
	PrevCommandID *string

	// Derived metadata (computed from command text)
	IsSudo    bool
	PipeCount int
	WordCount int
}

// CommandQuery defines parameters for querying commands.
type CommandQuery struct {
	SessionID        *string // Include only this session
	ExcludeSessionID string  // Exclude this session (for global queries)
	CWD              *string
	Prefix           string
	Substring        string // Substring match (case-insensitive via command_norm)
	Limit            int
	Offset           int  // Skip this many results (for pagination)
	SuccessOnly      bool // Only return successful commands (exit code 0)
	FailureOnly      bool // Only return failed commands (exit code != 0)
	Deduplicate      bool // Group by command_norm, return most recent per unique command
}

// HistoryRow represents a deduplicated command history entry.
type HistoryRow struct {
	Command     string
	TimestampMs int64
}

// CacheEntry represents a cached AI response.
type CacheEntry struct {
	CacheKey        string
	ResponseJSON    string
	Provider        string
	CreatedAtUnixMs int64
	ExpiresAtUnixMs int64
	HitCount        int64
}

// WorkflowRun represents a workflow execution run.
type WorkflowRun struct {
	RunID        string
	WorkflowName string
	WorkflowHash string
	WorkflowPath string
	Status       string // "running", "passed", "failed", "cancelled"
	StartedAt    int64  // unix ms
	EndedAt      int64  // unix ms
	DurationMs   int64
}

// WorkflowStep represents a single step within a workflow run.
type WorkflowStep struct {
	RunID       string
	StepID      string
	MatrixKey   string // Composite key per D16
	Status      string // "running", "passed", "failed", "skipped"
	Command     string
	ExitCode    int
	DurationMs  int64
	StdoutTail  string
	StderrTail  string
	OutputsJSON string
}

// WorkflowStepUpdate contains fields for updating a workflow step.
type WorkflowStepUpdate struct {
	RunID       string
	StepID      string
	MatrixKey   string
	Status      string
	ExitCode    int
	DurationMs  int64
	StdoutTail  string
	StderrTail  string
	OutputsJSON string
}

// WorkflowAnalysis represents an AI analysis of a workflow step.
type WorkflowAnalysis struct {
	RunID       string
	StepID      string
	MatrixKey   string
	Decision    string // "approve", "reject", "needs_human", "error"
	Reasoning   string
	FlagsJSON   string
	Prompt      string
	RawResponse string
	DurationMs  int64
	AnalyzedAt  int64 // unix ms
}

// WorkflowAnalysisRecord is a stored analysis record with an auto-generated ID.
type WorkflowAnalysisRecord struct {
	ID          int64
	RunID       string
	StepID      string
	MatrixKey   string
	Decision    string
	Reasoning   string
	FlagsJSON   string
	Prompt      string
	RawResponse string
	DurationMs  int64
	AnalyzedAt  int64
}

// WorkflowRunQuery defines parameters for querying workflow runs.
type WorkflowRunQuery struct {
	RunID        string
	WorkflowName string
	Status       string
	Limit        int
	Offset       int
}
