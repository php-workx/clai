# Council Review: Scope Judge -- Tier 0 Workflow Execution Plan

```json
{
  "verdict": "WARN",
  "confidence": "HIGH",
  "key_insight": "Plan includes a Tier 1 feature (RunArtifact JSONL, FR-39/FR-40), under-scopes config integration work, and has a false dependency chain that inflates the timeline by one full wave.",
  "findings": [
    {
      "severity": "critical",
      "category": "scope",
      "description": "Issue 15 (RunArtifact JSONL) implements FR-39 and FR-40, which are explicitly listed as Tier 1 features in spec section 2.2. The spec's Tier 0 acceptance criteria at line 3228 includes 'RunArtifact (JSONL) written for every run', creating an internal contradiction in the spec itself. However, the plan should follow the tier table, not the acceptance criteria list. The plan has Tier 1 work leaking in.",
      "location": "Issue 15 vs. spec section 2.2 (lines 303-304)",
      "recommendation": "Either (a) remove Issue 15 entirely from Tier 0 and delete the JSONL acceptance criterion, or (b) formally acknowledge this as a Tier 0 scope expansion with a new decision log entry (D29). Given that the JSONL artifact is referenced in the build sequence at section 3.8 step 5 and the acceptance criteria, the spec author likely intended it for Tier 0 despite the table placement. Clarify and document. If removed, this saves ~0.5 weeks."
    },
    {
      "severity": "significant",
      "category": "decomposition",
      "description": "Issue 19 (Exit codes + error messages) is a false separate issue. Exit codes are integer constants and a switch statement -- this is 30-50 lines of code at most. The error message quality work is inseparable from the components that generate the errors (Issues 7, 11, 12, 18). Making Issue 19 depend on Issue 18 creates a sequential wave (Wave 4 -> Wave 5) for what is essentially a code review pass, not a distinct implementation unit.",
      "location": "Issue 19, Wave 4",
      "recommendation": "Merge Issue 19 into Issue 18. Define the exit code constants as part of the CLI command issue. Error message quality should be a review criterion on Issues 7, 11, and 12, not a separate deliverable. This eliminates the Wave 4 -> Wave 5 sequential dependency for Issue 20."
    },
    {
      "severity": "significant",
      "category": "decomposition",
      "description": "Issue 4 (Workflow validator) and Issue 3 (YAML parser) should be a single issue. The validator is ~100 lines that operate directly on the types defined in Issue 3. Splitting them creates a Wave 1 -> Wave 2 dependency for trivially coupled code. In practice, the developer writing the parser will write validation checks at the same time. The 'expression reference validation' mentioned as a scope judge 'must-add' depends on the expression syntax from Issue 7, which means Issue 4 cannot be fully complete until after Issue 7 anyway -- creating a hidden circular dependency.",
      "location": "Issue 3 and Issue 4",
      "recommendation": "Merge Issue 4 into Issue 3. Move the expression-reference validation (checking that ${{ steps.X }} references valid step IDs) into Issue 7 where the expression engine is built. This keeps the parser+validator in Wave 1 and eliminates one Wave 2 item."
    },
    {
      "severity": "significant",
      "category": "parallelism",
      "description": "Issue 14 (Human review UI) is listed in Wave 3 with a dependency on Issue 12 (LLM analysis). However, the review UI only needs the analysis result types (decision, reasoning, flags) -- not the LLM integration itself. The InteractionHandler interface and TerminalReviewer can be built and tested independently with mock analysis results. Placing it in Wave 3 delays it unnecessarily.",
      "location": "Issue 14, Wave 3",
      "recommendation": "Move Issue 14 to Wave 2 with a dependency only on Issue 3 (types). The review UI consumes structured data (decision enum, reasoning string, flags map) -- it does not call the LLM. This allows Wave 3 to integrate the reviewer into the executor without having to build and test the UI at the same time as the executor."
    },
    {
      "severity": "significant",
      "category": "scope",
      "description": "Issue 17 (Config extension) includes SearchPaths, RetainRuns, StrictPermissions, and SecretFile fields. SearchPaths is only consumed by name-based workflow discovery (used by 'workflow list' and name-based 'workflow run my-workflow'), which are Tier 1 features. RetainRuns is explicitly stated as 'deferred to Tier 1' (spec line 31: 'Run retention deferred to Tier 1'). StrictPermissions is a Tier 1 concern (permission checking is not in any Tier 0 acceptance criterion). SecretFile is a Tier 1 feature (FR-34, .secrets file loading). This issue smuggles four Tier 1 config fields into Tier 0.",
      "location": "Issue 17 acceptance criteria, spec sections 2.2 and 14.1",
      "recommendation": "Reduce WorkflowsConfig to Tier 0 fields only: Enabled, DefaultMode, DefaultShell, LogDir. Defer SearchPaths, RetainRuns, StrictPermissions, and SecretFile to Tier 1. The struct can still be defined with these fields (Go zero values are harmless), but the plan should not list them as acceptance criteria for Tier 0, and no Tier 0 code should consume them."
    },
    {
      "severity": "significant",
      "category": "timeline",
      "description": "Wave 1 has 7 parallel issues which is a large batch, but the actual risk is in the false sequencing from Wave 4 to Wave 5. Issue 20 (integration tests) depends on Issue 19 (exit codes), which depends on Issue 18 (CLI commands). This creates a three-issue sequential chain spanning two waves for the final stretch. With the merge recommendation (Issue 19 into Issue 18), Issue 20 moves from Wave 5 to Wave 4, collapsing the plan from 5 waves to 4 waves. The 5-6 week estimate becomes 4.5-5 weeks.",
      "location": "Waves 4-5",
      "recommendation": "After merging Issue 19 into Issue 18, rename Wave 5 into Wave 4 (there is only one wave needed for CLI + integration tests if exit codes are part of the CLI issue). The integration test can begin as soon as the CLI command is minimally functional."
    },
    {
      "severity": "minor",
      "category": "scope",
      "description": "Issue 13 specifies an 'llmTimeout' that 'accounts for cold-start (120s first call -- M20)'. This is a timeout policy decision that adds complexity. The spec says the first AnalyzeStepOutput call may hit a 90-second cold-start. A simple 120s timeout for all calls (not just the first) would be simpler and sufficient for Tier 0. Distinguishing first-call vs. subsequent-call timeout is optimization that can wait.",
      "location": "Issue 13, M20 reference",
      "recommendation": "Use a single 120s timeout for all AnalyzeStepOutput calls in Tier 0. Remove the first-call-specific timeout logic. If it becomes a problem (unlikely in Tier 0 with sequential execution), add the optimization in Tier 1."
    },
    {
      "severity": "minor",
      "category": "decomposition",
      "description": "Issue 9 (limitedBuffer) is a standalone issue for what is approximately 60 lines of code (a ring buffer with a mutex). This is too granular. It is a utility type consumed only by the executor (Issue 11) and LLM analysis (Issue 12). Making it a separate Wave 1 issue with its own acceptance criteria and test suite adds process overhead disproportionate to its size.",
      "location": "Issue 9",
      "recommendation": "Merge Issue 9 into Issue 11 (Sequential executor). The limitedBuffer is an internal implementation detail of the executor. Alternatively, if it must remain separate for parallel development, acknowledge that it is a 1-2 hour task, not a multi-day issue."
    },
    {
      "severity": "minor",
      "category": "scope",
      "description": "The plan acceptance criteria for Issue 2 says 'PruneWorkflowRuns stubbed (returns 0, Tier 1)'. Stubs are a form of scope creep -- they add code, tests, and interface surface that serves no Tier 0 purpose. The Go interface pattern allows adding methods later without breaking existing implementations (via embedding or extension).",
      "location": "Issue 2",
      "recommendation": "Do not stub PruneWorkflowRuns. Omit it from the Store interface entirely in Tier 0. Add it when Tier 1 needs it. This is consistent with the spec's additive interface principle (C4)."
    },
    {
      "severity": "minor",
      "category": "scope",
      "description": "The Tier 0 acceptance criteria at spec line 3242 includes 'isTransientError uses typed error checks (no string matching) (M7)'. The plan's Issue 12 (LLM analysis integration) does not explicitly call out implementing isTransientError or LLM retry logic. This is an omission in the plan -- the spec requires it for Tier 0 but the plan does not track it.",
      "location": "Issue 12 vs. spec line 3242",
      "recommendation": "Add isTransientError implementation and LLM retry logic to Issue 12's acceptance criteria. This is approximately 30 lines of code but is a correctness requirement."
    }
  ],
  "recommendation": "Apply the three merge operations (Issue 19 -> 18, Issue 4 -> 3, Issue 9 -> 11), move Issue 14 to Wave 2, resolve the FR-39/FR-40 Tier 1 leak, strip Tier 1 config fields from Issue 17, and add the missing isTransientError criterion to Issue 12. This reduces the plan from 20 issues / 5 waves to 17 issues / 4 waves while tightening scope alignment with the spec.",
  "schema_version": 1
}
```

