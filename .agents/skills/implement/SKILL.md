---
name: implement
description: 'Execute a single beads issue with full lifecycle. Triggers: "implement", "work on task", "fix bug", "start feature", "pick up next issue".'
---

# Implement Skill

> **Quick Ref:** Execute single issue end-to-end. Output: code changes + commit + closed issue.

**YOU MUST EXECUTE THIS WORKFLOW. Do not just describe it.**

Execute a single issue from start to finish.

**Requires:** bd CLI (beads) for issue tracking. Falls back to plain descriptions if unavailable.

## Execution Steps

Given `/implement <issue-id-or-description>`:

### Step 0: Check Issue State (Resume Logic)

Before starting implementation, check if resuming:

1. **Check if issue is in_progress:**
```bash
bd show <issue-id> --json 2>/dev/null | jq -r '.status'
```

2. **If status = in_progress AND assigned to you:**
   - Look for checkpoint in issue notes: `bd show <id> --json | jq -r '.notes'`
   - Resume from last checkpoint step
   - Announce: "Resuming issue from Step N"

3. **If status = in_progress AND assigned to another agent:**
   - Report: "Issue claimed by <agent> - use `bd update <id> --assignee self --force` to override"
   - Do NOT proceed without explicit override

4. **Store checkpoints after each major step:**
```bash
bd update <issue-id> --append-notes "CHECKPOINT: Step N completed at $(date -Iseconds)" 2>/dev/null
```

### Step 0a: Check Ratchet Status (RPI Workflow)

**Before implementation, verify prior workflow gates passed:**

```bash
# Check if ao CLI is available
if command -v ao &>/dev/null; then
  # Check if research and plan phases completed
  RATCHET_STATUS=$(ao ratchet status --json 2>/dev/null || echo '{}')
  RESEARCH_DONE=$(echo "$RATCHET_STATUS" | jq -r '.research.completed // false')
  PLAN_DONE=$(echo "$RATCHET_STATUS" | jq -r '.plan.completed // false')

  if [ "$RESEARCH_DONE" = "true" ] && [ "$PLAN_DONE" = "true" ]; then
    echo "Ratchet: Prior gates passed (research + plan complete)"
  elif [ "$RESEARCH_DONE" = "false" ] || [ "$PLAN_DONE" = "false" ]; then
    echo "WARNING: Prior gates not complete. Run /research and /plan first."
    echo "  Research: $RESEARCH_DONE"
    echo "  Plan: $PLAN_DONE"
    echo ""
    echo "Override with: ao ratchet skip <gate> --reason 'manual override'"
  fi

  # Get current spec path for reference
  SPEC_PATH=$(ao ratchet spec 2>/dev/null || echo "")
  if [ -n "$SPEC_PATH" ]; then
    echo "Ratchet: Current spec at $SPEC_PATH"
  fi
else
  echo "Ratchet: ao CLI not available - skipping gate check"
fi
```

**Fallback:** If ao is not available, proceed without ratchet checks. The skill continues normally.

### Step 1: Get Issue Details

**If beads issue ID provided** (e.g., `gt-123`):
```bash
bd show <issue-id> 2>/dev/null
```

**If plain description provided:** Use that as the task description.

**If no argument:** Check for ready work:
```bash
bd ready 2>/dev/null | head -3
```

### Step 2: Claim the Issue

```bash
bd update <issue-id> --status in_progress 2>/dev/null
```

### Step 3: Gather Context

**USE THE TASK TOOL** to explore relevant code:

```
Tool: Task
Parameters:
  subagent_type: "Explore"
  description: "Gather context for: <issue title>"
  prompt: |
    Find code relevant to: <issue description>

    1. Search for related files (Glob)
    2. Search for relevant keywords (Grep)
    3. Read key files to understand current implementation
    4. Identify where changes need to be made

    Return:
    - Files to modify (paths)
    - Current implementation summary
    - Suggested approach
    - Any risks or concerns
```

### Step 4: Implement the Change

Based on the context gathered:

1. **Edit existing files** using the Edit tool (preferred)
2. **Write new files** only if necessary using the Write tool
3. **Follow existing patterns** in the codebase
4. **Keep changes minimal** - don't over-engineer

### Step 5: Verify the Change

**Success Criteria (all must pass):**
- [ ] All existing tests pass (no new failures introduced)
- [ ] New code compiles/parses without errors
- [ ] No new linter warnings (if linter available)
- [ ] Change achieves the stated goal

Check for test files and run them:
```bash
# Find tests
ls *test* tests/ test/ __tests__/ 2>/dev/null | head -5

# Run tests (adapt to project type)
# Python: pytest
# Go: go test ./...
# Node: npm test
# Rust: cargo test
```

