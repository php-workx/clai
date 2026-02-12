#!/usr/bin/env bash
# bash-optimizer-test.sh — Tests for the PreToolUse:Bash optimizer hook
#
# Usage: bash hooks/bash-optimizer-test.sh
#
# Runs all test cases and reports pass/fail. Exit 0 if all pass, 1 if any fail.

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
HOOK="$SCRIPT_DIR/bash-optimizer.sh"
PASS=0
FAIL=0

# Disable logging during tests
export BASH_OPTIMIZER_LOG=0

# ── Helpers ────────────────────────────────────────────────────────

make_input() {
  local cmd="$1"
  jq -n --arg cmd "$cmd" '{
    session_id: "test-session",
    tool_use_id: "toolu_test",
    hook_event_name: "PreToolUse",
    tool_name: "Bash",
    tool_input: { command: $cmd }
  }'
}

# assert_deny: command should be denied (JSON output with permissionDecision=deny)
assert_deny() {
  local test_name="$1" cmd="$2"
  local output exit_code
  output=$(make_input "$cmd" | "$HOOK" 2>/dev/null)
  exit_code=$?

  local decision jq_status
  decision=$(printf '%s' "$output" | jq -r '.hookSpecificOutput.permissionDecision // ""' 2>/dev/null)
  jq_status=$?

  if [[ "$jq_status" != "0" ]]; then
    printf "  FAIL  %-45s  malformed JSON output\n" "$test_name"
    if [[ -n "$output" ]]; then
      printf "        raw: %s\n" "$(printf '%s' "$output" | head -c 200)"
    fi
    ((FAIL++))
  elif [[ "$exit_code" == "0" && "$decision" == "deny" ]]; then
    printf "  PASS  %-45s  (deny)\n" "$test_name"
    ((PASS++))
  else
    printf "  FAIL  %-45s  exit=%d decision=%s\n" "$test_name" "$exit_code" "$decision"
    if [[ -n "$output" ]]; then
      printf "        output: %s\n" "$(printf '%s' "$output" | head -c 200)"
    fi
    ((FAIL++))
  fi
}

# assert_allow: command should be allowed (no JSON output or allow decision)
assert_allow() {
  local test_name="$1" cmd="$2"
  local output exit_code
  output=$(make_input "$cmd" | "$HOOK" 2>/dev/null)
  exit_code=$?

  local decision jq_status
  decision=""
  jq_status=0
  if [[ -n "$output" ]]; then
    decision=$(printf '%s' "$output" | jq -r '.hookSpecificOutput.permissionDecision // ""' 2>/dev/null)
    jq_status=$?
  fi

  if [[ "$jq_status" != "0" ]]; then
    printf "  FAIL  %-45s  malformed JSON output\n" "$test_name"
    printf "        raw: %s\n" "$(printf '%s' "$output" | head -c 200)"
    ((FAIL++))
  elif [[ "$exit_code" == "0" && "$decision" != "deny" ]]; then
    printf "  PASS  %-45s  (allow)\n" "$test_name"
    ((PASS++))
  else
    printf "  FAIL  %-45s  exit=%d decision=%s\n" "$test_name" "$exit_code" "$decision"
    if [[ -n "$output" ]]; then
      printf "        output: %s\n" "$(printf '%s' "$output" | head -c 200)"
    fi
    ((FAIL++))
  fi
}

# assert_modified: command should be allowed with modified input
assert_modified() {
  local test_name="$1" cmd="$2" expected_fragment="$3"
  local output exit_code
  output=$(make_input "$cmd" | "$HOOK" 2>/dev/null)
  exit_code=$?

  local decision new_cmd jq_status
  decision=$(printf '%s' "$output" | jq -r '.hookSpecificOutput.permissionDecision // ""' 2>/dev/null)
  jq_status=$?
  new_cmd=""
  if [[ "$jq_status" == "0" ]]; then
    new_cmd=$(printf '%s' "$output" | jq -r '.hookSpecificOutput.updatedInput.command // ""' 2>/dev/null)
    jq_status=$?
  fi

  if [[ "$jq_status" != "0" ]]; then
    printf "  FAIL  %-45s  malformed JSON output\n" "$test_name"
    if [[ -n "$output" ]]; then
      printf "        raw: %s\n" "$(printf '%s' "$output" | head -c 200)"
    fi
    ((FAIL++))
  elif [[ "$exit_code" == "0" && "$decision" == "allow" && "$new_cmd" == *"$expected_fragment"* ]]; then
    printf "  PASS  %-45s  (modified: %s)\n" "$test_name" "$expected_fragment"
    ((PASS++))
  else
    printf "  FAIL  %-45s  exit=%d decision=%s new_cmd=%s\n" "$test_name" "$exit_code" "$decision" "$new_cmd"
    ((FAIL++))
  fi
}

