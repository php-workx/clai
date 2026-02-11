# Plan Review: Missing Requirements Judge

**Date:** 2026-02-11
**Reviewer:** Council Member 1 -- THE MISSING REQUIREMENTS JUDGE
**Target:** `.agents/plans/2026-02-11-workflow-execution-tier0.md`
**Spec:** `specs/tech_workflows_v4.md` v4.2
**Model:** Claude Opus 4.6

```json
{
  "verdict": "WARN",
  "confidence": "HIGH",
  "key_insight": "Seven spec requirements have no issue owner, and three critical integration seams between issues are unspecified",
  "findings": [
    {
      "severity": "critical",
      "category": "requirements",
      "description": "TTY detection (spec SS4.3) is not covered by any issue. The spec requires TTYDetector interface with platform-specific implementations (tty_unix.go, tty_windows.go), mode auto-detection (SS4.2 item 4: interactive if TTY, non-interactive-fail otherwise), and the Non-TTY fallback (MR-1: warn and fall back when --mode interactive but no TTY). No issue mentions TTYDetector or tty detection files. This is load-bearing: without it, mode selection cannot work, interactive/non-interactive behavior is broken, and the display module (Issue 16) cannot decide between TTY and non-TTY output modes.",
      "location": "Spec SS4.2, SS4.3 -- no corresponding issue",
      "recommendation": "Add TTY detection to Issue 14 (Human review UI) or Issue 18 (CLI commands), since both consume it. Alternatively create a standalone Wave 1 issue since it has no dependencies and is consumed by multiple Wave 3/4 issues."
    },
    {
      "severity": "critical",
      "category": "requirements",
      "description": "Pre-run dependency detection (spec SS16) is not covered by any issue. The spec defines CheckDependencies(), auto-detection of 'claude' when analyze:true steps exist, exit code 8 (ExitDependencyMissing), and --require-deps / --skip-dep-check flags. The plan's Issue 18 lists --daemon and --verbose as flags but not --require-deps or --skip-dep-check. Tier 0 acceptance criteria SS24.1 does not explicitly list dependency checking, but exit code 8 IS in the exit codes (Issue 19), creating a gap: the exit code is defined but the code that triggers it does not exist.",
      "location": "Spec SS16, SS15.1 exit code 8 -- no corresponding issue",
      "recommendation": "Create a new Wave 2 issue for dependency detection (depends on Issue 3 for WorkflowDef.Requires field), or fold into Issue 4 (validator) since dependency checking is conceptually a pre-execution validation step."
    },
    {
      "severity": "critical",
      "category": "integration",
      "description": "The daemon client / gRPC connection layer between the CLI (Issue 18) and the daemon handlers (Issue 13) has no owner. Issue 13 implements the server-side handlers, and Issue 18 wires the CLI. But nowhere does any issue describe: (a) how the CLI establishes a gRPC connection to claid, (b) who implements the EnsureDaemon() call to auto-start claid if not running, (c) how the --daemon=false flag suppresses IPC and routes to the D24 fallback path. The existing codebase has IPC code in internal/ipc/ but the plan never mentions extending it for workflow RPCs. This is the glue between the two halves of the system.",
      "location": "Between Issue 13 (daemon handlers) and Issue 18 (CLI commands) -- gap in dependency graph",
      "recommendation": "Add a Wave 3 or Wave 4 issue explicitly covering the workflow daemon client: gRPC dial, EnsureDaemon, graceful degradation when daemon is unavailable, and the --daemon=false code path. Alternatively, make this an explicit sub-task within Issue 18 with its own acceptance criteria."
    },
    {
      "severity": "significant",
      "category": "requirements",
      "description": "Workflow file discovery (spec SS7.4) is partially covered. Issue 18 mentions 'workflow run <file>' and stdin via '-', but the name-lookup path (searching .clai/workflows/, ~/.clai/workflows/, and config search_paths) is not called out in any issue's acceptance criteria. The spec has an entire file (discovery.go) in the package layout (SS21.1). Issue 17 defines SearchPaths config but nobody consumes it.",
      "location": "Spec SS7.4 name lookup -- not in Issue 18 acceptance criteria",
      "recommendation": "Add file discovery acceptance criteria to Issue 18: 'workflow run my-workflow looks up .clai/workflows/my-workflow.yaml and ~/.clai/workflows/my-workflow.yaml'. This is relatively low effort but important for usability."
    },
    {
      "severity": "significant",
      "category": "requirements",
      "description": "File permission checks (spec SS19.2-19.4) are not covered by any issue. The spec requires checkFilePermissions() with platform-specific implementations (security_unix.go, security_windows.go) and the strict_permissions config field. In Tier 0 this is a warning only, but it is still specified behavior. Issue 17 defines StrictPermissions in WorkflowsConfig, but no issue implements the check.",
      "location": "Spec SS19.2-19.5 -- no corresponding issue",
      "recommendation": "Add file permission checking as an acceptance criterion to Issue 4 (validator) or Issue 18 (CLI commands). Since it is warning-only in Tier 0, it can be minimal."
    },
    {
      "severity": "significant",
      "category": "requirements",
      "description": "The WorkflowRunStartRequest proto message in the plan's Issue 1 lists workflow_hash (M12) and started_at_unix_ms (M18), but does NOT list the workflow_hash field in the WorkflowRunStartRequest -- the spec SS13.1 proto definition for WorkflowRunStartRequest has only run_id, workflow_name, workflow_path, execution_mode, params_json (5 fields). The M12 finding says workflow_hash is in SQLite but missing from proto. The plan notes M12 was addressed but the spec proto message still has only 5 fields. This means either the proto needs a 6th field, or workflow_hash is computed server-side from workflow_path. The plan does not clarify which approach.",
      "location": "Issue 1 acceptance criteria vs spec SS13.1 WorkflowRunStartRequest",
      "recommendation": "Clarify in Issue 1: either add workflow_hash as field 6 to WorkflowRunStartRequest (CLI computes hash before sending), or document that the daemon does not receive the hash and the column is populated by the CLI via a separate path. The former is simpler."
    },
    {
      "severity": "significant",
      "category": "integration",
      "description": "The runner's orchestration of all components (Issue 11) depends on the LLM analysis integration (Issue 12), but Issue 11 is listed as Wave 3 without a dependency on Issue 12, and Issue 12 is also Wave 3. The spec's execution flow (SS20.1 step 7: 'If analyze: true -> LLM analysis') means the runner MUST call the analyzer. In the plan, Issue 11 and 12 are both Wave 3 with no declared dependency between them. This creates a sequencing problem: the runner cannot be fully tested without the analyzer, and the analyzer cannot be integration-tested without the runner.",
      "location": "Issue 11 (executor) and Issue 12 (LLM analysis) -- missing cross-dependency",
      "recommendation": "Either: (a) Add Issue 12 as a dependency of Issue 11 and move Issue 12 earlier (Wave 2 is possible since its real deps are Issues 3, 9, 10), or (b) define a clear interface boundary in Issue 11 that the analyzer plugs into, with Issue 11 using a mock analyzer for its own tests. Option (b) is implicitly the plan's intent but should be explicit in Issue 11's acceptance criteria."
    },
    {
      "severity": "significant",
      "category": "requirements",
      "description": "The plan's Issue 14 (Human review UI) lists '[q]uestion uses single-turn QueryFast()' as an acceptance criterion, which is correct per the scope judge's ruling. However, Issue 14 does not specify who provides the LLM client for this. The TerminalReviewer in spec SS11.2 has an 'llm *ReviewSession' field. In Tier 0, [q]uestion should call QueryFast() directly (not via ReviewSession which is Tier 1). But Issue 14 does not declare a dependency on any LLM path -- neither Issue 12 (LLM analysis) nor Issue 13 (daemon handlers). The [q]uestion feature needs an LLM client to be injected into the reviewer.",
      "location": "Issue 14 acceptance criteria -- missing LLM dependency for [q]uestion",
      "recommendation": "Add a dependency from Issue 14 to Issue 12 (or at minimum to the LLMQuerier interface from Issue 13), and add acceptance criteria: 'TerminalReviewer accepts an LLM query function for [q]uestion; Tier 0 uses single-turn QueryFast()'. The plan already has Issue 14 depending on Issue 12, but the specific [q]uestion LLM path is not called out."
    },
    {
      "severity": "significant",
      "category": "requirements",
      "description": "Execution mode selection logic (spec SS4.2) -- the priority chain (CLI flag > env var > config > auto-detect) -- is not owned by any specific issue. Issue 17 defines the config, Issue 18 defines CLI flags, but nobody implements the mode resolution function that combines all four sources. This is a small but critical function that determines the entire execution behavior.",
      "location": "Spec SS4.2 -- resolution logic not owned by any issue",
      "recommendation": "Add mode resolution to Issue 18 (CLI commands) acceptance criteria, or to Issue 14 (review UI) since it determines which InteractionHandler to instantiate."
    },
    {
      "severity": "minor",
      "category": "requirements",
      "description": "The plan's Issue 19 lists exit code 3 as 'user cancelled/Ctrl+C' but the spec SS15.1 defines exit code 3 as ExitHumanReject (human rejected a step) and exit code 4 as ExitCancelled (Ctrl+C). Issue 19 conflates codes 3 and 4.",
      "location": "Issue 19 acceptance criteria",
      "recommendation": "Fix Issue 19 to match spec: code 3 = human reject, code 4 = Ctrl+C cancel. Both are Tier 0 exit codes."
    },
    {
      "severity": "minor",
      "category": "requirements",
      "description": "The Cobra command group for workflow commands is mentioned in Issue 18 ('groupWorkflow') but the existing codebase only defines groupCore and groupSetup in internal/cmd/root.go. The plan does not specify adding the new group definition or what its display title should be.",
      "location": "Issue 18 vs internal/cmd/root.go",
      "recommendation": "Add to Issue 18: 'Add groupWorkflow Cobra group with title Workflow Commands: to root.go AddGroup call'."
    },
    {
      "severity": "minor",
      "category": "requirements",
      "description": "The plan does not mention sanitizePathComponent() (spec SS7.5) which is required for safe log file paths and artifact directories. This function prevents path traversal from step IDs or matrix keys. It has both Unix and Windows concerns.",
      "location": "Spec SS7.5 -- not in any issue",
      "recommendation": "Add to Issue 15 (RunArtifact) or Issue 3 (parser/types) since it operates on step IDs and matrix keys that come from YAML."
    },
    {
      "severity": "minor",
      "category": "requirements",
      "description": "The spec SS12.5 defines a full log file layout where full stdout/stderr is written to per-step log files under ~/.clai/workflow-logs/<run-id>/. Issue 15 only mentions JSONL event log. The full stdout/stderr log files are separate from the JSONL artifact and from the 4KB tails stored in SQLite. Nobody owns the full log file writing.",
      "location": "Spec SS12.5 log file layout -- not in Issue 15 or Issue 11",
      "recommendation": "Add full stdout/stderr log file writing to Issue 11 (executor) or Issue 15 (artifact). The executor already captures output via limitedBuffer for tails; it also needs to tee to log files. This is a data flow concern that crosses Issue 9 (limitedBuffer), Issue 5 (ShellAdapter), and Issue 15."
    },
    {
      "severity": "minor",
      "category": "requirements",
      "description": "Stdin handling for non-interactive steps (spec SS11.3, MR-7) -- steps should receive /dev/null stdin -- is not mentioned in any issue. The stdinForMode() function is a small but important correctness detail to prevent subprocess hangs.",
      "location": "Spec SS11.3 -- not in any issue",
      "recommendation": "Add to Issue 5 (ShellAdapter) or Issue 11 (executor): 'Non-interactive steps receive nil stdin (os.DevNull)'. Small addition."
    },
    {
      "severity": "minor",
      "category": "testing",
      "description": "The plan's Issue 20 mentions 'make test passes' and 'make lint passes' but does not mention 'go test -race' which is a hard CI gate per spec SS23.6. Race detection is particularly important for limitedBuffer (Issue 9) and RunArtifact (Issue 15) which both use mutexes.",
      "location": "Issue 20 acceptance criteria vs spec SS23.6",
      "recommendation": "Add to Issue 20: 'go test -race ./internal/workflow/... passes with zero races'."
    }
  ],
  "recommendation": "Address the three critical findings before implementation begins: (1) add TTY detection to an existing issue or create a new one, (2) add dependency detection to an issue, (3) clarify daemon client ownership. The significant findings should be resolved by adding acceptance criteria to existing issues. None of these require new architectural decisions -- they are gaps in coverage, not design flaws.",
  "schema_version": 1
}
```

