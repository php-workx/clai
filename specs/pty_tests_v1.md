# PTY Test Plan (Unit, Integration, Expect, Docker, End-to-End)

**Spec under test:** `/Users/runger/.claude-worktrees/clai/hungry-swartz/specs/pty_tech_v1.md`  
**Scope:** `clai-wrap` (Rust), daemon/storage paths (Go), and cross-shell behavior  
**Goal:** Maximize PTY feature coverage while keeping default test lanes fast and reliable.

## 1. Test Strategy Summary

This plan follows a speed-first pyramid:

1. Unit: fastest, deterministic logic and parser/state tests.
2. Integration: component wiring with real PTY/daemon/storage boundaries.
3. Expect: interactive shell behavior checks with scripted terminal IO.
4. Docker: distro and shell portability checks for interactive flows.
5. End-to-end: highest-fidelity full scenarios and acceptance checks.

Runtime optimization rules:

- P0 behavior must be covered by Unit or Integration and run on every PR.
- Expect smoke subset runs on PR; full expect suite runs on merge/nightly.
- Docker and full End-to-End run nightly or release gates.
- Flaky/slow tests are isolated, never blocking fast feedback lanes.

## 2. Categories and Success Criteria

### 2.1 Unit

Purpose:

- Validate deterministic logic without process orchestration.
- Catch regressions in parsing, state machines, policy decisions, and edge rules.

Pass criteria:

- 100% pass rate on Linux/macOS/Windows runners for pure logic tests.
- No test relying on wall-clock sleeps where fake clocks are possible.

### 2.2 Integration

Purpose:

- Validate module boundaries and behavior across PTY, daemon, and storage layers.

Pass criteria:

- PTY mode transitions, fallback paths, and protocol semantics match spec.
- DB migration and retention behavior validated against real SQLite.

### 2.3 Expect

Purpose:

- Validate interactive shell behavior (bash/zsh/fish) through scripted terminal sessions.

Pass criteria:

- Prompt, key chord, insertion, and restoration behaviors match real shell behavior.

### 2.4 Docker

Purpose:

- Validate shell and distro variability with reproducible environments.

Pass criteria:

- Core interactive tests pass in Alpine, Ubuntu, Debian, Fedora.

### 2.5 End-to-End

Purpose:

- Validate user-facing acceptance workflows and cross-component outcomes.

Pass criteria:

- Critical scenarios pass with artifacts (logs/screenshots/transcripts) for failures.

## 3. Execution Lanes and Runtime Budgets

| Lane | Trigger | Categories | Runtime Target | Blocking |
|---|---|---|---|---|
| Fast Local | pre-commit / local dev | Unit + selected Integration | <= 6 min | Yes |
| PR Core | CI on PR | Unit + Integration + Expect smoke | <= 15 min | Yes |
| PR Extended | CI optional/manual | Full Expect + selected End-to-End smoke | <= 25 min | Optional |
| Nightly | scheduled | Full Unit + Integration + Expect + Docker + End-to-End | <= 75 min | Yes (nightly signal) |
| Release Gate | before release | Nightly set + extra stress passes | <= 120 min | Yes |

Commands mapped to current repo:

- Go unit/integration baseline: `make test`
- Rust tests baseline: `make test-rust`
- Expect tests: `make test-interactive`
- Docker interactive tests: `make test-docker`
- Full baseline: `make test-all`

Security gate (required by spec):

- `cargo audit` must run in CI, pre-commit, and `make dev`.

## 4. Feature-to-Category Coverage Matrix

