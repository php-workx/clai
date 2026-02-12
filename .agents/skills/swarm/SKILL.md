---
name: swarm
description: 'Spawn isolated agents for parallel task execution. Level 1: Task tool (pure Claude-native). Level 2: tmux + Agent Mail (process isolation, persistence). Triggers: "swarm", "spawn agents", "parallel work".'
---

# Swarm Skill

Spawn isolated agents to execute tasks in parallel. Fresh context per agent (Ralph Wiggum pattern).

**Execution Levels:**
- **Level 1** (default) - Pure Claude-native using Task tool background agents
- **Level 2** (`--level=2`) - tmux sessions + Agent Mail for robust coordination

**Integration modes:**
- **Direct** - Create TaskList tasks, invoke `/swarm`
- **Via Crank** - `/crank` creates tasks from beads, invokes `/swarm` for each wave

## Architecture (Mayor-First)

```
Mayor (this session)
    |
    +-> Plan: TaskCreate with dependencies
    |
    +-> Identify wave: tasks with no blockers
    |
    +-> Spawn: Task tool (run_in_background=true) for each
    |       Each agent completes its task atomically
    |
    +-> Wait: <task-notification> arrives
    |
    +-> Validate: Review changes when complete
    |
    +-> Repeat: New plan if more work needed
```

## Execution

Given `/swarm`:

### Step 1: Ensure Tasks Exist

Use TaskList to see current tasks. If none, create them:

```
TaskCreate(subject="Implement feature X", description="Full details...")
TaskUpdate(taskId="2", addBlockedBy=["1"])  # Add dependencies after creation
```

### Step 2: Identify Wave

Find tasks that are:
- Status: `pending`
- No blockedBy (or all blockers completed)

These can run in parallel.

### Step 3: Spawn Agents

For each ready task, spawn a background agent:

```
Task(
  subagent_type="general-purpose",
  run_in_background=true,
  prompt="Execute task #<id>: <subject>

<description>

Work autonomously. Create/edit files as needed. Verify your work."
)
```

**Important:** Agents cannot access TaskList/TaskUpdate. Mayor must:
1. Wait for `<task-notification>`
2. Verify work was done
3. Call `TaskUpdate(taskId, status="completed")`

### Step 4: Wait for Notifications

Agents send `<task-notification>` automatically when complete:
- No polling needed
- Mayor receives notification with task result
- Then Mayor updates TaskList and spawns next wave

### Step 5: Validate & Review

When agents complete:
1. Check git status for changes
2. Review diffs
3. Run tests/validation
4. Commit combined work if needed

### Step 6: Repeat if Needed

If more tasks remain:
1. Check TaskList for next wave
2. Spawn new agents
3. Continue until all done

## Example Flow

```
Mayor: "Let's build a user auth system"

1. /plan → Creates tasks:
   #1 [pending] Create User model
   #2 [pending] Add password hashing (blockedBy: #1)
   #3 [pending] Create login endpoint (blockedBy: #1)
   #4 [pending] Add JWT tokens (blockedBy: #3)
   #5 [pending] Write tests (blockedBy: #2, #3, #4)

2. /swarm → Spawns agent for #1 (only unblocked task)

3. Agent #1 completes → #1 now completed
   → #2 and #3 become unblocked

4. /swarm → Spawns agents for #2 and #3 in parallel

5. Continue until #5 completes

6. /vibe → Validate everything
```

## Key Points

- **Pure Claude-native** - No tmux, no external scripts
- **Background agents** - `run_in_background=true` for isolation
- **Wave execution** - Only unblocked tasks spawn
- **Mayor orchestrates** - You control the flow
- **Atomic execution** - Each agent works until task done

## Integration with AgentOps

This ties into the full workflow:

```
/research → Understand the problem
/plan → Decompose into beads issues
/crank → Autonomous epic loop
    └── /swarm → Execute each wave in parallel
/vibe → Validate results
/post-mortem → Extract learnings
```

**Direct use (no beads):**
```
TaskCreate → Define tasks
/swarm → Execute in parallel
```

The knowledge flywheel captures learnings from each agent.

## Task Management Commands

```
# List all tasks
TaskList()

# Mark task complete after notification
TaskUpdate(taskId="1", status="completed")

# Add dependency between tasks
TaskUpdate(taskId="2", addBlockedBy=["1"])
```

## When to Use Swarm

| Scenario | Use |
|----------|-----|
| Multiple independent tasks | `/swarm` (parallel) |
| Sequential dependencies | `/swarm` with blockedBy |
| Mix of both | `/swarm` spawns waves, each wave parallel |

