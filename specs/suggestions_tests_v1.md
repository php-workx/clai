# Suggestions Engine V1 Test Plan (`suggestions_tests_v1.md`)

Source of truth:
- `/Users/runger/.claude-worktrees/clai/spec-reviews/specs/suggestions_tech_v1.md`

Scope:
- Covers all non-excluded requirements in `suggestions_tech_v1.md`.
- Explicitly excludes workflow feature-family runtime behavior (per tech spec Section 1.3 and Section 24).

## 1) Test Strategy and Category Boundaries

### 1.1 Category Definitions
- Unit: deterministic logic, normalization, scoring math, API validation, schema/SQL behavior with in-memory or temp DB, no shell process orchestration.
- Integration: daemon/runtime module interaction, real SQLite files, IPC clients, command lifecycle ordering, retention/maintenance behavior.
- Expect: interactive shell behavior with `go-expect` (`bash/zsh/fish`), prompt safety, hooks, session lifecycle, human-facing shell contract.
- Docker: expect tests on distro matrix (alpine/ubuntu/debian/fedora) to catch shell/toolchain/environment variance.
- End-to-end: full scenario-level validation via `tests/e2e/*.yaml` runner with shell UI behavior and suggestion flows.

### 1.2 Coverage vs Runtime Optimization Rules
- Put all parsing/scoring/validation invariants in Unit first.
- Use Integration for cross-component correctness and schema/IPC contracts.
- Keep Expect focused on shell contract and non-blocking guarantees, not feature math.
- Use Docker only for cross-distro confidence; do not duplicate Unit/Integration logic there.
- Use E2E for end-user flow confidence, smoke + critical regressions only on PR; full suite nightly.

## 2) Execution Lanes (Fast -> Full)

### Lane A: Dev Fast (target <= 6 min)
- Command:
```bash
go test ./internal/suggestions/normalize ./internal/suggestions/score ./internal/suggestions/search ./internal/suggestions/alias ./internal/suggestions/dismissal ./internal/suggestions/learning ./internal/suggestions/recovery ./internal/suggestions/projecttype ./internal/suggestions/typo ./internal/config ./internal/ipc
```
- Purpose: immediate feedback on core deterministic logic.

### Lane B: PR Required (target <= 20 min)
- Command:
```bash
go test ./internal/suggestions/... ./internal/daemon/... ./internal/ipc/... ./internal/config/... ./tests/integration/...
```
- Plus shell contract smoke:
```bash
go test ./tests/expect/... -run "Test.*(Hook|Session|NoDaemon|Performance_.*IntegrationOverhead)" -v
```
- Purpose: block regressions on runtime behavior and API/DB contracts.

### Lane C: Pre-merge Full Unix Host (target <= 35 min)
- Command:
```bash
make test
make test-interactive
```
- Purpose: comprehensive host-level validation including all expect tests.

### Lane D: Nightly / Release (target <= 90 min)
- Command:
```bash
make test-all
```
- Plus E2E suggestions scenarios:
```bash
# Start server
make test-server SHELL=bash
# Run tests with e2e harness against tests/e2e/suggestions-tests.yaml
```
- Purpose: cross-distro and high-fidelity end-to-end confidence.

## 3) Feature-to-Category Traceability Matrix

