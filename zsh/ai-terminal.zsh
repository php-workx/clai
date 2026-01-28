# ai-terminal.zsh - AI Terminal Integration for ZSH
# Source this file in your .zshrc:
#   source ~/projects/ai-terminal/zsh/ai-terminal.zsh
#
# Features:
#   1. Auto-extract suggested commands from output (Tab to accept)
#   2. Auto-diagnose errors with Claude Code
#
# Configuration (set these BEFORE sourcing this file):
#   AI_TERMINAL_AUTO_DIAGNOSE=true   # Auto-diagnose on errors (default: true)
#   AI_TERMINAL_AUTO_EXTRACT=true    # Auto-extract commands (default: true)
#   AI_TERMINAL_BIN=~/projects/ai-terminal/bin  # Path to scripts

# ============================================
# Configuration
# ============================================

: ${AI_TERMINAL_AUTO_DIAGNOSE:=true}
: ${AI_TERMINAL_AUTO_EXTRACT:=true}
: ${AI_TERMINAL_BIN:="${0:A:h}/../bin"}  # Auto-detect relative to this file
: ${AI_TERMINAL_CACHE:="$HOME/.cache/ai-terminal"}

# Ensure cache directory exists
mkdir -p "$AI_TERMINAL_CACHE"

# Export for child scripts
export AI_TERMINAL_CACHE
export AI_TERMINAL_BIN

# Files
_AI_SUGGEST_FILE="$AI_TERMINAL_CACHE/suggestion"
_AI_LAST_OUTPUT="$AI_TERMINAL_CACHE/last_output"

# ============================================
# Feature 1: Command Suggestion
# ============================================

# Show suggestion hint in right prompt when available
_ai_update_rprompt() {
    if [[ -s "$_AI_SUGGEST_FILE" ]]; then
        local suggestion=$(cat "$_AI_SUGGEST_FILE")
        if [[ -n "$suggestion" ]]; then
            RPROMPT="%F{242}→ $suggestion%f"
        else
            RPROMPT=""
        fi
    else
        RPROMPT=""
    fi
}

# ZLE widget: Accept suggestion with Tab (when buffer is empty)
_ai_accept_suggestion() {
    if [[ -z "$BUFFER" && -s "$_AI_SUGGEST_FILE" ]]; then
        local suggestion=$(cat "$_AI_SUGGEST_FILE")
        if [[ -n "$suggestion" ]]; then
            BUFFER="$suggestion"
            CURSOR=${#BUFFER}
            # Clear the suggestion after accepting
            > "$_AI_SUGGEST_FILE"
            RPROMPT=""
            zle redisplay
            return
        fi
    fi
    # Default behavior: normal tab completion
    zle expand-or-complete
}
zle -N _ai_accept_suggestion

# ZLE widget: Clear suggestion with Escape
_ai_clear_suggestion() {
    > "$_AI_SUGGEST_FILE"
    RPROMPT=""
    zle redisplay
}
zle -N _ai_clear_suggestion

# Bind keys
bindkey '^I' _ai_accept_suggestion    # Tab
bindkey '^[' _ai_clear_suggestion     # Escape

# ============================================
# Feature 2: Auto Error Diagnosis
# ============================================

# Track command before execution
_ai_preexec() {
    _AI_LAST_CMD="$1"
    _AI_CMD_START=$EPOCHSECONDS
}

# Check result after execution
_ai_precmd() {
    local exit_code=$?
    
    # Update prompt with any suggestions
    _ai_update_rprompt
    
    # Auto-diagnose if enabled and command failed
    if [[ "$AI_TERMINAL_AUTO_DIAGNOSE" == "true" && 
          $exit_code -ne 0 && 
          -n "$_AI_LAST_CMD" &&
          "$_AI_LAST_CMD" != "ai-fix" &&
          "$_AI_LAST_CMD" != _ai_* ]]; then
        
        echo ""
        echo -e "\033[38;5;214m⚡ Analyzing error...\033[0m"
        
        # Run diagnosis (in foreground so user sees it)
        "$AI_TERMINAL_BIN/ai-diagnose" "$_AI_LAST_CMD" "$exit_code" 2>/dev/null
    fi
    
    # Cleanup
    unset _AI_LAST_CMD _AI_CMD_START
}

# Register hooks
autoload -U add-zsh-hook
add-zsh-hook preexec _ai_preexec
add-zsh-hook precmd _ai_precmd

# ============================================
# Output Capture (via wrapper function)
# ============================================

# Wrap command execution to capture output
# Usage: run <command> - captures output and extracts suggestions
run() {
    # Run command, capture output, pass through ai-extract
    "$@" 2>&1 | "$AI_TERMINAL_BIN/ai-extract"
    return ${pipestatus[1]}  # Return original command's exit code
}

# Alternative: alias common commands to auto-capture
# Uncomment if you want automatic capture for specific commands:
# alias pip='run pip'
# alias npm='run npm'
# alias brew='run brew'

# ============================================
# Manual Commands
# ============================================

# Manually diagnose last command
ai-fix() {
    local cmd="${1:-$(fc -ln -1)}"
    "$AI_TERMINAL_BIN/ai-diagnose" "$cmd" "1"
}

# Ask Claude anything with terminal context
ai() {
    if [[ -z "$1" ]]; then
        echo "Usage: ai \"your question\""
        return 1
    fi
    
    local context="Working directory: $(pwd)
Shell: $SHELL
Recent commands:
$(fc -ln -5 2>/dev/null)

Question: $*"
    
    echo "$context" | claude --print
}

# Toggle auto-diagnose
ai-toggle() {
    if [[ "$AI_TERMINAL_AUTO_DIAGNOSE" == "true" ]]; then
        export AI_TERMINAL_AUTO_DIAGNOSE=false
        echo "Auto-diagnose: OFF"
    else
        export AI_TERMINAL_AUTO_DIAGNOSE=true
        echo "Auto-diagnose: ON"
    fi
}

# ============================================
# Startup Message
# ============================================

if [[ -o interactive ]]; then
    echo -e "\033[2m🤖 AI Terminal loaded. Commands: ai-fix, ai, ai-toggle, run\033[0m"
fi
