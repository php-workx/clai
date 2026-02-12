---
name: provenance
description: 'Trace knowledge artifact lineage and sources. Find orphans, stale citations. Triggers: "where did this come from", "trace this learning", "knowledge lineage".'
---

# Provenance Skill

Trace knowledge artifact lineage to sources.

## Execution Steps

Given `/provenance <artifact>`:

### Step 1: Read the Artifact

```
Tool: Read
Parameters:
  file_path: <artifact-path>
```

Look for provenance metadata:
- Source references
- Session IDs
- Dates
- Related artifacts

### Step 2: Trace Source Chain

```bash
# Check for source metadata in the file
grep -i "source\|session\|from\|extracted" <artifact-path>

# Search for related transcripts using ao
ao forge search "<artifact-name>" 2>/dev/null
```

### Step 3: Search Session Transcripts with CASS

**Use CASS to find when this artifact was discussed:**

```bash
# Extract artifact name for search
artifact_name=$(basename "<artifact-path>" .md)

# Search session transcripts
cass search "$artifact_name" --json --limit 5
```

**Parse CASS results to find:**
- Sessions where artifact was created/discussed
- Timeline of references
- Related sessions by workspace

**CASS JSON output fields:**
```json
{
  "hits": [{
    "title": "...",
    "source_path": "/path/to/session.jsonl",
    "created_at": 1766076237333,
    "score": 18.5,
    "agent": "claude_code"
  }]
}
```

### Step 4: Build Lineage Chain

```
Transcript (source of truth)
    ↓
Forge extraction (candidate)
    ↓
Human review (promotion)
    ↓
Pattern recognition (tier-up)
    ↓
Skill creation (automation)
```

### Step 5: Write Provenance Report

```markdown
# Provenance: <artifact-name>

## Current State
- **Tier:** <0-3>
- **Created:** <date>
- **Citations:** <count>

## Source Chain
1. **Origin:** <transcript or session>
   - Line/context: <where extracted>
   - Extracted: <date>

2. **Promoted:** <tier change>
   - Reason: <why promoted>
   - Date: <when>

## Session References (from CASS)
| Date | Session | Agent | Score |
|------|---------|-------|-------|
| <date> | <session-id> | <agent> | <score> |

## Related Artifacts
- <related artifact 1>
- <related artifact 2>
```

### Step 6: Report to User

Tell the user:
1. Artifact lineage
2. Original source
3. Promotion history
4. Session references (from CASS)
5. Related artifacts

## Finding Orphans

```bash
/provenance --orphans
```

Find artifacts without source tracking:
```bash
# Files without "Source:" or "Session:" metadata
for f in .agents/learnings/*.md; do
  grep -L "Source\|Session" "$f" 2>/dev/null
done
```

## Finding Stale Artifacts

```bash
/provenance --stale
```

Find artifacts where source may have changed:
```bash
# Artifacts older than their sources
find .agents/ -name "*.md" -mtime +30 2>/dev/null
```

## Key Rules

- **Every insight has a source** - trace it
- **Track promotions** - know why tier changed
- **Find orphans** - clean up untracked knowledge
- **Maintain lineage** - provenance enables trust
- **Use CASS** - find when artifacts were discussed