| Tech Spec Area | Primary Category | Secondary Category | Why |
| --- | --- | --- | --- |
| Shell contract, interactivity, prompt safety | Expect | Docker, E2E | Requires real shell behavior and prompt semantics |
| Ingestion path, fire-and-forget timeouts, ordering | Integration | Expect | Needs daemon+IPC with bounded waits; verify no prompt block in shell |
| IPC path, transport, socket ownership, health | Integration | E2E | Runtime contract and endpoint behavior |
| Startup lock/migration/stale socket | Integration | Docker | Process lifecycle and filesystem semantics |
| Schema/write-path/retention/maintenance | Unit | Integration | SQL and transaction rules + scheduler behavior |
| Normalization/alias/pipeline/project-type | Unit | Integration | Deterministic transform rules + runtime propagation |
| Retrieval priority/ranking/confidence/reasons | Unit | Integration | Deterministic scoring and source ordering |
| Feedback/learning/suppression | Unit | Integration | State-machine + profile updates + persistence |
| Task discovery and playbook (`after`, `after_failure`) | Unit | Integration, E2E | Parser/validation + runtime trigger behavior |
| Search (`fts/prefix/describe/auto`) and tags | Unit | Integration, E2E | Query semantics + backend fallback |
| Caching and latency budgets | Unit | Integration, E2E | cache behavior + bounded runtime performance |
| Security/privacy/incognito | Unit | Integration, Expect | mode semantics + runtime isolation + shell fail-open |
| API validation/error model | Unit | Integration | request/response contract correctness |
| Observability/doctor/config | Unit | Integration | key visibility + reload/fallback rules |

## 4) Detailed Test Cases

## 4.1 Unit Tests

| ID | Area | Target Files | Scenario | Assertions |
| --- | --- | --- | --- | --- |
| UT-VAL-001 | Request validation | `internal/suggestions/api/validate_test.go` | `SuggestRequest` required fields and bounds | Reject invalid `session_id`, clamp `limit`, clamp `cursor_pos` |
| UT-VAL-002 | Request validation | `internal/suggestions/api/validate_test.go` | `last_cmd_*` field validation | Clamp invalid `last_cmd_ts_ms`, accept missing `last_event_seq` as default |
| UT-VAL-003 | Feedback validation | `internal/suggestions/api/validate_test.go` | `RecordFeedbackRequest` fields | Reject empty `suggested_text`, clamp `latency_ms` |
| UT-VAL-004 | Search validation | `internal/suggestions/api/validate_test.go` | `mode/scope/query` rules | Reject unknown mode/scope, truncate query |
| UT-CON-001 | Config validation | `internal/config/suggestions_config_test.go` | timeout/size/range checks | Invalid key falls back with warning |
| UT-CON-002 | Config reload | `internal/config/suggestions_config_test.go` | reload invalid value | Previous valid value is preserved |
| UT-ING-001 | UTF-8 repair | `internal/suggestions/ingest/utf8_test.go` | malformed bytes | replacement-char behavior consistent |
| UT-ING-002 | event size bounds | `internal/suggestions/normalize/eventsize_test.go` | >16384 bytes cmd | truncation and marker behavior |
| UT-NRM-001 | deterministic normalization | `internal/suggestions/normalize/normalize_test.go` | repeated same inputs | identical `cmd_norm` and template id |
| UT-NRM-002 | command-specific patterns | `internal/suggestions/normalize/normalize_test.go` | git/npm/go/pytest forms | slots replace expected tokens |
| UT-NRM-003 | pipeline splitting | `internal/suggestions/normalize/pipeline_test.go` | quoted operators/subshell/heredoc | split only unquoted allowed operators |
| UT-NRM-004 | alias pre-normalization | `internal/suggestions/normalize/alias_test.go` | alias expansion depth/cycle | bounded expansion, cycle-safe |
| UT-ALS-001 | alias capture parsing | `internal/suggestions/alias/capture_test.go` | bash/zsh/fish alias forms | parse maps correctly |
| UT-ALS-002 | alias manager rendering | `internal/suggestions/alias/manager_test.go` | reverse render preferred alias | stable longest-prefix mapping |
| UT-PRJ-001 | project marker detection | `internal/suggestions/projecttype/detector_test.go` | marker scanning + `.clai/project.yaml` override | override wins over auto-detect |
| UT-GIT-001 | repo key and canonicalization | `internal/suggestions/git/context_test.go` | remote/no-remote, symlink path | stable `repo_key` formula behavior |
| UT-SCR-001 | transition/frequency updates | `internal/suggestions/score/transition_test.go`, `internal/suggestions/score/frequency_test.go` | score math | decays/log scaling inputs correct |
| UT-SCR-002 | pipeline contribution | `internal/suggestions/score/pipeline_test.go` | pipeline transition signal | contributes only when context matches |
| UT-SCR-003 | slot correlation | `internal/suggestions/score/slot_test.go` | dependent slots | reject low-confidence mixed tuples |
| UT-RNK-001 | scorer determinism | `internal/suggestions/suggest/scorer_test.go`, `internal/suggestions/suggest/property_test.go` | same state => same top-k order | stable tie-break ordering |
| UT-RNK-002 | confidence gate | `internal/suggestions/suggest/scorer_test.go` | include_low_confidence false/true | hidden vs returned low-confidence items |
| UT-EXP-001 | explainability | `internal/suggestions/explain/explain_test.go` | reasons sorted by contribution | max reasons + threshold behavior |
| UT-RCV-001 | failure recovery | `internal/suggestions/recovery/engine_test.go` | failure class mapping + score | `code:127` strong typo/recovery trigger |
| UT-TYP-001 | typo model | `internal/suggestions/typo/typo_test.go` | DL matching + ranking boost | expected typo candidate priority |
| UT-FBK-001 | feedback math | `internal/suggestions/feedback/feedback_test.go` | accepted/dismissed/edited rules | source/template priors updated |
| UT-LRN-001 | online learning guards | `internal/suggestions/learning/learning_test.go` | eta decay + clamps | weights stay in configured bounds |
| UT-DSM-001 | suppression state machine | `internal/suggestions/dismissal/dismissal_test.go` | temporary/learned/permanent/unblock | transitions and resets correct |
| UT-DIS-001 | discovery parser/runtime | `internal/suggestions/discovery/parser_test.go`, `internal/suggestions/discovery/discovery_test.go` | candidate eligibility and cooldown | gated candidates only |
| UT-PLY-001 | playbook parser | `internal/suggestions/playbook/playbook_test.go` | `after` and `after_failure` validation | missing refs/cycles rejected |
| UT-SRH-001 | FTS backend | `internal/suggestions/search/fts5_test.go` | FTS query behavior | expected ranking and fields |
| UT-SRH-002 | describe and auto | `internal/suggestions/search/describe_test.go`, `internal/suggestions/search/auto_test.go` | deterministic merge behavior | backend and matched tags reported |
| UT-CCH-001 | L1/L2/L3 cache semantics | `internal/suggestions/suggest/cache_l1_test.go`, `cache_l2_test.go`, `cache_l3_test.go` | hit/miss/invalidation | invalidation on command end |
| UT-MTN-001 | maintenance loops | `internal/suggestions/maintenance/maintenance_test.go` | prune/checkpoint/optimize scheduling | operations run in correct windows |
| UT-INV-001 | invariants | `internal/suggestions/invariant/invariant_test.go` | I1-I15 checks | invariant assertions hold |
| UT-SEC-001 | security primitives | `internal/suggestions/security_test.go`, `internal/suggestions/recovery/safety_test.go` | risk tagging and unsafe recovery filtering | risky commands penalized/suppressed |

