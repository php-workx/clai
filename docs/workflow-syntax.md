# Workflow Syntax Reference

Workflows let you define multi-step shell automations with LLM analysis gates, matrix expansion, and secret management. Each workflow is a YAML file that describes a sequence of steps to execute.

Workflow files are strictly parsed — unrecognized YAML keys produce an error, catching typos early.

## Minimal example

```yaml
name: Hello World
jobs:
  greet:
    steps:
      - id: hello
        name: Say hello
        run: echo "Hello from clai workflows!"
```

## Workflow file location

clai discovers workflow files from two directories, checked in order:

| Priority | Path | Scope |
|----------|------|-------|
| 1 | `.clai/workflows/` | Project-local (relative to working directory) |
| 2 | `~/.clai/workflows/` | User-global |

Files may use `.yaml` or `.yml` extensions. You can also pass a direct file path.

> **Note:** The user-global directory defaults to `~/.clai/workflows/` on Unix and `%APPDATA%\clai\workflows\` on Windows. Override the base directory with `$CLAI_HOME`.

When you run a workflow by name (e.g. `clai workflow run deploy`), clai searches for `deploy.yaml` and `deploy.yml` in the directories above, using the first match.

---

## Top-level keys

### `name`

**Required.** The display name of the workflow.

| | |
|---|---|
| **Type** | `string` |

```yaml
name: Infrastructure Compliance Check
```

### `description`

**Optional.** A human-readable description of what the workflow does.

| | |
|---|---|
| **Type** | `string` |

```yaml
description: Validates Pulumi stacks against compliance policies
```

### `env`

**Optional.** Workflow-level environment variables available to all steps.

| | |
|---|---|
| **Type** | `map<string, string>` |

Values are available as `$VAR` in shell commands and as `${{ env.VAR }}` in expressions. Workflow-level env has the lowest user-defined precedence — it is overridden by job, step, matrix, and `--var` CLI flags.

```yaml
env:
  AWS_REGION: us-east-1
  LOG_LEVEL: info
```

### `secrets`

**Optional.** Secrets to load before execution. Secret values are masked in all captured output (replaced with `***`).

| | |
|---|---|
| **Type** | `list` of [secret definitions](#secret-definition) |

```yaml
secrets:
  - name: API_TOKEN
    from: env
  - name: DB_PASSWORD
    from: file
    path: ~/.secrets/db-password
  - name: DEPLOY_KEY
    from: interactive
    prompt: "Enter the deployment key:"
```

#### Secret definition

| Key | Type | Required | Description |
|-----|------|----------|-------------|
| `name` | `string` | Yes | Environment variable name for the secret |
| `from` | `string` | Yes | Source: `"env"`, `"file"`, or `"interactive"` |
| `path` | `string` | When `from: file` | Path to the file containing the secret value |
| `prompt` | `string` | When `from: interactive` | Prompt text shown when requesting the value interactively |

**Valid `from` values:**

| Value | Description |
|-------|-------------|
| `env` | Read from an existing environment variable (currently supported) |
| `file` | Read from a file on disk |
| `interactive` | Prompt the user at runtime |

> **Note:** `file` and `interactive` are accepted in YAML but not yet functional at runtime. Only `env` is currently implemented.

### `requires`

**Optional.** External tools that must be available on `$PATH` before the workflow runs.

| | |
|---|---|
| **Type** | `list` of `string` |

Each entry must be a non-empty string.

```yaml
requires:
  - pulumi
  - jq
  - aws
```

### `jobs`

**Required.** A map of job definitions. Each key is the job ID.

| | |
|---|---|
| **Type** | `map<string, job>` |

Currently, workflows support exactly one job (v0). Defining more than one job is a validation error.

```yaml
jobs:
  compliance:
    steps:
      - id: check
        name: Run checks
        run: echo "checking..."
```

---

## `jobs.<job_id>`

### `jobs.<job_id>.name`

**Optional.** Display name for the job.

| | |
|---|---|
| **Type** | `string` |

```yaml
jobs:
  deploy:
    name: Production Deploy
    steps: [...]
