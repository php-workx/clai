#!/usr/bin/env bash
# Mechanical spec cross-reference checker
#
# Usage: ./scripts/spec-cross-reference.sh <spec-file>
#
# Extracts references from spec and verifies they exist:
# - File paths (*.go, *.py, *.ts, etc.)
# - Code references (backtick-quoted identifiers)
# - Link targets (markdown links)
#
# Output: Markdown table showing what exists vs what's missing

set -euo pipefail

SPEC_FILE="${1:-}"

if [[ -z "$SPEC_FILE" ]]; then
    echo "Usage: $0 <spec-file>"
    echo ""
    echo "Extracts file references from spec and verifies they exist."
    echo ""
    echo "Example:"
    echo "  $0 .agents/specs/my-feature.md"
    exit 1
fi

if [[ ! -f "$SPEC_FILE" ]]; then
    echo "Error: Spec file not found: $SPEC_FILE"
    exit 1
fi

echo "## Cross-Reference Check: $SPEC_FILE"
echo ""
echo "**Generated:** $(date -u +%Y-%m-%dT%H:%M:%SZ)"
echo ""

# ============================================================================
# FILE REFERENCES
# ============================================================================
echo "### File References"
echo ""
echo "| Path | Exists? | Line in Spec |"
echo "|------|---------|--------------|"

# Directory of the spec file, for resolving relative paths
SPEC_DIR="$(cd "$(dirname "$SPEC_FILE")" && pwd)"

# Extract file paths with common extensions
grep -noE '[a-zA-Z0-9_/.=-]+\.(go|py|ts|tsx|js|jsx|sh|md|yaml|yml|json|toml)' "$SPEC_FILE" 2>/dev/null | while IFS=: read -r line_num path; do
    # Skip if it's a URL
    if [[ "$path" == http* ]]; then
        continue
    fi

    # Check from CWD first, then relative to the spec file's directory
    if [[ -f "$path" ]] || [[ -f "$SPEC_DIR/$path" ]]; then
        printf '| `%s` | YES | %s |\n' "$path" "$line_num"
    else
        printf '| `%s` | **NO** | %s |\n' "$path" "$line_num"
    fi
done || true

echo ""

# ============================================================================
# CODE REFERENCES (backtick-quoted identifiers)
# ============================================================================
echo "### Code References"
echo ""
echo "Checking for definitions of backtick-quoted identifiers:"
echo ""

# Extract backtick-quoted identifiers using a temp file approach
# to avoid shell quoting issues with backticks
TEMP_REFS=$(mktemp)
# shellcheck disable=SC2064
trap "rm -f $TEMP_REFS" EXIT

# Use sed to extract content between backticks that looks like identifiers
sed -n 's/.*`\([A-Z][a-zA-Z0-9_]*\)`.*/\1/p' "$SPEC_FILE" | sort -u > "$TEMP_REFS"
sed -n 's/.*`\([a-z]*[A-Z][a-zA-Z0-9_]*\)`.*/\1/p' "$SPEC_FILE" | sort -u >> "$TEMP_REFS"
sort -u "$TEMP_REFS" -o "$TEMP_REFS"

if [[ ! -s "$TEMP_REFS" ]]; then
    echo "_No code references found (no backtick-quoted identifiers)_"
else
    echo "| Reference | Found? | Location |"
    echo "|-----------|--------|----------|"

    while read -r name; do
        [[ -z "$name" ]] && continue

        # Search for common definition patterns
        location=$(grep -rn \
            -e "func $name" \
            -e "func ($name" \
            -e "type $name " \
            -e "class $name" \
            -e "def $name" \
            -e "const $name " \
            -e "interface $name" \
            --include="*.go" --include="*.py" --include="*.ts" --include="*.js" \
            . 2>/dev/null | head -1 || true)

        if [[ -n "$location" ]]; then
            # Extract just file:line
            file_line=$(echo "$location" | cut -d: -f1-2)
            printf '| `%s` | YES | %s |\n' "$name" "$file_line"
        else
            printf '| `%s` | **NO** | - |\n' "$name"
        fi
    done < "$TEMP_REFS"
fi

echo ""

# ============================================================================
# MARKDOWN LINKS
# ============================================================================
echo "### Markdown Link Targets"
echo ""

# Extract markdown link targets [text](path)
links=$(grep -oE '\]\([^)]+\)' "$SPEC_FILE" 2>/dev/null | sed 's/\](//' | sed 's/)//' | grep -v '^http' | sort -u || true)

if [[ -z "$links" ]]; then
    echo "_No local markdown links found_"
else
    echo "| Link Target | Exists? |"
    echo "|-------------|---------|"

    echo "$links" | while read -r link; do
        # Skip pure anchors (links to headings within same doc)
        if [[ "$link" == \#* ]]; then
            continue
        fi

        # Strip anchor fragment before checking file existence
        file_path="${link%%#*}"

        if [[ -e "$file_path" ]]; then
            printf '| `%s` | YES |\n' "$link"
        else
            printf '| `%s` | **NO** |\n' "$link"
        fi
    done
fi

echo ""

# ============================================================================
# SUMMARY
# ============================================================================
echo "### Summary"
echo ""
echo "- **File references checked:** Cross-reference file paths mentioned in spec"
echo "- **Code references checked:** Functions/types in backticks verified to exist"
echo "- **Links checked:** Markdown link targets verified"
echo ""
echo "_Items marked **NO** should be reviewed - they may indicate stale references or typos._"
