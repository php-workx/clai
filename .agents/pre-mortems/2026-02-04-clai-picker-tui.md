# Pre-Mortem: clai-picker TUI (History Picker) — Round 2

**Date:** 2026-02-04
**Spec:** specs/2026-02-04-clai-picker-tui.md
**Previous Pre-Mortem:** Round 1 found 6 CRITICAL + 9 HIGH gaps; all were addressed in spec.

## Round 1 Verification

All 15 previously identified gaps have been **VERIFIED as resolved** in the current spec:

| # | Gap | Verified At | Status |
|---|-----|-------------|--------|
| 1 | gRPC HistoryFetch RPC | lines 240-263 | RESOLVED |
| 2 | Input sanitization | lines 133-136 | RESOLVED |
| 3 | Config keys (HistoryConfig) | lines 101, 181 | RESOLVED |
| 4 | Bubbletea prerequisites | lines 144, 179-182 | RESOLVED |
| 5 | Provider timeout (200ms) | line 110 | RESOLVED |
| 6 | Backend fallback clarity | lines 328-331 | RESOLVED |
| 7 | State machine enum | lines 205-214 | RESOLVED |
| 8 | Selection bounds clamping | line 217 | RESOLVED |
| 9 | File lock for concurrency | lines 150, 196 | RESOLVED |
| 10 | CLAI_SESSION_ID lifecycle | line 106 | RESOLVED |
| 11 | Tab change resets paging | lines 32, 116 | RESOLVED |
| 12 | TERM=dumb exit 2 | lines 89, 125, 194 | RESOLVED |
| 13 | RequestID in Request | line 219 | RESOLVED |
| 14 | Active tab indicator | line 33 | RESOLVED |
| 15 | Shell glue examples | lines 283-324 | RESOLVED |

## Round 2: New Findings

### Checklist Verification (10 taxonomy categories)

| Category | Item | Present? | Location | Complete? |
|----------|------|----------|----------|-----------|
| Interface | gRPC proto definition | yes | lines 240-263 | yes |
| Interface | Stdout/stderr contract | yes | lines 83-90, 232-235 | yes |
| Interface | Storage query protocol (substring) | partial | line 51, 113 | **partial** — storage only supports prefix, not substring |
| Timing | Provider timeout | yes | line 110 | yes — 200ms hard deadline |
| Timing | Debounce semantics | yes | lines 48, 116 | yes — cancel on new keystroke/tab |
| Timing | Up key during loading state | no | — | **no** — unspecified behavior |
| Error | Provider failure UX | yes | line 110 | yes — error state + retry |
| Error | Daemon dies mid-fetch | partial | line 268 | **partial** — covers "unavailable" but not mid-flight crash |
| Error | File lock stale after crash | no | line 150 | **no** — no PID check or stale detection |
| Safety | Input sanitization | yes | lines 133-136 | yes |
| Safety | ANSI regex coverage | partial | line 135 | **partial** — misses OSC sequences |
| Safety | CLAI_CACHE dir missing | no | line 196 | **no** — mkdir not specified |
| Rollback | Cancel restores buffer | yes | line 41 | yes |
| Deps | Bubbletea/lipgloss | yes | line 144 | yes |
| Deps | Storage substring support | no | — | **no** — codebase only has prefix match |
| State | State machine | yes | lines 205-214 | yes |
| State | Selection bounds | yes | line 217 | yes |
| State | Pagination with dedup | partial | lines 113-114, 149 | **partial** — offset semantics unclear |
| UX | CJK double-width chars | no | — | **no** — display width != byte length |
| UX | Up key buffering during load | no | — | **no** — rapid presses during loading |
| Operational | CLAI_DEBUG logging | yes | line 107 | yes |
| Operational | Shell exit code 2 handling | partial | lines 296, 331 | **partial** — shell glue examples don't check for exit 2 |

## Findings

### HIGH (Should Fix Before Implementation)

