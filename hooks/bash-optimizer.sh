#!/usr/bin/env bash
# bash-optimizer.sh — PreToolUse:Bash hook for Claude Code
#
# Intercepts Bash tool calls to:
#   1. Block commands that should use dedicated tools (Read, Grep, Glob, Edit, Write)
#   2. Auto-fix common mistakes (mkdir without -p)
#   3. Log all decisions to an audit JSONL file
#
# Exit codes:
#   0 = allow (with optional JSON on stdout for decisions)
#   2 = block (stderr message sent as feedback to Claude)
#   other = non-blocking error (ignored by Claude Code)
#
# Install: symlink to ~/.claude/hooks/bash-optimizer.sh
# Config:  add PreToolUse hook in ~/.claude/settings.json

set -euo pipefail

# ── Configuration ──────────────────────────────────────────────────
LOG_DIR="${HOME}/.claude/logs"
LOG_FILE="${LOG_DIR}/bash-optimizer.jsonl"
ENABLE_LOGGING="${BASH_OPTIMIZER_LOG:-1}"

# ── Read input ─────────────────────────────────────────────────────
INPUT=$(cat)
COMMAND=$(printf '%s' "$INPUT" | jq -r '.tool_input.command // ""')
TOOL_USE_ID=$(printf '%s' "$INPUT" | jq -r '.tool_use_id // ""')
SESSION_ID=$(printf '%s' "$INPUT" | jq -r '.session_id // ""')

# Fast exit: empty command
if [[ -z "$COMMAND" ]]; then
  exit 0
fi

# ── Helpers ────────────────────────────────────────────────────────

# Log a decision to the audit file
log_decision() {
  local rule="$1" decision="$2" reason="$3"
  if [[ "$ENABLE_LOGGING" == "1" ]]; then
    mkdir -p "$LOG_DIR"
    local ts
    ts=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
    local cmd_escaped
    cmd_escaped=$(printf '%s' "$COMMAND" | head -c 200 | jq -Rs '.')
    printf '{"ts":"%s","session":"%s","tool_use_id":"%s","rule":"%s","decision":"%s","reason":"%s","command":%s}\n' \
      "$ts" "$SESSION_ID" "$TOOL_USE_ID" "$rule" "$decision" "$reason" "$cmd_escaped" \
      >> "$LOG_FILE"
  fi
}

# Emit deny JSON and exit
deny() {
  local rule="$1" reason="$2"
  log_decision "$rule" "deny" "$reason"
  jq -n \
    --arg reason "$reason" \
    '{
      hookSpecificOutput: {
        hookEventName: "PreToolUse",
        permissionDecision: "deny",
        permissionDecisionReason: $reason
      }
    }'
  exit 0
}

# Emit allow-with-modified-input JSON and exit
allow_modified() {
  local rule="$1" reason="$2" new_command="$3"
  log_decision "$rule" "allow_modified" "$reason"
  jq -n \
    --arg reason "$reason" \
    --arg cmd "$new_command" \
    '{
      hookSpecificOutput: {
        hookEventName: "PreToolUse",
        permissionDecision: "allow",
        permissionDecisionReason: $reason,
        updatedInput: { command: $cmd }
      }
    }'
  exit 0
}

# ── Extract first command (before pipes/chains) ───────────────────
# We only analyze the first command segment to detect "standalone" usage.
# Commands after | are pipeline consumers and are fine.

# Check if the ENTIRE command starts with the tool (not after a pipe)
# A command like "go test | grep FAIL" has grep AFTER a pipe — that's ok.
# A command like "grep pattern file" starts with grep — that's wrong tool.

# Get the first segment (before any | or && or ;)
# But be careful with quoted strings containing these chars
FIRST_SEGMENT=$(printf '%s' "$COMMAND" | sed 's/|.*//' | sed 's/&&.*//' | sed 's/;.*//')

# Strip leading whitespace and env vars (FOO=bar ...)
STRIPPED="$FIRST_SEGMENT"
while [[ "$STRIPPED" =~ ^[[:space:]]*[A-Z_]+=[^[:space:]]+ ]]; do
  STRIPPED=$(printf '%s' "$STRIPPED" | sed 's/^[[:space:]]*[A-Z_]*=[^[:space:]]* *//')
done
STRIPPED=$(printf '%s' "$STRIPPED" | sed 's/^[[:space:]]*//')

# Get the base command (first word)
BASE_CMD=$(printf '%s' "$STRIPPED" | awk '{print $1}')

