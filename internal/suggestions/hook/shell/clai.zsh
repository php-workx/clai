# clai.zsh - clai suggestions engine v3 hook for Zsh
# This hook sends command events to the daemon for ingestion.
# See spec Section 17.1 for details.
#
# NOTE: Suggestion widgets (Ctrl+Space, Tab, etc.) are provided by the main
# shell integration at internal/cmd/shell/zsh/clai.zsh. This hook focuses
# solely on event ingestion for the daemon.

# Guard: only in interactive shells
[[ -o interactive ]] || return 0

# Guard: don't re-source
(( ${+_CLAI_V3_LOADED} )) && return 0
_CLAI_V3_LOADED=1

# Check clai-hook exists
(( ${+commands[clai-hook]} )) || return 0

# Load datetime module for EPOCHREALTIME
zmodload zsh/datetime 2>/dev/null

# Session ID (lazy init)
typeset -g _CLAI_SESSION_ID=""
typeset -g _CLAI_PREEXEC_TS=""
typeset -g _CLAI_LAST_CMD=""

_clai_sha256() {
  if (( ${+commands[shasum]} )); then
    shasum -a 256
  elif (( ${+commands[sha256sum]} )); then
    sha256sum
  else
    cat  # fallback
  fi
}

_clai_get_session_id() {
  if [[ -z "$_CLAI_SESSION_ID" ]]; then
    local session_file="${XDG_RUNTIME_DIR:-/tmp}/clai/session.$$"
    if [[ -f "$session_file" ]]; then
      _CLAI_SESSION_ID=$(<"$session_file")
    else
      # Fallback: generate locally
      _CLAI_SESSION_ID=$(printf '%s-%s-%s' "${HOST:-localhost}" "$$" "$EPOCHREALTIME" | _clai_sha256 | cut -c1-16)
    fi
  fi
  echo "$_CLAI_SESSION_ID"
}

_clai_preexec() {
  # Skip if recording disabled
  [[ -n "$CLAI_NO_RECORD" ]] && return

  # Store command and start time
  _CLAI_LAST_CMD="$1"
  _CLAI_PREEXEC_TS="$EPOCHREALTIME"
}

_clai_precmd() {
  local exit_code=$?

  # Skip if no command recorded or recording disabled
  [[ -z "$_CLAI_LAST_CMD" || -n "$CLAI_NO_RECORD" ]] && return

  # Skip if command is clai-hook itself
  [[ "$_CLAI_LAST_CMD" == clai-hook* ]] && return

  # Calculate duration
  local duration_ms=0
  if [[ -n "$_CLAI_PREEXEC_TS" ]]; then
    local now="$EPOCHREALTIME"
    duration_ms=$(( (now - _CLAI_PREEXEC_TS) * 1000 ))
    duration_ms=${duration_ms%.*}  # truncate to int
  fi

  # Get timestamp
  local ts_ms=$(( EPOCHREALTIME * 1000 ))
  ts_ms=${ts_ms%.*}

  # Determine ephemeral flag
  local ephemeral=0
  [[ -n "$CLAI_EPHEMERAL" ]] && ephemeral=1

  # Fire and forget
  CLAI_CMD="$_CLAI_LAST_CMD" \
  CLAI_CWD="$PWD" \
  CLAI_EXIT="$exit_code" \
  CLAI_TS="$ts_ms" \
  CLAI_DURATION_MS="$duration_ms" \
  CLAI_SHELL="zsh" \
  CLAI_SESSION_ID="$(_clai_get_session_id)" \
  CLAI_EPHEMERAL="$ephemeral" \
  clai-hook ingest 2>/dev/null &!

  # Clear for next command
  _CLAI_LAST_CMD=""
  _CLAI_PREEXEC_TS=""
}

# Cleanup on shell exit
_clai_cleanup() {
  local session_file="${XDG_RUNTIME_DIR:-/tmp}/clai/session.$$"
  [[ -f "$session_file" ]] && rm -f "$session_file"
}

# Register hooks
autoload -Uz add-zsh-hook
add-zsh-hook preexec _clai_preexec
add-zsh-hook precmd _clai_precmd
add-zsh-hook zshexit _clai_cleanup
