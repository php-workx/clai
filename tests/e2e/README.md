# E2E Testing with Claude + Playwright

This directory contains end-to-end test plans for AI-assisted terminal testing.

## Quick Start

1. **Start the terminal server:**
   ```bash
   make test-server
   ```

2. **Run tests with the local Playwright runner:**
   ```bash
   # One-time deps
   cd tests/e2e && npm init -y && npm install playwright js-yaml

   # In repo root
   node tests/e2e/run-e2e.js --shell bash --url http://127.0.0.1:8080
   node tests/e2e/run-e2e.js --shell zsh --url http://127.0.0.1:8080
   node tests/e2e/run-e2e.js --shell fish --url http://127.0.0.1:8080
   ```

3. **Ask Claude to run tests (alternative):**
   ```
   Run e2e tests from tests/e2e/example-test-plan.yaml against http://localhost:8080
   ```

4. **Review results** - pass/fail summary and per-test JSON are written to `.tmp/e2e-runs/`

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