## 4.2 Integration Tests

| ID | Area | Target Files | Scenario | Assertions |
| --- | --- | --- | --- | --- |
| IT-DAE-001 | single instance lock | `internal/daemon/lockfile_test.go`, `internal/daemon/lifecycle_test.go` | second daemon start | fails with daemon-unavailable semantics |
| IT-DAE-002 | startup ordering | `internal/daemon/server_test.go` | migrations then listener | startup sequence ordering enforced |
| IT-DAE-003 | stale socket cleanup | `internal/daemon/lifecycle_test.go` | pre-existing stale socket | daemon unlinks and binds successfully |
| IT-DAE-004 | signal behavior | `internal/daemon/lifecycle_test.go` | `SIGHUP`, `SIGPIPE` behavior | reload executes; SIGPIPE-safe |
| IT-ING-001 | ingest/suggest ordering | `internal/daemon/suggest_handlers_test.go` | immediate suggest after command_end | bounded wait then fallback context works |
| IT-ING-002 | queue backpressure | `internal/daemon/ingestion_queue_test.go` | queue overflow | drop-oldest low-priority, keep critical events |
| IT-ING-003 | fire-and-forget | `internal/ipc/client_test.go`, `internal/ipc/dial_test.go` | daemon unavailable/timeouts | caller returns quickly without blocking |
| IT-API-001 | API contract | `internal/daemon/handlers_test.go`, `internal/daemon/suggest_handlers_test.go` | response structure | required fields and error model present |
| IT-API-002 | health endpoint | `internal/daemon/server_test.go` | `GET /healthz` | always available when daemon running |
| IT-DB-001 | transactional write path | `internal/suggestions/ingest/writepath_test.go`, `internal/suggestions/db/db_test.go` | event update flow | aggregate updates atomically consistent |
| IT-DB-002 | SQLITE_BUSY retry | `internal/suggestions/db/db_test.go` | forced busy contention | single retry then bounded drop behavior |
| IT-DB-003 | corruption recovery | `internal/suggestions/db/recovery_test.go` | malformed DB startup | rotate-and-recover behavior |
| IT-DB-004 | retention + FTS sync | `internal/suggestions/maintenance/maintenance_test.go`, `internal/suggestions/search/fts5_test.go` | prune old events | FTS rows remain synchronized |
| IT-SRH-001 | backend reporting | `tests/integration/suggest_test.go` + search tests | fts vs fallback | `SearchResponse.backend` populated correctly |
| IT-RNK-001 | deterministic top-k | `tests/integration/suggest_test.go` | repeat request same state | identical ordering and reasons |
| IT-FBK-001 | feedback persistence | `internal/suggestions/feedback/feedback_test.go` + daemon handlers | accepted/dismissed/never/unblock lifecycle | persisted state influences later ranking |
| IT-INC-001 | incognito modes | `tests/integration/session_test.go` + daemon handlers | `off`, `ephemeral`, `no_send` | persistence and ingest behavior match mode |
| IT-PLY-001 | playbook conditional triggers | `internal/suggestions/playbook/playbook_test.go` + integration runtime | `after`/`after_failure` candidates | trigger only in correct context |