# ── Rule 1: cat <file> → Read ─────────────────────────────────────
if [[ "$BASE_CMD" == "cat" ]]; then
  # Allow: heredocs (cat << or cat <<'), piping out (cat file | ...)
  # Allow: cat with -n flag (numbered output) — but Read does this too
  # Deny: simple "cat <file>" usage
  if printf '%s' "$COMMAND" | grep -qE '<<'; then
    : # heredoc — allow
  elif printf '%s' "$COMMAND" | grep -qE '\|'; then
    : # has pipe — probably part of pipeline, allow
  elif printf '%s' "$COMMAND" | grep -qE '>'; then
    : # redirect — allow (e.g., cat a b > c)
  else
    deny "cat_read" "Use the Read tool instead of 'cat'. Read is the dedicated tool for reading file contents and supports line offsets/limits."
  fi
fi

# ── Rule 2: grep/rg → Grep tool ───────────────────────────────────
if [[ "$BASE_CMD" == "grep" || "$BASE_CMD" == "rg" ]]; then
  # Allow: receiving pipe input (cmd | grep)
  # The FIRST_SEGMENT check already strips pipe suffixes, so if BASE_CMD
  # is grep, it means the command STARTS with grep (standalone).
  # But also check: is it the full command or just the first part?
  if printf '%s' "$COMMAND" | grep -qE '^\s*(grep|rg)\b'; then
    deny "grep_tool" "Use the Grep tool instead of '$BASE_CMD'. Grep is the dedicated search tool with ripgrep backend, supports regex, file type filters, and context lines."
  fi
fi

# ── Rule 3: find → Glob tool ──────────────────────────────────────
if [[ "$BASE_CMD" == "find" ]]; then
  # Allow: find with -exec (can't replicate with Glob)
  # Allow: find with -delete (destructive — needs Bash)
  if printf '%s' "$COMMAND" | grep -qE '\-exec|\-delete|\-print0'; then
    : # complex find — allow
  else
    deny "find_glob" "Use the Glob tool instead of 'find'. Glob supports patterns like '**/*.go' and is faster for file discovery."
  fi
fi

# ── Rule 4: head/tail <file> → Read tool ──────────────────────────
if [[ "$BASE_CMD" == "head" || "$BASE_CMD" == "tail" ]]; then
  # Allow: receiving pipe input (cmd | head)
  if printf '%s' "$COMMAND" | grep -qE '^\s*(head|tail)\b'; then
    # Check it's not receiving from a pipe in the FULL command
    if ! printf '%s' "$COMMAND" | grep -qE '\|[[:space:]]*(head|tail)\b'; then
      deny "head_tail_read" "Use the Read tool instead of '$BASE_CMD'. Read supports offset and limit parameters for reading specific line ranges."
    fi
  fi
fi

# ── Rule 5: sed -i / awk (editing files) → Edit tool ──────────────
if [[ "$BASE_CMD" == "sed" ]]; then
  if printf '%s' "$COMMAND" | grep -qE 'sed\s+(-i|--in-place)'; then
    deny "sed_edit" "Use the Edit tool instead of 'sed -i'. Edit provides exact string replacement with uniqueness checking."
  fi
fi
if [[ "$BASE_CMD" == "awk" ]]; then
  # awk is almost always used for text processing on files
  # Allow in pipelines
  if printf '%s' "$COMMAND" | grep -qE '^\s*awk\b' && ! printf '%s' "$COMMAND" | grep -qE '\|[[:space:]]*awk\b'; then
    deny "awk_edit" "Use the Read tool (for extraction) or Edit tool (for modification) instead of 'awk'. Dedicated tools are preferred."
  fi
fi

# ── Rule 6: mkdir without -p → auto-fix ───────────────────────────
if [[ "$BASE_CMD" == "mkdir" ]]; then
  if ! printf '%s' "$COMMAND" | grep -qE 'mkdir\s+.*-p|mkdir\s+-p'; then
    # Add -p flag
    NEW_CMD=$(printf '%s' "$COMMAND" | sed 's/mkdir /mkdir -p /')
    allow_modified "mkdir_fix" "Auto-added -p flag to mkdir" "$NEW_CMD"
  fi
fi

# ── Rule 7: echo ... > file → Write tool ──────────────────────────
if [[ "$BASE_CMD" == "echo" || "$BASE_CMD" == "printf" ]]; then
  # Deny only when redirecting to a file (> or >>)
  if printf '%s' "$COMMAND" | grep -qE '>\s*[^&]'; then
    deny "echo_write" "Use the Write tool instead of '$BASE_CMD > file'. Write is the dedicated tool for creating/overwriting files."
  fi
fi

# ── Default: allow ─────────────────────────────────────────────────
# No rule matched — pass through silently (no JSON output needed)
exit 0
