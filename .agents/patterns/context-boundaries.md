# Pattern: Fresh Context Per Phase

**Tier:** 2 (Pattern)
**Source:** AgentOps multi-epic post-mortem

## Problem

Long sessions accumulate errors. Context pollution causes drift.

## Solution

Fresh Claude session for each RPI phase:
- /research → new session
- /plan → new session
- /implement → new session
- /post-mortem → new session

## The 40% Rule

| Context % | Success Rate |
|-----------|--------------|
| <40%      | 98%          |
| 40-60%    | ~50%         |
| >60%      | ~1%          |

At 35% context, checkpoint and consider new session.
