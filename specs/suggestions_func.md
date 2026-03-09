# clai Suggestions Engine — Functional Specification

**Version:** 1.0 (Draft)
**Date:** 2026-02-19
**Status:** RFC

---

## 1. Purpose

This document defines the **functional requirements** for the clai suggestions engine. It describes what users experience and what behaviors the product guarantees, independent of implementation details.

This functional spec is derived from:
- `./specs/suggestions_tech_v1.md`
- `./specs/suggestions_tests_v1.md`

This spec is the user-facing contract for the suggestions feature family and excludes workflow-connected suggestion behavior.

---

## 2. Motivation

Terminal users repeatedly type similar commands, recover from similar failures, and switch between project contexts that imply predictable next actions. The suggestions engine exists to:

1. reduce typing and recall effort
2. speed up recovery after failed commands
3. surface relevant tasks for the current repository and project type
4. keep shell interaction fast and non-blocking even when the daemon is unavailable

---

## 3. Scope and Guarantees

### 3.1 In Scope

Suggestions for bash, zsh, and fish on Unix-like platforms, including:
- command ingestion and normalization
- ranked suggestion generation
- search over command history
- feedback-driven learning and suppression
- discovery/task suggestions
- privacy/incognito behaviors
- diagnostics and operational reliability

### 3.2 Out of Scope

- workflow-connected suggestion behavior and workflow mining signals
- PTY transcript parsing
- cloud sync and multi-device profile sharing
- Windows-native runtime behavior for this release

### 3.3 Product Guarantees

The product SHALL guarantee:
- fail-open shell behavior (never block command execution due to suggestions subsystem failure)
- deterministic ranking for identical state and inputs
- bounded latency with cache-first fallback behavior
- local-first privacy defaults

---

## 4. Functional Requirements

### 4.1 Shell Experience and Availability

**FR-1:** The system SHALL support suggestions in interactive `bash`, `zsh`, and `fish` sessions on Unix-like systems.

**FR-2:** The system SHALL be inactive in non-interactive shells by default.

**FR-3:** Suggestion capture and retrieval SHALL be fail-open: command execution MUST continue even if helper, daemon, IPC, or storage is unavailable.

**FR-4:** Suggestion rendering SHALL be non-invasive by default, preserving native completion bindings unless users explicitly opt in to overrides.

**FR-5:** The system SHALL support line-oriented display, with optional shell-specific affordances (for example ghost text where supported).

**FR-6:** The suggest path SHOULD meet user-perceived low-latency targets (warm path sub-50ms P95, hard timeout bounded).

### 4.2 Session and Command Lifecycle

**FR-7:** The shell integration SHALL emit lifecycle events for session start/end and command start/end.

**FR-8:** Command-end ingestion SHALL include command text, cwd, timestamp, session id, exit code, and incognito mode; optional context (repo/branch/duration) SHALL be included when available.

**FR-9:** Empty command submissions SHALL be treated as valid events and SHALL NOT crash ingestion.

**FR-10:** Oversized command payloads SHALL be truncated safely and marked as truncated.

**FR-11:** Event transmission SHALL be fire-and-forget from shell/hook perspective (no prompt-blocking ACK wait).

**FR-12:** The daemon SHALL auto-start opportunistically when suggestion functionality is requested and no daemon is available.

### 4.3 Context Understanding

**FR-13:** Command normalization SHALL be deterministic, preserving executable/flag structure while replacing variable arguments with stable slots (for example `<path>`, `<num>`, `<sha>`, `<url>`, `<msg>`).

**FR-14:** Alias handling SHALL normalize equivalent alias/expanded forms to a single command identity while preserving original text for display/audit.

**FR-15:** Alias capture SHALL support shell-native alias sources and bounded expansion with cycle protection.

**FR-16:** Repository context SHALL be derived from canonical repo root and remote metadata when present, producing stable per-repo identity.

**FR-17:** Project-type context SHALL be detected from marker files and MAY be overridden by `.clai/project.yaml`.

**FR-18:** Pipeline-aware parsing SHALL detect command segments/operators and enable segment-aware continuation suggestions.

### 4.4 Candidate Generation

**FR-19:** The system SHALL generate candidates from multiple deterministic sources, including transition history, frequency priors, failure recovery, pipeline continuation, project-type affinity, and discovery/task sources.

**FR-20:** Candidate retrieval source ordering SHALL be deterministic for identical input context.

