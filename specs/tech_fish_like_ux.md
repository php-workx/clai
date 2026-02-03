# Fish-like UX: Phase 1 Requirements

**Scope:** Phase 1 (internal launch). This document defines the target UX contract for delivering a fish-like interactive experience in clai across zsh, bash, and fish, without changing the user's shell or terminal emulator.

This is a behavioral and interaction spec, not an implementation task list.

---

## 1. Goals

The goal of the Fish-like UX layer is to provide:

- Immediate, low-latency inline autosuggestions (zsh, fish)
- Tab-triggered suggestion picker with consistent behavior across all shells
- Context-aware history (session-first)
- Predictable, muscle-memory-friendly keybindings
- Identical conceptual behavior across shells
- Zero disruption to normal shell usage

This UX must:

- Never auto-execute commands
- Never block typing
- Degrade gracefully when clai is disabled or unavailable

---

## 2. Non-Goals

- Reimplementing filesystem/path completion
- Perfect parity with fish internals
- Providing session restore / replay (Phase 2+)
- PowerShell support (Phase 2+)

---

## 3. Core UX Concepts

### 3.1 Autosuggestion Model

As the user types, clai displays one inline suggestion ("ghost text") — dimmed text appended after the cursor position. The suggestion represents the best-ranked candidate.

Suggestions are derived from:

1. Current session history
2. Current working directory / repo history
3. Global history
4. Extracted commands (from prior outputs)

AI is not used for inline autosuggestions in Phase 1.

### 3.2 Ghost Text Rendering

Ghost text is the primary suggestion UI. It is rendered as dimmed (gray) text appended inline after the cursor, matching fish's native autosuggestion appearance.

**Per-shell rendering strategy:**

- **Zsh:** Render ghost text via `POSTDISPLAY` or equivalent ZLE buffer overlay. If `zsh-autosuggestions` is detected, temporarily disable it while clai suggestions are active to avoid duplicates.
- **Fish:** Provide suggestions via fish's autosuggestion hook. Suppress fish's native autosuggestions while clai is active to avoid duplicates (for example, by setting `fish_autosuggestion_enabled=0` while clai is active).
- **Bash:** No ghost text. Bash's readline does not support inline overlays without corrupting cursor positioning. Suggestions are only visible via the Tab-triggered picker (see Section 4.2).

### 3.3 Visibility Rules

Ghost text (zsh, fish) is hidden when:

- Buffer is empty
- Cursor is not at end-of-line
- UI is in paused mode (alternate screen, raw TTY)
- The suggestion picker or history picker is open
- The user edits the buffer or moves the cursor left (ghost text clears implicitly)

### 3.4 Terminology

- **Suggestion:** A full command candidate ranked by clai (history or extracted commands).
- **Completion:** Token-level expansions (paths, flags, commands) provided by the shell.

---

## 4. Suggestion Acceptance & Picker

### 4.1 Ghost Text Acceptance (zsh, fish)

| Action | Key | Description |
|--------|-----|-------------|
| Accept full suggestion | `→` (Right Arrow) | Insert entire ghost text into buffer |
| Accept next token | `Alt + →` | Insert next token from ghost text |

**Token definition:** A token is a whitespace-delimited word or a path segment (split on `/`). Accepting a token includes any trailing whitespace up to the next token.

Ghost text clears on any edit or leftward cursor movement. There is no explicit dismiss key.

Examples:

- Buffer `git ` with ghost text `commit -m "fix"`: `Alt+→` inserts `commit `.
- Buffer `/usr/` with ghost text `local/bin/clai`: `Alt+→` inserts `local/`.

### 4.2 Suggestion Picker (all shells)

The suggestion picker displays multiple ranked suggestions in a menu area below the prompt.

**Invocation:**

- `Tab` opens the suggestion picker
- If the buffer has text, suggestions are filtered by that prefix
- If the buffer is empty, top-ranked suggestions are shown

**Navigation:**

