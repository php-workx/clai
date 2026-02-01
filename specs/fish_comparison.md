# Fish → clai Mapping

## 1. Inline Autosuggestions (the fish signature feature)

**fish:** Ghost text appears as you type, accept with →

**clai equivalent:** Same UX via line editor integration:

- zsh: ZLE widget that updates right-buffer / RBUFFER-style overlay
- bash: Readline `rl_insert_text` + keybindings (harder but doable)
- PowerShell: PSReadLine "prediction" API / handlers

**clai data source:** session + cwd + global history ranking (no AI needed)

**Phase 1 recommendation:** ✅ Must-have

This is the "user feels it in 10 seconds" feature.

---

## 2. Smart History Ranking (recency + frequency, not dumb grep)

**fish:** Suggestions get better automatically based on what you actually do

**clai equivalent:** Your SQLite command table + ranking heuristics:

- session boost
- cwd/repo boost
- success bias
- decay over time

**Phase 1 recommendation:** ✅ Must-have

This is what makes autosuggestions not annoying.

---

## 3. Ctrl+R / History Search That's Actually Good

**fish:** Interactive history search is smooth and forgiving

**clai equivalent:**

- Alt-h opens a picker (fzf-style) over:
  - session history by default
  - toggle to cwd/global
- insert selected command into buffer

**Phase 1 recommendation:** ✅ Should-have

It's high-value and doesn't require PTY or AI.

---

## 4. Good Completions with Descriptions (flags, what they do)

**fish:** Completions include human-readable descriptions and value suggestions

**clai equivalent (Phase 1):**

- don't replace shell completion
- provide "assist" suggestions that include short descriptions for common tools
- optionally parse `--help` and cache flags later (Phase 2-ish)

**Phase 1 recommendation:** ⚠️ Defer (unless you keep it tiny)

You'll burn time on parsing + edge cases. History-first wins sooner.

---

## 5. Autosuggestions Incorporate Filesystem Paths

**fish:** Suggests real paths and files as you type

**clai equivalent:**

- lightweight local path completion for common contexts
- but this quickly overlaps with native completion

**Phase 1 recommendation:** ❌ Skip

Let native completion handle it for now. You're building "intelligence", not reimplementing completion.

---

## 6. "Works Out of the Box"

**fish:** Minimal config needed

**clai equivalent:**

- install + one-line rc integration
- sane defaults
- visible but non-intrusive
- easy off switch

**Phase 1 recommendation:** ✅ Must-have

If this fails, adoption fails.

---

## 7. "Command Not Found" Suggestions

**fish:** Helpful hints when you type unknown commands

**clai equivalent:**

- if exit code indicates not found (commonly 127 on POSIX shells)
- suggest:
  - correct command from history ("did you mean…?")
  - install hint for common tools (brew/apt/choco) without being too smart

**Phase 1 recommendation:** ✅ Should-have

Very noticeable win, easy to implement.

---

## 8. Better Error Messaging / Fixes

**fish:** Decent messaging, but not AI-level

**clai equivalent:**

- On failure, show a compact "next steps" panel:
  - heuristic fixes first
  - AI fixes only on hotkey or explicit opt-in
- If `privacy.capture_stderr=on-failure` and safe → use stderr tail

**Phase 1 recommendation:** ✅ Should-have

But keep it quiet and minimal.

---

## 9. Universal Variables / Shared State

**fish:** Variables can persist across sessions (`set -U`)

**clai equivalent:**

- not needed; your daemon provides shared state implicitly (history, caches)

**Phase 1 recommendation:** ❌ Skip

This is fish-specific and not what users want from clai.

---

## The "Fish-like Bundle" for Phase 1 (Minimum Lovable Product)

If you do just these, you'll get 80% of the fish feel:

1. Inline autosuggestions (history-based, session/cwd/global ranking)
2. Session-first history (tab feels self-contained)
3. Great history search (picker to insert past commands)
4. On-failure helper (tiny next-step suggestions + optional AI)
5. Command-not-found assistance
6. Instant on/off + offline mode

Everything else is gravy.

---

## Concrete UX Spec (Copyable)

Here's a tight interaction contract you can adopt:

### As You Type

| Action | Behavior |
|--------|----------|
| (typing) | show 1 ghost suggestion (best match) |
| `→` | accepts the full suggestion |
| `Alt-→` | accepts one word/token |
| `Alt-]` / `Alt-[` | cycles suggestions |

### History Picker

| Hotkey | Behavior |
|--------|----------|
| `Alt-h` | open history picker (session default) |
| `Ctrl-g` | toggle scope session/cwd/global |

### On Command Failure