**If tests exist:** All tests must pass. Any failure = verification failed.

**If no tests exist:** Manual verification required:
- [ ] Syntax check passes (file compiles/parses)
- [ ] Imports resolve correctly
- [ ] Can reproduce expected behavior manually
- [ ] Edge cases identified during implementation are handled

**If verification fails:** Do NOT proceed to Step 5a. Fix the issue first.

### Step 5a: Verification Gate (MANDATORY)

**THE IRON LAW:** NO COMPLETION CLAIMS WITHOUT FRESH VERIFICATION EVIDENCE

Before reporting success, you MUST:

1. **IDENTIFY** - What command proves this claim works?
2. **RUN** - Execute the FULL command (fresh, not cached output)
3. **READ** - Check full output AND exit code
4. **VERIFY** - Does output actually confirm the claim?
5. **ONLY THEN** - Make the completion claim

**Forbidden phrases without fresh verification evidence:**
- "should work", "probably fixed", "seems to be working"
- "Great!", "Perfect!", "Done!" (without output proof)
- "I just ran it" (must run it AGAIN, fresh)

#### Rationalization Table

| Excuse | Reality |
|--------|---------|
| "Too simple to verify" | Simple code breaks. Verification takes 10 seconds. |
| "I just ran it" | Run it AGAIN. Fresh output only. |
| "Tests passed earlier" | Run them NOW. State changes. |
| "It's obvious it works" | Nothing is obvious. Evidence or silence. |
| "The edit looks correct" | Looking != working. Run the code. |

**Store checkpoint:**
```bash
bd update <issue-id> --append-notes "CHECKPOINT: Step 5a verification passed at $(date -Iseconds)" 2>/dev/null
```

### Step 6: Commit the Change

If the change is complete and verified:
```bash
git add <modified-files>
git commit -m "<descriptive message>

Implements: <issue-id>"
```

### Step 7: Close the Issue

```bash
bd update <issue-id> --status closed 2>/dev/null
```

### Step 7a: Record Implementation in Ratchet Chain

**After successful issue closure, record in ratchet:**

```bash
# Check if ao CLI is available
if command -v ao &>/dev/null; then
  # Get the commit hash as output artifact
  COMMIT_HASH=$(git rev-parse HEAD 2>/dev/null || echo "")
  CHANGED_FILES=$(git diff --name-only HEAD~1 2>/dev/null | tr '\n' ',' | sed 's/,$//')

  if [ -n "$COMMIT_HASH" ]; then
    # Record successful implementation
    ao ratchet record implement \
      --output "$COMMIT_HASH" \
      --files "$CHANGED_FILES" \
      --issue "<issue-id>" \
      2>&1 | tee -a .agents/ratchet.log

    if [ $? -eq 0 ]; then
      echo "Ratchet: Implementation recorded (commit: ${COMMIT_HASH:0:8})"
    else
      echo "Ratchet: Failed to record - chain.jsonl may need repair"
    fi
  else
    echo "Ratchet: No commit found - skipping record"
  fi
else
  echo "Ratchet: ao CLI not available - implementation NOT recorded"
  echo "  Run manually: ao ratchet record implement --output <commit>"
fi
```

**On failure/blocker:** Record the blocker in ratchet:

```bash
if command -v ao &>/dev/null; then
  ao ratchet record implement \
    --status blocked \
    --reason "<blocker description>" \
    2>/dev/null
fi
```

**Fallback:** If ao is not available, the issue is still closed via bd but won't be tracked in the ratchet chain. The skill continues normally.

### Step 8: Report to User

Tell the user:
1. What was changed (files modified)
2. How it was verified (with actual command output)
3. Issue status (closed)
4. Any follow-up needed
5. **Ratchet status** (implementation recorded or skipped)

**Output completion marker:**
```
<promise>DONE</promise>
```

If blocked or incomplete:
```
<promise>BLOCKED</promise>
Reason: <why blocked>
```

```
<promise>PARTIAL</promise>
Remaining: <what's left>
```

## Key Rules

- **Explore first** - understand before changing
- **Edit, don't rewrite** - prefer Edit tool over Write tool
- **Follow patterns** - match existing code style
- **Verify changes** - run tests or sanity checks
- **Commit with context** - reference the issue ID
- **Close the issue** - update status when done

## Without Beads

If bd CLI not available:
1. Skip the claim/close status updates
2. Use the description as the task
3. Still commit with descriptive message
4. Report completion to user

---

## Level 2 Mode: Agent Mail Coordination

> **When:** Agent Mail MCP tools are available (Level 2 Olympus or multi-agent coordination)

