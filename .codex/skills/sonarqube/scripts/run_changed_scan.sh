#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

usage() {
  cat <<'EOF'
Usage: run_changed_scan.sh [options]

Runs sonar-scanner against files changed on the current branch, then exports
actionable SonarQube findings at or above the selected severity threshold.

Options:
  --base-ref <ref>            Git ref used to compute changed files
  --severity <level>          blocker|high|medium|low|info (default: high)
  --project-key <key>         Sonar project key (auto-detected if omitted)
  --host-url <url>            SonarQube URL (default: SONAR_HOST_URL or http://localhost:9000)
  --output-dir <dir>          Output directory (default: .sonarqube)
  --list-only                 Print aggregated findings by severity and exit 0
  --token <token>             Sonar token
  --user <user>               Sonar username
  --password <password>       Sonar password
  --no-autostart              Do not start docker SonarQube container automatically
  --help                      Show this help

Exit codes:
  0 = scan complete, no findings at/above threshold on changed files
  1 = failure
  3 = findings exist at/above threshold on changed files
EOF
}

log() {
  printf '[sonarqube] %s\n' "$*"
}

fail() {
  printf '[sonarqube] error: %s\n' "$*" >&2
  exit 1
}

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    fail "required command not found: $1"
  fi
}

ref_exists() {
  git rev-parse --verify --quiet "$1^{commit}" >/dev/null 2>&1
}

detect_base_ref() {
  local candidates=("origin/main" "main" "origin/master" "master")
  local ref
  for ref in "${candidates[@]}"; do
    if ref_exists "$ref"; then
      printf '%s' "$ref"
      return 0
    fi
  done
  return 1
}

read_system_status() {
  curl -fsS "${HOST_URL%/}/api/system/status" 2>/dev/null \
    | sed -n 's/.*"status"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p'
}

ensure_sonarqube_available() {
  local status
  status="$(read_system_status || true)"
  if [[ "$status" == "UP" ]]; then
    return 0
  fi

  if [[ "$AUTOSTART" != "true" ]]; then
    fail "SonarQube is not reachable at $HOST_URL and autostart is disabled"
  fi

  require_cmd docker

  if docker ps -a --format '{{.Names}}' | grep -Fxq "$CONTAINER_NAME"; then
    log "starting existing container $CONTAINER_NAME"
    docker start "$CONTAINER_NAME" >/dev/null
  else
    log "creating container $CONTAINER_NAME from $CONTAINER_IMAGE"
    docker run -d --name "$CONTAINER_NAME" -p 9000:9000 "$CONTAINER_IMAGE" >/dev/null
  fi

  log "waiting for SonarQube to become ready at $HOST_URL"
  local waited=0
  while [[ "$waited" -lt "$WAIT_SECONDS" ]]; do
    status="$(read_system_status || true)"
    if [[ "$status" == "UP" ]]; then
      log "SonarQube is ready"
      return 0
    fi
    sleep 5
    waited=$((waited + 5))
  done

  fail "timed out waiting for SonarQube to become ready"
}

HOST_URL="${SONAR_HOST_URL:-http://localhost:9000}"
BASE_REF="${SONAR_BASE_REF:-}"
SEVERITY="${SONAR_SEVERITY_THRESHOLD:-high}"
PROJECT_KEY="${SONAR_PROJECT_KEY:-}"
OUTPUT_DIR="${SONAR_OUTPUT_DIR:-.sonarqube}"
SONAR_TOKEN="${SONAR_TOKEN:-}"
SONAR_USER="${SONAR_USER:-}"
SONAR_PASSWORD="${SONAR_PASSWORD:-}"
AUTOSTART="${SONAR_AUTOSTART_CONTAINER:-true}"
CONTAINER_NAME="${SONARQUBE_CONTAINER_NAME:-clai-sonarqube}"
CONTAINER_IMAGE="${SONARQUBE_IMAGE:-sonarqube:lts-community}"
WAIT_SECONDS="${SONARQUBE_WAIT_SECONDS:-300}"
LIST_ONLY="${SONAR_LIST_ONLY:-false}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --base-ref)
      BASE_REF="${2:-}"
      shift 2
      ;;
    --severity)
      SEVERITY="${2:-}"
      shift 2
      ;;
    --project-key)
      PROJECT_KEY="${2:-}"
      shift 2
      ;;
    --host-url)
      HOST_URL="${2:-}"
      shift 2
      ;;
    --output-dir)
      OUTPUT_DIR="${2:-}"
      shift 2
      ;;
    --list-only)
      LIST_ONLY="true"
      shift
      ;;
    --token)
      SONAR_TOKEN="${2:-}"
      shift 2
      ;;
    --user)
      SONAR_USER="${2:-}"
      shift 2
      ;;
    --password)
      SONAR_PASSWORD="${2:-}"
      shift 2
      ;;
    --no-autostart)
      AUTOSTART="false"
      shift
      ;;
    --help|-h)
      usage
      exit 0
      ;;
    *)
      fail "unknown option: $1"
      ;;
  esac
