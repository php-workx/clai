# Manual Test Plan: clai Suggestions Engine v3

This manual test plan covers all functionalities from `specs/tech_suggestions_v3.md`.

**Prerequisites:**
- clai built and installed (`make install`)
- clai daemon running (`clai daemon start`)
- All three shells available: bash (4.0+), zsh (5.0+), fish (3.0+)
- fzf installed (for picker tests)
- SQLite with FTS5 support

---

## 1. Shell Integration & Command Capture

### 1.1 Basic Command Ingestion

| ID | Test | Steps | Expected | Shell |
|----|------|-------|----------|-------|
| SI-001 | Simple command captured | 1. Run `echo "test123"`<br>2. Run `clai search "test123"` | Command appears in search results | All |
| SI-002 | Command with quotes | 1. Run `git commit -m "fix: \"quoted\" work"`<br>2. Run `clai search "quoted"` | Command captured correctly with quotes | All |
| SI-003 | Command with newlines | 1. Run multi-line command (heredoc)<br>2. Search for it | Command captured | bash, zsh |
| SI-004 | Command with pipes | 1. Run `cat file \| grep foo \| wc -l`<br>2. Search for it | Full pipeline captured | All |
| SI-005 | Command with redirects | 1. Run `echo foo > /tmp/test.txt 2>&1`<br>2. Search for it | Redirects captured | All |
| SI-006 | Unicode command | 1. Run `echo "日本語 émojis 🎉"`<br>2. Search for it | Unicode preserved (or lossy converted) | All |
| SI-007 | Exit code captured | 1. Run `false` (exit 1)<br>2. Run `true` (exit 0)<br>3. Check daemon logs/debug | Exit codes recorded correctly | All |
| SI-008 | CWD captured | 1. `cd /tmp && echo test`<br>2. Check search results | CWD shows /tmp | All |

### 1.2 Session ID Management

| ID | Test | Steps | Expected | Shell |
|----|------|-------|----------|-------|
| SS-001 | Session ID stable | 1. Note session ID<br>2. Run 10 commands<br>3. Check session ID | Same session ID throughout | All |
| SS-002 | New shell = new session | 1. Note session ID<br>2. Open new shell<br>3. Check session ID | Different session ID | All |
| SS-003 | Session file created | 1. Start new shell<br>2. Check `$XDG_RUNTIME_DIR/clai/session.$$` | File exists with session ID | All |
| SS-004 | Session file cleanup | 1. Note session file path<br>2. Exit shell<br>3. Check file | File removed on exit | All |
| SS-005 | Fallback without daemon | 1. Stop daemon<br>2. Start new shell<br>3. Check session ID | Session ID generated locally | All |

### 1.3 Duration Capture

| ID | Test | Steps | Expected | Shell |
|----|------|-------|----------|-------|
| DU-001 | Zsh microsecond precision | 1. Run `sleep 0.5`<br>2. Check duration in debug | ~500ms recorded | zsh |
| DU-002 | Fish millisecond precision | 1. Run `sleep 1`<br>2. Check `$CMD_DURATION` or debug | ~1000ms recorded | fish |
| DU-003 | Bash second precision | 1. Run `sleep 2`<br>2. Check duration | ~2000ms (seconds only) | bash |
| DU-004 | Long command duration | 1. Run `sleep 10`<br>2. Check duration | ~10000ms | All |

### 1.4 Interactive Shell Detection

| ID | Test | Steps | Expected | Shell |
|----|------|-------|----------|-------|
| IS-001 | Interactive shell works | 1. Open interactive shell<br>2. Run command<br>3. Search for it | Command captured | All |
| IS-002 | Non-interactive skipped | 1. Run `bash -c 'echo noninteractive'`<br>2. Search for it | NOT captured (hook skipped) | bash |
| IS-003 | Script skipped | 1. Create script with echo<br>2. Run script<br>3. Search for echo | NOT captured | All |
| IS-004 | Subshell skipped | 1. Run `(echo subshell)`<br>2. Search for it | May or may not capture (depends on tty) | All |

### 1.5 Large Command Handling

| ID | Test | Steps | Expected | Shell |
|----|------|-------|----------|-------|
| LC-001 | Normal command (< 32KB) | 1. Run normal command<br>2. Search for it | Captured normally | All |
| LC-002 | Large command (> 32KB) | 1. Generate 40KB echo<br>2. Run it<br>3. Search | Skipped or truncated (no crash) | All |
| LC-003 | No E2BIG error | 1. Run very large command<br>2. Check for errors | No E2BIG, graceful skip | All |