```

### `jobs.<job_id>.needs`

**Optional.** List of job IDs that this job depends on. Reserved for future multi-job support.

| | |
|---|---|
| **Type** | `list` of `string` |

Each referenced job must exist in the workflow. Circular dependencies are detected and rejected.

```yaml
jobs:
  build:
    steps: [...]
  deploy:
    needs: [build]
    steps: [...]
```

### `jobs.<job_id>.env`

**Optional.** Job-level environment variables. Overrides workflow-level `env`.

| | |
|---|---|
| **Type** | `map<string, string>` |

```yaml
jobs:
  deploy:
    env:
      ENVIRONMENT: production
      VERBOSE: "true"
    steps: [...]
```

### `jobs.<job_id>.strategy`

**Optional.** Strategy configuration for matrix expansion.

### `jobs.<job_id>.strategy.matrix`

Matrix definition for running steps across parameter combinations.

#### `jobs.<job_id>.strategy.matrix.include`

**Required** (within matrix). Each entry is a map representing one parameter combination.

| | |
|---|---|
| **Type** | `list` of `map<string, string>` |

All entries must have the **same set of keys**. Inconsistent keys across entries is a validation error. Values are accessible in expressions via `${{ matrix.KEY }}`.

```yaml
strategy:
  matrix:
    include:
      - stack: dev
        region: us-east-1
        risk: low
      - stack: staging
        region: us-west-2
        risk: medium
      - stack: prod
        region: us-east-1
        risk: high
```

#### `jobs.<job_id>.strategy.matrix.exclude`

**Optional.** Combinations to skip. Reserved for future use.

| | |
|---|---|
| **Type** | `list` of `map<string, string>` |

### `jobs.<job_id>.strategy.fail_fast`

**Optional.** Whether to stop on the first matrix combination failure.

| | |
|---|---|
| **Type** | `boolean` |
| **Default** | `true` |

```yaml
strategy:
  fail_fast: false
  matrix:
    include:
      - stack: dev
      - stack: staging
```

### `jobs.<job_id>.steps`

**Required.** An ordered list of step definitions. Must contain at least one step.

| | |
|---|---|
| **Type** | `list` of [step definitions](#jobsjob_idsteps-1) |

---

## `jobs.<job_id>.steps[*]`

### `steps[*].id`

**Required.** Unique identifier for the step within the job.

| | |
|---|---|
| **Type** | `string` |

Used to reference step outputs in expressions (`${{ steps.<id>.outputs.<KEY> }}`). Duplicate IDs within the same job are a validation error.

```yaml
- id: preview
  name: Preview changes
  run: pulumi preview
```

### `steps[*].name`

**Required.** Display name for the step. Supports [expressions](#expressions).

| | |
|---|---|
| **Type** | `string` |

```yaml
- id: deploy
  name: "Deploy ${{ matrix.stack }}"
  run: pulumi up --yes
```

### `steps[*].run`

**Required.** Shell command to execute. Supports [expressions](#expressions).

| | |
|---|---|
| **Type** | `string` |

Multi-line commands use YAML literal block scalars (`|`):

```yaml
- id: setup
  name: Configure environment
  run: |
    export KUBECONFIG=~/.kube/config
    kubectl cluster-info
    kubectl get nodes
```

### `steps[*].env`

**Optional.** Step-level environment variables. Overrides workflow and job env, but is overridden by matrix variables and `--var` CLI flags. Expression values are supported.

| | |
|---|---|
| **Type** | `map<string, string>` |

```yaml
- id: deploy
  name: Deploy
  env:
    STACK: ${{ matrix.stack }}
    PULUMI_CONFIG_PASSPHRASE: ${{ env.PASSPHRASE }}
  run: pulumi up --stack $STACK --yes