Level 2 mode enhances /implement with real-time coordination via MCP Agent Mail. This enables:
- Progress reporting to orchestrators (Mayor/Delphi)
- Help requests instead of blocking on user input
- Receiving guidance mid-execution
- Coordination with parallel workers (file reservations)

### Level Detection

Detect the operational level at skill start:

```bash
# Check for Agent Mail availability
AGENT_MAIL_AVAILABLE=false

# Method 1: Check if running inside /demigod context
if [ -n "$OLYMPUS_DEMIGOD_ID" ]; then
    AGENT_MAIL_AVAILABLE=true
fi

# Method 2: Check for explicit flag
# /implement <issue-id> --agent-mail --thread-id <id>
```

**Level Semantics:**
| Level | Condition | Behavior |
|-------|-----------|----------|
| `level_1` | No Agent Mail | Current behavior (all steps above) |
| `level_2` | Agent Mail available | Enhanced coordination (steps below) |

### New Parameters

| Parameter | Description | Default |
|-----------|-------------|---------|
| `--agent-mail` | Enable Agent Mail coordination | `false` |
| `--thread-id` | Thread ID for message grouping (usually bead-id) | `<issue-id>` |
| `--demigod-id` | Agent identity for message sender | `demigod-<issue-id>` |
| `--orchestrator` | Who to send status messages to | `mayor@olympus` |

### Level 2 Execution Steps

When `--agent-mail` is enabled OR `$OLYMPUS_DEMIGOD_ID` is set:

#### L2 Step 0: Initialize Agent Mail Session

```bash
# If not already registered (Demigod handles this), register self
# This is typically done by /demigod, but implement can work standalone
if [ -z "$OLYMPUS_DEMIGOD_ID" ]; then
    # Standalone Level 2 mode - need to register
    DEMIGOD_ID="${DEMIGOD_ID:-demigod-$(date +%s)}"
else
    DEMIGOD_ID="$OLYMPUS_DEMIGOD_ID"
fi
```

**MCP Tool Call (if standalone):**
```
Tool: mcp__mcp-agent-mail__register_agent
Parameters:
  project_key: "olympus"
  program: "implement-skill"
  model: "claude-opus-4-5-20250101"
  task_description: "Implementing <issue-id>"
```

#### L2 Step 1: Send ACCEPTED Message

After claiming the issue (Step 2 in base flow), notify the orchestrator:

**MCP Tool Call:**
```
Tool: mcp__mcp-agent-mail__send_message
Parameters:
  project_key: "olympus"
  sender_name: "<demigod-id>"
  to: "<orchestrator>"
  subject: "BEAD_ACCEPTED"
  body_md: |
    Accepted bead: <issue-id>
    Title: <issue-title>
    Starting implementation at: <timestamp>
  thread_id: "<issue-id>"
  ack_required: false
```

#### L2 Step 2: Check Inbox Before Major Steps

Before Steps 3, 4, 5 in base flow, check for incoming messages:

**MCP Tool Call:**
```
Tool: mcp__mcp-agent-mail__fetch_inbox
Parameters:
  project_key: "olympus"
  agent_name: "<demigod-id>"
```

**Handle incoming messages:**
| Message Type | Action |
|--------------|--------|
| `GUIDANCE` | Incorporate guidance into approach |
| `NUDGE` | Respond with progress update |
| `TERMINATE` | Exit gracefully (checkpoint work) |

#### L2 Step 3: Send PROGRESS Messages Periodically

After each major step, send progress update:

**Timing:**
- After context gathering (Step 3)
- During implementation (Step 4) - every ~5 minutes of work
- After verification (Step 5)

**MCP Tool Call:**
```
Tool: mcp__mcp-agent-mail__send_message
Parameters:
  project_key: "olympus"
  sender_name: "<demigod-id>"
  to: "<orchestrator>"
  subject: "PROGRESS"
  body_md: |
    Bead: <issue-id>
    Step: <current-step>
    Status: <what's happening>
    Context usage: <approximate %>
    Files touched: <list>
  thread_id: "<issue-id>"
  ack_required: false
```

#### L2 Step 4: Send HELP_REQUEST When Stuck

**Replace user prompts with Agent Mail help requests:**

Instead of:
```
"I'm stuck on X. Should I proceed with Y or Z?"
```

