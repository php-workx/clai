# Bug Report: Zsh Startup Delay on First clai Init

**Date:** 2026-02-07
**Severity:** high
**Status:** fix-designed

## Symptom
In zsh, shell startup can take several seconds, while switching to bash/fish in the same session appears instant.

## Expected Behavior
Shell startup should be consistently fast across first launch and subsequent shells.

## Phase 1: Root Cause
- `clai init <shell>` generates `CLAI_SESSION_ID` when the env var is missing.
- Existing code used `uuid.New().String()` in `internal/cmd/init.go`.
- `uuid.New()` uses crypto randomness; on entropy-constrained systems this can block.
- Nested shells inherit `CLAI_SESSION_ID`, so generation is skipped and startup appears instant.

Root-cause location:
- `internal/cmd/init.go` session ID generation path.

## Phase 2: Pattern Analysis
Working path:
- Nested shells with inherited `CLAI_SESSION_ID` do not call UUID generation.

Broken/slow path:
- First shell with no `CLAI_SESSION_ID` triggers entropy-dependent UUID generation.

## Phase 3: Hypothesis
Hypothesis:
- Replacing crypto-random UUID generation with a non-blocking generator will remove first-start stalls while preserving ID format compatibility.

Validation:
- Added format + uniqueness test for generated IDs.
- Ran performance and full dev gate; all checks passed.

## Phase 4: Implementation
Implemented in code:
- Replaced `uuid.New().String()` with `generateSessionID()` in `internal/cmd/init.go`.
- New generator derives 16 bytes from hostname + time + pid + ppid, hashes with SHA-256, and sets UUID v4 bits.
- Added `TestGenerateSessionID_FormatAndUniqueness` in `internal/cmd/init_test.go`.

## Verification
- `go test ./internal/cmd -run 'TestGenerateSessionID_FormatAndUniqueness|TestInitPlaceholderReplacement|TestRunInit_.*' -count=1`
- `go test ./tests/expect -run TestPerformance_InitCommandFast -count=1 -v`
- `make dev`

All passed.
