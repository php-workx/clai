# Spaces — Test Plan (Unit, Integration, Concurrency, Shell, Manual)

Separate testing document for the Spaces feature.

**Scope:** Phase 1 quality plan. Covers automated unit/integration/concurrency and shell-level automation (zsh, bash, fish), plus a manual test checklist.

---

## 1. Testing Goals

1. **Verify Spaces correctness:**
   - create/rename/list/use/detach
   - command assignment behavior (stoplist + warm-up + fingerprint)
   - ranking and picker scope behavior
   - indicator behavior (title + env var)

2. **Verify isolation and concurrency:**
   - multiple sessions in parallel
   - concurrent RPC calls
   - no cross-session leakage

3. **Verify shell compatibility:**
   - zsh, bash, fish integration harness
   - consistent behavior across shells

4. **Verify stability:**
   - daemon restarts
   - schema migrations (`space_id` addition)
   - retention/cleanup does not break Spaces

---

## 2. Test Pyramid & Tooling

### 2.1 Unit Tests (fast)

Go unit tests for:

- stoplist matching
- command tokenization (best-effort)
- fingerprint derivation and update cadence
- auto-assignment decision logic
- ranking ordering when attached vs detached
- retention selection logic

### 2.2 Integration Tests (real components)

- Start the daemon in-process or as a subprocess
- Use a real SQLite DB in a temp directory
- Use gRPC client stubs against a real socket
- Simulate sessions and commands via RPC

### 2.3 Concurrency / Race Tests

- Run parallel clients against daemon
- Use Go race detector (`-race`)
- Stress with randomized event ordering

### 2.4 Shell Automation Tests (black-box)

Use PTY-based harness to run real shells:

- zsh, bash, fish
- Source clai init scripts
- Drive keystrokes / capture prompt/title updates

### 2.5 Manual Testing

- Checklist for UX correctness
- Verify "it feels fish-like"

### 2.6 Containerization & Repeatability

Shell automation tests are prone to host variability (shell versions, locale, terminal settings).

**Policy:**

- Prefer running shell automation tests inside Docker containers on Linux CI for repeatability.
- Run a small set of smoke shell tests natively on macOS (Phase 1) to validate mac-specific behaviors.
- Treat container-based results as the default signal for correctness.

**Rationale:**

- Consistent shell versions and dependencies
- Isolated HOME and config
- Easier to reproduce failures

---

## 3. Test Environment & Harness

### 3.1 Common Setup

Each test suite uses isolated temp dirs:

- `CLAI_HOME` → temp dir for `~/.clai`
- DB file: `CLAI_HOME/state.db`
- socket: `CLAI_HOME/run/clai.sock`

Additional requirements:

- Provide deterministic clock in unit tests (inject time provider)
- Ensure tests never touch real user dotfiles

### 3.2 Daemon Lifecycle for Tests

Provide a test helper:

```go
StartTestDaemon(ctx, homeDir) -> (addr, cleanup)
```

Must support:

- clean shutdown
- forced crash/restart

### 3.3 gRPC Client Helper

```go
NewTestClient(addr) // with timeouts
```

Helper functions:

- `SessionStart(name, shell, cwd)`
- `AttachSpace(session, space)`
- `CommandRun(session, cmd, exit, stderrTail)`
- `Suggest(session, buffer, includeAI=false)`

---

## 4. Unit Test Plan

### 4.1 Stoplist Matching

**Cases:**

- matches on first token only
- whitespace and newline normalization does not break matching
- commands that start with stoplisted token but have args
- non-stoplisted commands

### 4.2 Warm-up Rules

**Cases:**

- new space with < `WARMUP_MIN_ASSIGNED`: auto-assign non-stoplist
- after reaching threshold: fingerprint required
- switching spaces mid-session resets warm-up only for new space

### 4.3 Fingerprint Derivation

**Cases:**

- frequency + recency weighting selects expected roots
- two-token roots for `aws eks`, `gcloud sql`, etc.
- stoplisted roots are excluded
- minimum score threshold prevents noise roots
- recompute cadence triggers at Δ commands and/or time interval

### 4.4 Auto-assignment Decision Logic

**Matrix tests:**

- attached/unattached
- stoplisted/non-stoplisted
- warm-up vs post-warm-up
- fingerprint match vs mismatch

**Expected:**

- only eligible commands are assigned to space
- ineligible commands remain unassigned

### 4.5 Ranking Ordering

**Cases:**

- attached: space > session > cwd > global
- detached: session/cwd/global only
- when space history is empty, fallback behaves correctly

### 4.6 Manual Overrides

