# Fish-like UX: Phase 1 Requirements

**Scope:** Phase 1 (internal launch). This document defines the exact UX contract for delivering a fish-like interactive experience in clai across zsh, bash, and PowerShell, without changing the user's shell or terminal emulator.

This is a behavioral and interaction spec, not an implementation task list.

---

## 1. Goals

The goal of the Fish-like UX layer is to provide:

- Immediate, low-latency inline autosuggestions
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

- Replacing native shell completion systems
- Reimplementing filesystem/path completion
- Perfect parity with fish internals
- Providing session restore / replay (Phase 2+)

---

## 3. Core UX Concepts

### 3.1 Autosuggestion Model

- As the user types, clai displays one inline suggestion ("ghost text")
- The suggestion represents the best-ranked candidate
- Suggestions are derived from:
  1. Current session history
  2. Current working directory / repo history
  3. Global history
  4. Extracted commands (from prior outputs)

AI is not used for inline autosuggestions in Phase 1.

### 3.2 Visibility Rules

- Suggestions appear after ≥ 2 typed characters
- Suggestions are hidden when:
  - buffer is empty
  - cursor is not at end-of-line
  - UI is in paused mode (alternate screen, raw TTY)

---

## 4. Suggestion Acceptance & Navigation

### 4.1 Canonical Keybindings (Conceptual)

| Action | Key | Description |
|--------|-----|-------------|
| Accept full suggestion | `→` (Right Arrow) | Insert entire suggestion |
| Accept next token | `Alt + →` | Insert next word/token |
| Cycle next suggestion | `Alt + ]` | Move forward in ranked list |
| Cycle previous suggestion | `Alt + [` | Move backward |
| Dismiss suggestion | `Esc` | Clear suggestion overlay |

These bindings are consistent across shells, subject to shell capabilities.

---

## 5. History Search / Picker

### 5.1 Invocation

- `Alt + h` opens the history picker

### 5.2 Default Scope

- Initial scope: current session only

### 5.3 Scope Cycling

Inside the picker:

| Key | Scope |
|-----|-------|
| `Ctrl + s` | session history |
| `Ctrl + d` | cwd / repo history |
| `Ctrl + g` | global history |

### 5.4 Behavior

- Picker is insert-only (never executes)
- Selected command replaces the current buffer
- Picker must be cancelable at any time

---

## 6. On-Failure UX ("clai hint")

### 6.1 Trigger

After a command completes with non-zero exit code:

- clai may display a compact hint block (2–4 lines max)

### 6.2 Content Rules

Hint block may include:

- short failure summary (heuristic)
- up to 2 suggested next commands

AI-powered explanations are not shown automatically.

### 6.3 Failure Actions

| Action | Key |
|--------|-----|
| Explain last error (AI) | `Alt + e` |
| Insert suggested fix | `Alt + f` |

Inserted fixes are never executed automatically.

---

## 7. Command-Not-Found Assistance

### 7.1 Detection

Triggered when exit code indicates command-not-found (shell-specific):

- POSIX shells: commonly exit code 127
- PowerShell: `$LASTEXITCODE` or error record

### 7.2 Behavior

Show a minimal hint:

- possible intended command from history
- install hint for common tools (best-effort)

No automatic installation or execution.

---

## 8. Per-Shell Behavior

### 8.1 zsh

**Line Editor:**

- Implemented via ZLE widgets
- Suggestions rendered as right-buffer overlay

**Keybindings:**

| Action | Binding |
|--------|---------|
| Accept suggestion | `→` |
| Accept token | `Alt + →` |
| Cycle next | `Alt + ]` |
| Cycle prev | `Alt + [` |
| History picker | `Alt + h` |
| Explain error | `Alt + e` |
| Fix error | `Alt + f` |

**Notes:**

- Must not override Tab completion
- Must respect user custom keymaps

---

### 8.2 bash

**Line Editor:**

- Implemented via Readline bindings
- Ghost text emulated by temporary buffer overlay

**Keybindings:**

| Action | Binding |
|--------|---------|
| Accept suggestion | `→` |
| Accept token | `Alt + →` |
| Cycle next | `Alt + ]` |
| Cycle prev | `Alt + [` |
| History picker | `Alt + h` |

**Notes:**

- Behavior may be slightly less fluid than zsh
- Must not interfere with existing readline history navigation

---

### 8.3 PowerShell

**Line Editor:**

- Implemented via PSReadLine prediction APIs / handlers
- Uses native prediction rendering when available

**Keybindings:**

| Action | Binding |
|--------|---------|
| Accept suggestion | `→` |
| Accept token | `Alt + →` |
| History picker | `Alt + h` |
| Explain error | `Alt + e` |
| Fix error | `Alt + f` |

**Notes:**

- Must respect PSReadLine keybinding precedence
- Prediction source must be marked as external (clai)

---

## 9. Performance Requirements

- Inline suggestion latency: < 20 ms (non-AI)
- UI updates must be async and cancellable
- No visible lag during typing

---

## 10. Degradation & Escape Hatches

### 10.1 Automatic Degradation

- If daemon unavailable → no suggestions, shell works normally
- If UI paused → suggestions hidden

### 10.2 User Controls

- `clai off` / `clai on`
- `CLAI_OFF=1`
- Suggestions disabled entirely via config

---

## 11. Acceptance Criteria (Phase 1)

Phase 1 Fish-like UX is considered complete when:

- [ ] Users see inline suggestions within seconds of install
- [ ] Suggestions feel relevant within a single session
- [ ] No commands are executed automatically
- [ ] Users can disable clai instantly without restarting the shell
- [ ] UX behaves consistently across zsh, bash, and PowerShell

---

## 12. Future Extensions (Out of Scope)

- Multi-suggestion inline stacks
- Inline documentation / flag descriptions
- Session restore / replay
- SSH-aware UX

---

## 13. Rationale

**fish demonstrates that:**

- Inline autosuggestions change user behavior
- History quality matters more than feature count

**clai's approach:**

- Bring these benefits to all shells
- Without requiring shell replacement
- With stronger safety and portability guarantees
