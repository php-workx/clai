# clai.zsh - clai shell integration for Zsh
# Source this from your .zshrc or install via: clai install
#
# Features:
#   1. Command lifecycle logging (preexec/precmd)
#   2. Inline command suggestions with right-arrow to accept
#   3. Voice mode with ` prefix
#   4. Error diagnosis via `run` wrapper
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
export CLAI_SESSION_ID="${CLAI_SESSION_ID:-$(uuidgen 2>/dev/null || cat /proc/sys/kernel/random/uuid 2>/dev/null || echo $$-$RANDOM)}"
CLAI_SESSION_ID="${CLAI_SESSION_ID:l}"  # Lowercase

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
        --shell=zsh &!
}

# Session end (on shell exit)
__clai_session_end() {
    command clai-shim session-end \
        --session-id="$CLAI_SESSION_ID" &!
}

# ============================================
# Command Lifecycle Hooks
# ============================================

# Track command state
typeset -g __CLAI_CMD_ID=""
typeset -g __CLAI_CMD_START=0
typeset -g __CLAI_LAST_CMD=""

# Before command execution (preexec)
__clai_preexec() {
    __CLAI_CMD_ID=$(uuidgen 2>/dev/null || cat /proc/sys/kernel/random/uuid 2>/dev/null || echo $$-$RANDOM)
    __CLAI_CMD_ID="${__CLAI_CMD_ID:l}"
    __CLAI_CMD_START=$(($(date +%s) * 1000 + $(date +%N 2>/dev/null | cut -c1-3 || echo 0)))
    __CLAI_LAST_CMD="$1"

    command clai-shim log-start \
        --session-id="$CLAI_SESSION_ID" \
        --command-id="$__CLAI_CMD_ID" \
        --cwd="$PWD" \
        --command="$1" &!
}

# After command execution (precmd)
__clai_precmd() {
    local exit_code=$?

    # Skip if no command was run
    [[ -z "$__CLAI_CMD_ID" ]] && return

    local now=$(($(date +%s) * 1000 + $(date +%N 2>/dev/null | cut -c1-3 || echo 0)))
    local duration=$((now - __CLAI_CMD_START))

    command clai-shim log-end \
        --session-id="$CLAI_SESSION_ID" \
        --command-id="$__CLAI_CMD_ID" \
        --exit-code="$exit_code" \
        --duration="$duration" &!

    # Auto-diagnose on failure if enabled
    if [[ "$CLAI_AUTO_DIAGNOSE" == "true" && $exit_code -ne 0 && -n "$__CLAI_LAST_CMD" ]]; then
        echo ""
        echo -e "\033[38;5;214m⚡ Analyzing error...\033[0m"
        command clai diagnose "$__CLAI_LAST_CMD" "$exit_code" 2>/dev/null
    fi

    # Update suggestion display
    _clai_update_suggestion

    # Clear tracking
    __CLAI_CMD_ID=""
    __CLAI_CMD_START=0
}

# ============================================
# Command Suggestions
# ============================================

# Current suggestion state
typeset -g _CLAI_CURRENT_SUGGESTION=""

# Update suggestion based on current buffer
_clai_update_suggestion() {
    local suggestion=""

    if [[ -z "$BUFFER" ]]; then
        # Empty buffer - check for AI suggestion from cache
        suggestion=$(command clai suggest 2>/dev/null)
    else
        # Has content - search history
        suggestion=$(command clai suggest "$BUFFER" 2>/dev/null)
    fi

    _CLAI_CURRENT_SUGGESTION="$suggestion"

    # Show suggestion as ghost text after cursor
    if [[ -n "$suggestion" && "$suggestion" != "$BUFFER" ]]; then
        if [[ -z "$BUFFER" ]]; then
            POSTDISPLAY="${suggestion}"
        elif [[ "$suggestion" == "$BUFFER"* ]]; then
            # Only show suffix if suggestion starts with current buffer
            POSTDISPLAY="${suggestion#$BUFFER}"
        else
            # Suggestion doesn't start with buffer, don't show suffix
            POSTDISPLAY=""
        fi
    else
        POSTDISPLAY=""
    fi
}

# ZLE widget: Update suggestion after each character
_clai_self_insert() {
    zle .self-insert
    _clai_update_suggestion
}
zle -N self-insert _clai_self_insert

# ZLE widget: Update suggestion after backspace
_clai_backward_delete_char() {
    zle .backward-delete-char
    _clai_update_suggestion
}
zle -N backward-delete-char _clai_backward_delete_char

