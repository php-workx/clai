# Spaces — Technical Specification (v0.1)

> File: `specs/tech-spaces.md`
>
> Scope: Phase 1. This document specifies **Spaces** as named, resumable command-context collections. Spaces are **not** process/session restoration. They scope history, ranking, and suggestions.
>
> This is a concrete technical + UX contract, intended to be implemented against.

---

## 1. Concept & Intent

### 1.1 Definition

A **Space** is a named container that groups commands and their derived context into a reusable **knowledge context**.

Spaces allow users to:

* mentally separate workflows (e.g. `k8s-prod`, `db-migration`, `deploy-foo`)
* resume a workflow days or weeks later
* get sharper suggestions and recall

A Space is **not**:

* a restored shell process
* a replay of commands
* a guarantee of environmental state

"Resuming" a Space means **attaching the current shell session to that Space**.

---

## 2. Core Properties

### 2.1 Space Characteristics

Each Space:

* has a unique name
* persists across shell sessions
* owns a subset of commands
* influences suggestion ranking

Each shell **Session**:

* may be attached to **zero or one Space** at a time
* can switch Spaces at any time

Commands executed while a session is attached to a Space are automatically assigned to that Space.

---

## 3. User Experience

### 3.1 User Engagement & Onboarding

Spaces must be **explicitly user-driven**, but gently encouraged.

Phase 1 onboarding rules:

* clai does **not** auto-create Spaces on install
* clai does **not** scan or cluster global history to create Spaces

Instead, clai may:

* show a one-time onboarding hint:

  > "Spaces help you keep workflows like `k8s-prod` or `deploy-foo`. Attach a Space to make suggestions and history feel ‘project-specific’. Press **Alt-s** to create or switch Spaces."

* suggest creating a Space based on **local heuristics only**, e.g.:

	* repeated execution of the same tool (`kubectl`, `terraform`, `psql`) within a short time window
	* suggestion must be explicit and dismissible (y/N)

User-facing positioning (Phase 1):

* Spaces are **labels for your command memory**, not a recording of your terminal UI.
* Spaces do not auto-run anything.
* Spaces do not upload anything when offline mode is enabled.

### 3.2 Switch / Resume Space

* **Keybinding:** `Alt + s`
* Opens Space selector
* Selecting a Space immediately attaches the current session

Effect:

* autosuggestions prioritize Space history
* history picker defaults to Space scope

### 3.3 Create Space

* **Keybinding:** `Alt + Shift + s`
* Prompt for Space name
* Attaches current session to newly created Space

Suggested UX hint:

> "Attach this session to Space ‘k8s-prod’?"

### 3.4 Detach from Space

* **Command:** `clai space detach`
* **Keybinding:** `Alt + s` → select "Detach"
* Session returns to unscoped (default) behavior

UX requirement:

* Active Space name must always be visible (prompt or title)
* Detach must be one action away

---

* **Command:** `clai space detach`
* Session returns to unscoped (default) behavior

---

### 3.2 History Picker Integration

The universal picker (`Alt + h`) gains a **Space scope**:

Picker scopes:

* Session
* Space (if attached)
* Global

Default picker scope when attached to a Space: **Space**.

---

### 3.3 Terminal Title (Best-Effort)

When a session is attached to a Space, clai **may** set the terminal title to:

```
[space-name] — shell
```

Notes:

* Implemented via standard OSC title escape sequences
* Best-effort only; failure is acceptable
* Never relied upon for correctness

---

## 4. CLI Interface (Fallback / Automation)

### 4.1 Commands

```bash
clai space list
clai space use <name>
clai space new <name>
clai space rename <old> <new>
clai space detach
```

Behavior:

* `use` attaches current session
* `detach` clears Space association
* `rename` updates display name only

---

## 5. Data Model

### 5.1 Tables

#### `spaces`

```sql
CREATE TABLE IF NOT EXISTS spaces (
  space_id TEXT PRIMARY KEY,
  name TEXT NOT NULL UNIQUE,
  description TEXT,
  created_at_unix_ms INTEGER NOT NULL,
  updated_at_unix_ms INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_spaces_name ON spaces(name);
```

#### `session_spaces`

Tracks current Space attachment per session.

```sql
CREATE TABLE IF NOT EXISTS session_spaces (
  session_id TEXT PRIMARY KEY,
  space_id TEXT NOT NULL,
  attached_at_unix_ms INTEGER NOT NULL,

  FOREIGN KEY(session_id) REFERENCES sessions(session_id) ON DELETE CASCADE,
  FOREIGN KEY(space_id) REFERENCES spaces(space_id) ON DELETE CASCADE
);
```

Notes:

* One-to-one: a session can be attached to only one Space

---

### 5.2 Command Association & History Model

