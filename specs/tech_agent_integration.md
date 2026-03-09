# clai Agent Integration — Technical Specification

This document specifies how clai integrates with AI coding agents (Claude Code, Cursor, Copilot CLI, etc.) to create a feedback loop between human shell usage and agent command execution.

---

## 0) Motivation

### 0.1 The Problem

AI coding agents run hundreds of Bash commands per session. Analysis of 6,871 Bash tool calls across 98 Claude Code sessions revealed:

| Metric | Value |
|--------|-------|
| Error rate | 8.8% (608 errors) |
| Wrong tool usage | 7.3% (502 calls used `cat`/`grep`/`find` instead of dedicated tools) |
| Estimated waste | ~443K tokens, ~4,100 minutes |
| Retry rate | 0.7% (49 same-command retries after failure) |

Agents have no project-specific command knowledge. They don't know that in _this_ repo, `make dev` is the gate command, or that `bd sync --from-main` runs at session end, or that after `go test` fails people usually run `go test -v ./specific/package`. Humans know this from experience — and clai already captures that experience.

### 0.2 The Opportunity

clai already tracks:
- **Per-project command patterns** (repo_key scoping, cwd-aware history)
- **Command sequences** (Markov bigrams via transition scoring)
- **Slot values** (branch names, paths, test targets — via exponential-decay histograms)
- **Git context** (branch, dirty status, repo_key)
- **Error patterns** (exit codes, duration outliers)

If we feed agent commands into clai's history AND surface clai's suggestions to agents, we get a **bidirectional intelligence loop**:

```
Human runs commands in shell
        │
        ▼
  clai captures history ──► suggestion model learns
        │                         │
        │                         ▼
  Agent queries clai ◄──── project-specific suggestions
        │
        ▼
  Agent runs smarter commands
        │
        ▼
  clai captures agent commands ──► model improves further
```

### 0.3 Goals
- Zero-latency command ingestion from agents (fire-and-forget)
- Sub-100ms suggestion queries (must not slow down agent tool calls)
- Graceful degradation when daemon is unavailable
- Agent-agnostic: hooks are shell scripts, any agent can use them
- No changes to clai's core suggestion engine — use existing RPCs

### 0.4 Non-goals
- Forcing agents to follow suggestions (advisory only)
- Replacing agent's own command knowledge (supplement, not override)
- Real-time streaming of agent sessions to clai UI
- Multi-machine sync of agent history

---

## 1) Architecture

### 1.1 Integration Points

Claude Code provides three hook events for Bash tool calls:

| Hook Event | Timing | Can Modify? | Use Case |
|-----------|--------|-------------|----------|
| `PreToolUse:Bash` | Before execution | Yes (deny/modify/add context) | Inject suggestions, block wrong tools |
| `PostToolUse:Bash` | After execution | No (observe only) | Feed commands into clai history |
| `SessionStart` | Session begins | No | Initialize clai session |

### 1.2 Component Diagram

```
┌─────────────────────────────────────────────────────────────────┐
│ Claude Code                                                      │
│                                                                  │
│  ┌──────────────┐    ┌──────────────┐    ┌──────────────────┐   │
│  │ SessionStart │    │ PreToolUse   │    │ PostToolUse      │   │
│  │ Hook         │    │ :Bash Hook   │    │ :Bash Hook       │   │
│  └──────┬───────┘    └──────┬───────┘    └──────┬───────────┘   │
│         │                   │                    │               │
└─────────┼───────────────────┼────────────────────┼───────────────┘
          │                   │                    │
          ▼                   ▼                    ▼
┌─────────────────┐ ┌──────────────────┐ ┌──────────────────────┐
│ clai-hook       │ │ clai suggest     │ │ clai-hook ingest     │
│ session-start   │ │ --format=json    │ │ (env vars)           │
│                 │ │ --limit=5        │ │                      │
└────────┬────────┘ └────────┬─────────┘ └───────────┬──────────┘
         │                   │                        │
         ▼                   ▼                        ▼
┌────────────────────────────────────────────────────────────────┐
│ clai daemon (gRPC over Unix socket)                            │
│                                                                │
│  SessionStart │ Suggest / NextStep │ CommandStarted/Ended      │
│               │                    │                           │
│  ┌────────────┴────────────────────┴───────────────┐          │
│  │ SQLite: command_event, slot_value, session, ...  │          │
│  └──────────────────────────────────────────────────┘          │
└────────────────────────────────────────────────────────────────┘
```

