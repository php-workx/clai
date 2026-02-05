# clai.fish - clai suggestions engine v3 hook for Fish
# This hook sends command events to the daemon for ingestion.
# See spec Section 17.3 for details.

# Guard: only in interactive shells
status is-interactive; or exit 0

# Guard: don't re-source
set -q _CLAI_V3_LOADED; and exit 0
set -g _CLAI_V3_LOADED 1

# Check clai-hook exists
command -q clai-hook; or exit 0

# Session ID
set -g _CLAI_SESSION_ID ""

function _clai_sha256
  if command -q shasum
    shasum -a 256
  else if command -q sha256sum
    sha256sum
  else
    cat  # fallback
  end
end

function _clai_get_session_id
  if test -z "$_CLAI_SESSION_ID"
    set -l session_file (test -n "$XDG_RUNTIME_DIR"; and echo "$XDG_RUNTIME_DIR"; or echo "/tmp")"/clai/session."(echo %self)
    if test -f "$session_file"
      set _CLAI_SESSION_ID (cat "$session_file")
    else
      # Fallback: generate locally
      set _CLAI_SESSION_ID (printf '%s-%s-%s' (hostname) %self (date +%s) | _clai_sha256 | cut -c1-16)
    end
  end
  echo $_CLAI_SESSION_ID
end

function _clai_postexec --on-event fish_postexec
  set -l exit_code $status
  set -l cmd $argv[1]

  # Skip if recording disabled
  test -n "$CLAI_NO_RECORD"; and return

  # Skip clai-hook commands
  string match -q "clai-hook*" -- "$cmd"; and return

  # CMD_DURATION is in milliseconds
  set -l duration_ms $CMD_DURATION

  # Timestamp
  set -l ts_ms (date +%s)000

  # Ephemeral flag
  set -l ephemeral 0
  test -n "$CLAI_EPHEMERAL"; and set ephemeral 1

  # Get session ID (must evaluate before env call)
  set -l session_id (_clai_get_session_id)

  # Fire and forget (fish uses env for inline var assignment)
  env CLAI_CMD="$cmd" \
      CLAI_CWD="$PWD" \
      CLAI_EXIT="$exit_code" \
      CLAI_TS="$ts_ms" \
      CLAI_DURATION_MS="$duration_ms" \
      CLAI_SHELL="fish" \
      CLAI_SESSION_ID="$session_id" \
      CLAI_EPHEMERAL="$ephemeral" \
      clai-hook ingest 2>/dev/null &
  disown 2>/dev/null
end

# Cleanup on shell exit
function _clai_cleanup --on-event fish_exit
  set -l session_file (test -n "$XDG_RUNTIME_DIR"; and echo "$XDG_RUNTIME_DIR"; or echo "/tmp")"/clai/session."(echo %self)
  test -f "$session_file"; and rm -f "$session_file"
end
