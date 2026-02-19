# Project Architecture

## Project Overview

- **Files:** 589
- **Symbols:** 8574
- **Edges:** 13934
- **Languages:** go (369), markdown (174), bash (10), yaml (10), toml (1), json (1)

## Directory Structure

| Directory | Files | Primary Language |
|-----------|-------|------------------|
| `internal/` | 341 | go |
| `.agents/` | 135 | markdown |
| `tests/` | 26 | go |
| `specs/` | 26 | markdown |
| `./` | 16 | yaml |
| `cmd/` | 15 | go |
| `docs/` | 13 | markdown |
| `.beads/` | 7 | yaml |
| `gen/` | 3 | go |
| `scripts/` | 2 | bash |
| `hooks/` | 2 | bash |
| `.github/` | 2 | yaml |
| `proto/` | 1 |  |

## Entry Points

- `cmd/clai-hook/main.go`
- `cmd/clai-picker/main.go`
- `cmd/clai-shim/main.go`
- `cmd/clai/main.go`
- `cmd/claid/main.go`

## Key Abstractions

Top symbols by importance (PageRank):

| Symbol | Kind | Location |
|--------|------|----------|
| `Len func (q *IngestionQueue) Len() int` | method | `internal/daemon/ingestion_queue.go:124` |
| `contains func contains(s, substr string) bool` | function | `internal/cmd/status_test.go:408` |
| `Run func Run(ctx context.Context, cfg *ServerConfig...` | function | `internal/daemon/lifecycle.go:25` |
| `Close func (m *mockStore) Close() error` | method | `internal/daemon/handlers_test.go:188` |
| `String func (x SearchMode) String() string` | method | `gen/clai/v1/clai.pb.go:60` |
| `New func New(cfg *Config) *slog.Logger` | function | `internal/suggestions/log/log.go:42` |
| `Open func Open(ctx context.Context, opts Options) (*...` | function | `internal/suggestions/db/db.go:103` |
| `ExecContext func (d *DB) ExecContext(ctx context.Context, q...` | method | `internal/suggestions/db/db.go:369` |
| `NewServer func NewServer(cfg *ServerConfig) (*Server, error)` | function | `internal/daemon/server.go:145` |
| `Close func (s *SQLiteStore) Close() error` | method | `internal/storage/db.go:97` |
| `newTestStore func newTestStore(t *testing.T) *SQLiteStore` | function | `internal/storage/db_test.go:211` |
| `WithTimeout func WithTimeout(d time.Duration) SessionOption` | function | `tests/expect/expect.go:98` |
| `Len func (b *LimitedBuffer) Len() int` | method | `internal/workflow/buffer.go:82` |
| `Now func Now() int64` | function | `internal/suggestions/score/transition.go:319` |
| `Close func (d *DB) Close() error` | method | `internal/suggestions/db/db.go:275` |

## Architecture

- **Dependency layers:** 16
- **Cycles (SCCs):** 2
- **Layer distribution:** L0: 6574 symbols, L1: 1030 symbols, L2: 413 symbols, L3: 210 symbols, L4: 85 symbols

## Testing

**Test directories:** `tests/`
- **Test files:** 191
- **Source files:** 398
- **Test-to-source ratio:** 0.48

## Coding Conventions

Follow these conventions when writing code in this project:

- **Imports:** Prefer absolute imports (100% are cross-directory)
- **Test files:** *_test.go

## Complexity Hotspots

Average function complexity: 2.4 (5133 functions analyzed)

Functions with highest complexity (consider refactoring):

| Function | Complexity | Location |
|----------|-----------|----------|
| `TestCheckShellIntegrationWithPaths_ShellSpecific` | 61 | `internal/cmd/status_test.go:101` |
| `TestNewServer_TableDriven` | 35 | `internal/daemon/server_test.go:149` |
| `TestSaveAndLoadRoundTrip` | 33 | `internal/config/config_test.go:815` |
| `TestV2Integration_FullLifecycle` | 31 | `internal/daemon/v2_integration_test.go:28` |
| `TestGetRCFiles` | 30 | `internal/cmd/install_test.go:68` |
| `handleAnalysis` | 30 | `internal/cmd/workflow.go:313` |
| `TestShellScripts_DoubleUpSequenceSupport` | 28 | `internal/cmd/init_test.go:962` |
| `TestCP3_CommandLogging` | 28 | `tests/integration/session_test.go:100` |
| `QueryHistoryCommands` | 26 | `internal/daemon/handlers_test.go:117` |
| `FuzzExtractTemplate` | 26 | `internal/suggestions/normalize/fuzz_test.go:98` |

## Domain Keywords

- **Top domain terms:** history, command, session, handler, suggestion, workflow, suggest, provider, suggestions, empty, file, server, commands, lite, daemon, import, success, socket, shell, and

## Core Modules

Most-imported modules (everything depends on these):

| Module | Imported By | Symbols Used |
|--------|-------------|--------------|
| `internal/daemon/ingestion_queue.go` | 188 files | 749 |
| `internal/suggestions/git/context.go` | 179 files | 275 |
| `gen/clai/v1/clai.pb.go` | 154 files | 556 |
| `internal/provider/context.go` | 145 files | 164 |
| `internal/suggestions/db/db.go` | 114 files | 443 |
| `internal/daemon/lifecycle.go` | 101 files | 310 |
| `internal/cmd/status_test.go` | 83 files | 356 |
| `internal/suggestions/discovery/runner.go` | 83 files | 173 |
| `tests/integration/testutil.go` | 82 files | 442 |
| `internal/daemon/handlers_test.go` | 80 files | 306 |
