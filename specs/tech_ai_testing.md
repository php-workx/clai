# AI-Assisted Terminal Testing

Technical specification for using Claude + Playwright to test clai terminal features through a web-based terminal interface.

**Status:** Draft (POC Validated)
**Created:** 2026-02-05
**POC Date:** 2026-02-05

---

## 1. Problem Statement

clai integrates deeply with multiple shells (bash, zsh, fish) and provides interactive features:

- History picker with fuzzy search
- Command suggestions with ghost text
- PTY wrapper for stdout/stderr capture
- Voice-to-command conversion
- Session-based history management

Manual testing of these features is:

1. **Time-consuming** - Each feature must be tested across 3+ shells
2. **Error-prone** - Easy to miss edge cases or regressions
3. **Inconsistent** - Different testers may follow different procedures
4. **Blocking** - Development velocity limited by testing capacity

## 2. Proposed Solution

Use Claude Code with Playwright to execute structured test plans against a web-based terminal emulator.

### 2.1 Architecture Overview

```
┌─────────────────────────────────────────────────────────────────┐
│  Browser (Playwright-controlled by Claude)                      │
│  ┌───────────────────────────────────────────────────────────┐  │
│  │  Web Terminal Emulator (xterm.js)                         │  │
│  │  ┌─────────────────────────────────────────────────────┐  │  │
│  │  │ $ clai history                                      │  │  │
│  │  │ [picker UI appears]                                 │  │  │
│  │  │ > recent commands...                                │  │  │
│  │  └─────────────────────────────────────────────────────┘  │  │
│  └───────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────┘
        │
        │ WebSocket (bidirectional)
        ▼
┌─────────────────────────────────────────────────────────────────┐
│  Terminal Server (Go or Node.js)                                │
│  ┌───────────────────────────────────────────────────────────┐  │
│  │  - Spawns PTY with bash/zsh/fish                          │  │
│  │  - Bridges WebSocket ↔ PTY I/O                            │  │
│  │  - Sources clai shell integrations                        │  │
│  │  - Optionally wraps with clai-wrap                        │  │
│  └───────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────┘
```

### 2.2 Key Components

| Component | Technology | Purpose |
|-----------|------------|---------|
| Terminal emulator | xterm.js | Render terminal in browser |
| Backend server | gotty, ttyd, or custom | Spawn PTY, bridge WebSocket |
| Test executor | Claude + Playwright MCP | Navigate, type, screenshot, assert |
| Test definitions | YAML files | Declarative test scenarios |

## 3. Terminal Server Options

### 3.1 Option A: gotty (Recommended for Phase 1)

**Pros:**
- Single Go binary, easy to install
- Minimal configuration
- Battle-tested

**Cons:**
- Limited shell-switching without restart
- No built-in session management

**Installation:**
```bash
go install github.com/sorenisanerd/gotty@latest
```

**Usage:**
```bash
# Start with bash + clai integration
gotty -w bash --rcfile <(echo 'source ~/.clai/shell/bash.sh')

# Start with zsh
gotty -w zsh -c 'source ~/.clai/shell/zsh.zsh; exec zsh'
```

### 3.2 Option B: ttyd

**Pros:**
- C-based, very lightweight
- WebGL rendering option

**Cons:**
- Requires compilation on some systems

### 3.3 Option C: Custom xterm.js + node-pty

**Pros:**
- Full control over features
- Can add shell switching, session reset, etc.

**Cons:**
- More development effort
- Introduces Node.js dependency

### 3.4 Recommendation

Start with **gotty** for Phase 1. If limitations become blocking, build a custom solution in Phase 2.

## 4. Test Plan Format

Tests are defined in YAML files. See `tests/e2e/example-test-plan.yaml` for the complete schema reference.

### 4.1 Core Concepts

| Concept | Description |
|---------|-------------|
| Test | A single test case with steps and expectations |
| Step | An action (type, press, wait) or assertion |
| Expect | Conditions that must be true after steps complete |
| Setup | Commands to run before test steps |

### 4.2 Step Types

| Step | Description | Example |
|------|-------------|---------|
| `type` | Type text into terminal | `type: "echo hello"` |
| `press` | Press a key or key combination | `press: "Ctrl+R"` |
| `wait` | Wait for condition or duration | `wait: "1s"` or `wait_for: "text"` |

### 4.3 Expectation Types

| Expectation | Description |
|-------------|-------------|
| `screen_contains` | Text visible on screen |
| `screen_not_contains` | Text not visible on screen |
| `screen_matches` | Regex pattern matches screen content |
| `element_visible` | Visual element present (picker, prompt) |
| `cursor_position` | Cursor at expected location |