---

## 2. Incognito/Ephemeral Mode

### 2.1 Toggle Commands

| ID | Test | Steps | Expected | Shell |
|----|------|-------|----------|-------|
| IM-001 | Enable incognito | Run `clai incognito on` | "Incognito mode enabled" message | All |
| IM-002 | Disable incognito | Run `clai incognito off` | "Incognito mode disabled" message | All |
| IM-003 | Status check | 1. Enable incognito<br>2. Check `echo $CLAI_EPHEMERAL` | Returns 1 | All |
| IM-004 | Env var method | 1. `export CLAI_EPHEMERAL=1`<br>2. Run command<br>3. Search | NOT in persistent search | All |

### 2.2 Ephemeral Behavior

| ID | Test | Steps | Expected | Shell |
|----|------|-------|----------|-------|
| EP-001 | Not persisted | 1. Enable incognito<br>2. Run unique command<br>3. Disable incognito<br>4. Search | NOT found in history | All |
| EP-002 | Session suggestions work | 1. Enable incognito<br>2. Run `git status` 3x<br>3. Get suggestions | git status appears (in-memory) | All |
| EP-003 | No FTS indexing | 1. Enable incognito<br>2. Run command<br>3. Search via FTS | NOT indexed | All |
| EP-004 | No aggregate updates | 1. Enable incognito<br>2. Run command 100x<br>3. Disable<br>4. Check scores | Score not increased | All |

### 2.3 CLAI_NO_RECORD Mode

| ID | Test | Steps | Expected | Shell |
|----|------|-------|----------|-------|
| NR-001 | Skip ingestion | 1. `export CLAI_NO_RECORD=1`<br>2. Run command<br>3. Search | NOT captured at all | All |
| NR-002 | No daemon call | 1. Set CLAI_NO_RECORD<br>2. Stop daemon<br>3. Run command | No errors (hook skipped) | All |

---

## 3. Daemon & Transport

### 3.1 Daemon Lifecycle

| ID | Test | Steps | Expected | Shell |
|----|------|-------|----------|-------|
| DM-001 | Start daemon | `clai daemon start` | Daemon starts, shows PID | - |
| DM-002 | Check status | `clai daemon status` | Shows running, PID, uptime | - |
| DM-003 | Stop daemon | `clai daemon stop` | Graceful shutdown | - |
| DM-004 | Restart daemon | `clai daemon restart` | Stop + start | - |
| DM-005 | Double start | 1. Start daemon<br>2. Start again | Error: already running | - |

### 3.2 Signal Handling

| ID | Test | Steps | Expected | Shell |
|----|------|-------|----------|-------|
| SG-001 | SIGTERM shutdown | 1. Get daemon PID<br>2. `kill -TERM <pid>` | Graceful shutdown, exit 0 | - |
| SG-002 | SIGINT shutdown | 1. Start daemon foreground<br>2. Ctrl+C | Graceful shutdown | - |
| SG-003 | SIGHUP reload | 1. Modify discovery.yaml<br>2. `kill -HUP <pid>` | Config reloaded (check logs) | - |
| SG-004 | SIGPIPE ignored | 1. Connect to socket<br>2. Disconnect mid-write | No daemon crash | - |

### 3.3 Socket & Paths

| ID | Test | Steps | Expected | Shell |
|----|------|-------|----------|-------|
| SK-001 | Socket created | Start daemon, check path | Socket file exists | - |
| SK-002 | Socket permissions | Check socket file perms | 0700 directory | - |
| SK-003 | Stale socket cleanup | 1. Kill daemon -9<br>2. Start daemon | Cleans up stale socket | - |
| SK-004 | Custom socket path | `CLAI_SOCKET_PATH=/tmp/test.sock clai daemon start` | Uses custom path | - |

### 3.4 Fire-and-Forget Ingestion

| ID | Test | Steps | Expected | Shell |
|----|------|-------|----------|-------|
| FF-001 | No prompt blocking | 1. Stop daemon<br>2. Run commands<br>3. Measure prompt delay | < 20ms delay | All |
| FF-002 | Daemon busy handling | 1. Flood daemon with events<br>2. Run command | No prompt freeze | All |
| FF-003 | Timeout enforcement | 1. Simulate slow daemon<br>2. Run command | Event dropped, no hang | All |