| ID | Spec Area | Primary Category | Secondary Category | Priority |
|---|---|---|---|---|
| F01 | PTY launch, child lifecycle, exit code mapping | Integration | End-to-End | P0 |
| F02 | Raw mode enter/restore, alt-screen restore | Integration | Expect | P0 |
| F03 | Stream-aware non-TTY degradation | Integration | End-to-End | P0 |
| F04 | `--force-non-tty` passthrough semantics | Integration | Expect | P0 |
| F05 | Hotkey chord parser + timeout behavior | Unit | Expect | P0 |
| F06 | Picker open/close behavior and buffered flush ordering | Integration | Expect | P0 |
| F07 | Non-blocking PTY reader and overflow behavior | Unit | Integration | P0 |
| F08 | Bracketed paste tracking (`?2004h`/`?2004l`) and insertion | Unit | Integration | P0 |
| F09 | Resize debounce trailing-edge invariant | Unit | Integration | P0 |
| F10 | Unix signal behavior (`SIGWINCH`, `SIGINT`, `SIGTERM`, `SIGHUP`, `SIGTSTP`, `SIGCONT`) | Integration | End-to-End | P0 |
| F11 | Windows console/conpty behavior | Integration | End-to-End | P1 |
| F12 | JSON-RPC framing, methods, error code mapping | Unit | Integration | P0 |
| F13 | Protocol mismatch MUST fallback to standalone | Integration | End-to-End | P0 |
| F14 | Daemon default-on + timeout/reconnect + one-time warning | Integration | End-to-End | P0 |
| F15 | Stale socket ownership safety on `ECONNREFUSED` | Integration | End-to-End | P0 |
| F16 | OSC133 parser split packets, BEL/ST terminators | Unit | Integration | P0 |
| F17 | OSC133 startup watchdog fallback (500ms) | Integration | Expect | P0 |
| F18 | Shell injection ordering (zsh/bash/fish) | Expect | Docker | P0 |
| F19 | Bash login injection opt-in matrix | Expect | Docker | P0 |
| F20 | Color/term capability degrade (`TERM=dumb`, NO_COLOR, 256color) | Unit | Expect | P1 |
| F21 | Unicode width/render handling | Unit | Expect | P1 |
| F22 | Denylist and echo-gap privacy gates | Unit | Integration | P0 |
| F23 | Capture transfer policy (Hop1/Hop2, success tail=20 lines) | Integration | End-to-End | P0 |
| F24 | Assistant comment timing and shell comment syntax | Integration | Expect | P0 |
| F25 | DB schema, migration, retention pruning | Integration | Unit | P0 |
| F26 | Config surface (flags/env/config path defaults) | Unit | Integration | P1 |
| F27 | Migration/coexistence (`CLAI_WRAP=1` hook disable) | Expect | Docker | P1 |
| F28 | Troubleshooting and recovery behavior signals | End-to-End | Expect | P2 |
| F29 | Install-once auto-wrap + `clai pty on/off/status` controls | Integration | Expect | P0 |

## 5. Unit Test Plan

Primary code locations:

- Rust: `/Users/runger/.claude-worktrees/clai/hungry-swartz/clai-wrap/src`
- Go daemon/storage: `/Users/runger/.claude-worktrees/clai/hungry-swartz/internal/daemon`, `/Users/runger/.claude-worktrees/clai/hungry-swartz/internal/storage`

Target modules and assertions:

1. Hotkey and input routing
- Files: `clai-wrap/src/hotkey.rs`, `clai-wrap/src/input_router.rs`
- Assertions:
  - Chord detect within timeout.
  - Timeout forwards bytes unchanged.
  - No dropped bytes under rapid input.

2. Parser and protocol
- Files: `clai-wrap/src/osc133.rs`, `clai-wrap/src/jsonrpc.rs`, `internal/daemon/jsonrpc_test.go`
- Assertions:
  - Split packet reconstruction.
  - BEL and ST terminators accepted.
  - Unknown fields ignored.
  - Invalid requests map to declared codes.

3. Buffering and non-blocking rules
- Files: `clai-wrap/src/ring_buffer.rs`, `clai-wrap/src/io_threads.rs`, `clai-wrap/src/output_capture.rs`
- Assertions:
  - Overwrite-oldest ordering.
  - First-overflow warning once per picker session.
  - Producer path does not block on full buffer.

4. Privacy and capture policy
- Files: `clai-wrap/src/denylist.rs`, `clai-wrap/src/echo_gap.rs`, `clai-wrap/src/output_capture.rs`
- Assertions:
  - Denylist pause/resume transitions.
  - Echo-gap secure mode entry/exit.
  - Successful-command tail policy retains exactly last 20 lines for Hop 1 payload.

