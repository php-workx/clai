#!/bin/bash
# install.sh - Set up AI Terminal integration
# Run: ./install.sh

set -e

# Colors
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
CYAN='\033[0;36m'
RED='\033[0;31m'
RESET='\033[0m'

echo -e "${CYAN}🤖 AI Terminal Installer${RESET}"
echo ""

# Get the directory where this script is located
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
BIN_DIR="$SCRIPT_DIR/bin"
ZSH_FILE="$SCRIPT_DIR/zsh/ai-terminal.zsh"
ZSHRC="$HOME/.zshrc"

# Check prerequisites
echo -e "${YELLOW}Checking prerequisites...${RESET}"

# Check for ZSH
if [[ ! -x "$(command -v zsh)" ]]; then
    echo -e "${RED}Error: ZSH not found. This integration requires ZSH.${RESET}"
    exit 1
fi
echo "  ✓ ZSH found"

# Check for Claude CLI (warn but don't fail)
if ! command -v claude &> /dev/null; then
    echo -e "${YELLOW}  ⚠ Claude CLI not found. Install it from: https://docs.anthropic.com/en/docs/claude-code${RESET}"
else
    echo "  ✓ Claude CLI found"
fi

echo ""

# Make scripts executable
echo -e "${YELLOW}Making scripts executable...${RESET}"
chmod +x "$BIN_DIR"/*
echo "  ✓ Done"

echo ""

# Check if already installed
if grep -q "ai-terminal.zsh" "$ZSHRC" 2>/dev/null; then
    echo -e "${GREEN}Already installed in $ZSHRC${RESET}"
    echo ""
    echo "To update, just pull/copy new files to: $SCRIPT_DIR"
else
    # Ask to add to .zshrc
    echo -e "${YELLOW}Add to $ZSHRC?${RESET}"
    echo "This will add: source $ZSH_FILE"
    echo ""
    read -p "Proceed? [Y/n] " -n 1 -r
    echo ""
    
    if [[ $REPLY =~ ^[Yy]$ ]] || [[ -z $REPLY ]]; then
        # Add to .zshrc
        echo "" >> "$ZSHRC"
        echo "# AI Terminal Integration" >> "$ZSHRC"
        echo "source $ZSH_FILE" >> "$ZSHRC"
        echo -e "${GREEN}  ✓ Added to $ZSHRC${RESET}"
    else
        echo "Skipped. Add this manually to your .zshrc:"
        echo "  source $ZSH_FILE"
    fi
fi

echo ""

# Create cache directory
mkdir -p "$HOME/.cache/ai-terminal"
echo -e "${GREEN}  ✓ Created cache directory${RESET}"

echo ""
echo -e "${GREEN}✅ Installation complete!${RESET}"
echo ""
echo "Next steps:"
echo "  1. Restart your terminal or run: source ~/.zshrc"
echo "  2. Try: run echo 'Install with: \`brew install wget\`'"
echo "  3. Press Tab to accept the suggestion"
echo ""
echo "Commands:"
echo "  ai-fix     - Diagnose last failed command"
echo "  ai \"...\"   - Ask Claude anything"
echo "  ai-toggle  - Toggle auto-diagnosis on/off"
echo "  run <cmd>  - Run with output capture"
echo ""