**FR-21:** Prefix handling SHALL support both empty-prefix next-step mode and typed-prefix constrained mode.

**FR-22:** After a failed previous command, failure-recovery candidates SHALL be prioritized, including stronger typo recovery behavior for command-not-found style failures.

**FR-23:** Discovery/task candidates SHALL be gated to low-confidence or empty-context cases and SHALL NOT displace high-confidence predictive suggestions.

**FR-24:** Repository task discovery SHALL support package scripts, Makefile/just/task-style sources, and `.clai/tasks.yaml`.

**FR-25:** `.clai/tasks.yaml` SHALL support conditional triggers (`after`, `after_failure`) with validation for missing references and cycle rejection.

### 4.5 Ranking, Confidence, and Explainability

**FR-26:** Candidate ranking SHALL use a deterministic weighted model over transitions, frequency, success history, prefix match, affinity, feedback, project-type signals, task signals, failure recovery, and risk penalty.

**FR-27:** Feature contributions SHALL be normalized consistently before weighting.

**FR-28:** Confidence SHALL be computed per suggestion; low-confidence suggestions SHALL be hidden unless explicitly requested.

**FR-29:** Final ordering SHALL be deterministic using fixed tie-break rules.

**FR-30:** Suggestions SHALL be deduplicated and near-duplicates reduced in top results.

**FR-31:** Slot filling SHALL use learned value statistics with correlation checks to avoid invalid mixed tuples.

**FR-32:** Risk-aware behavior SHALL penalize or suppress unsafe candidates according to configured safeguards.

**FR-33:** Each returned suggestion SHALL include explainability reasons derived from top score contributions, excluding workflow-based reason types.

### 4.6 Feedback and Online Learning

**FR-34:** The system SHALL accept feedback actions including accepted, dismissed, edited, never, and unblock semantics.

**FR-35:** Implicit acceptance MAY be inferred when executed command text matches a recently shown top suggestion within a configured time window.

**FR-36:** Feedback SHALL update ranking priors and source affinity in the relevant scope.

**FR-37:** Dismissal handling SHALL implement persistent suppression levels (temporary, learned, permanent) with escalation on repeated dismissals.

**FR-38:** `never` SHALL create reversible permanent suppression; `unblock` SHALL clear it.

**FR-39:** Acceptance in the same context SHALL reduce/reset suppression for that context-candidate pair.

**FR-40:** Online learning SHALL adjust ranking weights with bounded, stable updates and persist learned profiles across restarts.

### 4.7 Search

**FR-41:** The CLI/API SHALL support search modes `fts`, `prefix`, `describe`, and `auto`.

**FR-42:** `auto` mode SHALL merge deterministic retrieval from full-text and describe/tag signals.

**FR-43:** If FTS is unavailable, the system SHALL fall back to bounded non-FTS search behavior instead of failing.

**FR-44:** Search responses SHALL indicate the active backend (`fts5` or fallback).

**FR-45:** Describe-mode search SHALL use deterministic tag extraction from a controlled vocabulary with optional extension support.

### 4.8 Privacy, Safety, and Incognito

**FR-46:** Suggestions data SHALL remain local by default.

**FR-47:** The system SHALL support incognito modes:
- `off`: normal persistent behavior
- `ephemeral`: in-session quality behavior without persistent writes
- `no_send`: do not send ingest events

**FR-48:** Sensitive-token redaction SHOULD be available for logs and non-essential surfaces.

**FR-49:** Info-level logs SHALL NOT include raw command text.

**FR-50:** Runtime socket/lock directories SHALL be user-private, and daemon operation SHALL be scoped to invoking user context.

### 4.9 API and CLI Contract

**FR-51:** The runtime API SHALL expose session lifecycle ingest, `Suggest`, `Search`, `RecordFeedback`, and diagnostics surfaces.

**FR-52:** `SuggestRequest` SHALL support context fields needed for race-safe ranking (`session`, `cwd`, `repo`, prefix, fallback last-command context, limits, low-confidence flag).

**FR-53:** `SuggestResponse` SHALL include ranked suggestions, explainability reasons, cache status, latency, and optional adaptive timing hint.

**FR-54:** `SearchRequest`/`SearchResponse` SHALL support scope/mode and return timestamps, normalization, repo context, ranking info, tags, and backend metadata.

**FR-55:** CLI surfaces SHALL include `clai suggest`, `clai search`, `clai suggest-feedback`, `clai suggestions doctor`, and daemon lifecycle commands.

