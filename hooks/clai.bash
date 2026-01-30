# clai.bash - clai shell integration for Bash
# Source this from your .bashrc or install via: clai install
#
# Features:
#   1. Command lifecycle logging (DEBUG trap + PROMPT_COMMAND)
#   2. Voice mode with ` prefix
#   3. Error diagnosis via `run` wrapper
#
# Configuration (set BEFORE sourcing):
#   CLAI_AUTO_DAEMON=true     # Auto-start daemon (default: true)
#   CLAI_AUTO_DIAGNOSE=false  # Auto-diagnose on failure (default: false)

# ============================================
# Configuration
# ============================================

: ${CLAI_AUTO_DAEMON:=true}
: ${CLAI_AUTO_DIAGNOSE:=false}
: ${CLAI_CACHE:="$HOME/.cache/clai"}

# Ensure cache directory exists
mkdir -p "$CLAI_CACHE"
export CLAI_CACHE

# Generate session ID once per shell instance
if [[ -z "$CLAI_SESSION_ID" ]]; then
    CLAI_SESSION_ID=$(uuidgen 2>/dev/null || cat /proc/sys/kernel/random/uuid 2>/dev/null || echo "$$-$RANDOM")
    CLAI_SESSION_ID="${CLAI_SESSION_ID,,}"  # Lowercase
    export CLAI_SESSION_ID
fi

# Files
_CLAI_SUGGEST_FILE="$CLAI_CACHE/suggestion"
_CLAI_LAST_OUTPUT="$CLAI_CACHE/last_output"

# ============================================
# Session Lifecycle
# ============================================

# Session start (runs once when shell starts)
__clai_session_start() {
    command clai-shim session-start \
        --session-id="$CLAI_SESSION_ID" \
        --cwd="$PWD" \
        --shell=bash &
    disown 2>/dev/null
}

# Session end (on shell exit)
__clai_session_end() {
    command clai-shim session-end \
        --session-id="$CLAI_SESSION_ID" &
    disown 2>/dev/null
}

trap '__clai_session_end' EXIT

# ============================================
# Command Lifecycle Hooks
# ============================================

# Track command state
__CLAI_CMD_ID=""
__CLAI_CMD_START=0
__CLAI_LAST_CMD=""
__CLAI_IN_PRECMD=false

# Before command execution (DEBUG trap)
__clai_preexec() {
    # Skip if inside PROMPT_COMMAND
    [[ "$__CLAI_IN_PRECMD" == "true" ]] && return
    # Skip clai commands
    [[ "$BASH_COMMAND" == clai-shim* ]] && return
    [[ "$BASH_COMMAND" == "command clai"* ]] && return
    # Skip prompt command itself
    [[ "$BASH_COMMAND" == "$PROMPT_COMMAND" ]] && return
    [[ "$BASH_COMMAND" == __clai_precmd* ]] && return

    __CLAI_CMD_ID=$(uuidgen 2>/dev/null || cat /proc/sys/kernel/random/uuid 2>/dev/null || echo "$$-$RANDOM")
    __CLAI_CMD_ID="${__CLAI_CMD_ID,,}"
    __CLAI_CMD_START=$(date +%s%3N 2>/dev/null || echo $(($(date +%s) * 1000)))
    __CLAI_LAST_CMD="$BASH_COMMAND"

    command clai-shim log-start \
        --session-id="$CLAI_SESSION_ID" \
        --command-id="$__CLAI_CMD_ID" \
        --cwd="$PWD" \
        --command="$BASH_COMMAND" &
    disown 2>/dev/null
}

# After command execution (PROMPT_COMMAND)
__clai_precmd() {
    local exit_code=$?
    __CLAI_IN_PRECMD=true

    # Skip if no command was tracked
    if [[ -n "$__CLAI_CMD_ID" ]]; then
        local now=$(date +%s%3N 2>/dev/null || echo $(($(date +%s) * 1000)))
        local duration=$((now - __CLAI_CMD_START))

        command clai-shim log-end \
            --session-id="$CLAI_SESSION_ID" \
            --command-id="$__CLAI_CMD_ID" \
            --exit-code="$exit_code" \
            --duration="$duration" &
        disown 2>/dev/null

        # Auto-diagnose on failure if enabled
        if [[ "$CLAI_AUTO_DIAGNOSE" == "true" && $exit_code -ne 0 && -n "$__CLAI_LAST_CMD" ]]; then
            echo ""
            echo -e "\033[38;5;214m⚡ Analyzing error...\033[0m"
            command clai diagnose "$__CLAI_LAST_CMD" "$exit_code" 2>/dev/null
        fi

        # Clear tracking
        __CLAI_CMD_ID=""
        __CLAI_CMD_START=0
    fi

    # Show any cached suggestions
    _clai_show_suggestion

    __CLAI_IN_PRECMD=false
}

