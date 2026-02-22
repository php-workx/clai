#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT_DIR="${E2E_OUT:-$ROOT_DIR/.tmp/e2e-runs}"
E2E_URL="${E2E_URL:-http://127.0.0.1:8080}"
E2E_SHELLS="${E2E_SHELLS:-bash zsh fish}"
E2E_PLANS="${E2E_PLANS:-tests/e2e/example-test-plan.yaml,tests/e2e/suggestions-tests.yaml}"
E2E_GREP="${E2E_GREP:-}"
INSTALL_DEPS="${E2E_INSTALL_DEPS:-1}"
E2E_INCLUDE_SUGGESTIONS_YAML="${E2E_INCLUDE_SUGGESTIONS_YAML:-0}"

NODE_MODULES_DIR="$ROOT_DIR/tests/e2e/node_modules"
PW_CONFIG="$ROOT_DIR/tests/e2e/playwright.config.cjs"
PW_YAML_SPEC="$ROOT_DIR/tests/e2e/yaml.spec.cjs"
PW_SUGGESTIONS_SPEC="$ROOT_DIR/tests/e2e/suggestions.spec.cjs"
E2E_REPORTER="${E2E_REPORTER:-line}"

require_cmd() {
	local c="$1"
	if ! command -v "$c" >/dev/null 2>&1; then
		echo "error: required command not found: $c" >&2
		exit 1
	fi
}

ensure_deps() {
	require_cmd node
	require_cmd npm

	if [[ "$INSTALL_DEPS" != "1" ]]; then
		return
	fi

	if [[ ! -d "$NODE_MODULES_DIR/@playwright/test" || ! -d "$NODE_MODULES_DIR/js-yaml" ]]; then
		echo "Installing e2e runner dependencies into tests/e2e/node_modules..."
		if [[ -f "$ROOT_DIR/tests/e2e/package-lock.json" ]]; then
			npm --prefix "$ROOT_DIR/tests/e2e" ci --no-fund --no-audit
		else
			npm --prefix "$ROOT_DIR/tests/e2e" install --no-fund --no-audit
		fi
	fi
}

run_shell_suite() {
	local shell_name="$1"
	local log_path="$OUT_DIR/run-$shell_name.log"
	local result_json="$OUT_DIR/results-$shell_name.json"
	local artifacts_dir="$OUT_DIR/artifacts-$shell_name"
	local yaml_plans="$E2E_PLANS"
	local rc=0

	: >"$log_path"
	echo "=== $shell_name ===" | tee -a "$log_path"

	trap 'make -C "$ROOT_DIR" test-server-stop >>"$log_path" 2>&1 || true; trap - EXIT INT TERM' EXIT INT TERM
	make -C "$ROOT_DIR" test-server TEST_SHELL="$shell_name" >>"$log_path" 2>&1

	local reporter="$E2E_REPORTER"
	if [[ ",$reporter," != *",json,"* ]]; then
		reporter="$reporter,json"
	fi

	local -a cmd
	cmd=(
		npx --prefix "$ROOT_DIR/tests/e2e"
		playwright test
		--config "$PW_CONFIG"
		--workers=1
		--reporter "$reporter"
		--output "$artifacts_dir"
	)

	if [[ "$E2E_INCLUDE_SUGGESTIONS_YAML" != "1" ]]; then
		yaml_plans="$(
			printf '%s' "$E2E_PLANS" \
				| tr ',' '\n' \
				| sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//' \
				| awk 'NF && $0 !~ /(^|\/)suggestions-tests\.yaml$/ {print}' \
				| paste -sd, -
		)"
	fi

	cmd+=("$PW_SUGGESTIONS_SPEC")
	if [[ -n "$yaml_plans" ]]; then
		cmd+=("$PW_YAML_SPEC")
	fi

	if [[ -n "$E2E_GREP" ]]; then
		cmd+=(--grep "$E2E_GREP")
	fi

	set +e
	env \
		E2E_SHELL="$shell_name" \
		E2E_URL="$E2E_URL" \
		E2E_PLANS="$yaml_plans" \
		PLAYWRIGHT_JSON_OUTPUT_FILE="$result_json" \
		"${cmd[@]}" 2>&1 | tee -a "$log_path"
	rc=${PIPESTATUS[0]}
	set -e

	make -C "$ROOT_DIR" test-server-stop >>"$log_path" 2>&1 || true

	if [[ -f "$result_json" ]]; then
		node -e '