### 3.5 Graceful Degradation

| ID | Test | Steps | Expected | Shell |
|----|------|-------|----------|-------|
| GD-001 | Shell works without daemon | 1. Stop daemon<br>2. Use shell normally | Shell functions normally | All |
| GD-002 | clai suggest without daemon | 1. Stop daemon<br>2. Run `clai suggest` | Error message, no crash | All |
| GD-003 | clai search without daemon | 1. Stop daemon<br>2. Run `clai search foo` | Error message, no crash | All |

---

## 4. Database & Migrations

### 4.1 Schema Migrations

| ID | Test | Steps | Expected | Shell |
|----|------|-------|----------|-------|
| MG-001 | Fresh DB creation | 1. Delete DB<br>2. Start daemon | Creates tables, runs migrations | - |
| MG-002 | Migration lock | 1. Start two daemons simultaneously | Only one migrates | - |
| MG-003 | Version check | 1. Manually set future version<br>2. Start daemon | Refuses to run | - |

### 4.2 Batching

| ID | Test | Steps | Expected | Shell |
|----|------|-------|----------|-------|
| BT-001 | Burst handling | 1. Run 100 commands rapidly<br>2. Check DB | Single transaction, no lock errors | All |
| BT-002 | Batch timing | 1. Run command<br>2. Wait 50ms<br>3. Check DB | Data persisted | All |

---

## 5. Command Normalization

### 5.1 Basic Normalization

| ID | Test | Steps | Expected | Shell |
|----|------|-------|----------|-------|
| NM-001 | Path → `<path>` | 1. Run `cat /etc/passwd`<br>2. Check cmd_norm | `cat <path>` | All |
| NM-002 | Number → `<num>` | 1. Run `head -n 100`<br>2. Check cmd_norm | `head -n <num>` | All |
| NM-003 | SHA → `<sha>` | 1. Run `git show abc1234`<br>2. Check cmd_norm | `git show <sha>` | All |
| NM-004 | URL → `<url>` | 1. Run `curl https://example.com`<br>2. Check cmd_norm | `curl <url>` | All |
| NM-005 | Message → `<msg>` | 1. Run `git commit -m "test"`<br>2. Check cmd_norm | `git commit -m <msg>` | All |

### 5.2 Command-Specific Rules

| ID | Test | Steps | Expected | Shell |
|----|------|-------|----------|-------|
| CS-001 | git commit -m | `git commit -m "any message"` | `git commit -m <msg>` | All |
| CS-002 | git checkout -b | `git checkout -b feature/x` | `git checkout -b <branch>` | All |
| CS-003 | kubectl -n | `kubectl get pods -n production` | `kubectl get pods -n <ns>` | All |
| CS-004 | npm run | `npm run custom-script` | `npm run <script>` | All |

---

## 6. Suggestions Engine

### 6.1 Basic Suggestions

| ID | Test | Steps | Expected | Shell |
|----|------|-------|----------|-------|
| SU-001 | Get suggestions | Run `clai suggest` | Returns 1-3 suggestions | All |
| SU-002 | Text format | `clai suggest --format=text` | Numbered list with reasons | All |
| SU-003 | JSON format | `clai suggest --format=json` | Valid JSON with suggestions array | All |
| SU-004 | fzf format | `clai suggest --format=fzf` | Plain commands, one per line | All |
| SU-005 | Limit flag | `clai suggest --limit=1` | Returns exactly 1 suggestion | All |
| SU-006 | Prefix filter | `clai suggest git` | Only git commands | All |

### 6.2 Suggestion Reasons

| ID | Test | Steps | Expected | Shell |
|----|------|-------|----------|-------|
| SR-001 | Transition reason | 1. Run `git add .` → `git commit`<br>2. Run `git add .`<br>3. Get suggestion | Shows "transition" reason | All |
| SR-002 | freq_repo reason | 1. Run `make build` 10x<br>2. Get suggestion | Shows "freq_repo" reason | All |
| SR-003 | freq_global reason | 1. Run `ls` many times globally<br>2. Get suggestion | Shows "freq_global" reason | All |
| SR-004 | project_task reason | 1. In npm project<br>2. Get suggestion | Shows "project_task" for npm scripts | All |
| SR-005 | Confidence score | Check JSON output | Has confidence field (0.0-1.0) | All |