## 4.3 Expect Tests (Interactive Shell)

| ID | Area | Target Files | Scenario | Assertions |
| --- | --- | --- | --- | --- |
| EX-BSH-001 | bash hook safety | `tests/expect/bash_test.go`, `tests/expect/hook_resilience_test.go` | init + interactive command run | prompt integrity, no recursion, no errors |
| EX-ZSH-001 | zsh integration | `tests/expect/zsh_test.go` | startup and suggestion behavior | ghost/hint behavior and shell stability |
| EX-FSH-001 | fish integration | `tests/expect/fish_test.go` | startup and command logging | expected functions and command flow |
| EX-HOOK-001 | no daemon fail-open | `tests/expect/hook_resilience_test.go` | daemon absent | commands still execute; hooks non-blocking |
| EX-SES-001 | session lifecycle | `tests/expect/hook_resilience_test.go` | shell enter/exit | session start/end events emitted |
| EX-CD-001 | repo context updates on cd | `tests/expect/hook_resilience_test.go` | move between repos | context changes reflected in ingestion path |
| EX-PRF-001 | integration overhead | `tests/expect/performance_test.go` | startup/source overhead | under configured threshold |
| EX-CLR-001 | color/TTY behavior | `tests/expect/zsh_test.go` + shell tests | NO_COLOR and non-tty-like contexts | no broken ANSI leakage |

## 4.4 Docker Tests (Cross-Distro)

| ID | Area | Target Files | Scenario | Assertions |
| --- | --- | --- | --- | --- |
| DK-MAT-001 | expect matrix baseline | `tests/docker/docker-compose.yml`, `tests/docker/Dockerfile.*` | run expect suite on alpine/ubuntu/debian/fedora | all pass with `-test.parallel=1` |
| DK-BSH-001 | bash version variance | docker images + `tests/expect/bash_test.go` | PROMPT_COMMAND behavior across distro bash | no startup/hook regressions |
| DK-ZSH-001 | zsh environment variance | docker images + `tests/expect/zsh_test.go` | zsh widget/init differences | stable behavior |
| DK-FSH-001 | fish environment variance | docker images + `tests/expect/fish_test.go` | fish init and functions | stable behavior |