---

## Analysis

### Methodology

I performed a systematic requirements trace from the spec to the plan. For each spec section (SS4 through SS26), I verified that every Tier 0 requirement appears as an acceptance criterion in at least one issue. I then examined the codebase to validate assumptions about existing infrastructure. Finally, I checked the dependency graph for integration seams that fall between issues.

### Critical Findings

#### 1. TTY Detection Has No Owner

The spec (SS4.2, SS4.3) defines a `TTYDetector` interface with platform-specific implementations that is consumed by three downstream concerns:

- **Mode auto-detection** (SS4.2 item 4): "interactive if TTY detected, non-interactive-fail otherwise"
- **Non-TTY fallback** (MR-1): warn and degrade when --mode interactive specified without TTY
- **Display formatting** (SS11.5): color/spinner decisions depend on `isatty()`

No issue in the plan mentions TTY detection, `tty_unix.go`, `tty_windows.go`, or the `TTYDetector` interface. This is not a minor omission -- it is the mechanism that determines which execution path the entire system takes.

#### 2. Pre-Run Dependency Detection Has No Owner

The spec (SS16) defines `CheckDependencies()`, auto-detection of `claude` when `analyze: true` steps exist, exit code 8 (`ExitDependencyMissing`), and the `--require-deps` / `--skip-dep-check` CLI flags. The plan's Issue 19 defines exit code 8, but no issue implements the code that would trigger that exit code. The dependency check function itself is straightforward (`exec.LookPath` in a loop) but needs to be wired into the execution flow before steps run.