1. **Storage layer lacks substring matching** (spec line 51 vs `internal/storage/commands.go:166-169`)
   - **Current state:** `CommandQuery.Prefix` only supports `LIKE prefix%`. Spec requires substring: `LIKE %query%`.
   - **Impact:** Search for "status" won't find "git status" — core feature broken.
   - **Fix:** Add `Substring string` field to `CommandQuery` in storage layer. Issue `ai-terminal-5em.2` (FetchHistory RPC) must include this storage change.

2. **Pagination offset ambiguity with deduplication** (spec lines 113-114, 149)
   - **Issue:** Spec says "paging applies to filtered result set" and "deduplicate by command text." But offset/limit semantics are undefined when dedup reduces pages.
   - **Scenario:** Fetch 100 items, dedup reduces to 75. Does offset=100 refer to pre-dedup or post-dedup position? If pre-dedup, next page may have duplicates of page 0.
   - **Fix:** Add to spec: "Deduplication happens **at the provider level** (SQL `GROUP BY command_norm`). Offset/limit apply to the deduplicated result set. Provider fetches from the DB using `GROUP BY command_norm ORDER BY MAX(ts_start_unix_ms) DESC LIMIT ? OFFSET ?`."

3. **CLAI_CACHE directory may not exist** (spec line 196)
   - **Scenario:** Fresh install, user runs `clai-picker` before `clai init`. `$CLAI_CACHE` doesn't exist. File lock fails.
   - **Fix:** Add to startup sequence (between steps 3 and 4): "Ensure `$CLAI_CACHE` directory exists (`os.MkdirAll`). If creation fails, exit 1 with error to stderr."

4. **Stale file lock after crash** (spec line 150)
   - **Scenario:** Picker crashes (SIGKILL, OOM). Lock file persists. Next picker invocation exits 1. User is stuck.
   - **Fix:** Use `flock()` (advisory lock), not lock file existence. `flock` auto-releases when process dies. If using Go's `syscall.Flock`, the lock is released on process exit regardless of crash. Add to spec: "Use `syscall.Flock` (exclusive advisory lock) on the lock file. Lock is auto-released on process exit."

5. **Shell glue doesn't handle exit code 2** (spec lines 296, 311, 322)
   - **Current state:** Zsh example (line 288): `result=$(clai-picker ...) || return` — treats all non-zero as cancel. Spec line 331: "Shell glue treats exit 2 as picker unavailable → fall back to native history."
   - **Fix:** Update all three shell glue examples to check exit code:
     ```
     Zsh:  if [[ $? -eq 2 ]]; then zle .up-line-or-history; return; fi
     Bash: if [ $? -eq 2 ]; then builtin history-search-backward; return; fi
     Fish: if test $status -eq 2; builtin history; return; end
     ```

6. **CJK character display width** (spec line 54)
   - **Issue:** Middle truncation uses byte/rune count, not display columns. CJK characters are 2 columns wide. Truncation may corrupt display.
   - **Fix:** Add to spec: "Use display-width-aware truncation (e.g., `go-runewidth`). Truncate based on terminal column count, not rune count."

7. **Up arrow conflict with existing shell bindings** (codebase: `clai.zsh:519-520`)
   - **Current state:** Zsh already binds `^[[A` to `_ai_menu_up` (suggestion menu). Picker wants the same key.
   - **Fix:** Issue `ai-terminal-5em.6` (shell integration) must replace the existing binding, not add a second one. Document the migration: old `_ai_menu_up` binding is removed; picker replaces it.

### MEDIUM (Worth Noting)

1. **Up key during loading state** — Spec doesn't specify behavior. Rapid Up presses during loading could queue multiple fetches. Fix: "During loading state, Up key is a no-op."

2. **Daemon dies mid-fetch** — Spec covers "unavailable" (line 268) but not crash during active RPC. gRPC context deadline handles this (200ms timeout applies), so behavior is correct but user gets no explanation why it timed out. Fix: Error message could say "Connection lost" instead of generic "Loading timed out."