const fs = require("fs");
const p = process.argv[1];
const d = JSON.parse(fs.readFileSync(p, "utf8"));
const s = d.stats || {};
const passed = s.expected || 0;
const skipped = s.skipped || 0;
const failed = (s.unexpected || 0) + (s.flaky || 0);
const total = passed + skipped + failed;
console.log(`SUMMARY ${p}: total=${total} pass=${passed} fail=${failed} skip=${skipped}`);
function collectFailures(suites, out) {
  for (const suite of suites || []) {
    for (const spec of suite.specs || []) {
      for (const t of spec.tests || []) {
        if (t.status === "unexpected" || t.status === "flaky") {
          out.push(`${spec.title}: ${t.status}`);
        }
      }
    }
    collectFailures(suite.suites, out);
  }
}
const failures = [];
collectFailures(d.suites, failures);
if (failures.length > 0) {
  console.log(`FAILURES ${p}:`);
  for (const f of failures) console.log(` - ${f}`);
}
' "$result_json" | tee -a "$log_path"
	else
		echo "warning: missing result json: $result_json" | tee -a "$log_path"
		rc=1
	fi

	return "$rc"
}

write_aggregate_reports() {
	node - "$OUT_DIR" "$E2E_SHELLS" <<'NODE'
const fs = require("fs");
const path = require("path");

const outDir = process.argv[2];
const shells = process.argv[3].trim().split(/\s+/).filter(Boolean);

const aggregate = {
  generated_at: new Date().toISOString(),
  shells: {},
  totals: { total: 0, passed: 0, failed: 0, skipped: 0 },
};

for (const sh of shells) {
  const p = path.join(outDir, `results-${sh}.json`);
  if (!fs.existsSync(p)) {
    aggregate.shells[sh] = { missing: true };
    continue;
  }
  const parsed = JSON.parse(fs.readFileSync(p, "utf8"));
  const stats = parsed.stats || {};
  const s = {
    total: (stats.expected || 0) + (stats.skipped || 0) + (stats.unexpected || 0) + (stats.flaky || 0),
    passed: stats.expected || 0,
    failed: (stats.unexpected || 0) + (stats.flaky || 0),
    skipped: stats.skipped || 0,
  };
  aggregate.shells[sh] = s;
  aggregate.totals.total += s.total || 0;
  aggregate.totals.passed += s.passed || 0;
  aggregate.totals.failed += s.failed || 0;
  aggregate.totals.skipped += s.skipped || 0;
}

const jsonOut = path.join(outDir, "results-all.json");
fs.writeFileSync(jsonOut, JSON.stringify(aggregate, null, 2));

const lines = [];
lines.push("# E2E Summary");
lines.push("");
lines.push(`Generated: ${aggregate.generated_at}`);
lines.push("");
lines.push("| shell | total | pass | fail | skip |");
lines.push("|---|---:|---:|---:|---:|");
for (const sh of shells) {
  const s = aggregate.shells[sh] || {};
  if (s.missing) {
    lines.push(`| ${sh} | - | - | - | - |`);
    continue;
  }
  lines.push(`| ${sh} | ${s.total||0} | ${s.passed||0} | ${s.failed||0} | ${s.skipped||0} |`);
}
lines.push(`| TOTAL | ${aggregate.totals.total} | ${aggregate.totals.passed} | ${aggregate.totals.failed} | ${aggregate.totals.skipped} |`);
lines.push("");
const mdOut = path.join(outDir, "summary.md");
fs.writeFileSync(mdOut, lines.join("\n"));
console.log("=== E2E Aggregate Summary ===");
for (const sh of shells) {
  const s = aggregate.shells[sh] || {};
  if (s.missing) {
    console.log(`${sh}: missing results`);
    continue;
  }
  console.log(`${sh}: total=${s.total||0} pass=${s.passed||0} fail=${s.failed||0} skip=${s.skipped||0}`);
}
console.log(`TOTAL: total=${aggregate.totals.total} pass=${aggregate.totals.passed} fail=${aggregate.totals.failed} skip=${aggregate.totals.skipped}`);
console.log(`Wrote ${jsonOut}`);
console.log(`Wrote ${mdOut}`);
NODE
}

main() {
	require_cmd make
	require_cmd gotty
	ensure_deps

	mkdir -p "$OUT_DIR"

	local overall_rc=0
	for shell_name in $E2E_SHELLS; do
		if ! run_shell_suite "$shell_name"; then
			overall_rc=1
		fi
	done

	write_aggregate_reports
	exit "$overall_rc"
}

main "$@"