# ── Tests ──────────────────────────────────────────────────────────

printf "=== Rule 1: cat → Read ===\n"
assert_deny   "cat single file"              "cat file.txt"
assert_deny   "cat with path"                "cat /etc/hosts"
assert_deny   "cat multiple files"           "cat a.txt b.txt"
assert_allow  "cat heredoc"                  "cat << 'EOF'\nhello\nEOF"
assert_allow  "cat heredoc (double angle)"   "cat <<EOF"
assert_allow  "cat piped out"               "cat file.txt | wc -l"
assert_allow  "cat with redirect"           "cat a.txt b.txt > combined.txt"

printf "\n=== Rule 2: grep/rg → Grep ===\n"
assert_deny   "standalone grep"             "grep -r 'pattern' src/"
assert_deny   "standalone rg"               "rg 'pattern' src/"
assert_deny   "grep with flags"             "grep -rn 'TODO' ."
assert_allow  "grep in pipeline"            "go test ./... | grep FAIL"
assert_allow  "grep in pipeline (spaced)"   "make test 2>&1 | grep -E 'error|fail'"

printf "\n=== Rule 3: find → Glob ===\n"
assert_deny   "simple find"                 "find . -name '*.go'"
assert_deny   "find with type"              "find /tmp -type f -name '*.log'"
assert_allow  "find with -exec"             "find . -name '*.tmp' -exec rm {} \\;"
assert_allow  "find with -delete"           "find /tmp -name '*.old' -delete"
assert_allow  "find with -print0"           "find . -name '*.go' -print0"

printf "\n=== Rule 4: head/tail → Read ===\n"
assert_deny   "head on file"                "head -n 20 file.txt"
assert_deny   "tail on file"                "tail -n 50 log.txt"
assert_deny   "head without flags"          "head README.md"
assert_allow  "head in pipeline"            "go test 2>&1 | head -20"
assert_allow  "tail in pipeline"            "docker logs app | tail -100"

printf "\n=== Rule 5: sed/awk → Edit ===\n"
assert_deny   "sed in-place"                "sed -i 's/old/new/g' file.txt"
assert_deny   "sed --in-place"              "sed --in-place 's/foo/bar/' config.yml"
assert_allow  "sed in pipeline"             "echo 'hello' | sed 's/h/H/'"
assert_allow  "sed without -i"              "sed 's/old/new/g' file.txt"
assert_deny   "awk standalone"              "awk '{print \$1}' data.csv"
assert_allow  "awk in pipeline"             "ps aux | awk '{print \$1}'"

printf "\n=== Rule 6: mkdir auto-fix ===\n"
assert_modified "mkdir without -p"          "mkdir /tmp/newdir"           "mkdir -p"
assert_modified "mkdir nested"              "mkdir /tmp/a/b/c"            "mkdir -p"
assert_allow    "mkdir with -p already"     "mkdir -p /tmp/already"

printf "\n=== Rule 7: echo > file → Write ===\n"
assert_deny   "echo to file"               "echo 'hello' > output.txt"
assert_deny   "echo append"                "echo 'line' >> log.txt"
assert_deny   "printf to file"             "printf 'data' > file.txt"
assert_allow  "echo to stdout"             "echo 'hello world'"
assert_allow  "echo to stderr"             "echo 'error' >&2"

printf "\n=== Rule 8: Passthrough (should all allow) ===\n"
assert_allow  "go test"                     "go test ./..."
assert_allow  "make build"                  "make build"
assert_allow  "git status"                  "git status"
assert_allow  "docker run"                  "docker run -d nginx"
assert_allow  "npm install"                 "npm install"
assert_allow  "bd show"                     "bd show ai-terminal-16q"
assert_allow  "kubectl get"                 "kubectl get pods"
assert_allow  "python script"              "python3 script.py"
assert_allow  "complex pipeline"           "go test -v ./... 2>&1 | tee test.log"
assert_allow  "env var prefix"             "FOO=bar go test ./..."
assert_allow  "empty command"              ""

# ── Summary ────────────────────────────────────────────────────────
printf "\n══════════════════════════════════════════\n"
printf "  Results: %d passed, %d failed\n" "$PASS" "$FAIL"
printf "══════════════════════════════════════════\n"

if [[ "$FAIL" -gt 0 ]]; then
  exit 1
fi
exit 0