3. **Fish `commandline -r` with multi-line commands** — Spec says "output full multi-line command as-is" (line 135). Fish's `commandline -r` should handle literal newlines, but behavior may vary. Fix: Add to manual testing checklist.

4. **ANSI regex coverage** — Spec regex `\x1b\[[0-9;]*[a-zA-Z]` misses OSC sequences (`\x1b]...\a`) and character set selection (`\x1b(B`). Fix: Use a more comprehensive regex or a library like `ansi.Strip()`.

5. **Concurrent writes during paging** — New commands inserted while picker pages through history can shift offsets. Mitigated by using timestamp-based dedup ordering, but edge cases remain. Fix: Consider timestamp cursor instead of offset for pagination (implementation detail, not spec change).

6. **Bash runtime `bind -x` detection** — Spec says "Bash 4.0+" (line 71) but doesn't specify runtime detection. A system could report Bash 4.0 but have readline compiled without `bind -x`. Fix: Shell glue should test `bind -x` availability at init time.

## Implicit Assumptions Found (New)

| Location | Assumption | Risk | Status |
|----------|------------|------|--------|
| line 149 | Dedup is per-page (implementation choice) | HIGH | Needs clarification: should be DB-level GROUP BY |
| line 166 | Storage supports substring LIKE | HIGH | False — only prefix. Must be added. |
| line 196 | CLAI_CACHE directory exists | HIGH | Could fail on fresh install |
| line 150 | File lock auto-releases on crash | MEDIUM | True for flock(), not for lockfile existence |
| line 288 | Shell glue treats exit 1 and 2 differently | MEDIUM | Examples don't distinguish them |
| line 54 | Rune count == display width | MEDIUM | False for CJK, emoji |
| line 519 (zsh) | Up arrow is unbound or can be replaced | MEDIUM | Already bound to _ai_menu_up |

## Edge Cases Without Handling (New)

| Location | Input | Boundary Missing | Suggested Handling |
|----------|-------|-----------------|-------------------|
| line 217 | Selection during loading→empty transition | State race | Clamp on every state change |
| line 196 | CLAI_CACHE missing | mkdir not specified | os.MkdirAll before lock |
| line 150 | Process crashed with lock held | No stale detection | Use flock() advisory lock |
| line 54 | CJK/emoji characters | Display width miscalc | Use go-runewidth library |
| line 288 | Exit code 2 from picker | Shell glue doesn't check | Add exit code branch |
| lines 113-114 | Dedup reduces page below limit | Short page ambiguity | DB-level GROUP BY dedup |

## Verdict

[x] ~~READY with caveats — 0 CRITICAL, 7 HIGH, 6 MEDIUM~~ **ALL HIGH RESOLVED IN SPEC**

All 7 HIGH findings have been addressed in the spec (`specs/tech_picker_tui.md`):

| HIGH Finding | Spec Fix |
|---|---|
| Storage substring matching | Added to line 268: requires `Substring` field or `SubstringMatch` flag |
| Pagination + dedup semantics | Added after line 149: DB-level GROUP BY, offset on deduped set |
| CLAI_CACHE mkdir | Added startup step 4: os.MkdirAll before lock |
| Stale file lock (use flock) | Added to steps 5 + line 151: syscall.Flock advisory lock |
| Shell glue exit code 2 | Updated all 3 shell examples with exit_code branching |
| CJK display width | Added to line 54: display-width-aware truncation via go-runewidth |
| Up arrow binding conflict | Added note at line 308: replaces existing _ai_menu_up binding |

Additional fixes:
- Loading state key behavior: Up/Down are no-ops during loading (line 220)
- ANSI regex: expanded to cover OSC and charset sequences (line 135)
- Bash version detection: runtime check added to shell glue (line 325-328)

**Updated verdict: READY for implementation** (0 CRITICAL, 0 HIGH, 6 MEDIUM remaining)