| Action | Key |
|--------|-----|
| Open picker | `Tab` |
| Move down | `↓` (Down Arrow) |
| Move up | `↑` (Up Arrow) |
| Accept selection | `Enter` |
| Dismiss picker | `Esc` |

**Behavior:**

- Selected command replaces the current buffer (insert-only, never executes)
- Ghost text is hidden while the picker is open
- The picker is cancelable at any time; `Esc` restores the original buffer
- Picker displays up to 5 suggestions (configurable)
- Picker merges clai suggestions with native shell completions, with clai suggestions ranked first

**Bash-specific:** Since bash has no ghost text, the picker is the sole suggestion UI. Tab opens it using readline's native completion display (`COMPREPLY`). Readline's `menu-complete` mode enables cycling through candidates with repeated Tab presses.

---

## 5. History Picker

### 5.1 Invocation

- `↑` (Up Arrow) opens the history picker
- The history picker uses the same menu area and navigation keys as the suggestion picker
- When clai is active, history navigation uses the clai picker (shell-native history navigation is overridden)

### 5.2 Prefix Filtering

- If the buffer has text, the history picker is pre-filtered to entries matching that prefix
- If the buffer is empty, recent history entries are shown

### 5.3 Scope

Initial scope is current session history. Inside the picker, scope can be cycled:

| Key | Scope |
|-----|-------|
| `Ctrl + x s` | Session history |
| `Ctrl + x d` | CWD / repo history |
| `Ctrl + x g` | Global history |

### 5.4 Behavior

- Selected command replaces the current buffer (insert-only, never executes)
- Ghost text is hidden while the picker is open
- The picker is cancelable at any time; `Esc` restores the original buffer
- Navigation keys are identical to the suggestion picker (`↑`, `↓`, `Enter`, `Esc`)

---

## 6. Per-Shell Behavior

### 6.1 Zsh

**Line Editor:**

- Implemented via ZLE widgets
- Ghost text rendered via `POSTDISPLAY` or equivalent inline overlay
- Picker rendered via `zle -M` (message area below prompt)

**Keybindings:**

| Action | Binding |
|--------|---------|
| Accept ghost text | `→` |
| Accept next token | `Alt + →` |
| Open suggestion picker | `Tab` |
| Open history picker | `↑` |
| Navigate picker down | `↓` |
| Accept picker selection | `Enter` |
| Dismiss picker | `Esc` |
| History scope: session | `Ctrl + x s` |
| History scope: cwd | `Ctrl + x d` |
| History scope: global | `Ctrl + x g` |

**Coexistence:**

- If `zsh-autosuggestions` is detected, disable it while clai suggestions are active
- Tab is owned by clai; native completions are merged into the picker results
- clai should be sourced after `zsh-autosuggestions` and `zsh-syntax-highlighting` to minimize POSTDISPLAY or highlight conflicts
- Must respect user custom keymaps

---

### 6.2 Bash

**Line Editor:**

- Implemented via Readline bindings and `complete -F`
- No ghost text — suggestions only via Tab-triggered picker
- Picker uses Readline's native completion display (`COMPREPLY`)

**Keybindings:**

| Action | Binding |
|--------|---------|
| Open suggestion picker | `Tab` |
| Cycle next suggestion | `Tab` (with `menu-complete`) |
| Open history picker | `↑` |
| Navigate picker down | `↓` |
| Accept picker selection | `Enter` |
| Dismiss picker | `Esc` |
| History scope: session | `Ctrl + x s` |
| History scope: cwd | `Ctrl + x d` |
| History scope: global | `Ctrl + x g` |

**Notes:**

- Requires Bash 3.2+ (macOS default)
- `menu-complete` binding enables single-Tab cycling through suggestions
- Clai suggestions appear first in COMPREPLY, followed by native completions
- Avoid blocking Tab: prefer cached suggestions or prefetch to keep completion responsive

---

### 6.3 Fish

**Line Editor:**

- Provide suggestions via fish's autosuggestion system
- Suppress fish's native autosuggestions while clai is active to avoid duplicates
- Picker rendered below prompt

**Keybindings:**