**Cases:**

- add last command assigns regardless of fingerprint
- remove last command unassigns
- adding to a different space changes `space_id` accordingly

### 4.7 Retention Selection Logic

**Cases:**

- space-tagged commands retained longer
- untagged commands pruned by age/cap
- ensure "keep last N unique per space" constraint holds

---

## 5. Integration Test Plan (Daemon + SQLite + gRPC)

### 5.1 CRUD Operations

**Test flows:**

1. Create space via RPC/CLI
2. Rename space
3. List spaces
4. Attach session to space
5. Detach

**Assertions:**

- DB rows correct
- `session_spaces` mapping correct

### 5.2 Command Assignment Flow

**Flow:**

1. Start session
2. Create and attach space `k8s-prod`
3. Run 30 commands mixing:
   - stoplisted (`ls`, `cd`)
   - relevant (`kubectl get pods`)
   - unrelated (`git status`)

**Assertions:**

- stoplisted commands not assigned
- during warm-up, unrelated non-stoplist may be assigned
- after warm-up + fingerprint, only matching commands assigned

### 5.3 Switch Spaces

**Flow:**

1. Attach to `k8s-prod`, run commands
2. Switch to `deploy-foo`, run commands

**Assertions:**

- assignments go to correct space
- no retroactive reassignment

### 5.4 Suggest / Picker Scope Behavior

**Flow:**

1. Attach to `k8s-prod`
2. Run `kubectl logs -n payments ...`
3. Request suggestions with buffer prefix `kub`

**Assertions:**

- suggestions include space commands prominently
- detach and request again: suggestions shift to session/global ranking

### 5.5 Indicator State (Non-shell)

Since terminal title isn't easily verifiable in pure RPC tests:

- verify daemon returns active space name in a status field (if present)
- verify env var value management in shell automation tests

### 5.6 Daemon Restart

**Flow:**

1. Create space and run commands
2. Kill daemon
3. Restart daemon
4. Reattach and request suggestions

**Assertions:**

- spaces persist
- history persists
- no corruption

### 5.7 Migration Test

- Start with DB schema without spaces
- Apply migration to add spaces, `session_spaces`, `commands.space_id`
- Verify no data loss

---

## 6. Concurrency & Isolation Test Plan

### 6.1 Parallel Sessions Isolation

- Start daemon
- Spawn N clients (e.g., 20)
- Each client starts a session and attaches to a unique space
- Each client runs commands that could overlap in content

**Assertions:**

- commands tagged only to that session's space
- suggestions for each session do not include commands from other spaces

### 6.2 Concurrent Attach/Detach

- Multiple goroutines attach/detach rapidly on the same session
- Run commands in between

**Assertions:**

- no panics
- consistent space assignment (race-safe)
- DB remains consistent

### 6.3 Out-of-order Events

- Send `CommandEnd` without `CommandStart`
- Duplicate `CommandEnd`
- Delayed `CommandStart`

**Assertions:**

- daemon tolerates events
- commands recorded without corruption

### 6.4 Stress: Suggest While Writing

- One goroutine simulates typing (frequent Suggest calls)
- Another goroutine submits commands

**Assertions:**

- Suggest remains low latency
- no deadlocks

### 6.5 Race Detector

Run full integration+concurrency suite with:

```bash
go test -race ./...
```

---

## 7. Shell Automation Tests (zsh, bash, fish)

**Goal:** Validate end-to-end UX behavior with real shells, without a human.

### 7.1 Harness Requirements

Use a PTY driver to:

- spawn a shell under a PTY
- source clai init scripts
- send keystrokes (including Alt combinations)
- read terminal output and prompt changes

**Hard requirements:**

- deterministic timeouts
- robust prompt detection (sentinel prompt or unique marker)
- works on macOS (Phase 1) and in Linux containers (CI)

### 7.2 PTY Test Driver Choice (Recommended)

**Preferred approach:** Go-native expect-like PTY harness.

**Options:**

1. **goexpect** (expect-like interaction)
   - Pros: familiar expect-style API
   - Cons: some forks are stale; verify maintenance

2. **creack/pty + custom expect loop** (recommended if you want full control)
   - Use `github.com/creack/pty` to spawn PTY
   - Implement:
     - read loop with ANSI-stripping
     - pattern matching for prompts
     - keystroke writer with proper escape sequences

**Rationale for option 2:**

- minimal dependencies
- full control over edge cases
- easier to run in CI

### 7.3 Running Shell Tests in Docker (Recommended)

Use Docker containers to run the harness against known-good shell versions.

**Recommended approach:**