```

### `steps[*].shell`

**Optional.** Controls how the `run` command is executed.

| | |
|---|---|
| **Type** | `boolean` or `string` |
| **Default** | `true` (platform default shell) |

| Value | Behavior |
|-------|----------|
| `true` (or omitted) | Use `/bin/sh` on Unix, `cmd.exe` on Windows |
| `false` | Argv mode — direct exec, no shell wrapping |
| `"sh"` | Use `/bin/sh` |
| `"bash"` | Use `bash` |
| `"zsh"` | Use `zsh` |
| `"fish"` | Use `fish` |
| `"pwsh"` | Use PowerShell |
| `"cmd"` | Use `cmd.exe` |

```yaml
- id: script
  name: Run bash script
  shell: bash
  run: |
    set -euo pipefail
    echo "Running in bash"
```

```yaml
- id: binary
  name: Run binary directly
  shell: false
  run: ./my-tool --flag value
```

### Reserved step fields

The following fields are accepted in YAML but ignored in the current version. They are reserved for future use:

- `if`
- `timeout_minutes`
- `retry`
- `continue_on_error`
- `working_directory`
- `outputs`

### `steps[*].analyze`

**Optional.** Whether to send step output to an LLM for analysis.

| | |
|---|---|
| **Type** | `boolean` |
| **Default** | `false` |

When `true`, the `analysis_prompt` field is required.

```yaml
- id: scan
  name: Security scan
  run: trivy fs --severity HIGH,CRITICAL .
  analyze: true
  analysis_prompt: "Are there any critical vulnerabilities that would block deployment?"
```

### `steps[*].analysis_prompt`

**Required when `analyze: true`.** Instructions for the LLM analysis of step output. Supports [expressions](#expressions).

| | |
|---|---|
| **Type** | `string` |

```yaml
- id: drift
  name: "Check drift for ${{ matrix.stack }}"
  run: pulumi preview --diff --stack ${{ matrix.stack }}
  analyze: true
  analysis_prompt: |
    Review the Pulumi preview output for the ${{ matrix.stack }} stack.
    Flag any unexpected resource deletions or replacements.
```

### `steps[*].risk_level`

**Optional.** Controls the review gate behavior after LLM analysis. Supports [expressions](#expressions).

| | |
|---|---|
| **Type** | `string` |
| **Default** | `"medium"` |

| Value | Behavior |
|-------|----------|
| `"low"` | Auto-proceed unless the LLM says halt |
| `"medium"` | Auto-proceed on "proceed" decision; human review otherwise |
| `"high"` | Always require human review |

```yaml
- id: deploy
  name: "Deploy ${{ matrix.stack }}"
  run: pulumi up --yes --stack ${{ matrix.stack }}
  analyze: true
  analysis_prompt: "Did the deployment complete successfully?"
  risk_level: ${{ matrix.risk }}
```

---

## Expressions

Expressions use the `${{ <expression> }}` syntax and are resolved at runtime.

### Supported locations

Expressions are resolved in the following step fields:

- `run`
- step `env` values
- `name`
- `risk_level`
- `analysis_prompt`

> **Note:** Workflow-level and job-level `env` values do not support expressions.

### Namespaces

| Namespace | Syntax | Description |
|-----------|--------|-------------|
| `env` | `${{ env.VAR_NAME }}` | Environment variable from the merged env context |
| `matrix` | `${{ matrix.KEY }}` | Value from the current matrix combination |
| `steps` | `${{ steps.STEP_ID.outputs.KEY }}` | Output from a previous step |

### Rules

- Expressions must follow the `namespace.key` format.
- Nesting is not allowed — `${{ ${{ }} }}` is invalid.
- Empty expressions (`${{ }}`) are invalid.
- Unknown namespaces produce an error.
- Unresolved references (missing keys) produce an error.

### Examples

```yaml
run: echo "Deploying to ${{ env.AWS_REGION }}"
```

```yaml
name: "Deploy ${{ matrix.stack }}"
```

```yaml
run: |
  echo "Previous step returned: ${{ steps.check.outputs.STATUS }}"