* **Global history**: all commands in the `commands` table
* **Session history**: commands filtered by `session_id`
* **Space history**: commands filtered by `space_id`

All commands are **always** part of global and session history.
Space assignment adds an *additional organizational label*.

The existing `commands` table gains a nullable `space_id`:

```sql
ALTER TABLE commands ADD COLUMN space_id TEXT;

CREATE INDEX IF NOT EXISTS idx_commands_space_time
  ON commands(space_id, ts_start_unix_ms DESC);

CREATE INDEX IF NOT EXISTS idx_commands_space_hash
  ON commands(space_id, command_hash, ts_start_unix_ms DESC);
```

Assignment rule:

* If a session is attached to a Space at command execution time → space assignment may occur (see §6)
* Otherwise → `space_id` remains NULL

---

### 5.2 Command Association

The existing `commands` table gains a nullable `space_id`:

```sql
ALTER TABLE commands ADD COLUMN space_id TEXT;

CREATE INDEX IF NOT EXISTS idx_commands_space_time
  ON commands(space_id, ts_start_unix_ms DESC);

CREATE INDEX IF NOT EXISTS idx_commands_space_hash
  ON commands(space_id, command_hash, ts_start_unix_ms DESC);
```

Assignment rule:

* If a session is attached to a Space at command execution time → set `space_id`
* Otherwise → `space_id` remains NULL

---

## 6. Suggestion & Ranking Semantics

### 6.1 Ranking Priority

When a session is attached to a Space, candidate ranking order is:

1. **Space history**
2. Session history
3. CWD / repo history
4. Global history

If no Space is attached, normal ranking applies.

---

### 6.2 Space-Specific Statistics

For each Space, daemon maintains derived stats (Phase 1, in-memory or cached):

* most frequent command prefixes
* most frequent full commands (by `command_hash`)
* recent failures

These stats are used only for ranking and filtering.

---

### 6.3 Soft Attachment & Auto-Assignment (Phase 1)

Spaces use **filtered auto-assignment** to avoid clutter while remaining usable without manual tagging.

When a session is attached to a Space, commands are evaluated for Space assignment.

A command is **auto-assigned** to the active Space if **all** conditions are met:

* command is not in the **default stoplist** (see §6.3.1)
* command matches the **Space fingerprint** (see §6.4), OR the Space is still in its **warm-up** phase (see §6.4.2)

Commands that do not meet these conditions:

* remain in session + global history
* are *not* assigned to the Space automatically

Manual assignment always remains available (see §6.5).

#### 6.3.1 Default Stoplist (Phase 1)

Stoplist commands are considered "navigation/noise" and are excluded from auto-assignment by default.

Default stoplist (initial):

* Navigation / shell:

	* `cd`, `pwd`, `pushd`, `popd`, `dirs`, `exit`, `logout`, `clear`, `reset`, `history`
* Listing / inspection:

	* `ls`, `la`, `ll`, `tree`, `find` (note: keep `find` in stoplist initially), `which`, `where`, `type`, `command`, `stat`
* File viewing:

	* `cat`, `less`, `more`, `head`, `tail` (note: keep `tail` in stoplist initially), `bat`
* Text helpers:

	* `echo`, `printf`, `env`, `printenv`

Notes:

* Stoplist matches on the **first token** (root command).
* Stoplist is configurable (Phase 2+), but shipped as a conservative default in Phase 1.
* Rationale: these commands appear in every workflow and will pollute Spaces.

---

### 6.4 Space Fingerprint

Each Space develops a fingerprint over time based on the commands assigned to it.

The fingerprint exists to answer: "What kinds of commands belong in this Space?"

#### 6.4.1 Fingerprint Representation

A fingerprint is a small set of **root patterns** that indicate the Space’s dominant tools.

* Root pattern types:

	* **root command** (first token), e.g. `kubectl`, `helm`, `terraform`, `psql`
	* **two-token root** for common tool families, e.g. `aws eks`, `aws rds`, `gcloud container`, `gcloud sql`

A Space may have multiple roots (e.g. `kubectl` + `helm` + `kustomize`).

#### 6.4.2 Warm-up Phase (to prevent “dead spaces”)

To avoid requiring manual tagging, new Spaces start in a warm-up mode.

Warm-up behavior:

* While a Space has fewer than `WARMUP_MIN_ASSIGNED` commands (recommended: **15**), it auto-assigns all commands that:

	* are not in the stoplist
	* are not obviously interactive (per denylist rules elsewhere)

After warm-up threshold is met:

* fingerprint is computed
* subsequent auto-assignment requires fingerprint match

#### 6.4.3 Fingerprint Derivation Heuristics (Phase 1)

Input:

* last `N` assigned commands in this Space (recommended: **200**)

