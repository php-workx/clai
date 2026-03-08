# E2E Testing with Claude + Playwright

This directory contains end-to-end tests for terminal behavior.
Suggestion tests run as native Playwright specs. Core tests remain YAML-driven.

## Quick Start

1. **Run full e2e suite (all shells):**
   ```bash
   make test-e2e
   ```

2. **Run one shell:**
   ```bash
   make test-e2e-shell E2E_SHELL=bash
   make test-e2e-shell E2E_SHELL=zsh
   make test-e2e-shell E2E_SHELL=fish
   ```

3. **Run a filtered subset:**
   ```bash
   make test-e2e E2E_GREP="suggest|search"
   ```

4. **Choose standard reporter format (Playwright):**
   ```bash
   make test-e2e E2E_REPORTER=line
   make test-e2e E2E_REPORTER=dot
   make test-e2e E2E_REPORTER=list
   make test-e2e E2E_REPORTER=junit
   ```

5. **Customize YAML plans/output (core suite only):**
   ```bash
   make test-e2e \
     E2E_PLANS="tests/e2e/example-test-plan.yaml" \
     E2E_OUT=".tmp/e2e-runs-custom" \
     E2E_URL="http://127.0.0.1:8080"
   ```

6. **Include legacy suggestions YAML in addition to native suggestions spec (diagnostic only):**
   ```bash
   make test-e2e E2E_INCLUDE_SUGGESTIONS_YAML=1
   ```

7. **Ask Claude to run tests (alternative):**
   ```
   Run e2e tests from tests/e2e/example-test-plan.yaml against http://localhost:8080
   ```

8. **Review results** - pass/fail summary and per-test JSON are written to `.tmp/e2e-runs/`
   - `.tmp/e2e-runs/results-<shell>.json`
   - `.tmp/e2e-runs/results-all.json`
   - `.tmp/e2e-runs/summary.md`
   - `.tmp/e2e-runs/artifacts-<shell>/...` (failure screenshots)

## Dependency Management

The suite runner installs Node dependencies from `tests/e2e/package.json` on demand.
It uses Playwright's standard test runner and reporters.
To preinstall manually:

```bash
npm --prefix tests/e2e install
```

## Directory Structure

```
tests/e2e/
├── README.md                 # This file
├── suggestions.spec.cjs      # Native Playwright suggestion tests
├── yaml.spec.cjs             # YAML bridge spec (core plans)
├── terminal_case_runner.cjs  # Shared terminal actions/assertions
├── example-test-plan.yaml    # Core functionality tests (picker, integration, CLI)
├── suggestions-tests.yaml    # Legacy YAML suggestions plan (kept for parity checks)
├── screenshots/
│   ├── baseline/             # Expected screenshots for visual regression
│   └── current/              # Screenshots from current test run
└── artifacts/                # Failure logs, buffer dumps, etc.
```

## Test Files

| File | Description |
|------|-------------|
| `suggestions.spec.cjs` | Native Playwright suggestion tests (primary path) |
| `yaml.spec.cjs` | Executes YAML-defined plans through shared terminal runner |
| `example-test-plan.yaml` | Core clai tests: history picker, shell integration, CLI commands, PTY wrapper, incognito, ingestion |
| `suggestions-tests.yaml` | Legacy YAML suggestions plan retained for migration parity checks |

## Writing Tests

Core YAML plans use this schema (`example-test-plan.yaml`):

```yaml
tests:
  - name: "Test name"
    shell: bash          # bash, zsh, or fish
    tags: [smoke, picker]
    setup:               # Commands to seed state
      - "echo setup"
    steps:               # Actions to perform
      - type: "command"
      - press: "Ctrl+R"
      - wait: "500ms"
    expect:              # Assertions
      - screen_contains: "expected text"
      - screen_not_contains: "error"
```

## Test Tags

Use tags to run subsets of tests:

| Tag | Description |
|-----|-------------|
| `smoke` | Quick sanity checks |
| `picker` | History/suggestion picker tests |
| `suggest` | clai suggest CLI tests |
| `ghost-text` | Ghost text inline suggestion tests |
| `search` | FTS5 history search tests |
| `discovery` | Project task discovery tests |
| `cache` | Pre-computed cache tests |
| `typo` | Typo correction tests |
| `slots` | Slot filling tests |
| `debug` | Debug endpoint tests |
| `integration` | Shell integration tests |
| `incognito` | Incognito/privacy mode tests |
| `pty` | PTY wrapper tests |
| `cross-shell` | Tests that should pass in all shells |

## Technical Details

See [specs/tech_ai_testing.md](../../specs/tech_ai_testing.md) for the full technical specification.
