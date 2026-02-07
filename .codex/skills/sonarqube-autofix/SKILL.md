---
name: sonarqube-autofix
description: Use when you need a coding agent to run a local SonarQube scan on current-branch changes and autonomously fix findings at or above a chosen severity threshold (default medium) without waiting for human checkpoints.
---

# SonarQube Autofix

**YOU MUST EXECUTE THIS WORKFLOW. Do not just describe it.**

Runs SonarQube against files changed on the current branch, then iteratively fixes findings at/above a target severity (`medium` by default) until clean or blocked.

## Inputs

- `severity`: `all|low|medium|high|critical` (default `medium`)
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

1. Run an initial scan.
```bash
bash "<path-to-skill>/scripts/run_changed_scan.sh" --severity "${SEVERITY:-medium}" --base-ref "${BASE_REF:-origin/main}"
```

2. Interpret exit code.
- `0`: no actionable findings; stop.
- `3`: actionable findings exist; continue fix loop.
- `1`: blocked (scanner/auth/infrastructure); surface blocker and stop.

3. Fix loop (no user checkpoints).
- Set `MAX_PASSES=8` unless user specified another limit.
- On each pass with exit code `3`, read `.sonarqube-autofix/findings.json` and fix highest-severity findings first.
- Keep changes minimal and local to files in `changed-files.txt`.
- After each pass run relevant verification (`make test` preferred; if too slow, run targeted tests for touched packages/files).
- Re-run `run_changed_scan.sh` after fixes.

4. Stop conditions.
- Stop successfully when scan exits `0`.
- Stop as blocked if findings are non-actionable in-code constraints (rule false positive, external dependency, or unsupported auto-fix) and report exact finding keys.
- Stop as failed if `MAX_PASSES` is reached and findings remain; report remaining findings by severity.

5. Completion behavior.
- Summarize files changed and remaining findings count (should be zero on success).
- Commit with a conventional commit message if repository policy expects autonomous commits.
- Never push automatically.

## Execution Rules

- Do not ask the user to review each finding during this workflow.
- Do not claim success without a fresh final scan (`exit 0`) and test evidence.
- Prioritize vulnerabilities and bugs over code smells when severities tie.
- If SonarQube is local and no credentials are provided, `run_changed_scan.sh` defaults to `admin/admin`.