Do this:
```
Tool: mcp__mcp-agent-mail__send_message
Parameters:
  project_key: "olympus"
  sender_name: "<demigod-id>"
  to: "chiron@olympus"  # Help goes to Chiron (coach)
  subject: "HELP_REQUEST"
  body_md: |
    Bead: <issue-id>
    Issue Type: STUCK | SPEC_UNCLEAR | BLOCKED | TECHNICAL

    ## Problem
    <describe the issue>

    ## What I Tried
    <approaches attempted>

    ## Files Touched
    - path/to/file.py
    - path/to/other.py

    ## Question
    <specific question needing answer>
  thread_id: "<issue-id>"
  ack_required: true
```

**Then wait for HELP_RESPONSE:**
```
# Poll inbox for response (max 2 minutes per help-response timeout)
for i in {1..24}; do
    # Check inbox
    # If HELP_RESPONSE found, continue with guidance
    # If timeout exceeded, proceed with best judgment or fail gracefully
    sleep 5
done
```

#### L2 Step 5: Report Completion via Agent Mail

After Step 7 (closing the issue), send completion message:

**On Success:**
```
Tool: mcp__mcp-agent-mail__send_message
Parameters:
  project_key: "olympus"
  sender_name: "<demigod-id>"
  to: "<orchestrator>"
  subject: "OFFERING_READY"
  body_md: |
    Bead: <issue-id>
    Status: DONE

    ## Changes
    - Commit: <commit-sha>
    - Files: <changed-files>

    ## Self-Validation
    - Tests: PASS/FAIL
    - Lint: PASS/FAIL
    - Build: PASS/FAIL

    ## Summary
    <brief description of what was implemented>
  thread_id: "<issue-id>"
  ack_required: true
```

**On Failure:**
```
Tool: mcp__mcp-agent-mail__send_message
Parameters:
  project_key: "olympus"
  sender_name: "<demigod-id>"
  to: "<orchestrator>"
  subject: "FAILED"
  body_md: |
    Bead: <issue-id>
    Status: FAILED

    ## Failure
    Type: TESTS_FAIL | BUILD_FAIL | SPEC_IMPOSSIBLE | ERROR
    Reason: <description>
    Internal Attempts: <count>

    ## Partial Progress
    - Commit: <partial-commit-sha if any>
    - Files: <files-modified>

    ## Recommendation
    <what needs to happen to unblock>
  thread_id: "<issue-id>"
  ack_required: false
```

### Level 2 File Reservations

When running in Level 2 with parallel workers, claim files before editing:

**Before Step 4 (implementation):**
```
Tool: mcp__mcp-agent-mail__file_reservation_paths
Parameters:
  project_key: "olympus"
  agent_name: "<demigod-id>"
  paths:
    - "path/to/file1.py"
    - "path/to/file2.py"
  exclusive: false  # Advisory, not blocking
```

**After Step 7 (completion):**
```
Tool: mcp__mcp-agent-mail__release_file_reservations
Parameters:
  project_key: "olympus"
  agent_name: "<demigod-id>"
```

### Level 2 Context Checkpoint

If context usage is high (>80%), send checkpoint before exiting:

```
Tool: mcp__mcp-agent-mail__send_message
Parameters:
  project_key: "olympus"
  sender_name: "<demigod-id>"
  to: "<orchestrator>"
  subject: "CHECKPOINT"
  body_md: |
    Bead: <issue-id>
    Reason: CONTEXT_HIGH

    ## Progress
    - Commit: <partial-commit-sha>
    - Description: <what's done, what remains>
    - Context usage: <pct>%

    ## Next Steps for Successor
    <guidance for replacement demigod>
  thread_id: "<issue-id>"
  ack_required: false
```

### Level 2 Key Rules

- **Always check inbox** - Messages may contain critical guidance
- **Send progress regularly** - Orchestrator needs visibility
- **Use HELP_REQUEST** - Don't block on user, ask Chiron
- **Reserve files** - Prevents conflicts with parallel workers
- **Checkpoint on context high** - Preserve progress for successor
- **Acknowledge important messages** - Confirms receipt to sender

### Level 2 vs Level 1 Summary

| Behavior | Level 1 | Level 2 |
|----------|---------|---------|
| Progress reporting | None | PROGRESS messages |
| When stuck | Ask user / proceed | HELP_REQUEST to Chiron |
| Completion | bd update only | OFFERING_READY + bd update |
| Failure | Report to user | FAILED message + report |
| File conflicts | Race conditions | Advisory reservations |
| Guidance | None | Check inbox for GUIDANCE |

### Integration with /demigod

When /implement is called from /demigod skill:
1. Environment variables are pre-set (`$OLYMPUS_DEMIGOD_ID`, etc.)
2. Agent registration already done
3. File reservations already claimed
4. Just focus on implementation + progress + completion messages

When /implement is called standalone with `--agent-mail`:
1. Must register self
2. Must claim own files
3. Full Level 2 behavior