## 4.5 End-to-End Tests

| ID | Area | Target Files | Scenario | Assertions |
| --- | --- | --- | --- | --- |
| E2E-SUG-001 | suggestions smoke | `tests/e2e/suggestions-tests.yaml` | basic suggest request/response flow | expected suggestions shown |
| E2E-SUG-002 | typo correction flow | `tests/e2e/suggestions-tests.yaml` | unknown command then correction | corrected candidate appears/high ranks |
| E2E-SUG-003 | slot filling | `tests/e2e/suggestions-tests.yaml` | repeated template with args | slot reuse shown with confidence behavior |
| E2E-SUG-004 | search modes | `tests/e2e/suggestions-tests.yaml` | `fts`, `prefix`, `describe`, `auto` | mode-specific behavior and backend field |
| E2E-SUG-005 | project discovery | `tests/e2e/suggestions-tests.yaml` | repo tasks + playbook triggers | task candidates included with source/reasons |
| E2E-SUG-006 | cache behavior | `tests/e2e/suggestions-tests.yaml` | repeated suggest in same context | cache hit status and latency improvement |
| E2E-SUG-007 | debug/health diagnostics | `tests/e2e/suggestions-tests.yaml` | health and debug endpoints | endpoints reachable and shaped correctly |
| E2E-SUG-008 | incognito behavior | `tests/e2e/suggestions-tests.yaml` | mode toggles | no_send/ephemeral/off behavior respected |

## 5) Test Data, Fixtures, and Determinism

### 5.1 Deterministic Inputs
- Use fixed clocks and seeded randomness for scorer and replay tests.
- Use synthetic command streams with known expected top-k outputs.
- Keep golden files for normalization and reason contributions under source control.

### 5.2 Fixture Repositories
Maintain fixture repos for:
- Go project (`go.mod`)
- Node project (`package.json`)
- Python project (`pyproject.toml`)
- mono-repo with nested directories for directory-scoped behavior
- playbook repo with `.clai/tasks.yaml` (`after` and `after_failure`)

### 5.3 Performance Fixture Profiles
- hot cache profile: repeated same session/context
- cold cache profile: random session/context
- burst profile: script-like 10k command ingestion

## 6) CI Scheduling and Gating

### 6.1 Required Gates (PR)
- Lane B required.
- Any failure in Unit/Integration blocks merge.
- Expect smoke subset blocks merge.

### 6.2 Pre-merge Gate
- Lane C required before merging major suggestions changes.

### 6.3 Nightly Gate
- Lane D required nightly and for release branch.
- Nightly failures create regression issues with category label: `unit`, `integration`, `expect`, `docker`, `e2e`.

## 7) Exit Criteria for This Test Plan

This plan is complete when:
- every active requirement in `suggestions_tech_v1.md` maps to at least one test case ID above
- workflow-excluded behavior has no required runtime test cases
- runtime-critical paths are covered in fast lanes (A/B)
- high-fidelity shell and distro confidence is covered in lanes C/D

## 8) Initial Implementation Backlog (Tests to Add or Tighten)

Priority order to maximize risk reduction per runtime minute:
1. IT-ING-001 ingest/suggest ordering bounded wait + fallback context.
2. IT-API-002 health endpoint always available.
3. IT-DB-001 transactional write-path consistency including slot/project/pipeline/failure updates.
4. UT-VAL-002/003 full request/feedback validation matrix coverage.
5. EX-HOOK-001 no-daemon fail-open across bash/zsh/fish.
6. DK-MAT-001 stable docker matrix run in CI nightly.
7. E2E-SUG-004 search mode/`backend` contract.
8. E2E-SUG-008 incognito mode end-to-end assertions.