- print a 2–4 line "clai hint" block
- `Alt-e`: explain last error (AI if enabled)
- `Alt-f`: insert a suggested fix (never run)

### Escape Hatches

- `clai off` / `clai on`
- `CLAI_OFF=1`
- `privacy.capture_stderr` default off

This gives you fish-like behavior without forcing users to learn clai subcommands.

---

## Open Questions (Optional)

To tighten this into a proper spec, the only choices are:

- Do you prefer right-arrow accept or tab accept?
- Do you want suggestions visible always or only after N chars (e.g., 2)?
- Do you want "clai hint" blocks always on failure or only with a hotkey?

**Defaults (if not specified):**

- `→` accept
- show after 2 chars (reduces noise)
- show hints on failure (compact), AI behind hotkey

---

## Fish is Great… But It Doesn't Solve Your Actual Wedge

**Fish's killer features are:**

- inline suggestions
- good defaults
- nice completions

**But fish does not give you:**

- cross-shell consistency (bash/zsh/PowerShell everywhere)
- an "intelligence layer" that works inside SSH sessions on servers
- output-aware command extraction / diagnostics (it doesn't capture output)
- centralized policies: offline mode, redaction, provider choice
- "bring your terminal brain with you" across machines/sessions

**Your wedge is not "make terminal pretty." It's:**

> Make the command line smarter in environments you can't control.

That's why clai can exist even with fish.

---

## Why People Would Still Want clai (Even If Fish Exists)

### 1. Most People Can't Use Fish Everywhere

- On servers, CI, containers, and minimal environments, you get bash/sh.
- In corporate environments, you often can't change the login shell.
- On Windows, the reality is PowerShell.
- Teams standardize on bash scripts; fish is intentionally not POSIX-compatible.

So even fish fans often switch back to bash/zsh for compatibility.

**clai's pitch:** Keep your preferred shell, and still get intelligence.

### 2. Fish Doesn't Solve the SSH Problem

This is your biggest differentiator.

- fish doesn't magically make your remote session smart.
- fish doesn't provide AI help on a remote Ubuntu box that lacks tooling.
- fish doesn't capture and analyze output across hops.

If you deliver:

> "AI assist inside SSH without remote install"

…that's something fish doesn't compete with.

### 3. Fish Doesn't Do Output-Aware Diagnosis/Extraction

Fish suggestions are history/completion driven. clai can also:

- detect failures and suggest next steps
- extract runnable commands from outputs ("Run: …")
- maintain a local daemon cache + structured "what happened" context

Even if you don't do full output capture initially, the architecture points there.

### 4. clai Can Be "Portable Muscle Memory"

People don't just want good UX; they want consistency.

If clai works similarly in:

- zsh on Mac
- bash in a Docker container
- PowerShell on Windows
- ssh to prod

…that consistency is valuable. Fish doesn't cover that.

---

## So Are Fish Users Your Target Audience?

Not as a primary wedge, but they're not excluded.

**Fish users don't need:**

- history-based autosuggestions (they already have it)

**Fish users might still want:**

- error diagnosis via AI (especially across tools)
- consistent behavior across shells/OS
- SSH intelligence without remote install
- a "unified suggestion brain" (especially if they use multiple machines)

In other words: fish users aren't your main target, but they can be enthusiastic adopters if you message it correctly.

---

## Your Real Primary Audience (Phase 1)

The people who will care immediately:

- zsh users on macOS (default-ish reality)
- bash users who won't switch shells
- PowerShell users who live in Windows terminals
- infra/platform folks who do lots of SSH + kubectl + terraform
- anyone stuck with "whatever shell the environment has"

That's a huge market compared to "people willing to switch to fish."

---

## Product Messaging That Avoids the "But Fish Exists" Objection

**Don't position clai as "better shell UX".**

Position it as:

- "fish-like intelligence for any shell" (local)
- "works in SSH sessions and on servers too" (differentiator)
- "AI help with guardrails, privacy controls, and offline mode"
- "no terminal emulator required"

So fish is not a competitor; it's a proof that people like this UX.

---

## Reality Check You Should Do

If your v0 only ships "fish-like autosuggestions for zsh", then yes — fish makes you look redundant.

To avoid that, your v0 must deliver at least one thing fish doesn't:

- cross-shell support (zsh + bash + PS)
- safety/privacy knobs + offline mode
- smarter context-based suggestions than fish (session + cwd + tool flow)
- error diagnosis workflow

If you hit those, you're not "fish but worse." You're "fish-like UX + portability + intelligence."

---

## Next Steps

If you want, tell me what your Phase 1 audience is (you personally + 3 colleagues? infra-heavy?), and I'll craft a positioning paragraph and a 30-second demo script that makes the "why clai?" obvious even to fish users.
