# Feasibility Review: Workflow Execution Tier 0 Implementation Plan

**Council Member:** 2 -- THE FEASIBILITY JUDGE
**Date:** 2026-02-11
**Plan:** `.agents/plans/2026-02-11-workflow-execution-tier0.md`
**Spec:** `specs/tech_workflows_v4.md` v4.2

```json
{
  "verdict": "WARN",
  "confidence": "HIGH",
  "key_insight": "The plan underestimates the Store interface expansion pain and the dual-LLM-path complexity, and Wave 3's critical-path issue (Issue 11) depends on 6 other issues landing cleanly -- any slip there cascades to Waves 4 and 5, making the 5-6 week estimate tight.",
  "findings": [
    {
      "severity": "critical",
      "category": "integration",
      "description": "Adding 9 methods to the Store interface (Issue 2) breaks the existing mockStore in handlers_test.go. The plan says 'additive, C4' but the Go type system enforces interface satisfaction -- the existing mockStore in internal/daemon/handlers_test.go implements storage.Store and MUST be updated with all 9 new method stubs or it will not compile. This is a hidden dependency: Issue 2 (Wave 1) silently forces immediate changes to internal/daemon/handlers_test.go (and any other test file with a Store mock). If this is not coordinated, 'make test' will be broken from Wave 1 onward until the stubs are added.",
      "location": "Issue 2 / internal/daemon/handlers_test.go",
      "recommendation": "Add stub methods returning errors to the existing mockStore in the same PR as Issue 2. Document this as an explicit sub-task. Do NOT merge Issue 2 without verifying 'make test' still passes."
    },
    {
      "severity": "critical",
      "category": "feasibility",
      "description": "Issue 12 (LLM analysis integration) has two completely separate code paths: (1) claid AnalyzeStepOutput RPC, and (2) direct QueryFast() fallback when daemon is unavailable (D24). The plan lists this as a single Wave 3 issue but it is effectively two features -- a gRPC client path and a local-execution path -- that must share identical prompt construction, response parsing, and error handling. Testing both paths doubles the matrix: mock-claid-succeeds, mock-claid-fails-fallback-succeeds, mock-claid-fails-fallback-fails, and each path has its own timeout/retry semantics. This is a 3x estimator compared to a single-path implementation.",
      "location": "Issue 12 / D24 fallback",
      "recommendation": "Split Issue 12 into 12a (analysis core: prompt building, response parsing, risk matrix) and 12b (transport: claid RPC client + QueryFast fallback). This surfaces the transport layer complexity explicitly and allows 12a to be tested independently."
    },
    {
      "severity": "significant",
      "category": "feasibility",
      "description": "The yaml.v3 KnownFields(true) + custom UnmarshalYAML + inline tag interaction (m16) is a known footgun in the Go YAML ecosystem. The spec's StepDef.UnmarshalYAML uses `yaml:\",inline\"` on the alias struct and a separate `ShellRaw interface{}` field. When KnownFields(true) is enabled on the decoder, the inline tag causes the decoder to treat ALL fields on the alias as 'known', BUT the ShellRaw field shadow may cause KnownFields to flag 'shell' as both known (via StepDef.Shell on the alias) and consumed (via ShellRaw). The actual behavior depends on yaml.v3 internals. This will require careful experimentation and could force a different parsing strategy (e.g., two-pass decode, or manual node walking).",
      "location": "Issue 3 / m16",
      "recommendation": "Spike the KnownFields + inline + custom UnmarshalYAML pattern FIRST before building the rest of the parser. If it fails, the fallback is two-pass parsing: decode once without KnownFields for data, then re-decode with KnownFields and an empty struct for strict validation. Budget 2-3 extra days."
    },
    {
      "severity": "significant",
      "category": "integration",
      "description": "Issue 13 (daemon handlers) requires adding an LLMQuerier field to the Server struct (internal/daemon/server.go). The Server struct currently has no LLM dependency -- it uses provider.Registry for AI operations. Adding an LLMQuerier that wraps claude.QueryFast() means the daemon package now imports internal/claude. The existing handlers use the provider.Registry pattern (TextToCommand, Diagnose, NextStep all call s.registry.GetBest()). The new AnalyzeStepOutput handler uses a completely different LLM dispatch path (claude.QueryFast via LLMQuerier). This architectural inconsistency will cause confusion and may create initialization-order issues, because claude.QueryFast() internally tries to connect to the Claude daemon socket, which has its own lifecycle separate from claid.",
      "location": "Issue 13 / internal/daemon/server.go",
      "recommendation": "Document explicitly that LLMQuerier is separate from provider.Registry and why. Consider whether AnalyzeStepOutput should eventually use provider.Registry instead. At minimum, the ServerConfig struct needs an optional LLMQuerier field, defaulting to claudeQuerier{}, with clear initialization in NewServer()."
    },
    {
      "severity": "significant",
      "category": "timeline",
      "description": "Wave 3 has Issue 11 (sequential executor) depending on 6 issues from Waves 1-2. This is the single largest fan-in in the entire plan. If ANY of Issues 5, 6, 7, 8, 9, or 10 slips, Issue 11 is blocked. Meanwhile, Issues 14, 15, and 16 all depend on Issue 11. This means Wave 3 is entirely serialized on Issue 11, and Issue 11 is the most complex issue in the plan (orchestrating the entire step execution loop). A realistic estimate for Issue 11 alone is 1-1.5 weeks given the number of interfaces it must integrate. The plan allocates 1.5-2 weeks for ALL of Wave 3 (5 issues including the executor).",
      "location": "Issue 11 (Wave 3)",
      "recommendation": "Consider starting Issue 11 with mock implementations of its dependencies in Wave 2, so the executor skeleton can be written before all Wave 1-2 interfaces are finalized. This parallelizes the work at the cost of some rework."
    },
    {
      "severity": "significant",
      "category": "feasibility",
      "description": "The ProcessController (Issue 6) Windows implementation requires Windows Job Objects via the windows.TerminateJobObject API. Go's standard library does not expose Job Object creation. This requires either: (a) direct syscall usage via golang.org/x/sys/windows, (b) a CGo wrapper, or (c) a third-party library. The existing spawn_windows.go in internal/ipc/ uses only CREATE_NEW_PROCESS_GROUP -- it does NOT use Job Objects. The plan says 'uses CREATE_NEW_PROCESS_GROUP + GenerateConsoleCtrlEvent' in the acceptance criteria but the spec (section 6.3) says 'creates a Windows Job Object to track the process tree' and 'TerminateJobObject'. These are contradictory -- GenerateConsoleCtrlEvent is NOT Job Object cleanup. Getting Job Objects right on Windows is non-trivial and untested in this codebase.",
      "location": "Issue 6 / internal/workflow/process_windows.go",
      "recommendation": "For Tier 0, simplify the Windows ProcessController to match the existing spawn_windows.go pattern (CREATE_NEW_PROCESS_GROUP + GenerateConsoleCtrlEvent only, no Job Objects). Defer Job Object support to Tier 1 when Windows daemon IPC is also added. The spec already says Windows Tier 0 is degraded ('engine runs, daemon IPC deferred'). This eliminates a high-risk Windows-specific implementation from the critical path."
    },
    {
      "severity": "significant",
      "category": "testing",
      "description": "Issue 6 (ProcessController) acceptance criteria say 'Unit tests with a subprocess that traps signals'. Writing reliable cross-platform signal-trapping tests in Go is notoriously difficult. On Unix, this requires spawning a child process that registers a SIGTERM handler, then verifying the handler ran -- but test races are common because signal delivery is asynchronous. On CI (GitHub Actions), signal delivery timing can be unpredictable. These tests frequently become flaky.",
      "location": "Issue 6",
      "recommendation": "Use integration-style tests with generous timeouts (5s) and a simple test helper binary (compiled as TestMain subprocess) that writes a sentinel file on SIGTERM receipt. Do NOT attempt to test signal handling with goroutine-based mock processes. Accept that these tests may need retry logic or 'skip on CI' annotations."
    },
    {
      "severity": "significant",
      "category": "integration",
      "description": "The plan's dependency graph shows Issue 14 (Human review UI) depending only on Issue 12 (LLM analysis). But TerminalReviewer.Review() in the spec (section 11.2) calls hr.llm.AskFollowUp() for the [q]uestion option, and runAdHocCommand() which 'uses ShellAdapter.ExecShell'. This means Issue 14 actually depends on Issue 5 (ShellAdapter) as well as Issue 12. Additionally, the [c]ommand option needs the step's environment and working directory, which come from the executor (Issue 11). The declared dependency chain is incomplete.",
      "location": "Issue 14 dependencies",
      "recommendation": "Add Issue 5 (ShellAdapter) and Issue 11 (executor, for environment context) as explicit dependencies of Issue 14. Alternatively, defer the [c]ommand option to a later wave and stub it for Tier 0."
    },
    {
      "severity": "significant",
      "category": "feasibility",
      "description": "The config extension (Issue 17) adds a WorkflowsConfig struct to Config. But the existing Config struct uses yaml.Unmarshal without KnownFields -- meaning adding a 'workflows:' key is safe. However, the existing Config.Get() and Config.Set() methods use a hardcoded section switch statement. Adding 'workflows' support requires adding a new case to Get(), Set(), Validate(), and potentially ListKeys(). The plan says 'Existing config loading unchanged (additive)' but the manual Get/Set dispatch pattern means this is NOT just adding a struct field -- it's modifying 6-8 methods.",
      "location": "Issue 17 / internal/config/config.go",
      "recommendation": "Acknowledge that Issue 17 touches more code than just adding a struct. Add getWorkflowsField(), setWorkflowsField() methods plus Validate() updates to the acceptance criteria. This is not hard but it is more work than the issue description suggests."
    },
    {
      "severity": "minor",
      "category": "feasibility",
      "description": "The plan specifies 'make proto' generates cleanly (Issue 1 acceptance). But the existing proto file uses proto3 syntax without field number gaps. Adding 4 new RPCs and 8 new messages to the same clai.proto file will produce a large diff and may cause merge conflicts if other work lands on main concurrently. The generated Go code in gen/clai/v1/ will have significant changes to the _grpc.pb.go file, which means the UnimplementedClaiServiceServer in server.go must gain 4 new no-op methods or compilation fails.",
      "location": "Issue 1 / proto/clai/v1/clai.proto",
      "recommendation": "Coordinate Issue 1 (proto) with Issue 13 (daemon handlers) -- the UnimplementedClaiServiceServer stub methods will be auto-generated but the Server struct in server.go must be verified to still embed it correctly. Consider landing Issue 1 + stub handlers together to keep 'make build' green."
    },
    {
      "severity": "minor",
      "category": "integration",
      "description": "google/shlex is listed as a dependency for shell tokenization (m17) but is not in go.mod. Adding a new dependency requires 'go mod tidy' and potentially updating the go.sum. This is trivial but the plan doesn't mention it.",
      "location": "Issue 5",
      "recommendation": "Add 'go get github.com/google/shlex' as an explicit first step of Issue 5."
    },
    {
      "severity": "minor",
      "category": "timeline",
      "description": "Wave 1 has 7 parallel issues. If a single developer is implementing this, 'parallel' is meaningless -- it is 7 sequential issues that happen to have no dependencies between them. The 1.5-2 week estimate for Wave 1 assumes parallelism. A single developer doing 7 foundational issues sequentially needs closer to 2.5-3 weeks for Wave 1 alone.",
      "location": "Execution Order / Wave 1",
      "recommendation": "Clarify whether the estimate assumes single-developer or multi-developer execution. If single developer, adjust Wave 1 to 2.5-3 weeks, pushing total to 6.5-8 weeks."
    }
  ],
  "recommendation": "Address the Store interface breakage (finding 1) and split Issue 12 (finding 2) before starting implementation. Spike the yaml.v3 KnownFields interaction (finding 3) in the first day of Wave 1. Simplify Windows ProcessController to match existing patterns (finding 6). With these mitigations the plan is achievable in 6-8 weeks (single developer) or 5-6 weeks (two developers on Wave 1).",
  "schema_version": 1
}
```

---

## Detailed Analysis

### 1. The Store Interface Expansion Is a Hidden Build-Breaker

The single most likely "day one" failure mode in this plan is Issue 2 (SQLite schema + Store interface extension). The plan correctly identifies that 9 new methods are added to the `Store` interface. What it does not surface is the **immediate downstream breakage** this causes.

The existing `mockStore` in `internal/daemon/handlers_test.go` (line 21) implements `storage.Store`. In Go, interface satisfaction is all-or-nothing. The moment Issue 2 adds `CreateWorkflowRun`, `UpdateWorkflowRun`, etc. to the `Store` interface, the existing `mockStore` no longer satisfies the interface. This means `make test` breaks instantly and stays broken until every test file with a Store mock is updated.

The plan says "additive, C4 -- no existing methods changed." This is true at the semantic level but false at the compilation level. Any code path that constructs a `mockStore` and passes it where a `Store` is expected will fail to compile. The fix is trivial (add stub methods that panic or return errors), but if it is not done in the same PR as the interface change, the repository is in a broken state.

**Risk:** HIGH. This will be hit immediately and block CI.

### 2. The Dual LLM Path (D24) Is Two Features Hiding as One Issue

Issue 12 describes "LLM analysis integration" as a single issue. In reality, D24 creates two completely separate code paths:

**Path A (claid RPC):** CLI connects to claid via gRPC, sends `AnalyzeStepOutputRequest`, receives `AnalyzeStepOutputResponse`. The LLM query happens server-side in the daemon handler. Error handling: gRPC connection errors, deadline exceeded, daemon not found.

**Path B (direct fallback):** CLI imports `internal/claude` and calls `QueryFast()` directly. Must construct the same prompt, parse the same response format, apply the same risk matrix. Error handling: Claude daemon socket errors, Claude CLI not found, subprocess timeout.

Both paths must produce identical `AnalysisResponse` structs and handle the same edge cases (empty response, malformed JSON, timeout). However, they log/persist differently (Path A persists in SQLite via the daemon; Path B does NOT persist -- the plan's risk note says "LLM analysis not persisted to SQLite").

Testing this requires at minimum 6 test cases: claid-up+LLM-ok, claid-up+LLM-fail, claid-up+LLM-unparseable, claid-down+fallback-ok, claid-down+fallback-fail, claid-down+fallback-unparseable. Each with 3 risk levels = 18 matrix combinations for the full decision path.

This is the kind of issue that estimates at 2-3 days but takes 7-10 days because of the combinatorial testing surface.

### 3. The yaml.v3 KnownFields + inline + UnmarshalYAML Interaction

The spec explicitly calls out m16 as a risk, and the plan repeats it. But neither provides a concrete mitigation strategy beyond "test explicitly."

The issue is fundamental to the Go yaml.v3 library. When `KnownFields(true)` is set on the decoder, it rejects any YAML key that doesn't map to a struct field. When `yaml:",inline"` is used in a wrapper struct (for the custom UnmarshalYAML pattern), the inline tag tells the decoder that all fields of the inlined struct are valid at the current level. The `ShellRaw interface{}` field is also at the current level.

The problem: `ShellRaw` and the inlined `stepAlias.Shell` both map to the YAML key `shell`. With `KnownFields(true)`, the decoder may:
- Accept `shell` because `ShellRaw` matches it (correct)
- Reject `shell` because after inline expansion, it sees a conflict
- Accept but decode into the wrong field

The actual behavior is undocumented and depends on yaml.v3 decoder implementation details. The yaml.v3 library has open issues about `KnownFields` + `inline` interactions.

If this pattern fails, the fallback (two-pass decode) works but is ugly and adds complexity to the parser. Budget accordingly.

### 4. Daemon Architecture: Two Separate Daemons, One Confusing Code Path

The plan references `claid` (the gRPC daemon in `cmd/claid/`) and the Claude daemon (the LLM subprocess in `internal/claude/daemon.go`). The `AnalyzeStepOutput` handler in claid calls `claude.QueryFast()`, which internally:

1. Checks if the Claude daemon is running (Unix socket at `~/.cache/clai/daemon.sock`)
2. If yes: sends prompt over the socket (NOT gRPC -- raw JSON over Unix socket)
3. If no: falls back to `claude --print` one-shot subprocess

This means a single `AnalyzeStepOutput` RPC traverses:
- CLI -> gRPC -> claid -> Unix socket -> Claude CLI subprocess -> Anthropic API

That is four process boundaries. The 120s timeout for cold-start (M20) is for the Claude daemon specifically, not the claid timeout. The claid handler needs to set its own context timeout that accommodates the Claude daemon cold start. If the Claude daemon is not running and `StartDaemonProcess()` is called (which waits up to 90 seconds in the current code), this single RPC call could block for 90+ seconds.

The plan mentions M20 ("120s timeout on first call") but does not specify WHERE this timeout is set. It cannot be a gRPC deadline (the CLI sets those), and it cannot be a simple `context.WithTimeout` in the handler (that would kill the handler but leave the Claude daemon startup in progress).

### 5. Issue 11 Is the Single Point of Failure

Issue 11 (sequential executor) is the most complex issue and the single largest bottleneck in the dependency graph. It has 6 input dependencies and 3 issues depend on its output. The executor must:

- Iterate matrix entries (from Issue 3 types)
- For each entry, iterate steps sequentially
- For each step: resolve expressions (Issue 7), build environment (multiple sources), execute via ShellAdapter (Issue 5), manage process lifecycle via ProcessController (Issue 6), capture output via limitedBuffer (Issue 9), parse $CLAI_OUTPUT (Issue 8), mask secrets (Issue 10)
- On failure: halt remaining steps, propagate error
- On cancellation: propagate ctx.Done(), clean up

This is not a matter of "wiring together" pre-built components. The executor must manage state transitions, error propagation, context lifecycle, and the interaction between all these subsystems. It is the integration point where interface mismatches between the 6 dependency issues will surface.

The plan allocates Wave 3 as "1.5-2 weeks" for 5 issues. Issue 11 alone is realistically 1-1.5 weeks of focused work including debugging integration issues. This leaves almost no time for Issues 12, 14, 15, and 16.

### 6. Windows ProcessController: Job Objects Are Over-Scoped for Tier 0

The spec says Windows uses Job Objects (`windows.TerminateJobObject`). But the existing codebase (`internal/ipc/spawn_windows.go`) uses only `CREATE_NEW_PROCESS_GROUP`. There is no Job Object code anywhere in the repository.

Implementing Job Objects in Go requires:
- `golang.org/x/sys/windows` for `CreateJobObject`, `AssignProcessToJobObject`, `SetInformationJobObject`, `TerminateJobObject`
- Correct `JOBOBJECT_EXTENDED_LIMIT_INFORMATION` setup with `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE`
- Handle inheritance: the Job Object must be uninheritable to prevent child processes from escaping

This is non-trivial and untestable without a Windows CI environment. Since Windows Tier 0 is already degraded (no daemon IPC), simplifying to `CREATE_NEW_PROCESS_GROUP` + `GenerateConsoleCtrlEvent` (matching the existing pattern) eliminates a significant risk.

### 7. Wave Dependencies Have Two Hidden Gaps

**Gap 1:** Issue 14 (Human review UI) lists only Issue 12 as a dependency. But the `[c]ommand` option calls `ShellAdapter.ExecShell()` (Issue 5) and needs the step's environment context from the executor (Issue 11). The plan does not declare these.

**Gap 2:** Issue 15 (RunArtifact) depends on Issue 11 (executor). But the artifact writer needs the `SecretStore` (Issue 10) for masking event data. Issue 10 is not listed as a dependency of Issue 15.

These gaps mean that Issue 14 and Issue 15 may discover they need types or functions that are not yet available when they start implementation, causing either backtracking or premature interface coupling.

### 8. Config Integration Is More Work Than Stated

Issue 17 says "Existing config loading unchanged (additive)" and "Unit test for config parsing with workflow section." Looking at the actual config code (`internal/config/config.go`), the Get/Set methods use manual switch-case dispatch:

```go
switch section {
case "daemon": ...
case "client": ...
case "ai": ...
// ...
}
```

Adding "workflows" requires:
- New case in `Get()`
- New case in `Set()`
- New `getWorkflowsField()` method
- New `setWorkflowsField()` method
- Updates to `Validate()` for workflow field validation
- Updates to `ListKeys()` if workflow config should be user-visible
- New `DefaultConfig()` entries for workflow defaults

This is 100-150 lines of boilerplate, not 20 lines. Not hard, but the issue description undersells it.

### 9. Timeline Assessment

The 5-6 week estimate is aggressive for a single developer. My revised estimate:

| Wave | Plan Estimate | Feasibility Estimate (1 dev) | Key Risk |
|------|--------------|------------------------------|----------|
| Wave 1 | 1.5-2 weeks | 2.5-3 weeks | 7 sequential issues, yaml.v3 spike |
| Wave 2 | 1-1.5 weeks | 1-1.5 weeks | Expression engine complexity |
| Wave 3 | 1.5-2 weeks | 2-2.5 weeks | Issue 11 integration bottleneck |
| Wave 4 | 0.5-1 week | 0.5-1 week | CLI wiring is straightforward |
| Wave 5 | 0.5-1 week | 1-1.5 weeks | Integration test debugging |
| **Total** | **5-6 weeks** | **7-9.5 weeks** | |

With two developers who can genuinely parallelize Wave 1 (one on parser/types/config, the other on shell/process/buffer), the total compresses to 5.5-7 weeks. The plan's 5-6 week estimate is achievable only with either parallel development or a developer who has deep prior experience with all the integration points (gRPC proto generation, yaml.v3 quirks, cross-platform process management, SQLite migrations).

### Summary

The plan is well-structured and demonstrates strong understanding of the spec and codebase. The wave decomposition is sound. The dependency tracking is mostly correct with the two gaps identified above. The primary risks are:

1. **Store interface breakage** -- a day-one build problem that is easily fixed but must be anticipated
2. **Dual LLM path complexity** -- Issue 12 needs splitting
3. **yaml.v3 KnownFields** -- needs a spike before committing to the parsing strategy
4. **Issue 11 as the critical path** -- the entire project hinges on the executor integration going smoothly
5. **Timeline optimism** -- 5-6 weeks assumes parallelism and no significant debugging cycles

With the recommended mitigations, this is a solid plan that will deliver a working Tier 0. Without them, it will likely take 7-8 weeks and hit preventable integration issues.
