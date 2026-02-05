# E2E Testing with Claude + Playwright

This directory contains end-to-end test plans for AI-assisted terminal testing.

## Quick Start

1. **Start the terminal server:**
   ```bash
   make test-server
   ```

2. **Ask Claude to run tests:**
   ```
   Run e2e tests from tests/e2e/example-test-plan.yaml against http://localhost:8080
   ```

3. **Review results** - Claude will report pass/fail for each test

## Directory Structure

```
tests/e2e/
├── README.md                 # This file
├── example-test-plan.yaml    # Reference test plan with schema docs
├── screenshots/
│   ├── baseline/             # Expected screenshots for visual regression
│   └── current/              # Screenshots from current test run
└── artifacts/                # Failure logs, buffer dumps, etc.
```

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
| `suggestions` | Ghost text suggestion tests |
| `integration` | Shell integration tests |
| `pty` | PTY wrapper tests |
| `cross-shell` | Tests that should pass in all shells |

## Technical Details

See [specs/tech_ai_testing.md](../../specs/tech_ai_testing.md) for the full technical specification.