| Action | Binding |
|--------|---------|
| Accept ghost text | `→` |
| Accept next token | `Alt + →` |
| Open suggestion picker | `Tab` |
| Open history picker | `↑` |
| Navigate picker down | `↓` |
| Accept picker selection | `Enter` |
| Dismiss picker | `Esc` |
| History scope: session | `Ctrl + x s` |
| History scope: cwd | `Ctrl + x d` |
| History scope: global | `Ctrl + x g` |

**Notes:**

- Must integrate with or replace fish's native autosuggestion output, not duplicate it
- Tab completion should blend clai suggestions with fish's native completions

---

### 6.4 Compatibility Notes

- Zsh ghost text can conflict with prompt/highlighting plugins; load order and temporary suppression of other autosuggestion sources are required
- Bash path completion should remain visible; clai suggestions are ranked first but not exclusive
- Tab hijacking is mitigated by merging native completions into the picker instead of replacing them
- UI must remain responsive under latency; stale or slow responses are discarded

---

## 7. Concurrency & Debounce

### 7.1 Debounce

Suggestion requests are debounced. After the user stops typing, a short delay (50–100 ms) elapses before a suggestion request is issued. Each new keystroke resets the timer.

### 7.2 Cancellation

When a new keystroke arrives while a suggestion request is in-flight, the in-flight request is cancelled (or its result is discarded). Only the most recently issued request is honored.

### 7.3 Staleness

Each suggestion response is tagged with the prefix it was requested for. If the buffer has changed since the request was issued, the response is discarded as stale.

### 7.4 Non-Blocking

Ghost text updates and picker rendering must never block typing. All suggestion fetches run asynchronously. If the daemon is slow or unavailable, the shell remains fully responsive with no visible delay.

For bash Tab completion, if suggestions are not ready, fall back to native completions without delay.

### 7.5 Ordering

If multiple responses arrive out of order, only the response matching the current buffer state is displayed. Earlier responses for outdated prefixes are silently dropped.

---

## 8. Performance Requirements

- UI updates must be async and cancellable
- No visible lag during typing
- Suggestion fetches must not block the line editor

Specific latency targets are deferred to Phase 2 pending architectural decisions around daemon communication (subprocess vs. coprocess vs. persistent connection).

---

## 9. Degradation & Escape Hatches

### 9.1 Automatic Degradation

- If daemon unavailable → no suggestions, shell works normally
- If UI paused (alternate screen, raw TTY) → suggestions hidden

### 9.2 User Controls

- `clai off` / `clai on` — toggle suggestions in current session
- `CLAI_OFF=1` — disable via environment variable
- Suggestions disabled entirely via config

---

## 10. Acceptance Criteria (Phase 1)

Phase 1 Fish-like UX is considered complete when:

- [ ] Zsh and fish users see inline ghost text suggestions as they type
- [ ] Bash users see suggestions via Tab-triggered picker
- [ ] Tab opens a suggestion picker in all three shells
- [ ] Arrow-up opens a history picker in all three shells
- [ ] Suggestions feel relevant within a single session
- [ ] No commands are executed automatically
- [ ] Users can disable clai instantly without restarting the shell
- [ ] Typing is never blocked by suggestion fetches

---

## 11. Future Extensions (Out of Scope for Phase 1)

- PowerShell support
- On-failure UX (automatic error hints after non-zero exit)
- Command-not-found assistance (exit code 127 detection)
- Specific sub-20ms latency targets
- Minimum character threshold before showing suggestions
- Multi-suggestion inline stacks
- Inline documentation / flag descriptions
- Session restore / replay
- SSH-aware UX
- Alt+e (explain error) / Alt+f (fix error) keybindings

---

## 12. Rationale

**fish demonstrates that:**

- Inline autosuggestions change user behavior
- History quality matters more than feature count

**clai's approach:**

- Bring these benefits to zsh, bash, and fish
- Without requiring shell replacement
- With stronger safety and portability guarantees
- Honest about per-shell capabilities: ghost text where the shell supports it, picker-first where it doesn't