5. Config and mode policy
- Files: `clai-wrap/src/config.rs`, `clai-wrap/src/cli.rs`, `clai-wrap/src/standalone.rs`, `internal/config/config.go`, `internal/cmd/pty_cmd.go`
- Assertions:
  - Stream-aware non-TTY mode decisions.
  - Daemon default-on unless `--no-daemon`.
  - `--bash-login-injection` default false.
  - `pty.enabled` default true and `clai pty on/off` persistence.

Unit commands:

- Rust: `cargo test --manifest-path clai-wrap/Cargo.toml`
- Go focused:
  - `go test ./internal/daemon -run Test -v`
  - `go test ./internal/storage -run Test -v`

## 6. Integration Test Plan

Primary suites:

- Rust PTY behavior: `/Users/runger/.claude-worktrees/clai/hungry-swartz/clai-wrap/tests/e2e_modes.rs`, `e2e_picker.rs`, `e2e_pty.rs`, `e2e_privacy_suggestions.rs`
- Go integration: `/Users/runger/.claude-worktrees/clai/hungry-swartz/tests/integration`

Required scenarios:

1. Runtime mode and fallback
- Daemon available -> full mode active.
- Daemon timeout at startup -> standalone.
- Mid-session daemon disconnect -> one reconnect attempt then standalone.
- Protocol mismatch -> MUST standalone fallback.

2. Stream-aware non-TTY matrix
- stdin non-TTY + stdout TTY: hotkey disabled, picker behavior preserved where valid.
- stdout non-TTY + stdin TTY: picker disabled, passthrough preserved.
- all non-TTY + no force: fail with clear error.
- all non-TTY + `--force-non-tty`: pure passthrough.

2b. Auto-wrap toggle behavior
- `clai pty off` persists `pty.enabled=false`.
- New interactive shell startup does not auto-exec `clai-wrap` when off.
- `clai pty on` persists `pty.enabled=true`.
- New interactive shell startup auto-execs `clai-wrap` when on and binary is available.
- `clai pty status` reports configured state and current-session wrap state.

3. IPC and storage
- `ping`, `command.start`, `command.end`, `output.chunk`, notification handling.
- stale socket ownership checks.
- capture persistence in `command_events` and `command_output`.
- retention prune by `expires_at`.

4. Capture transfer policy
- Failed command: Hop1 payload contains captured output.
- Successful command: Hop1 payload contains only last 20 lines.
- Successful command: Hop2 is not invoked.

Integration commands:

- Go: `go test -v ./tests/integration/...`
- Rust: `cargo test --manifest-path clai-wrap/Cargo.toml --tests`

## 7. Expect Test Plan (Interactive Shell)

Primary suite:

- `/Users/runger/.claude-worktrees/clai/hungry-swartz/tests/expect`

Required scenarios by shell (bash, zsh, fish):

1. Prompt startup and wrapper integrity
- prompt appears, input echo correct, command execution intact.

2. Hotkey and picker flow
- open picker, navigate, cancel, select, insert-only, insert-and-execute.

3. Assistant comment behavior
- failed command leads to prompt-safe comment render.
- comment syntax correct per shell.

4. Shell injection details
- zsh order (`.zshenv` then restore `ZDOTDIR`, then `.zshrc`, then hooks).
- bash interactive rc sourcing order.
- fish native OSC detection for supported versions.

5. Bash login opt-in matrix
- interactive non-login + opt-in OFF/ON.
- login + opt-in OFF/ON.
- verify startup-file sourcing, OSC marker availability, and preserved user env.

6. Auto-wrap startup matrix
- `pty.enabled=true` + interactive tty + `CLAI_WRAP` unset => shell handoff to `clai-wrap`.
- `pty.enabled=false` => no handoff.
- `CLAI_PTY_DISABLE=1` => no handoff even when enabled.
- `CLAI_WRAP=1` => no recursive handoff.

Expect command:

- `make test-interactive`

## 8. Docker Test Plan

Primary setup:

- `/Users/runger/.claude-worktrees/clai/hungry-swartz/tests/docker/docker-compose.yml`
- distros: Alpine, Ubuntu, Debian, Fedora.

Required assertions:

1. Shell startup and injection behavior consistent across distros.
2. `TERM`/color/degraded behavior consistent (including `TERM=dumb`).
3. Expect smoke set passes in all distros.
4. Bash login opt-in matrix spot checks in at least Ubuntu + Alpine.