---

## Detailed Analysis

### 1. Tier 1 Feature Leak: RunArtifact JSONL (Critical)

The most concerning scope problem is Issue 15 (RunArtifact JSONL). The spec's tier assignment table at section 2.2 (line 303-304) explicitly places FR-39 ("Structured execution log") and FR-40 ("Machine-readable log format") in **Tier 1**. Yet the Tier 0 acceptance criteria at line 3228 says "RunArtifact (JSONL) written for every run," and the build sequence at section 3.8 step 5 includes "JSONL event log."

This is an internal contradiction in the spec. The plan resolves it by including RunArtifact in Tier 0, but does so silently -- there is no decision log entry acknowledging the tier override. If the spec's tier table is authoritative, Issue 15 should be cut. If the acceptance criteria are authoritative, the plan should document why FR-39/FR-40 are pulled forward.

My recommendation: if RunArtifact is genuinely needed for Tier 0 (e.g., for debugging workflow runs or for the integration test to verify event sequences), keep it but add a formal decision entry. If it is a "nice to have," cut it and save ~0.5 weeks.

### 2. False Sequential Dependencies (Significant)

The plan's wave structure has unnecessary sequencing:

**Issue 19 (Exit codes) is not a real issue.** Exit codes are 5 integer constants and a switch statement in the CLI command. The plan makes Issue 19 depend on Issue 18 (CLI commands), and then Issue 20 (integration tests) depends on Issue 19. This creates a three-issue serial chain across Waves 4 and 5.