### 6.3 Pre-computation & Caching

| ID | Test | Steps | Expected | Shell |
|----|------|-------|----------|-------|
| PC-001 | Cache hit | 1. Run command<br>2. Immediately get suggestion | Returns in < 10ms | All |
| PC-002 | Cache in context | Check JSON response context | Shows `cache: hit` or `miss` | All |
| PC-003 | Cache TTL | 1. Get suggestion<br>2. Wait 35s<br>3. Get suggestion | Second may be cache miss | All |
| PC-004 | Cache invalidation | 1. Run command<br>2. Get suggestion (A)<br>3. Run different command<br>4. Get suggestion (B) | A ≠ B (updated) | All |

### 6.4 Scoring

| ID | Test | Steps | Expected | Shell |
|----|------|-------|----------|-------|
| SC-001 | Repo > global | 1. Run cmd only in repo 10x<br>2. Run different cmd globally 10x<br>3. In repo, get suggestion | Repo command ranked higher | All |
| SC-002 | Transition boost | 1. Establish A→B pattern<br>2. Run A<br>3. Get suggestion | B appears at top | All |
| SC-003 | Deduplication | Run same cmd in different contexts | Only one entry in suggestions | All |

---

## 7. Did You Mean? (Typo Correction)

### 7.1 Basic Typo Correction

| ID | Test | Steps | Expected | Shell |
|----|------|-------|----------|-------|
| DY-001 | Exit 127 trigger | 1. Run `gti status` (typo)<br>2. Get suggestion | `git status` suggested | All |
| DY-002 | Similarity threshold | 1. Run `git statsu` (close typo)<br>2. Get suggestion | `git status` suggested | All |
| DY-003 | No match for gibberish | 1. Run `xyzabc123`<br>2. Get suggestion | No typo correction | All |
| DY-004 | High-freq preferred | 1. Run common cmd 100x<br>2. Typo that cmd | Common cmd suggested over rare | All |

### 7.2 Edge Cases

| ID | Test | Steps | Expected | Shell |
|----|------|-------|----------|-------|
| DY-005 | Non-127 exit | 1. Run cmd that fails with exit 1<br>2. Get suggestion | Standard suggestions (not typo) | All |
| DY-006 | Empty command | 1. Press Enter (empty)<br>2. Get suggestion | No crash, normal suggestions | All |

---

## 8. Semantic Slot Filling

### 8.1 Slot Value Learning

| ID | Test | Steps | Expected | Shell |
|----|------|-------|----------|-------|
| SF-001 | Value histogram | 1. Run `kubectl -n production` 10x<br>2. Run `kubectl -n staging` 2x<br>3. Suggest kubectl | Suggests with `-n production` | All |
| SF-002 | Repo scope | 1. In repo A: run with value X<br>2. In repo B: run with value Y<br>3. In repo A: suggest | Uses X, not Y | All |
| SF-003 | Decay over time | 1. Run with value A 10x (old)<br>2. Run with value B 5x (recent)<br>3. Suggest | B may outrank A | All |

### 8.2 Confidence-Based Filling

| ID | Test | Steps | Expected | Shell |
|----|------|-------|----------|-------|
| SF-004 | High confidence fill | 1. Use same value 10x, other 1x<br>2. Suggest | Fills with common value | All |
| SF-005 | Low confidence no fill | 1. Use value A 3x, B 2x<br>2. Suggest | May not fill (close counts) | All |

---

## 9. FTS5 History Search

### 9.1 Basic Search

| ID | Test | Steps | Expected | Shell |
|----|------|-------|----------|-------|
| FT-001 | Simple query | 1. Run `echo unique_term_123`<br>2. `clai search unique_term` | Finds the command | All |
| FT-002 | Multiple results | 1. Run 5 different docker commands<br>2. `clai search docker` | Returns multiple matches | All |
| FT-003 | Limit flag | `clai search docker --limit=2` | Returns exactly 2 results | All |
| FT-004 | JSON output | `clai search docker --json` | Valid JSON with results array | All |
| FT-005 | Repo filter | `clai search --repo "command"` | Only results from current repo | All |

### 9.2 Edge Cases

| ID | Test | Steps | Expected | Shell |
|----|------|-------|----------|-------|
| FT-006 | No results | `clai search "xyznonexistent98765"` | Empty results, no error | All |
| FT-007 | Special characters | `clai search "test-with-dashes"` | Handles correctly | All |
| FT-008 | Quoted query | `clai search '"exact phrase"'` | Phrase search | All |
| FT-009 | FTS5 unavailable | 1. Use SQLite without FTS5<br>2. Search | Graceful error message | - |