Docker command:

- `make test-docker`

## 9. End-to-End Test Plan

Primary suite and framework:

- `/Users/runger/.claude-worktrees/clai/hungry-swartz/tests/e2e/pty-wrapper-tests.yaml`
- `/Users/runger/.claude-worktrees/clai/hungry-swartz/tests/e2e/README.md`

Critical end-to-end scenarios:

1. Full command lifecycle and suggestions
- failed command -> capture -> daemon suggestion -> prompt-safe assistant comment.

2. Fullscreen interoperability
- `vim`/`less` lifecycle with picker open/close and no terminal corruption.

3. SSH behavior
- local picker in remote SSH session; inserted command lands in remote shell input.

4. Stress and resilience
- high-output stream with picker open (no deadlock), overflow indicators as expected.
- signal interruptions restore terminal state.

5. Standalone resilience
- daemon unavailable from start and mid-session disconnect preserve shell usability.

Execution:

- Use E2E harness described in `tests/e2e/README.md`.
- Keep PR lane to a smoke subset; run full set nightly and on release gate.

## 10. Coverage by Spec Section

| Spec Section (`pty_tech_v1.md`) | Coverage Category | Primary Suite |
|---|---|---|
| 4 System Architecture | Unit + Integration | `clai-wrap/src/*` tests, `tests/integration/*` |
| 4.6 IPC Protocol | Unit + Integration | `clai-wrap/src/jsonrpc.rs`, `internal/daemon/jsonrpc_test.go` |
| 4.7 Daemon/Standalone | Integration + End-to-End | `clai-wrap/tests/e2e_modes.rs`, `tests/integration/daemon_*` |
| 6 Functional Requirements | Unit + Integration + Expect | Rust module tests + `tests/expect/*` |
| 7 Privacy/Capture | Unit + Integration | `clai-wrap/src/denylist.rs`, `echo_gap.rs`, storage tests |
| 8 Shell Integration | Expect + Docker | `tests/expect/*`, `tests/docker/*` |
| 9 Assistant Comment UX | Integration + Expect + End-to-End | `clai-wrap/src/assistant_comment.rs`, expect/e2e |
| 10 Storage/Retention | Integration | `internal/storage/*_test.go` |
| 11 Test Strategy | All | This document |
| 12 CI/Automation | CI lane config | `make` targets + CI workflow |
| 13 Config | Unit + Integration | `clai-wrap/src/config.rs`, CLI tests |
| 14 Migration/Coexistence | Expect + Docker | shell hook compatibility tests |
| 15 Milestones (M1-M7) | Progressive lane gating | PR + nightly + release lanes |
| 18 Review Checklist | Cross-cutting | all category gates |

## 11. CI and Gating Plan

PR required checks:

1. Go tests: `make test`
2. Rust tests: `make test-rust`
3. Interactive smoke: `make test-interactive` (smoke subset tagging)
4. Security: `cargo audit`

Nightly required checks:

1. `make test-all`
2. Full `make test-interactive`
3. `make test-docker`
4. Full E2E PTY wrapper suite

Release gate:

1. Repeat nightly suite.
2. Add stress loop (repeat key P0 scenarios multiple times).
3. Require zero known P0 flaky tests.

## 12. Failure Artifacts and Triage

Collect on failure:

- Test logs (Go + Rust + shell transcripts)
- PTY transcript dumps
- Daemon IPC logs
- SQLite snapshots for failing storage/capture cases
- E2E screenshots and step traces

Triage priority:

1. P0 regressions: block merge.
2. P1 regressions: block release, allow PR override only with explicit waiver.
3. P2 regressions: backlog with issue and owner.

## 13. Immediate Additions Recommended

These are the highest-value gaps to add first if missing:

1. Integration test for protocol mismatch => MUST standalone fallback.
2. Integration test for successful-command Hop1 tail exactly 20 lines.
3. Integration test ensuring successful commands never trigger Hop2 egress.
4. Expect test matrix for bash login opt-in ON/OFF.
5. Integration test for stream-aware non-TTY default behavior matrix.
6. CI gate wiring to enforce `cargo audit` in the same lanes as spec policy.
7. Expect/integration coverage for auto-wrap on/off and recursion guards.
