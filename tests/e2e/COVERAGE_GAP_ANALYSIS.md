# E2E Test Coverage Gap Analysis

Comparison of Manual Test Plan (141 tests) vs Automated E2E Tests.

## Summary

| Category | Manual Tests | E2E Covered | E2E Skipped | Gap |
|----------|--------------|-------------|-------------|-----|
| Shell Integration & Capture | 17 | 7 | 0 | 10 |
| Session ID Management | 5 | 1 | 0 | 4 |
| Duration Capture | 4 | 0 | 0 | 4 |
| Interactive Shell Detection | 4 | 0 | 0 | 4 |
| Large Command Handling | 3 | 0 | 0 | 3 |
| Incognito Mode | 12 | 8 | 0 | 4 |
| Daemon & Transport | 16 | 4 | 0 | 12 |
| Database & Migrations | 4 | 0 | 0 | 4 |
| Command Normalization | 9 | 0 | 0 | 9 |
| Suggestions Engine | 18 | 12 | 2 | 4 |
| Did You Mean? | 6 | 0 | 3 | 3 |
| Semantic Slot Filling | 5 | 0 | 3 | 2 |
| FTS5 Search | 9 | 6 | 0 | 3 |
| Task Discovery | 7 | 3 | 2 | 2 |
| Keybindings | 6 | 3 | 0 | 3 |
| Ghost Text | 7 | 4 | 0 | 3 |
| API Endpoints | 9 | 6 | 2 | 1 |
| Git Context | 6 | 0 | 0 | 6 |
| Error Handling | 5 | 0 | 0 | 5 |
| Performance | 5 | 0 | 0 | 5 |
| **TOTAL** | **141** | **54** | **12** | **75** |

**Coverage: 38% automated, 8% skipped, 54% not covered**

---

## Detailed Gap Analysis

### ✅ Well Covered (>80%)

#### Suggestions CLI (SU-*)
| Manual ID | E2E Test | Status |
|-----------|----------|--------|
| SU-001 | "clai suggest text format" | ✅ |
| SU-002 | "clai suggest text format" | ✅ |
| SU-003 | "clai suggest JSON format" | ✅ |
| SU-004 | "clai suggest fzf format" | ✅ |
| SU-005 | "clai suggest with limit" | ✅ |
| SU-006 | "clai suggest with prefix filters results" | ✅ |

#### Suggestion Reasons (SR-*)
| Manual ID | E2E Test | Status |
|-----------|----------|--------|
| SR-001 | "Suggestion shows transition reason" | ✅ |
| SR-002 | "Suggestion shows freq_repo reason" | ✅ |
| SR-003 | (implicit in freq_repo test) | ✅ |
| SR-004 | "Discovery shows project_task reason" | ✅ |
| SR-005 | "Suggestion includes confidence score" | ✅ |

#### Incognito Mode (IM-*, EP-*)
| Manual ID | E2E Test | Status |
|-----------|----------|--------|
| IM-001 | "Incognito mode toggle" | ✅ |
| IM-002 | (needs: clai incognito off test) | ❌ |
| IM-003 | (needs: echo $CLAI_EPHEMERAL check) | ❌ |
| IM-004 | "CLAI_EPHEMERAL affects suggestions" | ✅ |
| EP-001 | "Incognito commands not in global suggestions" | ✅ |
| EP-002 | "Incognito commands appear in session suggestions" | ✅ |
| EP-003 | (needs: explicit FTS check) | ❌ |
| EP-004 | (needs: aggregate score check) | ❌ |

#### FTS5 Search (FT-*)
| Manual ID | E2E Test | Status |
|-----------|----------|--------|
| FT-001 | "clai search basic query" | ✅ |
| FT-002 | (implicit in basic query) | ✅ |
| FT-003 | "clai search with limit" | ✅ |
| FT-004 | "clai search with JSON output" | ✅ |
| FT-005 | "clai search with repo filter" | ✅ |
| FT-006 | "clai search empty results" | ✅ |
| FT-007 | "clai search with special characters" | ✅ |
| FT-008 | (needs: quoted phrase search) | ❌ |
| FT-009 | (needs: FTS5 unavailable test) | ❌ |

---

### ⚠️ Partially Covered (30-80%)

#### Ghost Text (GT-*)
| Manual ID | E2E Test | Status |
|-----------|----------|--------|
| GT-001 | "Ghost text suggestion appears" | ✅ |
| GT-002 | (implicit in appears test) | ✅ |
| GT-003 | (needs: no suggestion test) | ❌ |
| GT-004 | "Right arrow accepts full suggestion" | ✅ |
| GT-005 | "Alt+Right accepts next token" | ✅ |
| GT-006 | "Escape clears suggestion" | ✅ |
| GT-007 | (needs: typing clears test) | ❌ |

