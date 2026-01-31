Fish → clai mapping

1) Inline autosuggestions (the fish signature feature)

fish: ghost text appears as you type, accept with →
clai equivalent: same UX via line editor integration:
•	zsh: ZLE widget that updates right-buffer / RBUFFER-style overlay
•	bash: Readline rl_insert_text + keybindings (harder but doable)
•	PowerShell: PSReadLine “prediction” API / handlers

clai data source: session + cwd + global history ranking (no AI needed)

Phase 1 recommendation: ✅ Must-have
This is the “user feels it in 10 seconds” feature.

⸻

2) Smart history ranking (recency + frequency, not dumb grep)

fish: suggestions get better automatically based on what you actually do
clai equivalent:
•	your SQLite command table + ranking heuristics:
•	session boost
•	cwd/repo boost
•	success bias
•	decay over time

Phase 1 recommendation: ✅ Must-have
This is what makes autosuggestions not annoying.

⸻

3) Ctrl+R / history search that’s actually good

fish: interactive history search is smooth and forgiving
clai equivalent:
•	Alt-h opens a picker (fzf-style) over:
•	session history by default
•	toggle to cwd/global
•	insert selected command into buffer

Phase 1 recommendation: ✅ Should-have
It’s high-value and doesn’t require PTY or AI.

⸻

4) Good completions with descriptions (flags, what they do)

fish: completions include human-readable descriptions and value suggestions
clai equivalent (Phase 1):
•	don’t replace shell completion
•	provide “assist” suggestions that include short descriptions for common tools
•	optionally parse --help and cache flags later (Phase 2-ish)

Phase 1 recommendation: ⚠️ Defer (unless you keep it tiny)
You’ll burn time on parsing + edge cases. History-first wins sooner.

⸻

5) Autosuggestions incorporate filesystem paths

fish: suggests real paths and files as you type
clai equivalent:
•	lightweight local path completion for common contexts
•	but this quickly overlaps with native completion

Phase 1 recommendation: ❌ Skip
Let native completion handle it for now. You’re building “intelligence”, not reimplementing completion.

⸻

6) “Works out of the box”

fish: minimal config needed
clai equivalent:
•	install + one-line rc integration
•	sane defaults
•	visible but non-intrusive
•	easy off switch

Phase 1 recommendation: ✅ Must-have
If this fails, adoption fails.

⸻

7) “Command not found” suggestions

fish: helpful hints when you type unknown commands
clai equivalent:
•	if exit code indicates not found (commonly 127 on POSIX shells)
•	suggest:
•	correct command from history (“did you mean…?”)
•	install hint for common tools (brew/apt/choco) without being too smart

Phase 1 recommendation: ✅ Should-have
Very noticeable win, easy to implement.

⸻

8) Better error messaging / fixes

fish: decent messaging, but not AI-level
clai equivalent:
•	On failure, show a compact “next steps” panel:
•	heuristic fixes first
•	AI fixes only on hotkey or explicit opt-in
•	If privacy.capture_stderr=on-failure and safe → use stderr tail

Phase 1 recommendation: ✅ Should-have
But keep it quiet and minimal.

⸻

9) Universal variables / shared state

fish: variables can persist across sessions (set -U)
clai equivalent:
•	not needed; your daemon provides shared state implicitly (history, caches)

Phase 1 recommendation: ❌ Skip
This is fish-specific and not what users want from clai.

⸻

The “Fish-like bundle” for Phase 1 (minimum lovable product)

If you do just these, you’ll get 80% of the fish feel:
1.	Inline autosuggestions (history-based, session/cwd/global ranking)
2.	Session-first history (tab feels self-contained)
3.	Great history search (picker to insert past commands)
4.	On-failure helper (tiny next-step suggestions + optional AI)
5.	Command-not-found assistance
6.	Instant on/off + offline mode

Everything else is gravy.

⸻

Concrete UX spec (copyable)

