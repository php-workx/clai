---
name: sonarqube-autofix
description: Use when you need a coding agent to run SonarQube/SonarCloud checks on current-branch changes, either list aggregated findings by severity or autonomously fix findings at/above a chosen severity threshold.
---

# SonarQube Autofix

**YOU MUST EXECUTE THIS WORKFLOW. Do not just describe it.**

Runs SonarQube/SonarCloud against files changed on the current branch, then iteratively fixes findings at/above a target severity (`high` by default) until clean or blocked.

## Inputs

- `action`: `autofix|list` (resolved from user intent)
- `mode`: `local|cloud` (default `local`)
- `severity`: `blocker|high|medium|low|info` (default `high`)
- `base_ref`: branch to diff against (default auto-detect: `origin/main`, `main`, `origin/master`, `master`)
- `host_url`: SonarQube URL (default `http://localhost:9000`)
- `auth`: prefer `SONAR_TOKEN`; fallback `SONAR_USER` + `SONAR_PASSWORD`

## Bundled Scripts

- `scripts/run_changed_scan.sh`: starts local SonarQube container if needed, scans changed files, writes findings
- `scripts/collect_changed_issues.py`: queries SonarQube API and filters findings to changed files at/above threshold

Outputs (default directory `.sonarqube-autofix/`):
- `changed-files.txt`
- `sonar-scanner.log`
- `findings.json`
- `findings.md`

## Autonomous Workflow

1. Resolve action from intent.
- `autofix` keywords: `autofix`, `fix`, `address`, `resolve`, `complete`, `remediate`.
- `list` keywords: `list`, `show`, `find`, `report`, `summary`, `aggregate`, `count`.
- If ambiguous, ask once: `Do you want autofix or list? (autofix/list)`.
- If unanswered/ambiguous, default to `autofix`.

2. Resolve mode.
- If user/context explicitly says `local` or `cloud`, use it.
- If not explicit, ask once: `Do you want a local scan or SonarCloud results? (local/cloud)`.
- If unanswered/ambiguous, default to `local`.

3. Resolve severity threshold.
- If user/context explicitly provides severity, use it.
- If missing, ask once: `Which severity threshold? (blocker/high/medium/low/info)`.
- If unanswered/ambiguous, default to `high`.

4. For `cloud` mode, check MCP availability automatically before scanning.
- Check available MCP servers and templates.
- If a Sonar/SonarQube/SonarCloud MCP server is available, use it as the primary source of findings.
- If MCP is unavailable, fall back to SonarCloud REST API only when auth/context exists.
- If neither MCP nor API access is available, report blocked with exact missing dependency and stop.

5. Run an initial scan for `local` mode.
```bash
bash "<path-to-skill>/scripts/run_changed_scan.sh" --severity "${SEVERITY:-high}" --base-ref "${BASE_REF:-origin/main}"
```

6. If `action=list`, print aggregated findings and stop (no code edits).
```bash
bash "<path-to-skill>/scripts/run_changed_scan.sh" --severity "${SEVERITY:-high}" --base-ref "${BASE_REF:-origin/main}" --list-only
```

7. Interpret exit code for `autofix`.
- `0`: no actionable findings; stop.
- `3`: actionable findings exist; continue fix loop.
- `1`: blocked (scanner/auth/infrastructure); surface blocker and stop.

8. Fix loop (no user checkpoints).
- Set `MAX_PASSES=8` unless user specified another limit.
- On each pass with exit code `3`, read `.sonarqube-autofix/findings.json` and fix highest-severity findings first.
- Keep changes minimal and local to files in `changed-files.txt`.
- After each pass run relevant verification (`make test` preferred; if too slow, run targeted tests for touched packages/files).
- Re-run `run_changed_scan.sh` after fixes.

9. Stop conditions.
- Stop successfully when scan exits `0`.
- Stop as blocked if findings are non-actionable in-code constraints (rule false positive, external dependency, or unsupported auto-fix) and report exact finding keys.
- Stop as failed if `MAX_PASSES` is reached and findings remain; report remaining findings by severity.

10. Completion behavior.
- Summarize files changed and remaining findings count (should be zero on success).
- Commit with a conventional commit message if repository policy expects autonomous commits.
- Never push automatically.

## Execution Rules

- Do not ask the user to review each finding during this workflow.
- Do not claim success without a fresh final scan (`exit 0`) and test evidence.
- Prioritize vulnerabilities and bugs over code smells when severities tie.
- If SonarQube is local and no credentials are provided, `run_changed_scan.sh` defaults to `admin/admin`.
