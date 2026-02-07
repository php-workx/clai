---
description: Run SonarQube workflow using the sonarqube skill (actions: list or autofix).
---

## User Input

```text
$ARGUMENTS
```

Use the `sonarqube` skill for this request.

Interpret `$ARGUMENTS` as follows:
- If it includes `list`, run action=`list`.
- If it includes `autofix`, run action=`autofix`.
- If it includes neither, ask once: `Do you want autofix or list? (autofix/list)`.
- Parse optional mode (`local` or `cloud`) and severity (`blocker|high|medium|low|info`, plus aliases `critical|major|minor`).

Then execute the workflow exactly as defined by the `sonarqube` skill.