- Build a small test image containing:
  - bash, zsh, fish
  - common utilities used in tests (coreutils, grep, sed)
  - optional: pwsh for non-Windows PowerShell smoke tests
- Run `go test` inside the container

**Container requirements:**

- PTY support (`/dev/pts` available)
- stable locale (`LANG=C.UTF-8` or equivalent)
- `TERM=xterm-256color`

**Why Docker here:**

- eliminates host RC/config differences
- makes failures reproducible

### 7.4 Shell Startup Modes (Isolated)

| Shell | Command |
|-------|---------|
| zsh | `zsh -f` (no user rc) |
| bash | `bash --noprofile --norc` |
| fish | `fish --no-config` |

**Set environment:**

- `HOME` to temp dir
- `CLAI_HOME` to temp dir
- ensure `TERM` is stable (e.g., `xterm-256color`)

**Additionally:** Set a deterministic prompt in the test harness (recommended):

- export a unique prompt marker such as `CLAI_TEST_PROMPT='__clai__> '` and configure the shell prompt accordingly in the isolated session.

### 7.5 Keystroke Encoding

Standardize keystrokes:

| Key | Encoding |
|-----|----------|
| Alt-x sequences | `ESC` + `x` |
| Alt + ] | `ESC` + `]` |
| Alt + [ | `ESC` + `[` |

Right arrow may be multi-byte escape depending on terminal; harness should send `ESC [ C`.

### 7.6 Assertions for Shell Tests

Shell tests should assert:

- space attach/detach works via hotkeys
- indicator changes (title or env var) in a detectable way
- stoplist filtering (indirectly via suggestion results)
- picker behaviors where feasible

### 7.7 zsh Cases

- Alt-s creates/attaches space
- indicator visible (prefer env var + title)
- run commands; verify stoplist filtering
- detach; verify indicator removed
- Alt-a adds last command to space

### 7.8 bash Cases

- similar to zsh, with readline behaviors
- ensure no interference with Tab completion

### 7.9 fish Cases

Even if fish is not a Phase 1 supported shell, test harness must confirm:

- clai does not break fish startup
- `clai space` CLI still works

Optional if minimal integration exists:

- verify environment indicator set/unset

### 7.10 Indicator Verification Strategy

Because title rendering differs by emulator, verify indicator using two methods:

1. **Environment hint (authoritative)**
   - assert `CLAI_SPACE_NAME` set/unset by printing it:
     - zsh/bash: `echo $CLAI_SPACE_NAME`
     - fish: `echo $CLAI_SPACE_NAME`
     - pwsh: `echo $env:CLAI_SPACE_NAME`

2. **Terminal title (best-effort)**
   - detect OSC title sequences in PTY output when possible
   - if not detectable, skip title assertion (do not fail tests)

### 7.11 PowerShell Automation Plan (Phase 1)

PowerShell UX relies on PSReadLine and behaves differently on Windows.

**Policy:**

- Add unit and integration tests for PowerShell-specific logic (status detection, env var behavior) that run on all OSes.
- Add shell-level automation for PowerShell on Windows CI runners as soon as feasible.

**Windows CI plan:**

- use a Windows runner (not a container) to avoid ConPTY limitations in Docker
- spawn pwsh under a PTY/ConPTY-capable harness
- validate:
  - attach/detach
  - env var indicator
  - basic hotkeys where PSReadLine permits

---

## 8. Manual Test Checklist (Core Functionality)

### 8.1 Setup

- [ ] Fresh install
- [ ] Verify onboarding message
- [ ] Verify Alt-s and Alt-h work

### 8.2 Spaces Creation & Switching

- [ ] Create `k8s-prod` and attach
- [ ] Verify indicator visible
- [ ] Run 10–20 kubectl/helm commands
- [ ] Switch to `deploy-foo`
- [ ] Verify suggestions now prefer deploy commands

### 8.3 Clutter Prevention

- [ ] While attached to `k8s-prod`, run a few unrelated commands
- [ ] Verify stoplist prevents `ls`, `cd` from being assigned
- [ ] After warm-up, verify unrelated commands stop being assigned

### 8.4 Manual Overrides

- [ ] Add last command to space via Alt-a
- [ ] Remove/unassign last command (if implemented)

### 8.5 Picker Behavior

- [ ] Alt-h defaults to Space scope when attached
- [ ] search works and inserts command

### 8.6 Detach

- [ ] Detach from space
- [ ] Confirm suggestions return to normal
- [ ] Confirm commands are still present in session/global history

### 8.7 Persistence

- [ ] Restart terminal
- [ ] Create new session
- [ ] Attach to existing space
- [ ] Verify suggestions are still good

### 8.8 Retention / Cleanup (Manual)

- [ ] Configure short retention in test build
- [ ] Verify old untagged commands pruned
- [ ] Verify space-tagged commands retained

---

## 9. Test Data & Fixtures

### 9.1 Golden Fixtures (Deterministic Sequences)

Create deterministic fixture suites that specify:

- input command stream (with attach/detach points)
- expected space assignments per command
- expected fingerprint roots after warm-up
- expected top suggestions for given prefixes

Represent fixtures as YAML/JSON files in the repo (recommended):

```yaml
name: k8s_space_basic
warmup_min_assigned: 15
space: k8s-prod
steps:
  - action: attach_space
    space: k8s-prod
  - action: run
    cmd: "ls"
    exit: 0
    expect:
      assigned_to_space: false
  - action: run
    cmd: "kubectl get pods -n payments"
    exit: 0
    expect:
      assigned_to_space: true
  - action: run
    cmd: "git status"
    exit: 0
    expect:
      assigned_to_space: true   # during warm-up
  - action: run
    cmd: "helm list -n payments"
    exit: 0
    expect:
      assigned_to_space: true
assertions:
  fingerprint_roots_any_of:
    - ["kubectl", "helm"]
  suggest:
    - buffer: "kub"
      expect_contains_prefix: "kubectl"
      expect_top_source: "space"
```

**Golden suites to include (minimum):**

1. **k8s_space_basic**
   - mix kubectl/helm + noise
   - verify stoplist filtering
   - verify warm-up then fingerprint gates

2. **deploy_space_basic**
   - terraform/make + noise
   - ensure terraform becomes root

3. **db_space_basic**
   - psql/pg_dump + noise
   - ensure psql becomes root

4. **switching_spaces**
   - attach k8s, run commands
   - switch to deploy, run commands
   - verify no retroactive reassignment

5. **manual_override_add_last**
   - command filtered out
   - add-last assigns regardless

6. **retention_tiers**
   - old global untagged pruned
   - space-tagged retained
   - keep-last-N-unique enforced

### 9.2 Noise & Stoplist Fixtures

Maintain a dedicated list of stoplist commands and noise patterns for regression:

- stoplist roots: `cd`, `pwd`, `ls`, `cat`, `less`, `clear`, …
- short commands: `k`, `g`, `..` (if you choose to treat as aliases)
- generic helpers: `echo`, `env`

### 9.3 Suggestion Fixture Assertions

For suggestion tests, assert:

- top suggestion text prefix
- that results are drawn from correct scope (space > session > global)
- ordering stability across repeated runs

### 9.4 Noise & Edge Fixtures

Provide fixtures for:

- heavy stoplist noise (navigation-heavy sessions)
- mixed workflows (k8s + deploy + db interleaved)
- accidental attachment scenarios
- very short sessions (1–2 commands)

---

## 10. CI Matrix, Benchmarks & Failure Artifacts

### 10.1 CI Matrix (Recommended)

**Linux (Docker-based):**

- unit + integration + concurrency/race
- shell automation (zsh/bash/fish)

**macOS (native smoke):**

- unit + integration
- minimal shell smoke (zsh) to confirm mac-specific prompt/title behaviors

**Windows (Phase 1 target):**

- unit + integration
- add PowerShell shell automation when harness is available

### 10.2 Performance Benchmarks (Regression Guard)

Add Go benchmarks to prevent suggestion queries from degrading over time.

**Benchmarks to include:**

- `BenchmarkSuggest_SpaceScope_200kRows`
- `BenchmarkSuggest_GlobalScope_200kRows`
- `BenchmarkFingerprint_Recompute_200Commands`

**Benchmark dataset generation:**

- create a temp DB with 100k–200k command rows
- include a few spaces with realistic command distributions

**Gates (Phase 1 suggested):**

- space-scoped suggest median < 20ms on CI class machines
- fingerprint recompute < 10ms for N=200 inputs

### 10.3 Failure Artifacts (Make Failures Debuggable)

On any failing integration/concurrency/shell test, capture:

- daemon logs
- the SQLite DB file (or a redacted snapshot)
- PTY transcript of the session (input/output stream)
- the fixture file that drove the failing case

Store artifacts in CI for download.

---

## 11. Pass/Fail Gates

- Unit suite must pass on all supported OSes
- Integration suite must pass on macOS and Linux
- Concurrency suite must pass under `-race`
- Shell automation must pass for zsh/bash in containers, and must not break fish
- Manual checklist must be executed before any tagged release