---

## 10. Project Task Discovery

### 10.1 Built-in Discovery

| ID | Test | Steps | Expected | Shell |
|----|------|-------|----------|-------|
| TD-001 | package.json scripts | 1. Create package.json with scripts<br>2. Get suggestion | npm scripts suggested | All |
| TD-002 | Makefile targets | 1. Create Makefile with targets<br>2. Get suggestion | make targets suggested | All |
| TD-003 | Task reason | Check suggestion JSON | Shows "project_task" reason | All |

### 10.2 User-Defined Discovery

| ID | Test | Steps | Expected | Shell |
|----|------|-------|----------|-------|
| UD-001 | Custom discovery.yaml | 1. Add Justfile discovery config<br>2. Create Justfile<br>3. Get suggestion | Just recipes suggested | All |
| UD-002 | Runner timeout | 1. Configure slow runner (>500ms)<br>2. Trigger discovery | Times out, logs warning | - |
| UD-003 | Output cap | 1. Configure runner with large output<br>2. Trigger discovery | Capped, no memory issue | - |
| UD-004 | Parse error handling | 1. Configure parser for bad output<br>2. Trigger discovery | Logs error, continues | - |

### 10.3 Debug Endpoints

| ID | Test | Steps | Expected | Shell |
|----|------|-------|----------|-------|
| DE-001 | /debug/tasks | `curl localhost:8765/debug/tasks` | Shows discovered tasks | - |
| DE-002 | /debug/discovery-errors | `curl localhost:8765/debug/discovery-errors` | Shows recent failures | - |

---

## 11. Widget & Keybinding Integration

### 11.1 Bash Integration

| ID | Test | Steps | Expected | Shell |
|----|------|-------|----------|-------|
| KB-001 | Ctrl+Space picker | 1. Press Ctrl+Space<br>2. Select suggestion | Command inserted on line | bash |
| KB-002 | fzf integration | 1. Press keybind<br>2. fzf opens | Can filter and select | bash |

### 11.2 Zsh Integration

| ID | Test | Steps | Expected | Shell |
|----|------|-------|----------|-------|
| KB-003 | Ctrl+Space widget | 1. Press Ctrl+Space<br>2. Select suggestion | Command inserted | zsh |
| KB-004 | ZLE widget | Check widget registered | `_clai_suggest_widget` exists | zsh |

### 11.3 Fish Integration

| ID | Test | Steps | Expected | Shell |
|----|------|-------|----------|-------|
| KB-005 | Alt+Space picker | 1. Press Alt+Space<br>2. Select suggestion | Command inserted | fish |
| KB-006 | Fish function | Check function exists | `_clai_suggest` defined | fish |

---

## 12. Ghost Text / Inline Suggestions

### 12.1 Display

| ID | Test | Steps | Expected | Shell |
|----|------|-------|----------|-------|
| GT-001 | Ghost text appears | 1. Type `git st`<br>2. Wait 300ms | "atus" shown as ghost text | All |
| GT-002 | Correct completion | 1. Type prefix of known command<br>2. See suggestion | Matches history | All |
| GT-003 | No suggestion | 1. Type unknown prefix<br>2. Wait | No ghost text | All |

### 12.2 Acceptance

| ID | Test | Steps | Expected | Shell |
|----|------|-------|----------|-------|
| GT-004 | Right arrow accepts | 1. See ghost text<br>2. Press Right | Full suggestion accepted | All |
| GT-005 | Alt+Right token | 1. See ghost text<br>2. Press Alt+Right | One token accepted | All |
| GT-006 | Escape clears | 1. See ghost text<br>2. Press Escape | Ghost text cleared | All |
| GT-007 | Typing clears | 1. See ghost text<br>2. Type different char | Ghost text cleared | All |

---

## 13. API Endpoints

### 13.1 /suggest Endpoint

| ID | Test | Steps | Expected | Shell |
|----|------|-------|----------|-------|
| AP-001 | Basic request | `curl localhost:8765/suggest` | JSON response with suggestions | - |
| AP-002 | Cache context | Check response | Includes cache hit/miss | - |
| AP-003 | Last cmd context | Check response | Includes last_cmd_norm | - |