#### Keybindings (KB-*)
| Manual ID | E2E Test | Status |
|-----------|----------|--------|
| KB-001 | "Ctrl+Space opens suggestion picker (bash)" | ✅ |
| KB-002 | (needs: fzf integration test) | ❌ |
| KB-003 | "Ctrl+Space opens suggestion picker (zsh)" | ✅ |
| KB-004 | (needs: ZLE widget check) | ❌ |
| KB-005 | "Alt+Space opens suggestion picker (fish)" | ✅ |
| KB-006 | (needs: fish function check) | ❌ |

#### Task Discovery (TD-*, UD-*)
| Manual ID | E2E Test | Status |
|-----------|----------|--------|
| TD-001 | "Discovery finds package.json scripts" | ✅ |
| TD-002 | "Discovery finds Makefile targets" | ✅ |
| TD-003 | "Discovery shows project_task reason" | ✅ |
| UD-001 | (needs: custom discovery.yaml test) | ❌ |
| UD-002 | (needs: runner timeout test) | ❌ |
| UD-003 | (needs: output cap test) | ❌ |
| UD-004 | (needs: parse error handling test) | ❌ |

#### Daemon Management (DM-*)
| Manual ID | E2E Test | Status |
|-----------|----------|--------|
| DM-001 | "clai daemon start works" | ✅ |
| DM-002 | "clai daemon status shows state" | ✅ |
| DM-003 | "clai daemon stop graceful shutdown" | ✅ |
| DM-004 | "clai daemon restart sequence" | ✅ |
| DM-005 | (needs: double start error test) | ❌ |

---

### ❌ Not Covered (0-30%)

#### Session ID Management (SS-*)
| Manual ID | Gap Description |
|-----------|-----------------|
| SS-001 | Session ID stability across commands |
| SS-002 | New shell = new session ID |
| SS-003 | Session file creation check |
| SS-004 | Session file cleanup on exit |
| SS-005 | Fallback without daemon |

**Suggested E2E tests:**
```yaml
- name: "Session ID stable across commands"
  shells: [bash, zsh, fish]
  steps:
    - type: "echo $CLAI_SESSION_ID > /tmp/sid1"
    - type: "echo test"
    - type: "echo $CLAI_SESSION_ID > /tmp/sid2"
    - type: "diff /tmp/sid1 /tmp/sid2"
  expect:
    - screen_not_contains: "differ"
```

#### Duration Capture (DU-*)
| Manual ID | Gap Description |
|-----------|-----------------|
| DU-001 | Zsh microsecond precision |
| DU-002 | Fish millisecond precision |
| DU-003 | Bash second precision |
| DU-004 | Long command duration |

**Suggested E2E tests:**
```yaml
- name: "Duration captured for slow command"
  shells: [bash, zsh, fish]
  steps:
    - type: "sleep 1"
    - type: "clai history --session=$CLAI_SESSION_ID --json | tail -1"
  expect:
    - screen_matches: "duration.*[0-9]{3,}"  # >100ms
```

#### Interactive Shell Detection (IS-*)
| Manual ID | Gap Description |
|-----------|-----------------|
| IS-001 | Interactive shell capture works |
| IS-002 | Non-interactive (`bash -c`) skipped |
| IS-003 | Script execution skipped |
| IS-004 | Subshell handling |

**Hard to test in E2E** - requires spawning non-interactive shells.

#### Large Command Handling (LC-*)
| Manual ID | Gap Description |
|-----------|-----------------|
| LC-001 | Normal command captured |
| LC-002 | >32KB command handled |
| LC-003 | No E2BIG error |

**Suggested E2E tests:**
```yaml
- name: "Large command doesn't crash"
  shell: bash
  steps:
    - type: "echo $(python3 -c 'print(\"x\"*40000)')"
    - wait: "1s"
  expect:
    - screen_contains: "TEST>"  # Prompt returned
```

#### Signal Handling (SG-*)
| Manual ID | Gap Description |
|-----------|-----------------|
| SG-001 | SIGTERM graceful shutdown |
| SG-002 | SIGINT (Ctrl+C) shutdown |
| SG-003 | SIGHUP config reload |
| SG-004 | SIGPIPE ignored |

**Hard to test in E2E** - requires signal sending to daemon.

#### Socket & Paths (SK-*)
| Manual ID | Gap Description |
|-----------|-----------------|
| SK-001 | Socket file created |
| SK-002 | Socket permissions |
| SK-003 | Stale socket cleanup |
| SK-004 | Custom socket path |

**Suggested E2E tests:**
```yaml
- name: "Socket file exists when daemon running"
  shell: bash
  steps:
    - type: "ls -la ${XDG_RUNTIME_DIR:-/tmp}/clai/daemon.sock"
  expect:
    - screen_contains: "srw"  # Socket file
```