Here’s a tight interaction contract you can adopt:
•	As you type:
•	show 1 ghost suggestion (best match)
•	→ accepts the full suggestion
•	Alt-→ accepts one word/token
•	Alt-] / Alt-[ cycles suggestions
•	Alt-h: open history picker (session default)
•	Ctrl-g: toggle scope session/cwd/global
•	On command failure:
•	print a 2–4 line “clai hint” block
•	Alt-e: explain last error (AI if enabled)
•	Alt-f: insert a suggested fix (never run)
•	Escape hatches:
•	clai off / clai on
•	CLAI_OFF=1
•	privacy.capture_stderr default off

This gives you fish-like behavior without forcing users to learn clai subcommands.

⸻

What I need from you (optional), but I’ll assume defaults if you don’t want to answer

To tighten this into a proper spec, the only choices are:
•	do you prefer right-arrow accept or tab accept?
•	do you want suggestions visible always or only after N chars (e.g., 2)?
•	do you want “clai hint” blocks always on failure or only with a hotkey?

If you don’t answer, I’d choose:
•	→ accept
•	show after 2 chars (reduces noise)
•	show hints on failure (compact), AI behind hotkey

--

Fish is great… but it doesn’t solve your actual wedge

Fish’s killer features are:
•	inline suggestions
•	good defaults
•	nice completions

But fish does not give you:
•	cross-shell consistency (bash/zsh/PowerShell everywhere)
•	an “intelligence layer” that works inside SSH sessions on servers
•	output-aware command extraction / diagnostics (it doesn’t capture output)
•	centralized policies: offline mode, redaction, provider choice
•	“bring your terminal brain with you” across machines/sessions

Your wedge is not “make terminal pretty.” It’s:

Make the command line smarter in environments you can’t control.

That’s why clai can exist even with fish.

⸻

Why people would still want clai (even if fish exists)

1) Most people can’t use fish everywhere
   •	On servers, CI, containers, and minimal environments, you get bash/sh.
   •	In corporate environments, you often can’t change the login shell.
   •	On Windows, the reality is PowerShell.
   •	Teams standardize on bash scripts; fish is intentionally not POSIX-compatible.

So even fish fans often switch back to bash/zsh for compatibility.

clai’s pitch: keep your preferred shell, and still get intelligence.

2) Fish doesn’t solve the SSH problem

This is your biggest differentiator.
•	fish doesn’t magically make your remote session smart.
•	fish doesn’t provide AI help on a remote Ubuntu box that lacks tooling.
•	fish doesn’t capture and analyze output across hops.

If you deliver:
•	“AI assist inside SSH without remote install”
that’s something fish doesn’t compete with.

3) Fish doesn’t do output-aware diagnosis/extraction

Fish suggestions are history/completion driven. clai can also:
•	detect failures and suggest next steps
•	extract runnable commands from outputs (“Run: …”)
•	maintain a local daemon cache + structured “what happened” context

Even if you don’t do full output capture initially, the architecture points there.

4) clai can be “portable muscle memory”

People don’t just want good UX; they want consistency.

If clai works similarly in:
•	zsh on Mac
•	bash in a Docker container
•	PowerShell on Windows
•	ssh to prod

…that consistency is valuable. Fish doesn’t cover that.

⸻

So are fish users your target audience?

Not as a primary wedge, but they’re not excluded.

Fish users don’t need:
•	history-based autosuggestions (they already have it)

Fish users might still want:
•	error diagnosis via AI (especially across tools)
•	consistent behavior across shells/OS
•	SSH intelligence without remote install
•	a “unified suggestion brain” (especially if they use multiple machines)

In other words: fish users aren’t your main target, but they can be enthusiastic adopters if you message it correctly.

⸻

Your real primary audience (Phase 1)

The people who will care immediately:
•	zsh users on macOS (default-ish reality)
•	bash users who won’t switch shells
•	PowerShell users who live in Windows terminals
•	infra/platform folks who do lots of SSH + kubectl + terraform
•	anyone stuck with “whatever shell the environment has”

That’s a huge market compared to “people willing to switch to fish.”

⸻

Product messaging that avoids the “but fish exists” objection

Don’t position clai as “better shell UX”.

Position it as:
•	“fish-like intelligence for any shell” (local)
•	“works in SSH sessions and on servers too” (differentiator)
•	“AI help with guardrails, privacy controls, and offline mode”
•	“no terminal emulator required”

So fish is not a competitor; it’s a proof that people like this UX.

⸻

Reality check you should do

If your v0 only ships “fish-like autosuggestions for zsh”, then yes — fish makes you look redundant.

To avoid that, your v0 must deliver at least one thing fish doesn’t:
•	cross-shell support (zsh + bash + PS)
•	safety/privacy knobs + offline mode
•	smarter context-based suggestions than fish (session + cwd + tool flow)
•	error diagnosis workflow

If you hit those, you’re not “fish but worse.” You’re “fish-like UX + portability + intelligence.”

If you want, tell me what your Phase 1 audience is (you personally + 3 colleagues? infra-heavy?), and I’ll craft a positioning paragraph and a 30-second demo script that makes the “why clai?” obvious even to fish users.
