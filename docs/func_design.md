clai — Architecture & Design

Living document. Captures decisions, scope, open questions, and design intent.
Update this as clai evolves.

⸻

1. Overview & Goals

clai is a universal intelligence layer for the command line.
It augments existing shells and terminal emulators with:
•	session-based (tab-based) command history
•	smart command suggestions and completions
•	command extraction from outputs
•	error diagnosis and next-step suggestions
•	text-to-command assistance
•	(later) intelligent support inside SSH / remote sessions

Non-Goals
•	clai is not a terminal emulator
•	clai is not a shell replacement
•	clai never auto-executes commands

⸻

2. UX Principles
   •	Install once, then it just works
   •	No need to type clai ... during normal usage
   •	Intelligence must never block typing
   •	AI is assistive, not authoritative
   •	Suggestions are inserted, never executed
   •	Safe-by-default, power-user configurable

⸻

3. Phase Plan

Phase 1 — Internal / Personal Launch

Primary goal: daily-driver usability for local shells.

Scope:
•	zsh, bash, PowerShell support
•	user-mode lazy-start daemon (no system services)
•	session-based history (one shell = one session)
•	history-based suggestions and completions
•	AI-based suggestions (on-demand)
•	error diagnosis on command failure
•	text-to-command assistance
•	offline mode (no AI)
•	basic redaction for AI calls

Out of scope:
•	SSH / remote intelligence
•	provider APIs
•	local ML models
•	advanced trust & safety workflows

⸻

Phase 2 — Wider / Public Use

Scope extensions:
•	SSH and remote session intelligence (no remote install)
•	optional remote context probing (read-only)
•	provider APIs (Claude, Codex, Gemini, etc.)
•	local / offline models
•	advanced trust & safety (risk scoring, confirmations)
•	data retention controls
•	richer completion sources (help/man/spec parsing)

⸻

4. High-Level Architecture

clai consists of five main components:
1.	Shell Integration
•	zsh, bash, PowerShell hooks
•	keybindings and line-editor integration
2.	PTY Wrapper / Session Host
•	wraps interactive shells (and later ssh)
•	captures command boundaries and metadata
3.	User-Mode Lazy-Start Daemon
•	central brain
•	manages sessions, history, suggestions, AI calls
4.	Provider Adapters
•	Phase 1: local CLI providers (Claude, Codex, Gemini)
•	streaming + cancellation support
5.	Local State Store
•	SQLite
•	session + global indexes

All intelligence and storage live in the daemon.
Shells remain thin clients.

⸻

5. Daemon Design
   •	Runs in user mode only
   •	No launchd / systemd / Windows services
   •	Lazy-started on first shell interaction
   •	Communicates via local IPC (Unix socket / named pipe)
   •	Single instance per user
   •	Auto-exits after idle timeout

Daemon Responsibilities
•	session lifecycle tracking
•	history storage and indexing
•	suggestion ranking
•	AI request orchestration
•	redaction and safety policies
•	caching and throttling

⸻

6. Session & Terminal Model

Sessions
•	One interactive shell instance = one session
•	Session ID generated at shell startup
•	Typically maps 1:1 with terminal tabs/panes
•	Session metadata includes:
•	shell type
•	start time
•	cwd
•	hostname / user

Command Boundaries

Phase 1 approach:
•	Use shell hooks for reliability
•	zsh/bash: preexec / precmd
•	PowerShell: PSReadLine hooks

Captured per command:
•	raw command buffer (including multi-line)
•	working directory
•	exit code
•	execution duration

⸻

7. Output Capture Policy

Phase 1

Always capture:
•	command string
•	cwd
•	exit code
•	duration

On failure (exit_code != 0) only:
•	bounded tail of stdout
•	bounded tail of stderr

Always attempt:
•	extraction of runnable commands from output tails

Phase 2
•	optional richer capture
•	user-configurable retention and scope

⸻

8. Suggestions & Completions

Suggestion Sources (Phase 1)
•	current session history
•	directory / repo-scoped history
•	global history
•	extracted commands from prior outputs
•	AI-generated suggestions (on demand)

Ranking Heuristics
•	session > cwd > global
•	recency weighting
•	success bias (prefer successful commands)
•	tool similarity (git after git, etc.)

Completions

Phase 1:
•	augment native shell completion
•	history-based suggestions only

Later:
•	static specs
•	--help parsing
•	man page parsing

⸻

9. AI Capabilities

Phase 1

AI is used for:
•	text-to-command suggestions (up to 3)
•	error diagnosis and next-step suggestions
•	predicting likely next commands (ranking only)

AI usage rules:
•	never auto-execute
•	async only (never block typing)
•	triggered explicitly or on failure

Phase 2
•	provider APIs
•	local models
•	deeper context awareness

⸻

10. Trust & Safety

Guaranteed
•	Never auto-execute commands
•	Insert-only behavior
•	Offline mode available
•	Explicit AI triggers

Phase 1 Safety
•	basic risk tagging (destructive patterns)
•	redaction of obvious secrets
•	entropy-based token redaction
•	hard payload size caps

Phase 2 Safety
•	advanced risk scoring
•	explicit confirmation flows
•	command safety interlocks
•	configurable data retention

⸻

11. Data Model & Storage

Stored
•	full command lines
•	working directory
•	exit code + duration
•	session metadata
•	output snippets on failure
•	extracted runnable commands

Not Stored
•	full command outputs
•	raw secrets

Storage
•	SQLite database
•	session-scoped and global indexes

⸻

12. Performance Constraints

Targets:
•	Non-AI suggestions: <20–50ms
•	AI responses: async, non-blocking
•	DB writes: buffered and batched
•	Daemon CPU/memory usage capped

⸻

13. Cross-Shell Support Matrix

Phase 1
•	zsh: full support
•	bash: full support
•	PowerShell: history, suggestions, AI triggers

Later
•	fish
•	other shells (best effort)

⸻

14. Remote Sessions (Future)

Phase 2 goals:
•	local wrapper around ssh
•	no remote installation required
•	optional remote context probing (read-only)
•	session-aware history across local and remote

⸻

15. Configuration & Operability

Planned commands:
•	clai status
•	clai doctor
•	clai logs
•	clai uninstall
•	clai config ...

⸻

16. Open Questions & Risks

Open Questions
•	robust alternate-screen detection strategy
•	PowerShell stdout/stderr edge cases
•	UX for destructive-command warnings
•	default AI context size

Known Risks
•	terminal edge cases and escape sequences
•	user trust erosion if suggestions feel unsafe
•	latency from provider CLIs

⸻

17. Decision Log
	•	2026-01: clai uses a user-mode lazy-start daemon
	•	2026-01: daemon is included in Phase 1 MVP
	•	2026-01: clai never auto-executes commands
	•	2026-01: shell hooks preferred for command boundaries