#### Fire-and-Forget (FF-*)
| Manual ID | Gap Description |
|-----------|-----------------|
| FF-001 | No prompt blocking |
| FF-002 | Daemon busy handling |
| FF-003 | Timeout enforcement |

**Hard to test in E2E** - requires timing measurements.

#### Graceful Degradation (GD-*)
| Manual ID | Gap Description |
|-----------|-----------------|
| GD-001 | Shell works without daemon |
| GD-002 | clai suggest without daemon |
| GD-003 | clai search without daemon |

**Suggested E2E tests:**
```yaml
- name: "Shell works without daemon"
  shell: bash
  setup:
    - "clai daemon stop"
  steps:
    - type: "echo 'test without daemon'"
    - wait: "500ms"
  expect:
    - screen_contains: "test without daemon"
    - screen_contains: "TEST>"
```

#### Database Migrations (MG-*)
| Manual ID | Gap Description |
|-----------|-----------------|
| MG-001 | Fresh DB creation |
| MG-002 | Migration locking |
| MG-003 | Version check |

**Hard to test in E2E** - requires DB manipulation.

#### Command Normalization (NM-*, CS-*)
| Manual ID | Gap Description |
|-----------|-----------------|
| NM-001 | Path → `<path>` |
| NM-002 | Number → `<num>` |
| NM-003 | SHA → `<sha>` |
| NM-004 | URL → `<url>` |
| NM-005 | Message → `<msg>` |
| CS-001-4 | Command-specific rules |

**Suggested E2E tests:**
```yaml
- name: "Normalization replaces path with slot"
  shells: [bash, zsh, fish]
  steps:
    - type: "cat /etc/passwd"
    - type: "clai suggest --format=json"
  expect:
    - screen_matches: "<path>"  # In normalized form
```

#### Git Context (GC-*)
| Manual ID | Gap Description |
|-----------|-----------------|
| GC-001 | repo_key computed |
| GC-002 | Branch detected |
| GC-003 | Non-repo handling |
| GC-004 | Context caching |
| GC-005 | Refresh on cd |
| GC-006 | TTL refresh |

**Suggested E2E tests:**
```yaml
- name: "Git repo context detected"
  shell: bash
  steps:
    - type: "cd /tmp/test-repo && git init"
    - type: "echo test"
    - type: "clai history --json | tail -1"
  expect:
    - screen_contains: "repo_key"
```

#### Scoring (SC-*)
| Manual ID | Gap Description |
|-----------|-----------------|
| SC-001 | Repo > global priority |
| SC-002 | Transition boost |
| SC-003 | Deduplication |

**Partially covered** by reason tests, but explicit scoring tests missing.

#### Error Handling (ER-*, RG-*)
| Manual ID | Gap Description |
|-----------|-----------------|
| ER-001 | Hook missing graceful |
| ER-002 | DB corruption recovery |
| ER-003 | Socket permission error |
| RG-001 | clai-hook not captured |
| RG-002 | clai suggest not captured |

**Suggested E2E tests:**
```yaml
- name: "clai commands not self-captured"
  shell: bash
  steps:
    - type: "clai suggest"
    - type: "clai search test"
    - type: "clai history --session=$CLAI_SESSION_ID"
  expect:
    - screen_not_contains: "clai suggest"
    - screen_not_contains: "clai search"
```

#### Performance (PF-*)
| Manual ID | Gap Description |
|-----------|-----------------|
| PF-001 | Suggestion < 20ms |
| PF-002 | Ingestion non-blocking |
| PF-003 | Search < 100ms |
| PF-004 | 100K commands scale |
| PF-005 | Burst ingestion |

**Hard to test in E2E** - requires timing instrumentation.

---

## Recommended Actions

### Priority 1: Easy Wins (add to existing tests)
1. Add "clai incognito off" test
2. Add session ID stability test
3. Add "typing clears ghost text" test
4. Add quoted phrase search test
5. Add recursion guard test (clai commands not captured)

### Priority 2: New Test Categories
1. Add duration capture tests (shell-specific)
2. Add git context tests
3. Add graceful degradation tests
4. Add normalization tests

### Priority 3: Infrastructure Needed
1. Signal handling tests (need daemon control)
2. Migration tests (need DB manipulation)
3. Performance tests (need timing framework)
4. Interactive detection tests (need non-interactive spawn)

---

## Coverage Metrics

```
Total Manual Tests:     141
Automated (active):      54 (38%)
Automated (skipped):     12 ( 8%)
Not Covered:             75 (53%)

By Priority:
  P1 (Easy Wins):        15 tests
  P2 (New Categories):   30 tests
  P3 (Infrastructure):   30 tests
```
