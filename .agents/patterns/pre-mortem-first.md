# Pattern: Pre-Mortem Before Implementation

**Tier:** 2 (Pattern)
**Source:** Knowledge Flywheel post-mortem (2026-01-22)

## Problem

Implementation failures are expensive. Debugging takes longer than preventing.

## Solution

Run /pre-mortem on P0/P1 work BEFORE /crank:

```bash
/pre-mortem .agents/specs/my-feature.md
# Review findings
# Then implement
/crank
```

## Evidence

Pre-mortem caught 6 critical issues before implementation:
- API group mismatches
- Path resolution errors
- Migration assumptions
- Schema drift

## When to Skip

- Bug fixes (already understood)
- Single-file changes (<50 lines)
- P2/P3 priority work