---

## 2) Layer 1: Feed (PostToolUse → clai history)

### 2.1 Purpose

After every Bash tool call, record the command in clai's history. This:
- Builds agent-specific patterns in clai's suggestion model
- Enables sequence-aware suggestions (what comes after `make lint`?)
- Populates slot values (which branches, paths, test targets does the agent use?)

### 2.2 Hook Script: `hooks/bash-feed.sh`

**Trigger:** `PostToolUse:Bash`

**Input (stdin):**
```json
{
  "session_id": "claude-abc123",
  "tool_name": "Bash",
  "tool_input": {
    "command": "go test ./internal/suggest/...",
    "timeout": 120000
  },
  "tool_result": {
    "stdout": "ok  ./internal/suggest 1.234s",
    "stderr": "",
    "exit_code": 0,
    "duration_ms": 1500
  }
}
```

> Note: `PostToolUse` hooks receive `tool_result` with execution results.

**Behavior:**

1. Extract command, exit_code, duration_ms, cwd from input
2. Generate or reuse `CLAI_SESSION_ID` (stored in `~/.claude/clai-session-id` per Claude session)
3. Call `clai-hook ingest` with proper env vars:

```bash
CLAI_CMD="$COMMAND" \
CLAI_CWD="$CWD" \
CLAI_EXIT="$EXIT_CODE" \
CLAI_TS=$(date +%s000) \
CLAI_SHELL="claude-code" \
CLAI_SESSION_ID="$SESSION_ID" \
CLAI_DURATION_MS="$DURATION_MS" \
clai-hook ingest &
```

4. Fire-and-forget: background the ingest call, never block the agent
5. Exit 0 immediately (PostToolUse hooks are observe-only)

### 2.3 Session Identity

Agent sessions need a stable `CLAI_SESSION_ID`:

- Generated once per Claude Code session (UUID v4)
- Stored at `$XDG_RUNTIME_DIR/clai/claude-session` or `~/.claude/clai-session-id`
- Created by the `SessionStart` hook on session init
- Used by both Feed and Suggest hooks

### 2.4 Shell Type

Commands from agents are tagged with `shell=claude-code` to distinguish from human shell history. This enables:
- Filtering agent commands from human suggestions if desired
- Analyzing agent vs human command patterns separately
- The suggestion ranker can weight agent and human history differently

### 2.5 Graceful Degradation

