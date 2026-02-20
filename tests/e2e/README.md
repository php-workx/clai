# E2E Testing with Claude + Playwright

This directory contains end-to-end test plans for AI-assisted terminal testing.

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

4. **Customize plans/output:**
   ```bash
   make test-e2e \
     E2E_PLANS="tests/e2e/suggestions-tests.yaml" \
     E2E_OUT=".tmp/e2e-runs-custom" \
     E2E_URL="http://127.0.0.1:8080"
   ```

5. **Ask Claude to run tests (alternative):**
   ```
   Run e2e tests from tests/e2e/example-test-plan.yaml against http://localhost:8080
   ```

6. **Review results** - pass/fail summary and per-test JSON are written to `.tmp/e2e-runs/`
   - `.tmp/e2e-runs/results-<shell>.json`
   - `.tmp/e2e-runs/results-all.json`
   - `.tmp/e2e-runs/summary.md`
   - `.tmp/e2e-runs/artifacts-<shell>/...` (failure screenshots)

## Dependency Management

The suite runner installs Node dependencies from `tests/e2e/package.json` on demand.
To preinstall manually:

```bash
npm --prefix tests/e2e install
```

## Directory Structure

```
tests/e2e/
├── README.md                 # This file
├── example-test-plan.yaml    # Core functionality tests (picker, integration, CLI)
├── suggestions-tests.yaml    # Suggestion engine tests (ghost text, typo, cache)
├── screenshots/
│   ├── baseline/             # Expected screenshots for visual regression
│   └── current/              # Screenshots from current test run
└── artifacts/                # Failure logs, buffer dumps, etc.
```

## Test Files

| File | Description |
|------|-------------|
| `example-test-plan.yaml` | Core clai tests: history picker, shell integration, CLI commands, PTY wrapper, incognito, ingestion |
| `suggestions-tests.yaml` | Suggestion engine tests: ghost text, clai suggest CLI, typo correction, slot filling, cache, FTS5 search, project discovery, debug endpoints (cross-shell coverage) |

## Writing Tests

See `example-test-plan.yaml` for the complete schema reference. Key elements:

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