### 4.4 Key Encoding

| Key | Encoding |
|-----|----------|
| Enter | `Enter` |
| Escape | `Escape` |
| Tab | `Tab` |
| Arrow keys | `Up`, `Down`, `Left`, `Right` |
| Ctrl combinations | `Ctrl+R`, `Ctrl+C`, `Ctrl+X` |
| Alt combinations | `Alt+H`, `Alt+S` |
| Chord sequences | `Ctrl+X Ctrl+V` (space-separated) |

## 5. Test Execution Flow

### 5.1 Manual Execution (Phase 1)

```
User                    Claude                      Browser
  │                        │                           │
  │  "Run e2e tests"       │                           │
  │───────────────────────>│                           │
  │                        │                           │
  │                        │  Read test-plan.yaml      │
  │                        │──────────────────────────>│
  │                        │                           │
  │                        │  Navigate to terminal URL │
  │                        │──────────────────────────>│
  │                        │                           │
  │                        │  For each test:           │
  │                        │    - Execute steps        │
  │                        │    - Check expectations   │
  │                        │    - Screenshot on fail   │
  │                        │──────────────────────────>│
  │                        │                           │
  │  Report: 15/17 passed  │                           │
  │<───────────────────────│                           │
```

### 5.2 Automated Execution (Phase 2)

```bash
# In Makefile
test-e2e:
    @./scripts/start-test-server.sh &
    @sleep 2
    @claude -p "Run e2e tests from tests/e2e/*.yaml against http://localhost:8080"
    @./scripts/stop-test-server.sh
```

## 6. Test Categories

### 6.1 History Picker Tests

- Picker opens with correct keybinding
- Fuzzy search filters results
- Arrow keys navigate selection
- Enter inserts selected command
- Escape cancels and restores buffer
- Scope switching (session/cwd/global)

### 6.2 Suggestion Tests

- Ghost text appears for known patterns
- Right arrow accepts full suggestion
- Alt+Right accepts next token
- Escape clears suggestion
- No suggestion for unknown patterns

### 6.3 PTY Wrapper Tests

- Output capture works correctly
- Stderr separate from stdout
- Exit codes propagated
- Signal handling (Ctrl+C)

### 6.4 Shell Integration Tests

- clai loads without errors
- Prompt hook fires correctly
- History recorded per session
- Session ID environment variable set

### 6.5 Cross-Shell Consistency

Each test should pass in:
- bash
- zsh
- fish (where applicable)

## 7. Visual Regression Testing

### 7.1 Screenshot Comparison

For UI-heavy features (picker, suggestions), capture screenshots and compare against baselines.

**Storage:**
```
tests/e2e/
  screenshots/
    baseline/
      history-picker-bash.png
      history-picker-zsh.png
    current/
      history-picker-bash.png
```

### 7.2 Diff Tolerance

Terminal rendering may vary slightly. Use perceptual diff with tolerance threshold.

## 8. Test Environment

### 8.1 Isolation Requirements

| Requirement | Implementation |
|-------------|----------------|
| Clean shell config | Use `--norc` or equivalent |
| Isolated CLAI_HOME | Temp directory per test |
| Fresh database | New state.db per suite |
| Deterministic prompt | Custom PS1/PROMPT |

### 8.2 Environment Variables

```bash
CLAI_HOME=/tmp/clai-test-xxx
CLAI_SESSION_ID=test-session-001
TERM=xterm-256color
PS1='TEST> '
```

## 9. Failure Handling

### 9.1 On Test Failure