# ============================================
# Command Suggestions
# ============================================

_clai_show_suggestion() {
    if [[ -s "$_CLAI_SUGGEST_FILE" ]]; then
        local suggestion=$(cat "$_CLAI_SUGGEST_FILE")
        if [[ -n "$suggestion" ]]; then
            echo -e "\033[38;5;242m→ $suggestion (use 'accept' to run)\033[0m"
        fi
    fi
}

# Accept suggestion command
accept() {
    if [[ -s "$_CLAI_SUGGEST_FILE" ]]; then
        local suggestion=$(cat "$_CLAI_SUGGEST_FILE")
        if [[ -n "$suggestion" ]]; then
            # Clear suggestion
            > "$_CLAI_SUGGEST_FILE"
            # Execute the suggestion
            echo -e "\033[2mRunning: $suggestion\033[0m"
            eval "$suggestion"
            return $?
        fi
    fi
    echo "No suggestion available"
    return 1
}

# Clear suggestion
clear-suggestion() {
    > "$_CLAI_SUGGEST_FILE"
    echo "Suggestion cleared"
}

# ============================================
# Voice Mode
# ============================================

# Check for ` prefix and convert voice input
_clai_check_voice_prefix() {
    local cmd="$1"
    if [[ "$cmd" == '`'* && ${#cmd} -gt 1 ]]; then
        local voice_input="${cmd#\`}"
        voice_input="${voice_input## }"

        echo "🎤 Converting: $voice_input"
        local result=$(command clai voice "$voice_input" 2>/dev/null)
        if [[ -n "$result" ]]; then
            echo "→ $result"
            echo "$result" > "$_CLAI_SUGGEST_FILE"
            echo -e "\033[2mUse 'accept' to run, or copy the command above\033[0m"
        fi
        return 0
    fi
    return 1
}

# DEBUG trap for voice prefix
_clai_debug_trap() {
    # Check for voice prefix
    if _clai_check_voice_prefix "$BASH_COMMAND"; then
        return 1  # Prevent original command (requires extdebug)
    fi
    # Normal preexec
    __clai_preexec
    return 0
}

# Enable extdebug for voice prefix blocking
shopt -s extdebug
trap '_clai_debug_trap' DEBUG

# ============================================
# Output Capture Wrapper
# ============================================

# Wrap command to capture output and auto-diagnose
run() {
    "$@" 2>&1 | tee "$_CLAI_LAST_OUTPUT" | command clai extract
    local exit_code=${PIPESTATUS[0]}

    if [[ $exit_code -ne 0 ]]; then
        echo ""
        echo -e "\033[38;5;214m⚡ Analyzing error...\033[0m"
        command clai diagnose "$*" "$exit_code" 2>/dev/null
    fi

    return $exit_code
}

# ============================================
# Helper Commands
# ============================================

# Diagnose last command
ai-fix() {
    local cmd="${1:-$(history 1 | sed 's/^[ ]*[0-9]*[ ]*//')}"
    command clai diagnose "$cmd" "1"
}

# Ask Claude with context
ai() {
    if [[ -z "$1" ]]; then
        echo "Usage: ai \"your question\""
        return 1
    fi
    local recent_cmds=$(history 5 | sed 's/^[ ]*[0-9]*[ ]*//' | tr '\n' ';')
    command clai ask --context "$recent_cmds" "$@"
}

# Voice to command
voice() {
    if [[ -z "$1" ]]; then
        echo "Usage: voice \"natural language description\""
        return 1
    fi
    command clai voice "$@"
}

# Daemon control
ai-daemon() {
    case "$1" in
        start)   command clai daemon start ;;
        stop)    command clai daemon stop ;;
        status)  command clai daemon status ;;
        restart) command clai daemon stop && command clai daemon start ;;
        *)
            echo "Usage: ai-daemon {start|stop|status|restart}"
            ;;
    esac
}

# ============================================
# Initialization
# ============================================

# Append to PROMPT_COMMAND
if [[ -z "$PROMPT_COMMAND" ]]; then
    PROMPT_COMMAND="__clai_precmd"
else
    PROMPT_COMMAND="__clai_precmd; $PROMPT_COMMAND"
fi

# Start session
if [[ $- == *i* ]]; then
    __clai_session_start

    # Start daemon if enabled
    if [[ "$CLAI_AUTO_DAEMON" == "true" ]]; then
        (command clai daemon start >/dev/null 2>&1 &)
    fi

    echo -e "\033[2m🤖 clai loaded. Commands: ai-fix, ai, voice, run, accept | \` prefix for voice mode\033[0m"
fi