Steps:

1. Tokenize each command into shell-like tokens (best-effort; do not fully parse quoting in Phase 1)
2. Extract candidates:

	* `t1` = first token
	* `t1 t2` = first two tokens if `t1` in {`aws`, `gcloud`, `az`, `kubectl`, `docker`} (expandable)
3. Filter candidates:

	* drop anything in stoplist (by `t1`)
	* drop candidates shorter than 2 characters
4. Score candidates:

	* score = frequency_weight + recency_weight
	* frequency_weight: count occurrences
	* recency_weight: decay by age (recent commands matter more)
5. Select top K roots (recommended: **K=3**), with minimum score threshold to avoid noise

Output:

* fingerprint roots set for the Space

Update cadence:

* recompute fingerprint when assigned command count increases by `Δ` (recommended: **10**) or every `T` minutes (recommended: **10 min**) whichever comes first.

#### 6.4.4 Fingerprint Seeding (User Engagement Link)

If clai suggested creating a Space due to tool repetition (e.g. repeated `kubectl`), seed the fingerprint with that tool root immediately.

* This makes the Space feel relevant from the first moment.
* Warm-up still applies, but the seed biases early assignment.

---

### 6.5 Manual Overrides

Users can always override auto-assignment:

* **Add last command to Space**

	* hotkey: `Alt + a`
	* CLI: `clai space add --last <space>`

* **Remove last command from Space** (optional Phase 1)

	* CLI: `clai space remove --last`

Manual assignment bypasses fingerprint filtering.

---

### 6.2 Space-Specific Statistics

For each Space, daemon maintains derived stats (Phase 1, in-memory or cached):

* most frequent commands
* most frequent parameters
* recent failures

These stats are used only for ranking and UX hints.

---

## 7. Lifecycle & Edge Cases

### 7.1 Session Start

* New sessions start **unattached**
* No automatic Space attachment in Phase 1

### 7.2 Session End

* Space persists independently of sessions
* Session-space attachment is discarded

### 7.3 Switching Spaces

* Switching Spaces does not retroactively reassign commands
* Only future commands are eligible for assignment

### 7.4 Accidental Attachment Protection

To reduce accidental clutter:

* active Space name should be visible in the prompt or terminal title (best-effort)
* `Alt + s` must allow fast detachment
* optional nudge: if N consecutive commands are not auto-assigned, clai may prompt:

  > "Still in Space ‘k8s-prod’? Detach? (y/N)"

---

## 8. Constraints & Non-Goals

Explicitly out of scope for v0.1:

* Assigning a command to multiple Spaces
* Automatic Space detection or suggestion
* Space-based environment variables
* Replaying or restoring shell process state
* Syncing Spaces across machines

---

## 9. Privacy & Trust Considerations

* Spaces do not change what data is captured
* They only organize existing history
* No global history is scanned to auto-create Spaces
* Space assignment is explicit and reversible
* Commands always remain accessible via session and global history

---

## 10. Acceptance Criteria (Phase 1)

Spaces are considered complete when:

* Users can create, rename, and attach to Spaces
* Switching Spaces visibly changes suggestions and history scope
* History picker defaults to Space scope when attached
* Auto-assignment keeps Spaces useful without manual tagging
* Stoplist prevents common noise from polluting Spaces
* Manual override (`Alt + a`) can add the last command to a Space
* No commands are auto-executed
* Detaching returns to default behavior

---

## 11. Retention & Cleanup Policy

To ensure long-term performance and trust, clai applies tiered retention.

### 11.1 Retention Tiers (Recommended Defaults)

* **Space-tagged commands**:

	* retain for 12–24 months (configurable)
	* always keep at least last N unique commands per Space (e.g. 1k)

* **Untagged global commands**:

	* retain for 90–180 days OR cap total rows (e.g. 100k–200k)

* **Session metadata**:

	* retain for 30–90 days unless referenced by Space-tagged commands

* **AI cache**:

	* strict TTL (hours to days)

### 11.2 Cleanup Execution

* Cleanup runs periodically (e.g. daily or on daemon start)
* Steps:

	1. purge expired AI cache entries
	2. delete expired untagged commands
	3. prune Space-tagged commands beyond retention thresholds
	4. optionally run `ANALYZE`; `VACUUM` only when DB exceeds size threshold

---

## 12. Future Extensions (Phase 2+)

Potential extensions (explicitly not implemented now):

* Space auto-suggestions (based on usage heuristics)
* Space-level workflows / notebooks
* Space export / sharing
* Space sync across machines
* Multi-Space tagging

---

## 12. User-facing Explanation (Copy)

Use this copy (or close variants) in onboarding, docs, and tooltips.

### 12.1 One-liner