done

case "$SEVERITY" in
  blocker|high|medium|low|info|critical|all)
    ;;
  *)
    fail "invalid severity '$SEVERITY' (expected blocker|high|medium|low|info)"
    ;;
esac

require_cmd git
require_cmd curl
require_cmd python3
require_cmd sonar-scanner

REPO_ROOT="$(git rev-parse --show-toplevel)"
OUTPUT_DIR_ABS="$OUTPUT_DIR"
if [[ "$OUTPUT_DIR" != /* ]]; then
  OUTPUT_DIR_ABS="$REPO_ROOT/$OUTPUT_DIR"
fi
mkdir -p "$OUTPUT_DIR_ABS"

if [[ -z "$BASE_REF" ]]; then
  BASE_REF="$(detect_base_ref || true)"
fi
if [[ -z "$BASE_REF" ]]; then
  fail "unable to detect base ref; pass --base-ref <ref>"
fi

if ! ref_exists "$BASE_REF"; then
  fail "base ref does not exist: $BASE_REF"
fi

BASE_COMMIT="$(git merge-base HEAD "$BASE_REF")"
CHANGED_FILE_LIST="$OUTPUT_DIR_ABS/changed-files.txt"

if [[ -z "$PROJECT_KEY" ]] && [[ -f "$REPO_ROOT/sonar-project.properties" ]]; then
  PROJECT_KEY="$(awk -F= '/^[[:space:]]*sonar\.projectKey[[:space:]]*=/{print $2; exit}' "$REPO_ROOT/sonar-project.properties" | tr -d '[:space:]')"
fi
if [[ -z "$PROJECT_KEY" ]]; then
  PROJECT_KEY="$(basename "$REPO_ROOT")"
fi

DIFF_FILES=()
while IFS= read -r file; do
  DIFF_FILES+=("$file")
done < <(git -C "$REPO_ROOT" diff --name-only --diff-filter=ACMRTUXB "$BASE_COMMIT..HEAD")
CHANGED_FILES=()
for file in "${DIFF_FILES[@]}"; do
  if [[ -f "$REPO_ROOT/$file" ]]; then
    CHANGED_FILES+=("$file")
  fi
done

: > "$CHANGED_FILE_LIST"
if [[ "${#CHANGED_FILES[@]}" -eq 0 ]]; then
  log "no changed files between $BASE_REF and HEAD"
  printf '{"summary":{"project_key":"%s","severity_threshold":"%s","changed_files":0,"findings":0,"severity_counts":{}},"findings":[]}\n' \
    "${PROJECT_KEY:-unknown}" "$SEVERITY" > "$OUTPUT_DIR_ABS/findings.json"
  cat > "$OUTPUT_DIR_ABS/findings.md" <<EOF
# SonarQube Findings (Changed Files)

No changed files detected between \`$BASE_REF\` and HEAD.

- Base ref: \`$BASE_REF\`
- Severity threshold: \`$SEVERITY\`
auto-fix loop can stop immediately.
EOF
  exit 0
fi

printf '%s\n' "${CHANGED_FILES[@]}" > "$CHANGED_FILE_LIST"
CHANGED_CSV="$(IFS=, ; printf '%s' "${CHANGED_FILES[*]}")"

if [[ -z "$SONAR_TOKEN" && -z "$SONAR_USER" && -z "$SONAR_PASSWORD" ]]; then
  if [[ "$HOST_URL" =~ ^https?://(localhost|127\.0\.0\.1)(:[0-9]+)?/?$ ]]; then
    SONAR_USER="admin"
    SONAR_PASSWORD="admin"
    log "using default local SonarQube credentials admin/admin"
  else
    fail "provide SONAR_TOKEN or --user/--password for SonarQube authentication"
  fi
fi

ensure_sonarqube_available

SCANNER_LOG="$OUTPUT_DIR_ABS/sonar-scanner.log"
SCANNER_CMD=(
  sonar-scanner
  "-Dsonar.host.url=$HOST_URL"
  "-Dsonar.projectKey=$PROJECT_KEY"
  "-Dsonar.inclusions=$CHANGED_CSV"
  "-Dsonar.qualitygate.wait=true"
  "-Dsonar.qualitygate.timeout=300"
)

if [[ -n "$SONAR_TOKEN" ]]; then
  SCANNER_CMD+=("-Dsonar.token=$SONAR_TOKEN")
else
  SCANNER_CMD+=("-Dsonar.login=$SONAR_USER" "-Dsonar.password=$SONAR_PASSWORD")
fi

log "running sonar-scanner for ${#CHANGED_FILES[@]} changed files"
(
  cd "$REPO_ROOT"
  "${SCANNER_CMD[@]}" >"$SCANNER_LOG" 2>&1
) || {
  log "scanner output:"
  tail -n 100 "$SCANNER_LOG" >&2 || true
  fail "sonar-scanner failed"
}

COLLECT_CMD=(
  python3
  "$SCRIPT_DIR/collect_changed_issues.py"
  --host-url "$HOST_URL"
  --project-key "$PROJECT_KEY"
  --changed-files "$CHANGED_FILE_LIST"
  --severity-threshold "$SEVERITY"
  --output-json "$OUTPUT_DIR_ABS/findings.json"
  --output-md "$OUTPUT_DIR_ABS/findings.md"
)

if [[ -n "$SONAR_TOKEN" ]]; then
  COLLECT_CMD+=(--token "$SONAR_TOKEN")
else
  COLLECT_CMD+=(--user "$SONAR_USER" --password "$SONAR_PASSWORD")
fi

set +e
"${COLLECT_CMD[@]}"
ISSUE_EXIT=$?
set -e

log "findings json: $OUTPUT_DIR_ABS/findings.json"
log "findings markdown: $OUTPUT_DIR_ABS/findings.md"
log "changed files: $CHANGED_FILE_LIST"

if [[ "$ISSUE_EXIT" -eq 0 ]]; then
  log "no findings at/above '$SEVERITY' on changed files"
elif [[ "$ISSUE_EXIT" -eq 3 ]]; then
  log "findings found at/above '$SEVERITY' on changed files"
else
  fail "failed to collect SonarQube findings"
fi

if [[ "$LIST_ONLY" == "true" ]]; then
  python3 - "$OUTPUT_DIR_ABS/findings.json" <<'PY'
import json
import sys

path = sys.argv[1]
with open(path, "r", encoding="utf-8") as fh:
    data = json.load(fh)

summary = data.get("summary", {})
counts = summary.get("severity_counts", {})
order = ("BLOCKER", "CRITICAL", "MAJOR", "MINOR", "INFO")

print("SonarQube Aggregated Findings")
print(f"- Project: {summary.get('project_key', 'unknown')}")
print(f"- Threshold: {summary.get('severity_threshold', 'unknown')}")
print(f"- Changed files: {summary.get('changed_files', 0)}")
print(f"- Findings: {summary.get('findings', 0)}")
print("- Severity counts:")
for sev in order:
    print(f"  - {sev}: {counts.get(sev, 0)}")
PY
  exit 0
fi

exit "$ISSUE_EXIT"