## Why This Works: Ralph Wiggum Pattern

This architecture follows the [Ralph Wiggum Pattern](https://ghuntley.com/ralph/) for autonomous agents.

**Core Insight:** Each `Task(run_in_background=true)` spawn = fresh context.

```
Ralph's bash loop:          Our swarm:
while :; do                 Mayor spawns Task → fresh context
  cat PROMPT.md | claude    Mayor spawns Task → fresh context
done                        Mayor spawns Task → fresh context
```

Both achieve the same thing: **fresh context per execution unit**.

### Why Fresh Context Matters

| Approach | Context | Problem |
|----------|---------|---------|
| Internal loop in agent | Accumulates | Degrades over iterations |
| Mayor spawns agents | Fresh each time | Stays effective at scale |

Making demigods loop internally would violate Ralph - context accumulates within the session. The loop belongs in Mayor (lightweight orchestration), fresh context belongs in demigods (heavyweight work).

### Key Properties

- **Mayor IS the loop** - Orchestration layer, manages state
- **Demigods are atomic** - One task, one spawn, one result
- **TaskList as memory** - State persists in task status, not context
- **Filesystem for artifacts** - Files written, commits made

This is **Ralph + parallelism**: the while loop is distributed across wave spawns, with multiple agents per wave.

## Integration with Crank

When `/crank` invokes `/swarm`:

1. **Crank** bridges beads issues to TaskList tasks
2. **Swarm** executes the TaskList wave with fresh-context agents
3. **Crank** syncs results back to beads

```
/crank epic-123
  └── bd ready → [ao-1, ao-2, ao-3]
      └── TaskCreate for each issue
          └── /swarm
              └── Spawn agents (fresh context each)
                  └── Complete TaskList tasks
      └── bd update --status closed
  └── Loop until epic DONE
```

This gives you:
- **Beads orchestration** (crank) - Epic lifecycle, issue tracking
- **Fresh-context execution** (swarm) - Ralph pattern for each issue

## Distinctions (Common Confusion)

| You Want | Use | Why |
|----------|-----|-----|
| Fresh-context parallel execution | `/swarm` | Each spawned background agent is a clean slate |
| Autonomous epic loop | `/crank` | Loops waves via swarm until epic closes |
| Just swarm, no beads | `/swarm` directly | Use TaskList only, skip beads integration |
| RPI progress gates | `/ratchet` | Tracks/locks progress; does not execute work by itself |

---

## Level 2 Mode: tmux + Agent Mail

> **When:** MCP Agent Mail is available AND you want true process isolation, persistent workers, and robust coordination.

Level 2 mode spawns real tmux sessions instead of Task tool background agents. Each demigod runs in its own Claude process with full lifecycle management.

### Why Level 2?

| Level 1 (Task tool) | Level 2 (tmux + Agent Mail) |
|---------------------|----------------------------|
| Background agents in Mayor's process | Separate tmux sessions |
| Coupled to Mayor lifecycle | Persistent if Mayor crashes |
| No inter-agent coordination | Agent Mail messaging |
| No file conflict prevention | File reservations |
| Simple, fast to spawn | More setup, more robust |
| Good for small jobs | Better for large/long jobs |

### Level Detection

At skill start, detect which level to use:

```bash
# Method 1: Explicit flag
# /swarm --level=2 <tasks>

# Method 2: Auto-detect Agent Mail availability
LEVEL=1

# Check for Agent Mail MCP tools (look for register_agent tool)
if mcp-tools 2>/dev/null | grep -q "mcp-agent-mail"; then
    AGENT_MAIL_AVAILABLE=true
fi

# Check for Agent Mail HTTP endpoint
if curl -s http://localhost:8765/health >/dev/null 2>&1; then
    AGENT_MAIL_HTTP=true
fi

# Level 2 requires: Agent Mail available + explicit flag or beads mode
if [ "$LEVEL_FLAG" = "2" ] && [ "$AGENT_MAIL_AVAILABLE" = "true" -o "$AGENT_MAIL_HTTP" = "true" ]; then
    LEVEL=2
fi
```

**Decision matrix:**

| `--level` | Agent Mail | Result |
|-----------|------------|--------|
| Not set | Not available | Level 1 |
| Not set | Available | Level 1 (explicit opt-in required) |
| `--level=1` | Any | Level 1 |
| `--level=2` | Not available | **Error: Agent Mail required** |
| `--level=2` | Available | Level 2 |

### Level 2 Invocation

```
/swarm --level=2 [--max-workers=N]
/swarm --level=2 --bead-ids ol-527.1,ol-527.2,ol-527.3
```

**Parameters:**

| Parameter | Description | Default |
|-----------|-------------|---------|
| `--level=2` | Enable tmux + Agent Mail mode | Required |
| `--max-workers=N` | Max concurrent demigods | 5 |
| `--bead-ids` | Specific beads to work (comma-separated) | Auto from `bd ready` |
| `--wait` | Wait for all demigods to complete | false |
| `--timeout` | Max time to wait (if --wait) | 30m |

### Level 2 Architecture

```
Mayor Session (this session)
    |
    +-> Level Detection: Agent Mail available → Level 2
    |
    +-> Identify wave: bd ready → [ol-527.1, ol-527.2, ol-527.3]
    |
    +-> Spawn: tmux new-session for each (via /spawn skill internally)
    |       demigod-ol-527-1 → runs /demigod ol-527.1
    |       demigod-ol-527-2 → runs /demigod ol-527.2
    |       demigod-ol-527-3 → runs /demigod ol-527.3
    |
    +-> Coordinate: Agent Mail messages
    |       Each demigod sends ACCEPTED, PROGRESS, DONE/FAILED
    |       Mayor monitors via fetch_inbox
    |       File reservations prevent conflicts
    |
    +-> Validate: On DONE, optionally run /vibe --remote
    |
    +-> Repeat: New wave when workers complete
```

### Level 2 Execution Steps

Given `/swarm --level=2`:

#### L2 Step 1: Pre-flight Checks

```bash
# Check tmux is available
which tmux >/dev/null 2>&1 || {
    echo "Error: tmux required for Level 2. Install: brew install tmux"
    exit 1
}

# Check claude CLI is available
which claude >/dev/null 2>&1 || {
    echo "Error: claude CLI required for Level 2"
    exit 1
}

# Check Agent Mail is available
AGENT_MAIL_OK=false
if curl -s http://localhost:8765/health >/dev/null 2>&1; then
    AGENT_MAIL_OK=true
fi
# OR check MCP tools

if [ "$AGENT_MAIL_OK" != "true" ]; then
    echo "Error: Agent Mail required for Level 2"
    echo "Start server: cd ~/gt/acfs-research/tier1/mcp_agent_mail && uv run python -m mcp_agent_mail.http"
    exit 1
fi
```

#### L2 Step 2: Register Mayor with Agent Mail

Register the Mayor session to receive messages from demigods.

```
MCP Tool: register_agent
Parameters:
  project_key: <absolute path to project>
  program: "claude-code"
  model: "opus-4.5"
  task_description: "Mayor orchestrating swarm for wave"
```

**Store the returned agent name as `MAYOR_NAME`.**

#### L2 Step 3: Identify Wave (Ready Beads)

Same as Level 1, get the beads to work:

```bash
# Get ready beads
READY_BEADS=$(bd ready --json 2>/dev/null | jq -r '.[].id' | head -$MAX_WORKERS)

# Or use explicit bead list
if [ -n "$BEAD_IDS" ]; then
    READY_BEADS=$(echo "$BEAD_IDS" | tr ',' '\n')
fi

# Count
WAVE_SIZE=$(echo "$READY_BEADS" | wc -l | tr -d ' ')
```

If no ready beads, exit with message.

#### L2 Step 4: Spawn Demigods via tmux

For each ready bead, spawn a demigod session:

```bash
for BEAD_ID in $READY_BEADS; do
    # Generate session name
    SESSION_NAME="demigod-$(echo $BEAD_ID | tr '.' '-')"

    # Check for existing session
    if tmux has-session -t "$SESSION_NAME" 2>/dev/null; then
        echo "Session $SESSION_NAME already exists, skipping"
        continue
    fi

    # Spawn demigod in new tmux session
    tmux new-session -d -s "$SESSION_NAME" "claude -p '/demigod $BEAD_ID'"

    # Verify session started
    if tmux has-session -t "$SESSION_NAME" 2>/dev/null; then
        echo "Spawned: $SESSION_NAME for $BEAD_ID"
        SPAWNED_COUNT=$((SPAWNED_COUNT + 1))
    else
        echo "Failed to spawn: $SESSION_NAME"
    fi

    # Rate limit to avoid API cascade
    sleep 2
done
```

**Key points:**
- Use `-d` for detached sessions (run in background)
- Session names derived from bead IDs for easy correlation
- Rate limit spawns (2 seconds) to avoid API rate limits
- Verify each session started before continuing

#### L2 Step 5: Monitor via Agent Mail

Poll Agent Mail inbox for demigod messages:

```
MCP Tool: fetch_inbox
Parameters:
  project_key: <project path>
  agent_name: <MAYOR_NAME>
  limit: 50
  include_bodies: true
```

**Process messages by type:**

| Subject Pattern | Action |
|-----------------|--------|
| `[<bead-id>] ACCEPTED` | Log demigod started work |
| `[<bead-id>] PROGRESS` | Log progress, check health |
| `[<bead-id>] HELP_REQUEST` | Alert for manual intervention or route to Chiron |
| `[<bead-id>] DONE` | Mark complete, check if wave finished |
| `[<bead-id>] FAILED` | Log failure, decide on retry or escalate |

#### L2 Step 6: Track Completion

Maintain completion state:

```bash
# Track per-bead status
declare -A BEAD_STATUS
for BEAD_ID in $READY_BEADS; do
    BEAD_STATUS[$BEAD_ID]="spawned"
done

# Update on DONE/FAILED messages
# BEAD_STATUS[$BEAD_ID]="done" or "failed"

# Check if wave complete
DONE_COUNT=0
FAILED_COUNT=0
for BEAD_ID in "${!BEAD_STATUS[@]}"; do
    case "${BEAD_STATUS[$BEAD_ID]}" in
        done) DONE_COUNT=$((DONE_COUNT + 1)) ;;
        failed) FAILED_COUNT=$((FAILED_COUNT + 1)) ;;
    esac
done

if [ $((DONE_COUNT + FAILED_COUNT)) -eq $WAVE_SIZE ]; then
    WAVE_COMPLETE=true
fi
```

#### L2 Step 7: Report Results

When wave completes (or on `--wait` timeout):

```markdown
## Swarm Level 2 Results

**Wave completed:** <timestamp>
**Beads in wave:** <WAVE_SIZE>
**Successful:** <DONE_COUNT>
**Failed:** <FAILED_COUNT>

### Completed Beads
| Bead ID | Demigod | Commit | Summary |
|---------|---------|--------|---------|
| ol-527.1 | GreenCastle | abc123 | Added auth middleware |
| ol-527.2 | BlueMountain | def456 | Fixed rate limiting |

### Failed Beads
| Bead ID | Demigod | Reason | Recommendation |
|---------|---------|--------|----------------|
| ol-527.3 | RedValley | Tests failed | Re-run with spec clarification |

### Active Sessions
| Session | Bead | Status | Runtime |
|---------|------|--------|---------|
| demigod-ol-527-1 | ol-527.1 | done | 15m |
| demigod-ol-527-2 | ol-527.2 | done | 12m |
| demigod-ol-527-3 | ol-527.3 | failed | 18m |
```

#### L2 Step 8: Cleanup Completed Sessions

Optionally clean up tmux sessions for completed beads:

```bash
# Clean up done sessions (keep failed for debugging)
for BEAD_ID in "${!BEAD_STATUS[@]}"; do
    if [ "${BEAD_STATUS[$BEAD_ID]}" = "done" ]; then
        SESSION_NAME="demigod-$(echo $BEAD_ID | tr '.' '-')"
        tmux kill-session -t "$SESSION_NAME" 2>/dev/null
    fi
done
```

**Or keep all sessions for review:** Use `--keep-sessions` flag to preserve all tmux sessions for post-mortem analysis.

### Level 2 Helper Skills

Use these companion skills with Level 2 swarm:

| Skill | Purpose |
|-------|---------|
| `/attach <session>` | Attach to a running demigod session for debugging |
| `/attach --list` | List all running demigod sessions |
| `/inbox` | Check Agent Mail for pending messages |
| `/vibe --remote <session>` | Validate demigod's work before accepting |

### Level 2 File Reservations

File reservations prevent conflicts when multiple demigods edit files.

**How it works:**
1. Each demigod claims files before editing (via Agent Mail `file_reservation_paths`)
2. If another demigod tries to claim the same file, it sees a conflict warning
3. Demigods release reservations when done

**Mayor can view reservations:**

```
MCP Tool: get_file_reservations (if available)
Parameters:
  project_key: <project path>
```

**On conflict:**
- Demigod sends PROGRESS message noting the conflict
- Mayor decides: wait, reassign, or allow parallel work
- Advisory reservations don't block, just warn

### Level 2 Error Handling

#### Demigod Session Crashes

If a tmux session dies unexpectedly:

```bash
# Check session health
tmux has-session -t "$SESSION_NAME" 2>/dev/null || {
    echo "Session $SESSION_NAME died"

    # Check bead status
    STATUS=$(bd show $BEAD_ID --json 2>/dev/null | jq -r '.status')

    if [ "$STATUS" = "in_progress" ]; then
        # Unclaim bead for retry
        bd update $BEAD_ID --status open --assignee "" 2>/dev/null
        echo "Bead $BEAD_ID released for retry"
    fi
}
```

#### Agent Mail Server Crashes

If Agent Mail becomes unavailable mid-swarm:

1. Demigods continue working (graceful degradation)
2. Messages queue locally (if supported)
3. Mayor loses visibility but work continues
4. On restart, poll beads status directly:

```bash
for BEAD_ID in $READY_BEADS; do
    STATUS=$(bd show $BEAD_ID --json 2>/dev/null | jq -r '.status')
    echo "$BEAD_ID: $STATUS"
done
```

#### Timeout Handling

If `--wait` times out:

```markdown
## Swarm Timeout

Wave did not complete within <timeout>.

### Still Running
| Session | Bead | Runtime | Action |
|---------|------|---------|--------|
| demigod-ol-527-3 | ol-527.3 | 32m | Consider `/attach` to check status |

### Options
1. Continue waiting: `/swarm --wait --timeout 60m`
2. Attach to slow workers: `/attach demigod-ol-527-3`
3. Kill and retry: `tmux kill-session -t demigod-ol-527-3`
```

### Level 2 vs Level 1 Summary

| Behavior | Level 1 | Level 2 |
|----------|---------|---------|
| Spawn mechanism | `Task(run_in_background=true)` | `tmux new-session -d` |
| Worker entry point | Inline prompt | `/demigod <bead-id>` |
| Process isolation | Shared parent | Separate processes |
| Persistence | Tied to Mayor | Survives Mayor crash |
| Coordination | None | Agent Mail messages |
| File conflicts | Race conditions | File reservations |
| Debugging | Limited | `/attach` to inspect |
| Resource overhead | Low | Medium (N tmux sessions) |
| Setup requirements | None | tmux + Agent Mail |

### When to Use Level 2

| Scenario | Recommendation |
|----------|---------------|
| Quick parallel tasks (<5 min each) | Level 1 |
| Long-running work (>10 min each) | Level 2 |
| Need to debug stuck workers | Level 2 |
| Multi-file changes across workers | Level 2 (file reservations) |
| Mayor might disconnect | Level 2 (persistence) |
| Complex coordination needed | Level 2 |
| Simple, isolated tasks | Level 1 |

### Example: Full Level 2 Swarm

```bash
# 1. Start Agent Mail (if not running)
cd ~/gt/acfs-research/tier1/mcp_agent_mail
uv run python -m mcp_agent_mail.http --host 127.0.0.1 --port 8765 &

# 2. In Claude session, run Level 2 swarm
/swarm --level=2 --max-workers=3 --wait

# Output:
# Pre-flight: tmux OK, Agent Mail OK
# Registered as Mayor: GoldenPeak
# Wave 1: 3 ready beads
# Spawning: demigod-ol-527-1 for ol-527.1
# Spawning: demigod-ol-527-2 for ol-527.2
# Spawning: demigod-ol-527-3 for ol-527.3
# Monitoring...
# [15:32] ACCEPTED from GreenCastle (ol-527.1)
# [15:32] ACCEPTED from BlueMountain (ol-527.2)
# [15:33] ACCEPTED from RedValley (ol-527.3)
# [15:40] PROGRESS from GreenCastle: Step 4 - implementing auth
# [15:45] DONE from GreenCastle (ol-527.1) - commit abc123
# [15:48] DONE from BlueMountain (ol-527.2) - commit def456
# [15:55] FAILED from RedValley (ol-527.3) - tests failed
#
# Wave complete: 2 done, 1 failed
# Sessions cleaned up (except failed)
# Use `/attach demigod-ol-527-3` to debug failed worker
```

### Fallback Behavior

If Level 2 requested but requirements not met:

```
Error: Level 2 requires tmux and Agent Mail.

Missing:
- [ ] tmux: Install with `brew install tmux`
- [x] Agent Mail: Running at localhost:8765

Falling back to Level 1? [y/N]
```

If user confirms, degrade to Level 1 execution. Otherwise, exit with error.