> **Spaces are named command contexts.** Attach a Space to make suggestions and history match what you’re working on (e.g. `k8s-prod`, `db`, `deploy`).

### 12.2 Short explanation

> A Space is a label for your command memory. When a Space is active, clai prioritizes the commands you’ve used in that Space for autosuggestions and history search. Spaces don’t restore processes, don’t replay commands, and never run anything automatically.

### 12.3 Trust-friendly notes

* Spaces only organize the history clai already stores.
* You can detach from a Space at any time.
* Offline mode disables AI/provider calls.

---

## 13. Active Space Indicator (Prompt & UI Integration)

This section defines how clai indicates an active Space to the user **without breaking existing shell prompts, themes, or terminal setups**.

The indicator is critical for:

* preventing accidental Space pollution
* reinforcing mental context ("I am in k8s-prod")
* making Spaces feel intentional, not magical

---

### 13.1 Design Principles

1. **Non-invasive by default**

	* Never rewrite or replace the user’s prompt
	* Never assume a specific theme or framework (Oh My Zsh, Starship, Powerlevel10k, etc.)

2. **Best-effort visibility**

	* Indicator should be visible in most setups
	* If visibility fails, functionality must remain correct

3. **Zero cognitive overhead**

	* User should notice the indicator subconsciously
	* No extra noise when no Space is active

4. **Shell-agnostic semantics**

	* Same conceptual behavior across zsh, bash, PowerShell

---

### 13.2 Indicator Semantics

* When **no Space** is attached:

	* No indicator is shown

* When a Space **is attached**:

	* The Space name must be visible *somewhere* during command entry
	* Indicator must update immediately on attach/detach

Indicator content:

```
[space-name]
```

Examples:

* `[k8s-prod]`
* `[deploy-foo]`

No additional metadata (IDs, icons, colors) in Phase 1.

---

### 13.3 Primary Mechanism: Terminal Title (Preferred)

clai should attempt to set the terminal title using standard OSC sequences when a Space is active.

Format:

```
[space-name] — <original-title>
```

Rules:

* Original title must be preserved and restored on detach
* Title updates must be idempotent
* Failure to set title must be silent

Rationale:

* Works across most terminal emulators
* Does not interfere with shell prompt rendering
* Easy to revert

---

### 13.4 Secondary Mechanism: Prompt Hint (Opt-in / Best-effort)

For shells that support prompt hooks, clai may expose an **optional prompt hint variable**.

Example environment variable:

```
CLAI_SPACE_NAME=k8s-prod
```

Users or prompt frameworks may choose to render this variable.

Important:

* clai **must not** inject text directly into PS1 / PROMPT by default
* clai may provide example snippets in documentation

Example (zsh, user opt-in):

```zsh
PROMPT='%F{cyan}[$CLAI_SPACE_NAME]%f '$PROMPT
```

---

### 13.5 Shell-Specific Notes

#### zsh

* Prefer terminal title updates via `precmd`
* Expose `CLAI_SPACE_NAME` env var
* Do not override existing `precmd` handlers

#### bash

* Prefer terminal title updates via `PROMPT_COMMAND`
* Expose `CLAI_SPACE_NAME` env var
* Avoid modifying `PS1` directly

#### PowerShell

* Prefer terminal title updates via `$Host.UI.RawUI.WindowTitle`
* Expose `$env:CLAI_SPACE_NAME`
* Do not override existing prompt functions

---

### 13.6 Update & Consistency Rules

* Indicator must update immediately on:

	* `clai space use <name>`
	* `clai space detach`
	* `Alt + s` attach/detach actions

* Indicator must be cleared on:

	* Space detach
	* Shell exit

* Indicator state must be session-local

---

### 13.7 Failure Modes (Acceptable)

The following are acceptable and must not be treated as errors:

* Terminal emulator ignores title updates
* User prompt theme hides the title
* User chooses not to render prompt hint

In all cases:

* Space assignment logic must remain correct
* No user-visible errors should be shown

---

### 13.8 Acceptance Criteria (Indicator)

The Active Space Indicator is considered complete when:

* Users can always determine whether a Space is active
* Attaching/detaching a Space gives immediate visual feedback
* No existing prompt configuration is broken
* Indicator never appears when no Space is active

---

## 14. Rationale

The Active Space Indicator is the primary guardrail against accidental Space clutter.

A subtle, reliable signal:

* builds user trust
* reinforces mental context
* enables safe auto-assignment

Without it, Spaces feel opaque and error-prone.

---

## 14. Rationale

Spaces provide:

* fish-like contextual memory
* Warp-like workflow separation
* shell-agnostic portability

They amplify the value of:

* autosuggestions
* history search
* error diagnosis

…without introducing PTY or process-state complexity.