#### 3. Daemon Client Layer Is Unowned

The plan has a clean split between daemon-side (Issue 13: handlers) and CLI-side (Issue 18: commands). But the glue between them -- establishing a gRPC connection, calling `EnsureDaemon()` to auto-start claid, and implementing the `--daemon=false` fallback path -- is not in any issue. The existing codebase has `internal/ipc/` with client code, but the plan never mentions extending it. Issue 18's description says "connect to claid (or fallback)" without specifying who implements the connection and fallback logic.

### Significant Findings

#### 4. Workflow File Discovery Is Incomplete

Issue 18 says `workflow run <file>` but does not mention name-based lookup (e.g., `clai workflow run my-workflow` searching `~/.clai/workflows/my-workflow.yaml`). The spec (SS7.4) defines a three-location search path. Issue 17 defines `SearchPaths` config but no issue consumes it. This is a usability feature, not a correctness one, but it is in the Tier 0 spec.

#### 5. File Permission Checks Not Covered

The spec (SS19.2-19.5) requires `checkFilePermissions()` with platform-specific files. Tier 0 is warning-only on Unix, no-op on Windows. Still, no issue mentions it.

#### 6. WorkflowRunStartRequest Proto Field Ambiguity

The plan's Issue 1 says `workflow_hash` field per M12, but the spec's proto definition for `WorkflowRunStartRequest` only has 5 fields (no hash). The plan claims M12 is addressed but does not specify whether the field is added to the proto or computed server-side.