```

---

## Step outputs

Steps can produce outputs that are available to subsequent steps via expressions.

### Writing outputs

Write `KEY=value` pairs to the file path in the `$CLAI_OUTPUT` environment variable. The `$CLAI_OUTPUT` file is automatically created for each step and removed after outputs are parsed.

```yaml
- id: version
  name: Get version
  run: |
    VERSION=$(cat VERSION)
    echo "VERSION=$VERSION" >> "$CLAI_OUTPUT"
    echo "SHA=$(git rev-parse --short HEAD)" >> "$CLAI_OUTPUT"
```

### Output file format

- One `KEY=value` pair per line
- Keys must match `[A-Za-z_][A-Za-z0-9_]*`
- Blank lines are skipped
- Lines starting with `#` are comments and skipped
- Lines without `=` are skipped with a warning

### Reading outputs

Use the `steps` expression namespace:

```yaml
- id: tag
  name: Tag release
  run: |
    git tag "v${{ steps.version.outputs.VERSION }}"
```

Step outputs are also exported as environment variables to all subsequent steps.

---

## Environment variable precedence

Environment variables are merged in the following order (lowest to highest precedence):

| Priority | Source | Description |
|----------|--------|-------------|
| 1 (lowest) | OS environment | Inherited from the parent process |
| 2 | Step outputs | Exported outputs from prior steps |
| 3 | Workflow `env` | Top-level `env` block |
| 4 | Job `env` | `jobs.<id>.env` block |
| 5 | Step `env` | `steps[*].env` block |
| 6 | Matrix variables | Current `strategy.matrix` combination |
| 7 (highest) | `--var` CLI flags | Variables passed on the command line |

A variable defined at a higher priority level overrides the same key from any lower level.

---

## Complete example

The following workflow demonstrates most features — matrix expansion, secrets, expressions, step outputs, LLM analysis, and risk levels:

```yaml
name: Pulumi Compliance
description: Validate and deploy Pulumi stacks with compliance gates

env:
  PULUMI_BACKEND_URL: s3://my-state-bucket
  LOG_LEVEL: info

secrets:
  - name: PULUMI_ACCESS_TOKEN
    from: env
  - name: DEPLOY_KEY
    from: file
    path: ~/.secrets/deploy-key

requires:
  - pulumi
  - jq

jobs:
  compliance:
    name: Stack Compliance
    env:
      PULUMI_CONFIG_PASSPHRASE: ""
    strategy:
      fail_fast: true
      matrix:
        include:
          - stack: dev
            region: us-east-1
            risk: low
          - stack: staging
            region: us-west-2
            risk: medium
          - stack: prod
            region: us-east-1
            risk: high

    steps:
      - id: select
        name: "Select stack: ${{ matrix.stack }}"
        run: pulumi stack select ${{ matrix.stack }}

      - id: preview
        name: "Preview ${{ matrix.stack }}"
        run: |
          pulumi preview --diff --json 2>&1 | tee /tmp/preview.json
          CHANGE_COUNT=$(jq '.changeSummary | to_entries | map(.value) | add // 0' /tmp/preview.json)
          echo "CHANGE_COUNT=$CHANGE_COUNT" >> "$CLAI_OUTPUT"
        analyze: true
        analysis_prompt: |
          Review the Pulumi preview for the ${{ matrix.stack }} stack in ${{ matrix.region }}.
          Flag any unexpected resource deletions or security group changes.
        risk_level: ${{ matrix.risk }}

      - id: deploy
        name: "Deploy ${{ matrix.stack }}"
        env:
          AWS_DEFAULT_REGION: ${{ matrix.region }}
          CHANGES: ${{ steps.preview.outputs.CHANGE_COUNT }}
        run: |
          echo "Applying $CHANGES changes to ${{ matrix.stack }}"
          pulumi up --yes --stack ${{ matrix.stack }}
        analyze: true
        analysis_prompt: |
          Did the deployment to ${{ matrix.stack }} complete successfully?
          Check for any errors or warnings in the output.
        risk_level: ${{ matrix.risk }}
```
