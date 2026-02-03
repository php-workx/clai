#!/bin/bash
# install.sh - Set up clai shell integration
# Run: ./install.sh

set -e

# Colors
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
CYAN='\033[0;36m'
RED='\033[0;31m'
DIM='\033[2m'
RESET='\033[0m'

echo -e "${CYAN}🤖 clai Installer${RESET}"
echo ""

# Get the directory where this script is located
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

# Check prerequisites
echo -e "${YELLOW}Checking prerequisites...${RESET}"

# Check for Go
if ! command -v go &> /dev/null; then
    echo -e "${RED}Error: Go not found. Install from: https://go.dev/dl/${RESET}" >&2
    exit 1
fi
echo "  ✓ Go found: $(go version | cut -d' ' -f3)"

# Check for Claude CLI (warn but don't fail)
if ! command -v claude &> /dev/null; then
    echo -e "${YELLOW}  ⚠ Claude CLI not found. Install it from: https://docs.anthropic.com/en/docs/claude-code${RESET}"
else
    echo "  ✓ Claude CLI found"
fi

echo ""

# Build and install the binary
echo -e "${YELLOW}Building clai binary...${RESET}"
cd "$SCRIPT_DIR"

if command -v make &> /dev/null; then
    make install
else
    go install ./cmd/clai
fi

# Verify installation
if ! command -v clai &> /dev/null; then
    echo -e "${RED}Warning: clai not found in PATH${RESET}"
    echo -e "${DIM}Make sure \$GOPATH/bin or \$HOME/go/bin is in your PATH${RESET}"
fi

echo "  ✓ Binary installed"

echo ""

# Detect shell and configure
detect_shell() {
    local shell_name
    shell_name=$(basename "$SHELL")
    echo "$shell_name"
    return 0
}

CURRENT_SHELL=$(detect_shell)
echo -e "${YELLOW}Detected shell: ${CURRENT_SHELL}${RESET}"

case "$CURRENT_SHELL" in
    zsh)
        RC_FILE="$HOME/.zshrc"
        INIT_LINE='eval "$(clai init zsh)"'
        ;;
    bash)
        if [[ -f "$HOME/.bashrc" ]]; then
            RC_FILE="$HOME/.bashrc"
        else
            RC_FILE="$HOME/.bash_profile"
        fi
        INIT_LINE='eval "$(clai init bash)"'
        ;;
    fish)
        RC_FILE="$HOME/.config/fish/config.fish"
        INIT_LINE='clai init fish | source'
        ;;
    *)
        echo -e "${YELLOW}Unsupported shell: $CURRENT_SHELL${RESET}"
        echo "Supported shells: zsh, bash, fish"
        echo ""
        echo "Add one of these to your shell config manually:"
        echo '  eval "$(clai init zsh)"   # for zsh'
        echo '  eval "$(clai init bash)"  # for bash'
        echo '  clai init fish | source   # for fish'
        exit 0
        ;;
esac

echo ""

# Check if already installed
if grep -q "clai init" "$RC_FILE" 2>/dev/null; then
    echo -e "${GREEN}Already configured in $RC_FILE${RESET}"
else
    # Ask to add to rc file
    echo -e "${YELLOW}Add to $RC_FILE?${RESET}"
    echo "This will add: $INIT_LINE"
    echo ""
    read -p "Proceed? [Y/n] " -n 1 -r
    echo ""

    if [[ $REPLY =~ ^[Yy]$ ]] || [[ -z $REPLY ]]; then
        # Ensure directory exists for fish
        mkdir -p "$(dirname "$RC_FILE")"

        # Add to rc file
        echo "" >> "$RC_FILE"
        echo "# clai Shell Integration" >> "$RC_FILE"
        echo "$INIT_LINE" >> "$RC_FILE"
        echo -e "${GREEN}  ✓ Added to $RC_FILE${RESET}"
    else
        echo "Skipped. Add this manually to your shell config:"
        echo "  $INIT_LINE"
    fi
fi

echo ""

# Create cache directory
mkdir -p "$HOME/.cache/clai"
echo -e "${GREEN}  ✓ Created cache directory${RESET}"

echo ""
echo -e "${GREEN}✅ Installation complete!${RESET}"
echo ""
echo "Next steps:"
echo "  1. Restart your terminal or run: source $RC_FILE"
echo "  2. Try: run echo 'Install with: \`brew install wget\`'"
if [[ "$CURRENT_SHELL" == "zsh" ]]; then
    echo "  3. Press Tab to accept the suggestion"
elif [[ "$CURRENT_SHELL" == "fish" ]]; then
    echo "  3. Press Alt+Enter to accept the suggestion"
else
    echo "  3. Type 'accept' to run the suggestion"
fi
echo ""
echo "Commands:"
echo "  ai-fix     - Diagnose last failed command"
echo "  ai \"...\"   - Ask Claude anything"
echo "  ai-toggle  - Toggle auto-diagnosis on/off"
echo "  run <cmd>  - Run with output capture"
echo ""
