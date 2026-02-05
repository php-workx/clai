# clai.bash - clai suggestions engine v3 hook for Bash
# This hook sends command events to the daemon for ingestion.
# See spec Section 17.2 for details.

# Guard: only in interactive shells
[[ $- == *i* ]] || return 0

# Guard: don't re-source
[[ -n "$_CLAI_V3_LOADED" ]] && return 0
_CLAI_V3_LOADED=1

# Check clai-hook exists
command -v clai-hook >/dev/null 2>&1 || return 0

# Session ID (lazy init)
_CLAI_SESSION_ID=""
_CLAI_PREEXEC_TS=""
_CLAI_LAST_CMD=""
_CLAI_LAST_HIST=""

# Version check helper
_clai_bash_version_ge() {
  local major=${BASH_VERSINFO[0]:-0}
  local minor=${BASH_VERSINFO[1]:-0}
  [[ $major -gt $1 ]] || { [[ $major -eq $1 ]] && [[ $minor -ge $2 ]]; }
}

_clai_sha256() {
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256
  elif command -v sha256sum >/dev/null 2>&1; then
    sha256sum
  else
    # Fallback: just use input (less unique but functional)
    cat
  fi
}

_clai_get_session_id() {
  if [[ -z "$_CLAI_SESSION_ID" ]]; then
    local session_file="${XDG_RUNTIME_DIR:-/tmp}/clai/session.$$"
    if [[ -f "$session_file" ]]; then
      _CLAI_SESSION_ID=$(<"$session_file")
    else
      # Fallback: generate locally
      _CLAI_SESSION_ID=$(printf '%s-%s-%s-%s' "${HOSTNAME:-localhost}" "$$" "$SECONDS" "$RANDOM$RANDOM" | _clai_sha256 | cut -c1-16)
    fi
  fi
  echo "$_CLAI_SESSION_ID"
}

# Preexec via DEBUG trap
_clai_preexec() {
  # Only capture on real commands, not PROMPT_COMMAND
  [[ -n "$COMP_LINE" ]] && return  # Skip during completion

  local this_hist
  this_hist=$(HISTTIMEFORMAT='' history 1)

  # Skip if same as last (prevents double-capture)
  [[ "$this_hist" == "$_CLAI_LAST_HIST" ]] && return
  _CLAI_LAST_HIST="$this_hist"

  # Extract command (remove history number)
  _CLAI_LAST_CMD="${this_hist#*[0-9]  }"
  _CLAI_PREEXEC_TS="$SECONDS"
}

_clai_postexec() {
  local exit_code=$?

  # Skip if no command or recording disabled
  [[ -z "$_CLAI_LAST_CMD" || -n "$CLAI_NO_RECORD" ]] && return

  # Skip clai-hook commands
  [[ "$_CLAI_LAST_CMD" == clai-hook* ]] && return

  # Calculate duration (seconds only in bash default mode)
  local duration_ms=0
  if [[ -n "$_CLAI_PREEXEC_TS" ]]; then
    duration_ms=$(( (SECONDS - _CLAI_PREEXEC_TS) * 1000 ))
  fi

  # Timestamp (seconds precision)
  local ts_ms
  ts_ms=$(date +%s)000

  # Ephemeral flag
  local ephemeral=0
  [[ -n "$CLAI_EPHEMERAL" ]] && ephemeral=1

  # Fire and forget (use subshell to background)
  (
    CLAI_CMD="$_CLAI_LAST_CMD" \
    CLAI_CWD="$PWD" \
    CLAI_EXIT="$exit_code" \
    CLAI_TS="$ts_ms" \
    CLAI_DURATION_MS="$duration_ms" \
    CLAI_SHELL="bash" \
    CLAI_SESSION_ID="$(_clai_get_session_id)" \
    CLAI_EPHEMERAL="$ephemeral" \
    clai-hook ingest 2>/dev/null
  ) &
  disown 2>/dev/null

  # Clear for next command
  _CLAI_LAST_CMD=""
  _CLAI_PREEXEC_TS=""
}

# Cleanup on shell exit
_clai_cleanup() {
  local session_file="${XDG_RUNTIME_DIR:-/tmp}/clai/session.$$"
  [[ -f "$session_file" ]] && rm -f "$session_file"
}
trap '_clai_cleanup' EXIT

# Set up DEBUG trap for preexec
trap '_clai_preexec' DEBUG

# Set up PROMPT_COMMAND for postexec
if _clai_bash_version_ge 4 4; then
  PROMPT_COMMAND+=('_clai_postexec')
else
  case "$PROMPT_COMMAND" in
    *_clai_postexec*) ;;
    '') PROMPT_COMMAND='_clai_postexec' ;;
    *';') PROMPT_COMMAND+='_clai_postexec' ;;
    *) PROMPT_COMMAND+=';_clai_postexec' ;;
  esac
fi