By merging Issue 19 into Issue 18, Issue 20 can start as soon as Issue 18 is done, collapsing from 5 waves to 4. The error message quality work mentioned in Issue 19 is not a standalone deliverable -- it is a quality attribute of the components that generate errors (expression engine, executor, LLM analysis).

**Issue 4 (Validator) should merge into Issue 3 (Parser).** The validator operates directly on the parser's output types. The developer writing `WorkflowDef` will naturally write validation at the same time. Additionally, Issue 4's acceptance criteria include "expression reference validation: `${{ steps.X.outputs.Y }}` verifies step X exists" -- but this requires understanding the expression syntax from Issue 7. The validator cannot be fully complete until Issue 7 exists, creating a hidden gap.

**Issue 14 (Human review UI) has a false dependency on Issue 12 (LLM analysis).** The review UI consumes structured data types (decision enum, reasoning string). It does not call the LLM. The `InteractionHandler` interface and `TerminalReviewer` can be built and tested with mock data in Wave 2, freeing Wave 3 to focus on integration rather than parallel UI construction.

### 3. Config Scope Creep (Significant)

Issue 17 lists `WorkflowsConfig` fields including `SearchPaths`, `RetainRuns`, `StrictPermissions`, and `SecretFile`. Checking these against the spec:

- **SearchPaths**: consumed only by name-based workflow discovery (`clai workflow run my-workflow`) and `workflow list`, both Tier 1.
- **RetainRuns**: the spec explicitly states "Run retention deferred to Tier 1" (line 31). The plan even acknowledges this in Issue 2 ("PruneWorkflowRuns stubbed").
- **StrictPermissions**: permission enforcement is not in any Tier 0 acceptance criterion.
- **SecretFile**: FR-34 (.secrets file loading) is Tier 1.

These fields can exist in the Go struct (zero values are harmless), but they should not appear in Tier 0 acceptance criteria and no Tier 0 code should reference them.

### 4. Over-Granular Decomposition (Minor)

Issue 9 (limitedBuffer) is approximately 60 lines of code: a byte slice ring buffer with a `sync.Mutex` and an `io.Writer` interface. Creating a standalone issue with its own acceptance criteria, wave assignment, and dependency tracking adds process overhead that exceeds the implementation effort. This should be an internal type within the executor package, built as part of Issue 11.

### 5. Missing Acceptance Criterion (Minor)

The spec's Tier 0 acceptance criteria at line 3242 requires: "`isTransientError` uses typed error checks (no string matching) (M7)." This implies LLM retry logic with transient error detection. Issue 12 (LLM analysis integration) does not mention `isTransientError` or retry logic in its acceptance criteria. This is a gap -- the spec requires it but the plan does not track it.

### 6. Unnecessary Stub (Minor)

Issue 2 includes "PruneWorkflowRuns stubbed (returns 0, Tier 1)." Stubs are scope creep in disguise. They add interface surface, implementation code, and test expectations for functionality that has no Tier 0 consumer. The Go interface pattern (C4, additive methods) allows adding `PruneWorkflowRuns` in Tier 1 without touching Tier 0 code. Omit it entirely.

### 7. Overall Issue Count and Timeline Assessment

With the recommended merges (19 -> 18, 4 -> 3, 9 -> 11), the plan drops from 20 issues to 17 issues across 4 waves instead of 5. The 5-6 week timeline becomes 4.5-5 weeks.

17 issues over 4.5-5 weeks is approximately 3.5 issues/week. Given that Wave 1 has 6 parallel foundational issues (after the Issue 3+4 merge), this is reasonable for a single developer or a small team. The critical path runs through: Wave 1 types -> Wave 2 expression engine -> Wave 3 executor -> Wave 4 CLI + tests. This is 4 sequential gates, each 1-1.5 weeks, which aligns with the 4.5-5 week estimate.

### Summary of Recommended Changes

| Action | From | To | Impact |
|--------|------|----|--------|
| Merge | Issue 19 -> Issue 18 | Collapses Waves 4-5 | -1 wave, -1 issue |
| Merge | Issue 4 -> Issue 3 | Eliminates false Wave 1->2 dep | -1 issue |
| Merge | Issue 9 -> Issue 11 | Reduces process overhead | -1 issue |
| Move | Issue 14 to Wave 2 | Better parallelism | No issue count change |
| Resolve | Issue 15 FR-39/FR-40 tier conflict | Decision log entry or removal | Clarifies scope |
| Trim | Issue 17 Tier 1 config fields | Strip from acceptance criteria | Tighter scope |
| Add | isTransientError to Issue 12 | Tracks spec requirement | No issue count change |
| Remove | PruneWorkflowRuns stub from Issue 2 | Less dead code | Tighter scope |