**FR-56:** Suggest/search output SHALL support `text` and `json`; interactive fuzzy (`fzf`) mode SHALL require TTY and fail with explicit unsupported-tty error otherwise.

**FR-57:** API errors SHALL follow structured codes/messages with retryability signal; `clai suggest` SHALL default to fail-open empty output on daemon failure unless strict mode is requested.

**FR-58:** A local health endpoint SHALL be available while daemon is running.

### 4.10 Reliability, Data Lifecycle, and Performance

**FR-59:** Daemon startup SHALL enforce single-instance semantics with stale lock/socket cleanup behavior.

**FR-60:** Schema migration SHALL run before serving requests, with startup refusal for unsupported newer schema versions.

**FR-61:** Corruption handling SHALL recover service by rotating corrupt artifacts and reinitializing clean state.

**FR-62:** Non-ephemeral command-end writes SHALL be transactional and atomic across primary aggregate updates.

**FR-63:** Retention and maintenance SHALL prune/checkpoint/optimize in bounded non-fatal background loops.

**FR-64:** Suggestion serving SHALL use multi-layer caching with deterministic invalidation on new command context changes.

**FR-65:** On hot-path deadline exhaustion, the system SHALL return best-available cache fallback when possible.

---

## 5. Version Scoping

### 5.0 v1 (Current Baseline)

v1 includes all functional requirements in Section 4, with these baseline architecture decisions:

1. single ingest path (`shell -> clai-shim -> gRPC -> claid`)
2. V2-native data model only (no backward-compat runtime dependency)
3. Unix-first runtime support
4. workflow-connected suggestion features excluded from runtime behavior

### 5.1 Deferred / Future

The following are explicitly not active in v1:

1. Windows-native runtime transport behavior
2. workflow-connected suggestion signals and mining
3. cloud-synced multi-device profiles

---

## 6. Verification Model

This functional spec is verified through five test categories:

1. Unit: deterministic normalization, ranking, validation, feedback math, suppression transitions, search/tag logic.
2. Integration: daemon lifecycle, IPC contracts, ingestion-ordering behavior, DB transactionality, migration/recovery, incognito persistence semantics.
3. Expect: interactive shell contract in bash/zsh/fish, prompt safety, no-daemon fail-open behavior.
4. Docker: cross-distro shell/runtime variance coverage.
5. End-to-end: scenario-level user flows (suggestions, failure recovery, discovery, search modes, diagnostics, incognito).

Passing criteria are defined in `./specs/suggestions_tests_v1.md`.

---

## 7. Example User Flows

### 7.1 Next-command Prediction in Repo Context

1. User runs commands in a repo (`git`, build, test).
2. Engine learns transitions scoped by session/repo/directory/project type.
3. User pauses at prompt.
4. `clai suggest` returns top commands with reasons such as transition/frequency/project-type.
5. User accepts a suggestion; feedback reinforces ranking.

### 7.2 Failure Recovery and Typo Assistance

1. User runs a command that fails (for example unknown command or non-zero exit).
2. Next suggestion call prioritizes recovery and typo-correction candidates.
3. Suggestions show risk and explainability reasons.
4. User dismisses bad recovery and marks one as never.
5. Future ranking suppresses the dismissed path and blocks permanent-never candidate until unblock.

### 7.3 Task Discovery and Search

1. User enters a repository with scripts/targets and optional `.clai/tasks.yaml`.
2. Engine discovers task candidates and applies conditional triggers when context matches.
3. User runs `clai search --mode auto` to find prior commands and related described intent.
4. Response indicates backend (`fts5` or fallback) and matched tags.

---

## 8. Glossary

| Term | Definition |
|---|---|
| Suggestion | A ranked candidate command returned to the user at prompt time |
| Session scope | Signals learned within one interactive shell session |
| Repo scope | Signals tied to one repository identity |
| Directory scope | Signals tied to cwd hierarchy context |
| Project type | Language/tool ecosystem inferred from marker files or overrides |
| Slot | Variable placeholder in normalized command template (for example `<path>`) |
| Failure recovery | Candidate command likely to resolve a recent failed command |
| Discovery | Low-priority candidate source for useful-but-not-yet-personalized commands |
| Suppression | Feedback-driven hiding/down-ranking of unwanted suggestions |
| Incognito `ephemeral` | In-memory behavior without persistent writes |
| Incognito `no_send` | Skip ingest transmission entirely |
| Explainability reason | A named score contribution shown to justify ranking |