1. Capture screenshot
2. Capture terminal buffer content
3. Log step that failed
4. Continue to next test (don't abort suite)

### 9.2 Artifacts

Store in `tests/e2e/artifacts/`:
- `{test-name}-failure.png`
- `{test-name}-buffer.txt`
- `test-run-{timestamp}.log`

## 10. Makefile Integration

```makefile
# Start terminal server for testing
test-server:
    gotty -w -p 8080 bash --rcfile scripts/test-shell-init.sh

# Run tests (requires Claude session)
test-e2e:
    @echo "Start 'make test-server' in another terminal"
    @echo "Then ask Claude: 'Run e2e tests from tests/e2e/*.yaml against http://localhost:8080'"

# Future: automated execution
test-e2e-auto:
    ./scripts/run-e2e-tests.sh
```

## 11. Phase Plan

### Phase 1: Foundation
- [ ] Set up gotty or equivalent
- [ ] Create example test plan YAML
- [ ] Test Claude + Playwright integration manually
- [ ] Define 10-15 core test cases

### Phase 2: Expansion
- [ ] Add all shell integration tests
- [ ] Add PTY wrapper tests
- [ ] Implement visual regression
- [ ] Create test runner script

### Phase 3: Automation
- [ ] Headless Claude execution
- [ ] CI integration
- [ ] Parallel test execution
- [ ] Multi-shell matrix

## 12. Limitations and Considerations

### 12.1 Known Limitations

- **Timing sensitivity**: Terminal rendering is asynchronous; tests need appropriate waits
- **Shell variations**: Keybindings may differ between shells
- **Terminal size**: Tests should specify or normalize terminal dimensions
- **Color handling**: ANSI colors may affect text matching

### 12.2 Security Considerations

- Test server should only bind to localhost
- No sensitive data in test commands
- Isolated environment prevents affecting real user data

## 13. Proof of Concept Results (2026-02-05)

A POC was conducted to validate the approach. Key findings:

### 13.1 What Works

| Capability | Status | Notes |
|------------|--------|-------|
| gotty serves web terminal | ✅ | Single command install via `go install` |
| Playwright navigation | ✅ | `browser_navigate` to localhost:8080 |
| Typing commands | ✅ | Use `pressSequentially` for character-by-character |
| Command execution | ✅ | Enter key submits commands |
| Output verification | ✅ | Accessibility snapshots capture terminal text |
| Screenshots | ✅ | PNG screenshots saved to `tests/e2e/screenshots/` |
| Ctrl key combinations | ✅ | Ctrl+C, Ctrl+U, Ctrl+R all work |
| clai integration loads | ✅ | Status line visible with session ID |

### 13.2 Challenges Discovered

| Issue | Impact | Mitigation |
|-------|--------|------------|
| Alt key handling | Medium | May need ESC+key sequence instead of Alt+key |
| Session state | High | gotty session may not register with daemon properly |
| Playwright `fill()` | Low | Use `pressSequentially` or clear line first with Ctrl+U |
| bash history expansion | Low | Avoid `!` in test strings or use single quotes |

### 13.3 Bugs Discovered

The POC discovered **one real bug** and demonstrated the importance of understanding expected shell behavior.

#### Bug #1: Session Not Registered with Daemon (ai-terminal-dle, P1) - FIXED

**Symptom:** Running `clai history` fails with "session not found" even though the session ID is displayed in the clai status header.

**Reproduction:**
1. Start a new bash shell
2. clai integration loads, shows session ID (e.g., `[3dce2552]`)
3. Run `clai history`
4. Error: `session not found (3dce2552-ffa6-4c78-9d61-47fb1206cfe6)`

**Root cause:** The bash and fish shell init scripts generated a session ID but never called `clai-shim session-start` to register it with the daemon.

**Fix:** Added `clai-shim session-start` call to `clai.bash` and `clai.fish` init scripts (zsh already had it).

#### False Positive: Buffer Concatenation After History Recall

**Initial observation:** When recalling a command with Up arrow and typing new text, the text concatenates (e.g., `echo secondecho third`).

**Conclusion:** This is **expected shell behavior**, not a bug. Up arrow recalls a command and positions the cursor at the end. Typing appends at cursor position. This is standard readline/bash behavior.

### 13.4 POC Screenshots

Screenshots captured during POC are stored in:
- `tests/e2e/screenshots/current/gotty-terminal-loaded.png` - Initial terminal state
- `tests/e2e/screenshots/current/bug-investigation-start.png` - Clean session start
- `tests/e2e/screenshots/current/bug1-session-not-found.png` - Session registration bug
- `tests/e2e/screenshots/current/bug2-buffer-concatenation.png` - Buffer concatenation bug

### 13.5 Recommended Next Steps

1. **Fix session registration** - Ensure gotty-spawned sessions register with daemon
2. **Test Alt key alternatives** - Try ESC+h sequence for history picker
3. **Create test initialization script** - Proper shell setup for isolated testing
4. **Document Playwright patterns** - Best practices for terminal interaction

## 14. References

- [xterm.js documentation](https://xtermjs.org/)
- [gotty repository](https://github.com/sorenisanerd/gotty)
- [Playwright MCP tools](../docs/playwright-mcp.md)
- [Shell integration testing](./shell-integration-testing.md)
- [Spaces test plan](./tech_spaces_testing.md)
