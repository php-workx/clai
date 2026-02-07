#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(git rev-parse --show-toplevel)"
SKILL_NAME="sonarqube"

SRC_CLAUDE="$REPO_ROOT/.agents/skills/$SKILL_NAME"
SRC_CODEX="$REPO_ROOT/.codex/skills/$SKILL_NAME"

if [[ ! -d "$SRC_CLAUDE" ]]; then
  echo "error: missing source Claude skill at $SRC_CLAUDE" >&2
  exit 1
fi
if [[ ! -d "$SRC_CODEX" ]]; then
  echo "error: missing source Codex skill at $SRC_CODEX" >&2
  exit 1
fi

if [[ -d "$HOME/.claude/skills" ]]; then
  DEST_CLAUDE_BASE="${CLAUDE_SKILLS_DIR:-$HOME/.claude/skills}"
else
  DEST_CLAUDE_BASE="${CLAUDE_SKILLS_DIR:-$HOME/.agents/skills}"
fi
DEST_CODEX_BASE="${CODEX_SKILLS_DIR:-$HOME/.codex/skills}"
DEST_CODEX_PROMPTS_BASE="${CODEX_PROMPTS_DIR:-$HOME/.codex/prompts}"

DEST_CLAUDE="$DEST_CLAUDE_BASE/$SKILL_NAME"
DEST_CODEX="$DEST_CODEX_BASE/$SKILL_NAME"

mkdir -p "$DEST_CLAUDE_BASE" "$DEST_CODEX_BASE"
mkdir -p "$DEST_CODEX_PROMPTS_BASE"

# Remove deprecated alias if present.
rm -rf "$DEST_CLAUDE_BASE/sonarqube-autofix" "$DEST_CODEX_BASE/sonarqube-autofix"

rm -rf "$DEST_CLAUDE" "$DEST_CODEX"
cp -R "$SRC_CLAUDE" "$DEST_CLAUDE"
cp -R "$SRC_CODEX" "$DEST_CODEX"

chmod +x "$DEST_CLAUDE/scripts/run_changed_scan.sh" \
  "$DEST_CLAUDE/scripts/collect_changed_issues.py" \
  "$DEST_CODEX/scripts/run_changed_scan.sh" \
  "$DEST_CODEX/scripts/collect_changed_issues.py"

PROMPT_SRC="$REPO_ROOT/.codex/prompts/sonarqube.md"
if [[ -f "$PROMPT_SRC" ]]; then
  cp "$PROMPT_SRC" "$DEST_CODEX_PROMPTS_BASE/sonarqube.md"
fi

cat <<EOF
Installed $SKILL_NAME:
- Claude: $DEST_CLAUDE
- Codex:  $DEST_CODEX
Codex slash command prompt:
- $DEST_CODEX_PROMPTS_BASE/sonarqube.md
Removed deprecated alias (if it existed): sonarqube-autofix
EOF