### 13.2 /search Endpoint

| ID | Test | Steps | Expected | Shell |
|----|------|-------|----------|-------|
| AP-004 | Basic search | `curl -X POST -d '{"query":"git"}' localhost:8765/search` | Results array | - |
| AP-005 | Repo filter | Include repo_key in request | Filtered results | - |
| AP-006 | Truncated flag | Large result set | `truncated: true` if applicable | - |

### 13.3 Debug Endpoints

| ID | Test | Steps | Expected | Shell |
|----|------|-------|----------|-------|
| AP-007 | /debug/scores | `curl localhost:8765/debug/scores` | Command scores | - |
| AP-008 | /debug/transitions | `curl localhost:8765/debug/transitions` | Markov bigrams | - |
| AP-009 | /debug/cache | `curl localhost:8765/debug/cache` | Cache state | - |

---

## 14. Git Context

### 14.1 Repo Detection

| ID | Test | Steps | Expected | Shell |
|----|------|-------|----------|-------|
| GC-001 | repo_key computed | 1. In git repo, run command<br>2. Check event | Has repo_key | All |
| GC-002 | Branch detected | Check event or debug | Has branch name | All |
| GC-003 | Non-repo handling | 1. In /tmp (non-repo)<br>2. Run command | No repo_key, still works | All |

### 14.2 Context Caching

| ID | Test | Steps | Expected | Shell |
|----|------|-------|----------|-------|
| GC-004 | Cached on cwd | 1. Run 10 commands same dir<br>2. Check daemon performance | No git calls per command | All |
| GC-005 | Refresh on cd | 1. cd to different repo<br>2. Run command | New repo_key | All |
| GC-006 | TTL refresh | Wait > 3s, run command | Context refreshed | All |

---

## 15. Error Handling & Edge Cases

### 15.1 Resilience

| ID | Test | Steps | Expected | Shell |
|----|------|-------|----------|-------|
| ER-001 | Hook missing | 1. Remove clai-hook from PATH<br>2. Use shell | Shell works, hook skipped | All |
| ER-002 | DB corruption | 1. Corrupt SQLite file<br>2. Start daemon | Error message, recoverable | - |
| ER-003 | Socket permission | 1. chmod 000 socket dir<br>2. Start daemon | Clear error message | - |

### 15.2 Recursion Guards

| ID | Test | Steps | Expected | Shell |
|----|------|-------|----------|-------|
| RG-001 | clai-hook not captured | Run `clai-hook` commands | NOT in history | All |
| RG-002 | clai suggest not captured | Run `clai suggest` | NOT in history | All |

---

## 16. Performance

### 16.1 Latency

| ID | Test | Steps | Expected | Shell |
|----|------|-------|----------|-------|
| PF-001 | Suggestion < 20ms | Time `clai suggest` | < 20ms with cache | All |
| PF-002 | Ingestion non-blocking | Time prompt return | < 5ms overhead | All |
| PF-003 | Search < 100ms | Time `clai search` | < 100ms for 10K history | All |

### 16.2 Scale

| ID | Test | Steps | Expected | Shell |
|----|------|-------|----------|-------|
| PF-004 | 100K commands | Load 100K command history | Still responsive | - |
| PF-005 | Burst ingestion | Run 100 commands in loop | No dropped events, no crash | All |

---

## Test Execution Checklist

### Pre-test Setup
- [ ] Build latest clai (`make build`)
- [ ] Install clai (`make install`)
- [ ] Start daemon (`clai daemon start`)
- [ ] Verify daemon running (`clai daemon status`)
- [ ] Clear test history if needed

### Shell Matrix
Run critical tests in all shells:

| Test Category | bash | zsh | fish |
|--------------|------|-----|------|
| Basic ingestion | [ ] | [ ] | [ ] |
| Incognito mode | [ ] | [ ] | [ ] |
| Suggestions | [ ] | [ ] | [ ] |
| Ghost text | [ ] | [ ] | [ ] |
| Search | [ ] | [ ] | [ ] |
| Keybindings | [ ] | [ ] | [ ] |

### Post-test Cleanup
- [ ] Stop daemon (`clai daemon stop`)
- [ ] Review daemon logs for errors
- [ ] Document any failures

---

## Version

- **Spec Version:** tech_suggestions_v3.md
- **Test Plan Version:** 1.0
- **Created:** 2026-02-06
- **Author:** clai team
