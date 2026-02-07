#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(git rev-parse --show-toplevel)"
SKILL_NAMES=("sonarqube" "sonarqube-autofix")

for SKILL_NAME in "${SKILL_NAMES[@]}"; do
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
done

if [[ -d "$HOME/.claude/skills" ]]; then
  DEST_CLAUDE_BASE="${CLAUDE_SKILLS_DIR:-$HOME/.claude/skills}"
else
  DEST_CLAUDE_BASE="${CLAUDE_SKILLS_DIR:-$HOME/.agents/skills}"
fi
DEST_CODEX_BASE="${CODEX_SKILLS_DIR:-$HOME/.codex/skills}"

mkdir -p "$DEST_CLAUDE_BASE" "$DEST_CODEX_BASE"

for SKILL_NAME in "${SKILL_NAMES[@]}"; do
  SRC_CLAUDE="$REPO_ROOT/.agents/skills/$SKILL_NAME"
  SRC_CODEX="$REPO_ROOT/.codex/skills/$SKILL_NAME"
  DEST_CLAUDE="$DEST_CLAUDE_BASE/$SKILL_NAME"
  DEST_CODEX="$DEST_CODEX_BASE/$SKILL_NAME"

  rm -rf "$DEST_CLAUDE" "$DEST_CODEX"
  cp -R "$SRC_CLAUDE" "$DEST_CLAUDE"
  cp -R "$SRC_CODEX" "$DEST_CODEX"

  chmod +x "$DEST_CLAUDE/scripts/run_changed_scan.sh" \
    "$DEST_CLAUDE/scripts/collect_changed_issues.py" \
    "$DEST_CODEX/scripts/run_changed_scan.sh" \
    "$DEST_CODEX/scripts/collect_changed_issues.py"

  cat <<EOF
Installed $SKILL_NAME:
- Claude: $DEST_CLAUDE
- Codex:  $DEST_CODEX
EOF
done