# ZLE widget: Accept suggestion with right arrow
_clai_forward_char() {
    if [[ -n "$POSTDISPLAY" && $CURSOR -eq ${#BUFFER} ]]; then
        # At end of buffer with suggestion - accept it
        BUFFER="$_CLAI_CURRENT_SUGGESTION"
        CURSOR=${#BUFFER}
        POSTDISPLAY=""
        _CLAI_CURRENT_SUGGESTION=""
        # Clear AI suggestion file
        > "$_CLAI_SUGGEST_FILE"
    else
        # Normal forward char
        zle .forward-char
    fi
}
zle -N forward-char _clai_forward_char

# ZLE widget: Clear suggestion with Escape
_clai_clear_suggestion() {
    POSTDISPLAY=""
    _CLAI_CURRENT_SUGGESTION=""
    > "$_CLAI_SUGGEST_FILE"
    zle redisplay
}
zle -N _clai_clear_suggestion

# ============================================
# Voice Mode
# ============================================

typeset -g _CLAI_VOICE_MODE=false

# ZLE widget: Enter voice mode
_clai_enter_voice_mode() {
    _CLAI_VOICE_MODE=true
    POSTDISPLAY=" 🎤 Voice mode"
    zle redisplay
}
zle -N _clai_enter_voice_mode

# ZLE widget: Execute with voice conversion
_clai_voice_accept_line() {
    # Check for ` prefix (voice input marker)
    if [[ "$BUFFER" == '`'* && ${#BUFFER} -gt 1 ]]; then
        local voice_input="${BUFFER#\`}"
        voice_input="${voice_input## }"
        BUFFER=""
        zle redisplay

        echo ""
        echo "🎤 Converting: $voice_input"
        local cmd=$(command clai voice "$voice_input" 2>/dev/null)
        if [[ -n "$cmd" ]]; then
            echo "→ $cmd"
        fi
        zle reset-prompt
        return
    fi

    # Check for explicit voice mode
    if [[ "$_CLAI_VOICE_MODE" == "true" && -n "$BUFFER" ]]; then
        _CLAI_VOICE_MODE=false
        POSTDISPLAY=""
        local voice_input="$BUFFER"
        BUFFER=""
        zle redisplay

        echo ""
        echo "🎤 Converting: $voice_input"
        local cmd=$(command clai voice "$voice_input" 2>/dev/null)
        if [[ -n "$cmd" ]]; then
            echo "→ $cmd"
        fi
        zle reset-prompt
        return
    fi

    _CLAI_VOICE_MODE=false
    zle accept-line
}
zle -N _clai_voice_accept_line

# ZLE widget: Cancel voice mode
_clai_cancel_voice_mode() {
    if [[ "$_CLAI_VOICE_MODE" == "true" ]]; then
        _CLAI_VOICE_MODE=false
    fi
    _clai_clear_suggestion
}
zle -N _clai_cancel_voice_mode

# Key bindings
bindkey '^M' _clai_voice_accept_line    # Enter
bindkey '^X^V' _clai_enter_voice_mode   # Ctrl+X Ctrl+V
bindkey '^[' _clai_cancel_voice_mode    # Escape

# ============================================
# Output Capture Wrapper
# ============================================

# Wrap command to capture output and auto-diagnose
run() {
    "$@" 2>&1 | tee "$_CLAI_LAST_OUTPUT" | command clai extract
    local exit_code=${pipestatus[1]}

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
    local cmd="${1:-$(fc -ln -1)}"
    command clai diagnose "$cmd" "1"
}

# Ask Claude with context
ai() {
    if [[ -z "$1" ]]; then
        echo "Usage: ai \"your question\""
        return 1
    fi
    local recent_cmds=$(fc -ln -5 2>/dev/null | tr '\n' ';')
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

# Register hooks
autoload -U add-zsh-hook
add-zsh-hook preexec __clai_preexec
add-zsh-hook precmd __clai_precmd
add-zsh-hook zshexit __clai_session_end

# Start session
if [[ -o interactive ]]; then
    __clai_session_start

    # Start daemon if enabled
    if [[ "$CLAI_AUTO_DAEMON" == "true" ]]; then
        (command clai daemon start >/dev/null 2>&1 &)
    fi

    echo -e "\033[2m🤖 clai loaded. Commands: ai-fix, ai, voice, run | \` prefix for voice mode\033[0m"
fi
