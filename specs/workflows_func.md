# clai Workflow Execution — Functional Specification

**Version:** 0.2 (Draft)
**Date:** 2026-02-11
**Status:** RFC

---

## 1. Purpose

This document defines the **functional requirements** for adding workflow execution capability to `clai` (Command Line Intelligence). It describes *what* the system should do, not *how* it should be implemented. A separate technical specification will follow.

The core idea: a local workflow executor that combines deterministic command execution with LLM-powered output analysis, enabling users to automate multi-step operational tasks where some steps require intelligent judgment before proceeding.

---

## 2. Motivation

Operational tasks like running Pulumi deployments across multiple stacks, remediating compliance findings, or executing multi-step infrastructure changes follow a recurring pattern:

1. Run a command (assume a role, run a preview, apply changes)
2. Look at the output
3. Decide whether to proceed, investigate further, or abort
4. Repeat for the next step or the next target

Today this is either done manually (slow, error-prone) or with bash scripts (fast but blind — scripts can't assess whether output is safe). There's a gap between "dumb automation" and "fully manual" that an LLM-assisted executor can fill.

---

## 3. Reference Projects

Two open-source projects inform this design. We reference their functionality to avoid reinventing solved problems and to identify what we can borrow, adapt, or intentionally omit.

### 3.1 Dagu (github.com/dagu-org/dagu)

**What it is:** A self-contained, Go-based workflow engine. Single binary, YAML workflow definitions, DAG-based step dependencies, built-in Web UI, file-system storage. No external database or message broker required.

**Relevant capabilities we should consider:**

| Capability | Description | Relevance to clai |
|---|---|---|
| YAML workflow definitions | Declarative step definitions with `command`, `depends`, `output`, `env` | Core — adopt similar structure |
| Step output capture | Steps can export variables via `output` field, referenced by downstream steps as `${VARIABLE}` | Core — needed for multi-step workflows |
| Preconditions | Steps can be gated on conditions (env var values, command exit codes, regex matches against output) | Core — useful for conditional execution |
| Chat/LLM executor | First-class step type for invoking LLMs (OpenAI, Anthropic, Gemini, Ollama). Multi-turn conversation support, extended thinking mode | Core — validates our LLM analysis approach |
| Lifecycle handlers | `onSuccess`, `onFailure`, `onExit` handlers at workflow level for cleanup and notification | Important — cleanup after failures |
| Parallel execution | DAG-based parallelism via `depends` graph; also inline parallel groups using nested arrays | Important for multi-stack scenarios |
| `maxActiveSteps` | Limit concurrent parallel steps | Nice-to-have — resource control |
| Sub-workflows | Call child workflows with parameters, full output/status propagation | Deferred to v1 |
| Secrets management | Reference-only secrets via KMS/Vault/OIDC, auto-redaction in logs | Important — we handle AWS/GCP credentials |
| Dry-run mode | Validate workflow without executing | Important — catch YAML errors before running |
| Stdout/stderr separation | Steps can route stdout and stderr to separate files | Nice-to-have |
| `continueOn` | Fine-grained control over what counts as failure: specific exit codes, output patterns | Deferred — v1 |
| Retry / repeat policies | Retry with backoff, polling with while/until | Deferred — v1 |
| `markSuccess` | Treat a step as successful even if it technically failed | Deferred — v1 |
| GitHub Actions executor | Delegates to nektos/act for running GH Actions inside Dagu workflows | Out of scope |
| Docker/SSH/HTTP executors | Run steps in containers, on remote hosts, or as HTTP calls | Out of scope |
| Web UI, scheduling, distributed execution | Operational features | Out of scope |

**Key insight from Dagu:** Their chat executor validates that LLM analysis as a workflow step is a proven pattern. Their `preconditions` mechanism provides flexible control flow without complex programming constructs.

### 3.2 nektos/act (github.com/nektos/act)

**What it is:** A Go tool that runs GitHub Actions workflows locally using Docker containers. Parses the GHA YAML schema and executes it with a local Docker runtime.

**Relevant capabilities we should consider:**

| Capability | Description | Relevance to clai |
|---|---|---|
| GHA YAML schema | `jobs`, `steps`, `strategy.matrix`, `needs`, `if`, `env`, `outputs` | Core — familiar syntax we should adopt |
| Matrix strategy | Generate job permutations from parameter combinations (`include`, `exclude`). CLI flag to run subset (`--matrix node:8`) | Core — essential for multi-stack/multi-target runs |
| `fail-fast` | Stop all matrix entries on first failure | Important — safety for production |
| Expression syntax | `${{ }}` with variable refs, comparisons, logical operators, function calls | Core — needed for interpolation and conditionals |
| Step `id` and `outputs` | Steps export outputs via `$GITHUB_OUTPUT` file; referenced as `${{ steps.<id>.outputs.<key> }}` | Core — same pattern needed |
| `if` conditionals | Job-level and step-level conditionals based on expressions | Important — conditional execution |
| Secrets handling | Interactive entry, env var, `.secrets` file, secure input prompt (no shell history) | Important — credential handling |
| `.env` file support | Load env vars from dotenv-format files | Nice-to-have — convenience |
| `--dry-run` mode | Parse and validate without executing | Important — validation |
| `--list` mode | List all jobs/steps without executing | Nice-to-have — discovery |
| Artifact server | Local artifact upload/download between jobs | Out of scope |
| Docker container execution | All steps run in Docker | Out of scope — we run on host |
| Event triggers | `on: push`, `on: pull_request`, etc. | Out of scope — manual only |
| Service containers | Sidecar services for jobs | Out of scope |

**Key insight from act:** The GitHub Actions YAML schema is widely known and well-documented. Adopting it (with extensions) means users don't need to learn a new format. The matrix strategy with `include`/`exclude` is the right abstraction for multi-target execution. Their `--matrix` CLI flag for running a subset is a great UX detail.

---

## 4. Functional Requirements

### 4.1 Workflow Definition

**FR-1:** The system SHALL accept workflows defined in YAML files.

**FR-2:** The workflow syntax SHALL be modeled on GitHub Actions workflow syntax, including the concepts of `jobs`, `steps`, `strategy.matrix`, `needs` (job dependencies), `if` (conditionals), and `env` (environment variables).

**FR-3:** The system SHALL extend the GHA syntax with the following clai-specific step attributes:
- `analyze` — boolean indicating the step's output should be sent to an LLM for analysis
- `analysis_prompt` — instructions for the LLM on what to look for
- `risk_level` — classification affecting human-in-the-loop behavior
- `context_from` / `context_for` — cross-step context passing for LLM analysis

**FR-4:** The system SHALL support workflow-level, job-level, and step-level environment variables, merged in that precedence order (step > job > workflow).

### 4.2 Step Execution

**FR-5:** Each step SHALL execute a shell command on the host system (no container requirement).

**FR-6:** Steps within a job SHALL execute sequentially by default.

**FR-7:** The system SHALL capture both stdout and stderr from each step.

**FR-8:** Steps SHALL be able to export output variables (key=value pairs) that are available to subsequent steps within the same job.

**FR-9:** Exported variables from prior steps SHALL be automatically available as environment variables to subsequent steps within the same job (auto-inheritance).

**FR-10:** Steps SHALL support a configurable timeout.

**FR-11:** Steps SHALL support a configurable working directory.

**FR-12:** Steps SHALL support a configurable shell (e.g., bash, zsh, sh).

### 4.3 Matrix Strategy

**FR-13:** Jobs SHALL support a `strategy.matrix` configuration that generates multiple runs of the same job with different parameter combinations.

**FR-14:** Matrix definitions SHALL support `include` (explicit parameter sets) and `exclude` (parameter sets to skip).

**FR-15:** Matrix entries SHALL be runnable in parallel or sequentially, controlled by a configuration flag.

**FR-16:** Matrix execution SHALL support `fail-fast` behavior: stop all remaining entries when one fails.

**FR-17:** The CLI SHALL support running a subset of matrix entries (e.g., `clai workflow run --matrix stack:ojin-staging`).

### 4.4 Control Flow

**FR-18:** Jobs SHALL support dependencies on other jobs via a `needs` field, forming a DAG.

**FR-19:** Steps and jobs SHALL support conditional execution via `if` expressions.

**FR-20:** Steps SHALL support preconditions that gate execution on environment variable values, command output patterns, or file existence checks.

**FR-21:** Workflows SHALL support lifecycle handlers:
- `onSuccess` — runs when the workflow completes successfully
- `onFailure` — runs when the workflow fails
- `onExit` — always runs, regardless of outcome (cleanup)

### 4.5 LLM Analysis

**FR-22:** When a step has `analyze: true`, the system SHALL send the step's output to a configured LLM along with the step's `analysis_prompt`.

**FR-23:** The LLM SHALL return a structured response containing:
- A decision: `proceed`, `halt`, or `needs_human`
- A reasoning explanation
- Optionally, a list of flagged items with severity levels

**FR-24:** If the LLM response cannot be parsed into the expected structure, the system SHALL treat it as `needs_human`.

**FR-25:** The system SHALL support a `risk_level` attribute on steps (`low`, `medium`, `high`) that modifies how the LLM's decision is applied:
- `low` — auto-proceed if LLM says proceed; auto-proceed on unparseable response
- `medium` — auto-proceed if LLM says proceed; require human input on unparseable response
- `high` — always require human confirmation, even if LLM says proceed

**FR-26:** Steps SHALL be able to provide their output as context for downstream LLM analysis steps via `context_for` / `context_from` attributes.

**FR-27:** The system SHALL truncate step output to fit within LLM context limits. The default maximum output size sent to the LLM SHALL be 100KB (~25k tokens). Output exceeding this limit SHALL be truncated with a strategy that preserves the beginning and end of the output (where the most important information typically is), with a clear marker indicating truncation occurred.

**FR-28:** The system SHALL support multiple LLM backends:

- **API-based providers:** Anthropic, OpenAI, and other providers via their respective APIs.
- **Local CLI tools:** The system SHALL support invoking local LLM CLI tools such as `claude`, `codex`, or `gemini` CLI, enabling users to leverage their existing LLM subscriptions without separate API keys.
- **claudemon (preferred for Claude):** The system SHALL support `claudemon`, a managed Claude CLI instance that clai starts and monitors. Communication with claudemon uses input/output streams, which is significantly faster than spawning a new CLI process per analysis step because the LLM session stays warm. This is the recommended backend for users with a Claude subscription.

**FR-29:** The LLM backend SHALL be configurable at both the global (clai config) and workflow level:

```yaml
# Example: workflow-level LLM configuration
llm:
  backend: claudemon          # claudemon | claude-cli | api
  # For API backends:
  # provider: anthropic
  # model: claude-sonnet-4-5-20250929
  # api_key_env: ANTHROPIC_API_KEY
```

### 4.6 Human Interaction

**FR-30:** When human input is required (due to risk level or LLM recommendation), the system SHALL pause execution and present:
- The step's name and command
- The step's output (or a summary)
- The LLM's analysis and reasoning (if applicable)
- A prompt for the user to approve, reject, or inspect further

**FR-31:** The human interaction interface SHALL be terminal-based (stdin/stdout) in v0/v1.

**FR-32:** The user SHALL be able to:
- Approve and continue
- Reject and halt the workflow
- Inspect the full step output before deciding
- Run an ad-hoc command for additional investigation before deciding

**FR-33:** During a human review pause, the user SHALL be able to continue the conversation with the LLM — asking follow-up questions about the output, requesting deeper analysis of specific items, or asking for comparisons with expected behavior — until they have enough information to make a decision. The LLM conversation context (including the original analysis and all follow-ups) SHALL be preserved throughout the review session and included in the execution log.

### 4.7 Secrets and Credentials

**FR-34:** The system SHALL support loading secrets from:
- Environment variables
- A `.secrets` file (dotenv format)
- Interactive secure input (not saved to shell history)

**FR-35:** Secrets SHALL be auto-redacted in log output.

**FR-36:** Secrets SHALL NOT be sent to the LLM as part of step output. The system SHALL scrub known secret values from output before LLM analysis.

### 4.8 Variable Interpolation

**FR-37:** The system SHALL support an expression syntax (e.g., `${{ }}`) for interpolating:
- Environment variables: `${{ env.VAR }}`
- Workflow variables: `${{ vars.VAR }}`
- Matrix parameters: `${{ matrix.key }}`
- Step outputs: `${{ steps.<id>.outputs.<key> }}`
- Job results: `${{ jobs.<id>.result }}`
- LLM analysis results: `${{ steps.<id>.analysis.decision }}`

**FR-38:** Expressions SHALL support string comparison and logical operators for use in `if` conditionals.

### 4.9 Execution Logging and Run History

**FR-39:** Every workflow run SHALL produce a structured log containing:
- Timestamp of each step's start and end
- The command that was executed
- The full stdout and stderr output
- The LLM's analysis (prompt, response, decision) if applicable
- The full LLM conversation transcript if the user engaged in follow-up questions during review
- The human's decision if applicable
- The final outcome (success, failure, skipped) of each step

**FR-40:** The log SHALL be written in a machine-readable format (JSON or similar) for downstream processing.

**FR-41:** The system SHALL also produce human-readable real-time output during execution (step names, status indicators, durations).

**FR-42:** Workflow run history — including per-step results, timestamps, and outcomes — SHALL be persisted in the clai database. This enables:
- Reviewing past runs (`clai workflow history <n>`)
- Comparing outcomes across runs
- Providing prior run context to LLM analysis in future versions

### 4.10 CLI Interface

**FR-43:** The workflow executor SHALL be invoked via the `clai` CLI, e.g.:
- `clai workflow run <file>` — execute a workflow
- `clai workflow run <file> --matrix key:value` — execute a subset of matrix entries
- `clai workflow validate <file>` — validate without executing (dry-run)
- `clai workflow list <file>` — list jobs and steps without executing
- `clai workflow run <file> --job <job_id>` — execute a specific job only
- `clai workflow history <n>` — show past runs

**FR-44:** The CLI SHALL support passing workflow parameters via command-line arguments (e.g., `-- STACK=ojin-production ROLE_ARN=arn:...`).

**FR-45:** The CLI SHALL support a `--dry-run` flag that parses the workflow, resolves the execution plan (including matrix expansion), and displays it without executing any commands.

---

## 5. Version Scoping

### 5.0 v0 — Minimum Viable Workflow (the Pulumi use case)

v0 delivers only what's needed to run the Pulumi compliance workflow described in Section 6. Everything else is deferred.

**v0 includes:**

| Area | What's in | FRs |
|---|---|---|
| Workflow parsing | YAML parsing, jobs, steps with `id`, `name`, `run`, `env` | FR-1, FR-2, FR-4 |
| Step execution | Sequential steps, shell execution on host, stdout/stderr capture | FR-5, FR-6, FR-7 |
| Output passing | Step output export via `$CLAI_OUTPUT`, auto-inheritance within job | FR-8, FR-9 |
| Matrix | `strategy.matrix` with `include`, sequential execution | FR-13, FR-14 |
| LLM analysis | `analyze` + `analysis_prompt`, structured response parsing | FR-22, FR-23, FR-24, FR-27 |
| LLM backend | claudemon support (primary), claude CLI fallback | FR-28, FR-29 |
| Risk levels | `risk_level` with decision matrix driving human prompts | FR-25 |
| Human interaction | Terminal-based approve/reject/inspect, **including LLM follow-up conversation** | FR-30, FR-31, FR-32, FR-33 |
| Secret scrubbing | Scrub secrets from LLM-bound output | FR-36 |
| Expression syntax | `${{ matrix.key }}`, `${{ steps.id.outputs.key }}`, `${{ env.VAR }}` | FR-37 (subset) |
| Basic logging | Real-time console output with step names and status | FR-41 |
| CLI | `clai workflow run <file>` | FR-43 (subset) |

**v0 explicitly excludes:**

- Parallel matrix execution (sequential only)
- `fail-fast` configuration (always fail-fast in v0)
- Job dependencies (`needs`)
- `if` conditionals / preconditions
- Lifecycle handlers (`onSuccess`, `onFailure`, `onExit`)
- Secrets from file or interactive input (env vars only)
- Secret auto-redaction in logs (only scrub before LLM)
- Structured JSON execution logs and run history persistence
- `--dry-run`, `--list`, `--matrix` CLI flags
- `--job` filtering
- Step timeout, working directory, shell configuration
- `context_from` / `context_for` cross-step LLM context
- Expression comparisons and logical operators
- API-based LLM providers (Anthropic/OpenAI API)
- Workflow-level LLM configuration (uses global clai config)

### 5.1 v1 — Full Specification

v1 implements the complete functional specification as described in Section 4.

### 5.2 Deferred to v2+

| Capability | Rationale for deferring |
|---|---|
| Container-based step execution (Docker) | Adds significant complexity; host execution is sufficient |
| Remote execution (SSH) | Complexity without immediate need |
| Web UI / Dashboard | clai is a CLI tool |
| Scheduling / cron triggers | Manual invocation is sufficient |
| Sub-workflows / composition | Useful but not essential for initial use cases |
| Artifact management between jobs | No current use case requires this |
| Distributed execution / worker routing | Enterprise feature |
| Event triggers (webhook, file watch) | v1 is manual-only |
| MCP server integration for LLM steps | Could enhance LLM capability but adds complexity |
| Retry / repeat policies | Important but not needed for v0/v1 Pulumi use case |
| `continueOn` (exit code / output pattern matching) | Fine-grained failure control — useful but deferrable |
| `markSuccess` | Best-effort steps — niche |
| LLM token usage tracking | Interesting but not essential |
| Notification hooks | Lifecycle handlers cover the need |

---

## 6. Example Workflow (v0 Target)

This is the workflow that v0 must be able to execute:

```yaml
name: pulumi-compliance-run
description: Run Pulumi preview and apply across all infrastructure stacks

env:
  AWS_REGION: eu-central-1

jobs:
  deploy:
    strategy:
      matrix:
        include:
          - stack: ojin-production
            role_arn: arn:aws:iam::111111:role/pulumi-deploy
            risk: high
          - stack: ojin-staging
            role_arn: arn:aws:iam::222222:role/pulumi-deploy
            risk: medium
          - stack: journee-dev
            role_arn: arn:aws:iam::333333:role/pulumi-deploy
            risk: low

    steps:
      - id: assume-role
        name: Assume AWS Role
        run: |
          CREDS=$(aws sts assume-role \
            --role-arn ${{ matrix.role_arn }} \
            --role-session-name clai-deploy \
            --query 'Credentials' --output json)
          echo "AWS_ACCESS_KEY_ID=$(echo $CREDS | jq -r .AccessKeyId)" >> $CLAI_OUTPUT
          echo "AWS_SECRET_ACCESS_KEY=$(echo $CREDS | jq -r .SecretAccessKey)" >> $CLAI_OUTPUT
          echo "AWS_SESSION_TOKEN=$(echo $CREDS | jq -r .SessionToken)" >> $CLAI_OUTPUT

      - id: preview
        name: Pulumi Preview
        run: pulumi preview --stack ${{ matrix.stack }} --diff
        analyze: true
        risk_level: ${{ matrix.risk }}
        analysis_prompt: |
          Review this Pulumi preview for stack "${{ matrix.stack }}".

          Flag as HALT if you see:
          - Any resource deletions that look unintentional
          - Any IAM policy or security group changes
          - Changes to database resources (RDS, DynamoDB)
          - More than 10 resources changing

          Flag as NEEDS_HUMAN if you see:
          - Between 5-10 resources changing
          - Any changes you're uncertain about

          If everything looks like routine config updates, PROCEED.

      - id: apply
        name: Pulumi Apply
        run: pulumi up --yes --stack ${{ matrix.stack }}
        risk_level: high
```

**What v0 does with this workflow:**

1. Parses the YAML and expands the matrix into 3 runs (ojin-production, ojin-staging, journee-dev)
2. Runs them **sequentially** (ojin-production first, then ojin-staging, then journee-dev)
3. For each matrix entry:
   - Executes `assume-role`, captures credentials into step outputs, auto-inherits them as env vars
   - Executes `preview`, captures output, sends it to claudemon with the analysis prompt
   - Parses the LLM response. Based on `risk_level`:
     - `low` (journee-dev): auto-proceeds if LLM says proceed
     - `medium` (ojin-staging): auto-proceeds if LLM says proceed, prompts human if unclear
     - `high` (ojin-production): always prompts human, even if LLM says proceed
   - During human review: user can ask the LLM follow-up questions ("what exactly changes in the security group?", "show me only the deletions", etc.)
   - If approved, executes `apply` (which also has `risk_level: high`, so always prompts)
4. If any matrix entry fails, stops (implicit fail-fast)

---

## 7. Resolved Design Decisions

1. **Workflow composition:** Deferred to v2. No `uses:` or sub-workflow support in v0 or v1.

2. **Cross-run LLM context:** Not needed. Each workflow run is independent. The LLM does not receive analysis from prior runs. Run history (FR-42) is for human review only.

3. **Ad-hoc commands during review:** Ad-hoc command output is visible only to the user, not fed back to the LLM. The LLM conversation during review (FR-33) is limited to follow-up questions about the original step output.

---

## 8. Glossary

| Term | Definition |
|---|---|
| **Workflow** | A YAML file defining a complete automation consisting of one or more jobs |
| **Job** | A named group of steps. Jobs can depend on other jobs and can be parameterized via matrix |
| **Step** | A single command execution within a job, with optional LLM analysis |
| **Matrix** | A strategy for running the same job multiple times with different parameters |
| **Analysis** | The act of sending a step's output to an LLM for intelligent review |
| **Risk level** | A classification (low/medium/high) that determines when human confirmation is required |
| **Lifecycle handler** | A command that runs on workflow success, failure, or exit |
| **Precondition** | A condition that must be true before a step executes |
| **claudemon** | A managed Claude CLI instance that clai starts and monitors, communicating via input/output streams for fast LLM interaction without per-request process spawning |