#### 7. Runner-Analyzer Dependency Gap

Issues 11 (executor) and 12 (LLM analysis) are both Wave 3 with no dependency between them. But the executor MUST call the analyzer when `analyze: true`. The interface boundary between them needs to be explicit.

#### 8. [q]uestion Feature LLM Path Unclear

Issue 14 says `[q]uestion uses single-turn QueryFast()` but does not declare how it obtains an LLM client. The dependency on Issue 12 is declared but the specific wiring for the question feature is not.

#### 9. Mode Resolution Logic Unowned

The four-source mode priority chain (CLI flag > env var > config > auto-detect) is in the spec (SS4.2) but not in any issue's acceptance criteria.

### Minor Findings

- **Exit code mismatch**: Issue 19 says code 3 = "user cancelled/Ctrl+C" but spec says 3 = human reject, 4 = Ctrl+C.
- **Cobra group**: Plan mentions `groupWorkflow` but does not specify adding it to root.go's `AddGroup` call alongside the existing `groupCore` and `groupSetup`.
- **sanitizePathComponent()**: Required by SS7.5 for safe log paths, not in any issue.
- **Full stdout/stderr log files**: SS12.5 defines per-step log files separate from JSONL artifact and SQLite tails. No issue owns their creation.
- **Stdin handling**: `stdinForMode()` from SS11.3 (steps get `/dev/null` stdin in non-interactive mode) not mentioned.
- **Race detection**: Issue 20 does not list `go test -race` as an acceptance criterion despite it being a hard CI gate in SS23.6.

### Codebase Assumption Verification

I verified the following assumptions against the actual codebase:

- **Store interface** (`internal/storage/store.go`): Confirmed. Current interface has 12 methods. Adding 9 workflow methods is additive as claimed.
- **Server struct** (`internal/daemon/server.go`): Confirmed. No `llm` field exists yet. Adding `LLMQuerier` requires modifying `NewServer()` and `ServerConfig`.
- **Migration system** (`internal/storage/db.go`): Confirmed. Uses `schema_meta` table with version-ordered migration array. Adding V3 follows existing pattern at line 161-173. Currently at V2.
- **Cobra groups** (`internal/cmd/root.go`): Only `groupCore` and `groupSetup` exist. A new `groupWorkflow` group must be added.
- **Config struct** (`internal/config/config.go`): No `Workflows` field exists. Adding `WorkflowsConfig` is additive.
- **Paths** (`internal/config/paths.go`): No `WorkflowLogDir()` method exists. Adding it is additive.
- **Sanitize package** (`internal/sanitize/`): Exists with `SanitizeText()` function. Issue 10 (secret masking) can integrate with it as claimed.

### Summary

The plan is architecturally sound and follows the spec's build sequence faithfully. The dependency graph is mostly correct. However, seven spec requirements have no issue owner, and three integration seams between issues need explicit ownership. None of these gaps require new architectural decisions -- they are coverage gaps that can be fixed by adding acceptance criteria to existing issues or creating 1-2 small new issues. The most important fix is adding TTY detection and daemon client ownership, as these are load-bearing infrastructure that multiple downstream issues depend on.
