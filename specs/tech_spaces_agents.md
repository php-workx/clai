# Spaces + Coding Agents — Improving Command Calling, Performance, and Reliability

**Purpose:** Capture our discussion on using Spaces as structured memory for coding agents (Codex CLI, Claude Code, Cursor, etc.) to reduce repeated command mistakes and improve execution reliability.

This document is intended as a checkpoint so we can resume later.

---

## 1. Problem Statement

Coding agents repeatedly make the same shell mistakes:

- Calling custom scripts with non-existent flags
- Using wrong `npm run` script names
- Using invalid `make` targets
- Guessing tool invocations incorrectly across repositories

These mistakes waste time and degrade trust.

**Goal:** Make agents more reliable and efficient by providing scoped, durable command memory and pre-execution validation.

---

## 2. Core Idea

### 2.1 Spaces as "Knowledge Contexts" for Agents

Spaces already provide:

- A named scope (`repo:foo`, `k8s-prod`, `deploy-bar`)
- A filtered command history with context
- Fingerprinting (dominant tools) and stoplist noise filtering

For agents, Spaces become:

- Structured working memory
- The source of "known-good commands" and "known-bad patterns"

### 2.2 Beads-like Memory, but for Shell Commands

Inspired by Beads (structured agent memory), apply the same principle to bash/script calling:

- Capture failures as structured "facts"
- Store durable, scoped memory by Space/repo
- Feed the memory back at the moment of action

**Reference:** Steve Yegge, "Introducing Beads: a coding agent memory system"

---

## 3. What to Store (Command Knowledge Base / CKB)

Store structured entries, not prose. Each entry should include:

| Field | Description |
|-------|-------------|
| `scope` | `space_id` (optionally `repo_root`) |
| `tool_root` | `make`, `npm`, `pnpm`, `poetry`, `./scripts/foo` |
| `intent` | (optional) build/test/lint/deploy |
| `bad_invocation_pattern` | normalized argv / signature |
| `error_signature` | small regex/hash derived from stderr tail |
| `fix_suggestions` | 1–3 corrected commands |
| `evidence` | `last_seen`, `count`, example stderr tails (bounded) |
| `verification` | whether a fix has succeeded later |

**Principle:** Preserve raw execution history for audit; dedupe only for presentation.

---

## 4. High-Value Error Buckets (Initial Scope)

Start with deterministic classification (fast, reliable), then add AI.

### 4.1 Unknown Flag / Option

- `"unknown option"`, `"flag provided but not defined"`

### 4.2 Missing Make Target / npm Script

- `make: *** No rule to make target ...`
- `npm ERR! missing script: ...`

### 4.3 Unknown Subcommand

- `"unknown command"`, `"invalid choice"`

For each bucket, store a memory entry and propose corrected invocations.

---

## 5. How Agents Use This (Behavior Loop)

### 5.1 Pre-execution Validation

Before running a command, the agent should call:

```
clai.validate(command, cwd, space)
```

Returns:

- `allow` | `warn` | `deny`
- Suggested fixes
- Short reason
- Confidence band

### 5.2 Execution via clai

Preferred execution path:

```
clai.run(command, cwd, space)
```

`clai.run` performs:

1. Validation
2. Optional gating (deny/warn)
3. Execution
4. Recording of outcomes (including bounded stderr tail)
5. CKB updates on failure

### 5.3 Post-failure Learning

After a failure:

1. Classify error bucket
2. Update CKB entry
3. Optionally run one discovery command (e.g., list make targets) and cache results

---

## 6. Ensuring the Agent Actually Uses clai

A key concern: a simple instruction in `AGENTS.md` is insufficient.

### 6.1 Enforcement Mechanisms

**Hard enforcement (best):**

- Disable the agent's native shell tool
- Allow only `clai.run` (via MCP) for command execution

**Hook-based enforcement:**

- Intercept attempted shell tool usage
- Block or rewrite unless validated

**Friction-based enforcement (IDE agents):**

- Require approval for all terminal commands
- Allowlist only `clai.run` as the fast path

**Universal fallback:**

- PATH shims/wrappers for common tools (`make`, `npm`, scripts)
- Validate before exec

### 6.2 Target Agents

- Codex CLI
- Claude Code
- Cursor and similar IDE agents

**Assumption:** MCP is broadly supported by modern agent ecosystems.

---

## 7. Spaces-Driven Performance Improvements

Spaces reduce thrash by providing:

- Correct command vocabulary per repo/context
- Ranked "next likely commands" without guessing
- Top successful verification sequences per Space
- Rapid lookup via picker/search

**Additions needed for agents:**

A Space Summary API returning:

- Fingerprint roots
- Top commands (unique + counts + last seen)
- Last failures
- Suggested verification sequence

This turns Spaces into machine-consumable, low-latency context.

---

## 8. Safety & Trust Posture

**Non-negotiables:**

- Never auto-execute suggested fixes
- Apply risk tagging to agent-proposed commands
- Offline mode disables provider calls
- Bounded capture of stderr tails

---

## 9. Proposed Interfaces (MCP Tools)

Minimum MCP tool set:

### 9.1 `clai.validate`

- **Input:** `command`, `cwd`, `space`
- **Output:** `allow`/`warn`/`deny` + fixes + rationale

### 9.2 `clai.run`

- Validates then executes
- Records results

### 9.3 `clai.space_summary`

- Returns space-level context to guide command choice

---

## 10. Open Questions

- Exact schema for CKB storage (SQLite tables vs files)
- How to discover allowed targets/scripts safely (`make`/`npm`)
- How to handle multi-repo/monorepo spaces
- How to enforce in each agent (Codex vs Claude vs Cursor)
- UX: when to prompt human vs block agent

---

## 11. Next Steps (When We Resume)

1. Decide on first target agent (Codex CLI vs Claude Code vs Cursor)
2. Draft MCP tool schemas and expected JSON contracts
3. Define initial deterministic error-signature library
4. Implement minimal CKB persistence tied to `space_id`
5. Add enforcement strategy for the chosen agent