- If `clai-hook` is not in PATH → skip silently
- If daemon is not running → `clai-hook ingest` drops the event silently (existing behavior)
- If any env var is missing → skip (don't crash the agent)

---

## 3) Layer 2: Suggest (PreToolUse → additionalContext)

### 3.1 Purpose

Before each Bash tool call, query clai for project-specific command suggestions and inject them as `additionalContext`. The agent sees these as hints:

```
"clai project context: common next commands in this repo after 'go test' failure:
  1. go test -v ./specific/package (score: 0.85, source: cwd)
  2. golangci-lint run --fix (score: 0.62, source: global)
  3. make lint (score: 0.45, source: session)"
```

### 3.2 Hook Script: `hooks/bash-suggest.sh`

**Trigger:** `PreToolUse:Bash`

**Input (stdin):**
```json
{
  "session_id": "claude-abc123",
  "tool_name": "Bash",
  "tool_input": {
    "command": "go test -v ./internal/suggest/..."
  }
}
```

**Behavior:**

1. Extract the command being proposed
2. Read the last command + exit code from a state file (written by PostToolUse hook)
3. Query clai for suggestions based on context:
   - `clai suggest --format=json --limit=5 "$PREFIX"` for prefix-based suggestions
   - In future: use `NextStep` RPC with last command + exit code
4. If suggestions are relevant, inject as `additionalContext`:

```json
{
  "hookSpecificOutput": {
    "hookEventName": "PreToolUse",
    "additionalContext": "clai suggests (based on project history): 1. make dev (score: 0.92) 2. go test -v ./... (score: 0.78)"
  }
}
```

5. If no suggestions or daemon unavailable → exit 0 silently (no JSON)

### 3.3 Timing Budget

The `PreToolUse` hook runs before every Bash command. It must be fast:

| Operation | Budget |
|-----------|--------|
| Hook startup (bash) | ~5ms |
| Read state file | ~1ms |
| `clai suggest` CLI call | ~50-100ms |
| jq formatting | ~5ms |
| **Total** | **<150ms** |

The `clai suggest` CLI uses a 500ms daemon timeout internally, but the hook timeout is set to 5 seconds in settings.json. If clai is slow, the hook falls through and the agent proceeds normally.

### 3.4 State File for Sequence Context

To enable sequence-aware suggestions (NextStep), the PostToolUse hook writes a state file:

```
~/.claude/clai-last-command.json
```

```json
{
  "command": "go test ./...",
  "exit_code": 1,
  "cwd": "/Users/runger/.claude-worktrees/clai/happy-hypatia",
  "ts": 1707264000000
}
```

The PreToolUse hook reads this to provide NextStep context. This is optional — if the file doesn't exist, suggestions fall back to prefix-only matching.

### 3.5 When NOT to Suggest

Skip suggestion injection when:
- The agent's command is very specific (long command with many flags) — it already knows what it wants
- The command is a simple passthrough (`git status`, `ls`) — no value in suggesting
- The daemon is not running — degrade silently
- The previous hook execution was < 1 second ago — avoid flooding with context

Heuristic: only inject suggestions when `command.length < 30` or when the last command failed.

---

## 4) Layer 3: Diagnose (Error Recovery)

### 4.1 Purpose

When a Bash command fails, query clai's Diagnose RPC for an explanation and fix suggestions. Write the diagnosis to a file that the next PreToolUse hook reads and injects.

### 4.2 Flow

```
PostToolUse (exit_code != 0)
  │
  ▼
clai diagnose → writes ~/.claude/clai-diagnosis.json
  │
  ▼
PreToolUse (next command)
  │
  ▼
reads diagnosis → injects as additionalContext
  │
  ▼
"Previous command 'go test ./...' failed (exit 1).
 clai diagnosis: test compilation error in suggest_test.go
 Suggested fixes:
   1. go test -v ./internal/suggest/... (run specific failing package)
   2. go vet ./... (check for errors)"
```

### 4.3 Constraints

- `Diagnose` RPC has a 5-second timeout — too slow for synchronous injection
- The PostToolUse hook writes diagnosis asynchronously (background process)
- The PreToolUse hook reads the file if it exists and was written recently (< 30s)
- Diagnosis is consumed once (deleted after injection)

---

## 5) Layer 0: Session Lifecycle

### 5.1 SessionStart Hook

On Claude Code session start:

1. Generate a new `CLAI_SESSION_ID` (UUID v4)
2. Store it in `~/.claude/clai-session-id`
3. Call `clai-hook session-start` to register with the daemon:

```bash
CLAI_SESSION_ID="$SESSION_ID" \
CLAI_CWD="$CWD" \
CLAI_SHELL="claude-code" \
clai-hook session-start
```

4. Optionally query clai for project context (frequent commands, common patterns) and write to a priming file that the first PreToolUse hook reads.

### 5.2 Session End

No explicit `SessionEnd` hook exists in Claude Code. Options:
- Rely on daemon's session timeout (sessions auto-close after inactivity)
- Use `Stop` hook if available to call `clai-hook session-end`

---

## 6) Configuration

### 6.1 settings.json Additions

```json
{
  "hooks": {
    "SessionStart": [
      {
        "matcher": "",
        "hooks": [
          {
            "type": "command",
            "command": "~/.claude/hooks/bash-session-init.sh",
            "timeout": 5
          }
        ]
      }
    ],
    "PreToolUse": [
      {
        "matcher": "Bash",
        "hooks": [
          {
            "type": "command",
            "command": "~/.claude/hooks/bash-optimizer.sh",
            "timeout": 5
          },
          {
            "type": "command",
            "command": "~/.claude/hooks/bash-suggest.sh",
            "timeout": 5
          }
        ]
      }
    ],
    "PostToolUse": [
      {
        "matcher": "Bash",
        "hooks": [
          {
            "type": "command",
            "command": "~/.claude/hooks/bash-feed.sh",
            "timeout": 5
          }
        ]
      }
    ]
  }
}
```

### 6.2 Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `CLAI_AGENT_FEED` | `1` | Enable/disable command feeding to clai |
| `CLAI_AGENT_SUGGEST` | `1` | Enable/disable suggestion injection |
| `CLAI_AGENT_DIAGNOSE` | `0` | Enable/disable error diagnosis (experimental) |

---

## 7) Data Flow Examples

### 7.1 Normal Command Execution

```
Agent wants to run: go test ./...

1. PreToolUse fires → bash-optimizer.sh (passes through)
                    → bash-suggest.sh (queries clai)
                    → clai suggest --format=json --limit=3 "go test"
                    → returns: [{"text":"go test -race ./...","source":"cwd","score":0.8}]
                    → injects additionalContext: "clai: in this project, common variant: go test -race ./..."

2. Agent runs: go test -race ./... (influenced by suggestion)

3. PostToolUse fires → bash-feed.sh
                     → clai-hook ingest (command="go test -race ./...", exit=0, duration=3400ms)
                     → writes last-command state file
```

### 7.2 Error Recovery

```
Agent runs: git commit -m "fix: resolve bug"  →  exit code 1 (pre-commit hook failed)

1. PostToolUse fires → bash-feed.sh
                     → clai-hook ingest (command="git commit ...", exit=1, duration=7200ms)
                     → writes last-command state file (exit_code=1)
                     → (async) clai diagnose → writes diagnosis file

2. Agent decides next command...

3. PreToolUse fires → bash-suggest.sh
                    → reads last-command: exit_code=1
                    → reads diagnosis: "pre-commit hook failed, make dev must pass first"
                    → injects: "Previous command failed. clai diagnosis: pre-commit failed.
                                Suggested fix: make dev"

4. Agent runs: make dev (influenced by diagnosis)
```

---

## 8) Implementation Phases

### Phase 1: Feed (PostToolUse → clai history) — LOW RISK

**Files:**
- `hooks/bash-feed.sh` (new) — PostToolUse hook
- `hooks/bash-session-init.sh` (new) — SessionStart hook

**Effort:** ~2 hours
**Risk:** None — fire-and-forget, observe-only, never blocks agent
**Value:** Immediate — clai starts learning from agent commands

### Phase 2: Suggest (PreToolUse → additionalContext) — MEDIUM RISK

**Files:**
- `hooks/bash-suggest.sh` (new) — PreToolUse suggestion injector
- Extend `bash-optimizer.sh` or chain as separate hook

**Effort:** ~4 hours
**Risk:** Low — worst case is adding unhelpful context. 5s timeout protects against hangs.
**Value:** High — project-specific command knowledge for every session

**Depends on:** Phase 1 (need history data to suggest from)

### Phase 3: Diagnose (Error Recovery) — EXPERIMENTAL

**Files:**
- Extend `hooks/bash-feed.sh` to call diagnose on errors
- Extend `hooks/bash-suggest.sh` to read diagnosis file

**Effort:** ~3 hours
**Risk:** Medium — async diagnosis may be stale; 5s Diagnose timeout adds background load
**Value:** Medium-high — could cut the 8.8% error rate

**Depends on:** Phase 2

### Phase 4: Plugin Packaging — FUTURE

- Convert hooks to a proper Claude Code plugin (hooks.json + manifest)
- Publish to marketplace
- Support Cursor, Copilot CLI, other agents

---

## 9) Metrics & Observability

### 9.1 Audit Log

All hooks log decisions to `~/.claude/logs/`:

| Log File | Contents |
|----------|----------|
| `bash-optimizer.jsonl` | Wrong-tool blocks, mkdir fixes |
| `bash-feed.jsonl` | Ingested commands (command, exit_code, duration) |
| `bash-suggest.jsonl` | Suggestions offered, whether they influenced the command |

### 9.2 Success Metrics

| Metric | Baseline (pre-hook) | Target |
|--------|---------------------|--------|
| Wrong-tool rate | 7.3% | < 1% |
| Error rate | 8.8% | < 5% |
| `mkdir` failure rate | 42% | 0% |
| `git commit` failure rate | 27% | < 15% |
| Agent command diversity | N/A | Track unique commands per session |

### 9.3 Re-analysis

Run the analysis scripts periodically to track improvement:

```bash
python3 hooks/analyze_bash_tools.py    # Full statistics
python3 hooks/analyze_waste.py         # Waste analysis
```

Compare before/after to measure hook effectiveness.

---

## 10) Privacy & Security

- Agent commands are stored in the same SQLite database as human commands
- Tagged with `shell=claude-code` for filtering
- Incognito mode (`CLAI_EPHEMERAL=1`) is respected — ephemeral agent commands are never persisted
- No commands are sent to external services (all processing is local)
- The daemon socket is user-scoped (permission 0700)
- Suggestion injection is advisory — agents are not forced to follow recommendations
