#!/usr/bin/env bash
# ci-local-release.sh — Local CI parity gate for release readiness.
# Runs the same checks that CI enforces before a tag is pushed.
# Exit 0 = GO, non-zero = NO-GO.
set -euo pipefail

TIMESTAMP=$(date -u +"%Y%m%dT%H%M%SZ")
ARTIFACT_DIR=".agents/releases/local-ci/${TIMESTAMP}"
mkdir -p "${ARTIFACT_DIR}"

pass=0
fail=0
warn=0

report() {
  local status="$1" check="$2" detail="${3:-}"
  case "$status" in
    PASS) ((pass++)); printf "  [PASS] %s\n" "$check" ;;
    FAIL) ((fail++)); printf "  [FAIL] %s — %s\n" "$check" "$detail" ;;
    WARN) ((warn++)); printf "  [WARN] %s — %s\n" "$check" "$detail" ;;
  esac
}

echo "=== Local CI Release Gate ==="
echo ""

# 1. Build
echo "--- Build ---"
if make build >/dev/null 2>&1; then
  report PASS "Build"
else
  report FAIL "Build" "make build failed"
fi

# 2. Format check
echo "--- Format ---"
FMT_OUTPUT=$(goimports -l . 2>/dev/null || true)
if [ -z "$FMT_OUTPUT" ]; then
  report PASS "Format (goimports)"
else
  report WARN "Format" "unformatted files: $(echo "$FMT_OUTPUT" | wc -l | tr -d ' ')"
fi

# 3. Lint
echo "--- Lint ---"
if make lint >/dev/null 2>&1; then
  report PASS "Lint (golangci-lint)"
else
  report FAIL "Lint" "golangci-lint found issues"
fi

# 4. Tests
echo "--- Tests ---"
if go test -race -count=1 ./... >"${ARTIFACT_DIR}/test-output.txt" 2>&1; then
  report PASS "Tests (race)"
else
  report FAIL "Tests" "test failures (see ${ARTIFACT_DIR}/test-output.txt)"
fi

# 5. Vulnerability scan
echo "--- Vulnerability Scan ---"
if command -v govulncheck >/dev/null 2>&1; then
  if govulncheck ./... >"${ARTIFACT_DIR}/vuln-report.txt" 2>&1; then
    report PASS "Vulnerability scan (govulncheck)"
  else
    report WARN "Vulnerability scan" "findings (see ${ARTIFACT_DIR}/vuln-report.txt)"
  fi
else
  report WARN "Vulnerability scan" "govulncheck not installed"
fi

# 6. SBOM generation (CycloneDX via go modules)
echo "--- SBOM ---"
if command -v cyclonedx-gomod >/dev/null 2>&1; then
  if cyclonedx-gomod mod -json -output "${ARTIFACT_DIR}/sbom-cyclonedx-go-mod.json" 2>/dev/null; then
    report PASS "SBOM (CycloneDX)"
  else
    report WARN "SBOM" "cyclonedx-gomod failed"
  fi
else
  # Fallback: generate a minimal SBOM from go list
  go list -m -json all >"${ARTIFACT_DIR}/go-modules.json" 2>/dev/null
  report WARN "SBOM" "cyclonedx-gomod not installed; wrote go-modules.json fallback"
fi

# 7. Security scan summary
echo "--- Security Summary ---"
cat >"${ARTIFACT_DIR}/security-gate-summary.json" <<SECEOF
{
  "timestamp": "${TIMESTAMP}",
  "lint": "$( [ $fail -eq 0 ] && echo pass || echo fail )",
  "vuln_scan": "$( [ -f "${ARTIFACT_DIR}/vuln-report.txt" ] && echo "run" || echo "skipped" )",
  "sbom": "$( [ -f "${ARTIFACT_DIR}/sbom-cyclonedx-go-mod.json" ] && echo "generated" || echo "fallback" )"
}
SECEOF
report PASS "Security gate summary"

# Summary
echo ""
echo "=== Summary ==="
echo "  Pass: ${pass}  Fail: ${fail}  Warn: ${warn}"
echo "  Artifacts: ${ARTIFACT_DIR}/"
echo ""

if [ "$fail" -gt 0 ]; then
  echo "Result: NO-GO (${fail} failures)"
  exit 1
else
  echo "Result: GO"
  exit 0
fi
