# clai Suggestions Engine V2 - Enhancement Appendix

This appendix preserves the full enhancement proposal detail previously embedded in the main spec. The canonical normative specification remains in /Users/runger/.claude-worktrees/clai/happy-hypatia/specs/tech_suggestions_ext_v1.md.

## 20) Enhancement Pack (Additive, No Removal)

This section integrates an additive set of V2 extensions. It does not remove any prior contracts in this document. Where an extended rule differs from earlier defaults, this section adds stricter or more specific behavior while keeping existing guarantees.

### 20.1 Project-Type-Aware Context Scoping

Problem:
- Repo-level affinity is not enough for cross-repo transfer inside the same technical domain (for example Go-to-Go workflows).

Detection:
- On `cwd` change (derived from consecutive command events), daemon scans upward for markers.
- Stop scanning at first `.git` root or after 5 directory levels.
- Multiple markers may match and produce multiple project types.
- Marker map:
- `go.mod` -> `go`
- `Cargo.toml` -> `rust`
- `pyproject.toml`, `setup.py`, `requirements.txt` -> `python`
- `package.json` -> `node`
- `Gemfile` -> `ruby`
- `pom.xml` -> `java-maven`
- `build.gradle` -> `java-gradle`
- `CMakeLists.txt` -> `cpp-cmake`
- `Makefile` -> `make`
- `Dockerfile`, `docker-compose.yml` -> `docker`
- `helmfile.yaml` -> `k8s-helm`
- `kustomization.yaml` -> `k8s-kustomize`
- `terraform/*.tf` -> `terraform`
- `serverless.yml` -> `serverless`
- `.clai/project.yaml` -> user-defined override.
- Persisted format in session context: sorted, pipe-delimited string, for example `docker|go|k8s-helm`.

Schema additions:
```sql
ALTER TABLE session ADD COLUMN project_types TEXT;

CREATE TABLE project_type_stat (
  project_type TEXT NOT NULL,
  template_id  TEXT NOT NULL,
  score        REAL NOT NULL,
  count        INTEGER NOT NULL,
  last_seen_ms INTEGER NOT NULL,
  PRIMARY KEY(project_type, template_id)
);

CREATE TABLE project_type_transition (
  project_type      TEXT NOT NULL,
  prev_template_id  TEXT NOT NULL,
  next_template_id  TEXT NOT NULL,
  weight            REAL NOT NULL,
  count             INTEGER NOT NULL,
  last_seen_ms      INTEGER NOT NULL,
  PRIMARY KEY(project_type, prev_template_id, next_template_id)
);
```

Ingestion extension:
- For non-ephemeral `command_end` with `repo_key`, resolve project types using repo-key cache (`60s` TTL) plus fsnotify invalidation.
- Upsert project-type frequency and transition rows for each active project type in the same write transaction.

Candidate retrieval extension:
- Add project-type transition retrieval source with cap `30`.
- For multiple active types, union candidates and keep max weight per template.

Ranking extension:
- Add feature `project_type_affinity` and weight `w9`.
- Formula extension appears in section 20.13.
- Default: `suggestions.weights.project_type_affinity=0.08`.

Custom types:
- `.clai/project.yaml` may define:
```yaml
project_types:
  - go
  - k8s-helm
  - custom:ml-pipeline
```
- When present, this file overrides auto-detection.

### 20.2 Pipeline and Compound Command Awareness

Problem:
- Single-command normalization loses high-signal transitions within compound commands and pipelines.

Splitter stage (before existing normalization):
- Split on unquoted/unescaped operators: `|`, `|&`, `&&`, `||`, `;`.
- Treat subshell bodies (`$()`, backticks) as opaque.
- Ignore `&` as background operator for pipeline modeling.

Schema additions:
```sql
CREATE TABLE pipeline_event (
  id               INTEGER PRIMARY KEY AUTOINCREMENT,
  command_event_id INTEGER NOT NULL REFERENCES command_event(id),
  position         INTEGER NOT NULL,
  operator         TEXT,
  cmd_raw          TEXT NOT NULL,
  cmd_norm         TEXT NOT NULL,
  template_id      TEXT NOT NULL,
  UNIQUE(command_event_id, position)
);

CREATE TABLE pipeline_transition (
  scope            TEXT NOT NULL,
  prev_template_id TEXT NOT NULL,
  next_template_id TEXT NOT NULL,
  operator         TEXT NOT NULL,
  weight           REAL NOT NULL,
  count            INTEGER NOT NULL,
  last_seen_ms     INTEGER NOT NULL,
  PRIMARY KEY(scope, prev_template_id, next_template_id, operator)
);

CREATE TABLE pipeline_pattern (
  pattern_hash     TEXT PRIMARY KEY,
  template_chain   TEXT NOT NULL,
  operator_chain   TEXT NOT NULL,
  scope            TEXT NOT NULL,
  count            INTEGER NOT NULL,
  last_seen_ms     INTEGER NOT NULL,
  cmd_norm_display TEXT NOT NULL
);
```

Ingestion extension:
- After `command_event` insert, split command.
- If segment count > 1:
- insert `pipeline_event` rows
- upsert adjacent `pipeline_transition` rows
- upsert full `pipeline_pattern`.
- Bound by `suggestions.pipeline_max_segments`.

Suggestion modes:
- Mode A (continuation): when prefix ends with pipeline/logical operator, predict only next segment.
- Mode B (pattern recall): suggest full known multi-step pipeline pattern starting from first segment.

Ranking integration:
- For pattern recall, apply `pipeline_confidence` as transition amplifier.
- For continuation mode, use pipeline-transition signal in place of generic transition for that candidate.

Edge-case rules:
- Respect quoted operators.
- Treat heredoc body as opaque first segment content.
- Do not split inside `$()` or backticks.

### 20.3 Error-Context Recovery Suggestions

Problem:
- Exit code `127` typo repair exists, but broader non-zero failure recovery is not modeled.

Schema addition:
```sql
CREATE TABLE failure_recovery (
  scope                TEXT NOT NULL,
  failed_template_id   TEXT NOT NULL,
  exit_code_class      TEXT NOT NULL,
  recovery_template_id TEXT NOT NULL,
  weight               REAL NOT NULL,
  count                INTEGER NOT NULL,
  success_rate         REAL NOT NULL,
  last_seen_ms         INTEGER NOT NULL,
  source               TEXT NOT NULL DEFAULT 'learned',
  PRIMARY KEY(scope, failed_template_id, exit_code_class, recovery_template_id)
);
```

Exit classes:
- `code:1`, `code:2`, `code:126`, `code:127`, `code:128+`, `code:255`, `nonzero`.

Ingestion extension:
- If previous command failed and current command succeeds, upsert failure->recovery edge and success-rate.
- If previous and current both fail, apply negative evidence decay for candidate recovery edge.

Retrieval extension:
- When last command failed, add `failure_recovery` source near top priority with cap `5`.

Ranking extension:
- Add feature weight `w10` (`suggestions.weights.failure_recovery=0.12`).
- `failure_recovery = recovery_weight * success_rate * recency_decay`.

Bootstrap patterns:
- Seed small set of common patterns with low initial weight and `source='bootstrap'`.
- Safety gate:
- bootstrap recoveries must pass existing risk policy before ranking.
- destructive or high-risk bootstrap recoveries may be disabled by default and gated by config.

### 20.4 Multi-Step Workflow Detection

Problem:
- First-order transitions cannot model longer workflows.

Schema additions:
```sql
CREATE TABLE workflow_pattern (
  pattern_id        TEXT PRIMARY KEY,
  template_chain    TEXT NOT NULL,
  display_chain     TEXT NOT NULL,
  scope             TEXT NOT NULL,
  step_count        INTEGER NOT NULL,
  occurrence_count  INTEGER NOT NULL,
  last_seen_ms      INTEGER NOT NULL,
  avg_duration_ms   INTEGER
);

CREATE TABLE workflow_step (
  pattern_id   TEXT NOT NULL REFERENCES workflow_pattern(pattern_id),
  step_index   INTEGER NOT NULL,
  template_id  TEXT NOT NULL,
  PRIMARY KEY(pattern_id, step_index)
);

CREATE INDEX idx_workflow_step_template ON workflow_step(template_id);
```

Background mining:
- Run every `suggestions.workflow_mine_interval_ms` or on session end.
- Generate contiguous subsequences of length `3-6`.
- Generate limited non-contiguous subsequences with max gap `2`.
- Promote subsequences with minimum count threshold.
- Prune subset patterns when longer patterns have near-equal support.

Runtime activation:
- In-memory activation state per session tracks pattern progress, recency, and score.
- Expire stale activations by timeout or lack of advancement.

Candidate generation:
- Add source `workflow` for next-step suggestion, cap `3`.
- Use existing slot fill rules for workflow next template.

Ranking integration:
- Workflow signal amplifies transition instead of adding new model dimension:
- `effective_transition = base_transition + workflow_boost * activation_score`.

UX metadata:
- Include workflow progress reasons in `SuggestResponse.reasons[]`.

### 20.5 Adaptive Suggestion Timing

Problem:
- Static suggestion timing is noisy for fast typists and delayed for exploratory users.

Adapter-level cadence model:
- Capture rolling typing metrics:
- chars/sec
- pause events over threshold
- prefix length at pause
- backspace count.

State machine:
- `IDLE` -> `TYPING` -> `FAST_TYPING` or `PAUSED` -> `REQUEST`.
- Request immediately on pause.
- Suppress frequent requests during fast typing.

Shell support matrix:
- `zsh`: full adaptive cadence via ZLE hooks.
- `fish`: rely on fish-native cadence entry points; adapter augments where possible.
- `bash`: fallback simplified policy on prompt update due hook limitations.

Daemon hinting:
- `SuggestResponse` may include:
```json
{
  "timing_hint": {
    "user_speed_class": "fast|moderate|exploratory",
    "suggested_pause_threshold_ms": 250
  }
}
```

### 20.6 Alias Resolution and Rendering

Problem:
- Alias-heavy environments fragment template learning.

Schema addition:
```sql
CREATE TABLE session_alias (
  session_id TEXT NOT NULL,
  alias_key  TEXT NOT NULL,
  expansion  TEXT NOT NULL,
  PRIMARY KEY(session_id, alias_key)
);
```

Session metadata:
- Capture alias map on `session_start` and keep in memory for normalization/rendering.

Normalization extension:
- First step expands alias in first token.
- Iterative expansion bounded by `suggestions.alias_max_expansion_depth`.
- Detect cycles and stop expansion safely.
- Continue with existing normalization on expanded command.

Dual representation:
- Keep `cmd_raw` unexpanded for display and audit.
- Normalize `cmd_norm` from expanded command for template identity.

Rendering extension:
- If alias-preferred rendering enabled, rewrite suggestion back to user alias form when mapping is available.

Mid-session alias changes:
- Recommended mode:
- re-snapshot aliases after commands beginning with `alias`, `unalias`, or `abbr`.

### 20.7 Persistent Dismissal Learning

Problem:
- Repeated dismissals should become durable context-specific suppression.

Schema addition:
```sql
CREATE TABLE dismissal_pattern (
  scope                  TEXT NOT NULL,
  context_template_id    TEXT NOT NULL,
  dismissed_template_id  TEXT NOT NULL,
  dismissal_count        INTEGER NOT NULL,
  last_dismissed_ms      INTEGER NOT NULL,
  suppression_level      TEXT NOT NULL,
  PRIMARY KEY(scope, context_template_id, dismissed_template_id)
);
```

State machine:
- `NONE` -> `TEMPORARY` -> `LEARNED` -> `PERMANENT`.
- `LEARNED` threshold defaults to 3 dismissals in same context.
- Explicit `never` feedback sets `PERMANENT` block.
- Explicit `unblock` feedback reverses permanent suppression.
- Acceptance in same context resets suppression.

Ranking integration:
- Add context-aware negative feedback contribution from dismissal state.
- `PERMANENT` suppression may filter candidate before ranking.

### 20.8 Directory-Scoped Frequency and Transitions

Problem:
- Repo-level scope is too coarse for monorepos.

Directory scope key:
- Compute path from repo root to cwd.
- Truncate to max depth (`suggestions.directory_scope_max_depth`, default `3`).
- Build scope id: `dir:` + hash(repo_key + "/" + dir_scope_path) prefix.

Storage strategy:
- Reuse existing `command_stat` and `transition_stat` `scope` column with `dir:<hash>` values.
- No new table required.

Ingestion extension:
- For non-ephemeral repo events, upsert `command_stat` and `transition_stat` in directory scope.

Retrieval extension:
- Add directory-scoped transitions between repo and project-type sources.

Affinity enhancement:
- `affinity = 0.4*repo_match + 0.3*dir_scope_match + 0.2*branch_match + 0.1*cwd_exact_match`.

### 20.9 Explainable Suggestions ("Why This?")

Reason model:
- Each suggestion returns top 1-3 reasons by contribution magnitude.
- Standard reason shape:
```go
type SuggestionReason struct {
  Type         string
  Description  string
  Contribution float64
}
```

Reason types:
- `transition`, `frequency`, `success`, `directory`, `project_type`, `task`, `workflow`, `failure_recovery`, `feedback`, `pipeline`, `discovery`.

Generation:
- Use weighted feature contributions post-score.
- Drop negligible reasons below `suggestions.explain_min_contribution`.

UX:
- CLI JSON always includes `reasons[]`.
- Interactive shell integrations may bind "show why" key and render concise hint line.

### 20.10 Team Workflow Playbooks (Extended `.clai/tasks.yaml`)

Extended file schema:
```yaml
tasks:
  - name: deploy-staging
    command: make deploy-staging
    description: Deploy to staging
  - name: smoke-test
    command: make smoke-test
    after: [deploy-staging]
    priority: high
  - name: lint-fix
    command: make lint-fix
    after_failure: [lint-check]

workflows:
  - name: full-deploy
    steps:
      - make test
      - make build
      - make deploy-staging
      - make smoke-test
      - make deploy-prod
```

Parser and validation:
- Existing simple format remains valid.
- Validate references in `after` and `after_failure`.
- Detect and reject circular trigger chains.
- Validate workflow step commands through normalizer.

Engine integration:
- `after` and `after_failure` create conditional task candidates with dedicated source tags.
- Playbook workflows seed `workflow_pattern` with high initial support count.

### 20.11 Command Discovery ("Did You Know?")

Goal:
- Low-priority discovery suggestions for useful commands user has not run.

Candidate pools:
- Project-type priors with zero personal usage.
- Playbook tasks never executed by user.
- Curated tool-common command sets.

Filters:
- Context relevance.
- Novelty (zero-count for user history).
- Source frequency threshold.
- Cooldown window (`suggestions.discovery_cooldown_hours`).

Display gate:
- Only when prefix is empty.
- Only when no high-confidence prediction exists.
- Never displace high-confidence predictive suggestions.

Feedback:
- Accepted discovery enters normal learning.
- Repeated dismissals feed persistent dismissal logic.

### 20.12 Natural-Language Command Recall

Problem:
- Users remember intent, not command text.

Schema alteration:
```sql
ALTER TABLE command_template ADD COLUMN tags TEXT;
```

Tagging:
- Extract bounded semantic tags during normalization using deterministic rules.
- Include tool, subcommand, flag semantics, argument patterns, and synonym mapping.
- Controlled vocabulary (built-in; configurable extension path).

Search modes:
- Extend `clai search` with `--mode describe` and `--mode auto`.
- `describe` uses tag overlap scoring.
- `auto` merges FTS and describe scores and deduplicates by template.

API extensions:
- `SearchRequest.mode` supports `fts|prefix|describe|auto`.
- `SearchResponse` may include `tags` and `matched_tags`.

### 20.13 Integrated Retrieval Priority and Ranking Formula

Extended retrieval priority:
1. Session transitions
2. Failure recovery candidates
3. Active workflow next-step
4. Pipeline pattern recall
5. Repo transitions
6. Directory-scoped transitions
7. Project-type transitions
8. Playbook conditional triggers
9. Global transitions
10. Discovery candidates (low priority gate)

Extended scoring:
```text
score = w1*transition_effective
      + w2*frequency
      + w3*success
      + w4*prefix
      + w5*affinity_enhanced
      + w6*task_extended
      + w7*feedback_extended
      + w9*project_type_affinity
      + w10*failure_recovery
      - w8*risk_penalty
```

Feature amplifiers:
- `transition_effective` includes workflow and pipeline amplifiers.
- `affinity_enhanced` includes directory scope effects.
- `task_extended` includes playbook triggers.
- `feedback_extended` includes persistent dismissal effects.

### 20.14 Schema Delta Summary (Additive)

New tables:
- `project_type_stat`
- `project_type_transition`
- `pipeline_event`
- `pipeline_transition`
- `pipeline_pattern`
- `failure_recovery`
- `workflow_pattern`
- `workflow_step`
- `session_alias`
- `dismissal_pattern`

Altered tables:
- `session.project_types` (column add)
- `command_template.tags` (column add)

### 20.15 Configuration Keys (Additive)

Project type:
- `suggestions.project_type_detection_enabled=true`
- `suggestions.project_type_cache_ttl_ms=60000`
- `suggestions.weights.project_type_affinity=0.08`

Pipeline:
- `suggestions.pipeline_awareness_enabled=true`
- `suggestions.pipeline_max_segments=8`
- `suggestions.pipeline_pattern_min_count=2`

Failure recovery:
- `suggestions.failure_recovery_enabled=true`
- `suggestions.failure_recovery_bootstrap_enabled=true`
- `suggestions.failure_recovery_min_count=2`
- `suggestions.weights.failure_recovery=0.12`

Workflow:
- `suggestions.workflow_detection_enabled=true`
- `suggestions.workflow_min_steps=3`
- `suggestions.workflow_max_steps=6`
- `suggestions.workflow_min_occurrences=3`
- `suggestions.workflow_max_gap=2`
- `suggestions.workflow_activation_timeout_ms=600000`
- `suggestions.workflow_boost=0.25`
- `suggestions.workflow_mine_interval_ms=600000`

Adaptive timing:
- `suggestions.adaptive_timing_enabled=true`
- `suggestions.typing_fast_threshold_cps=6.0`
- `suggestions.typing_pause_threshold_ms=300`
- `suggestions.typing_eager_prefix_length=3`

Alias:
- `suggestions.alias_resolution_enabled=true`
- `suggestions.alias_max_expansion_depth=3`
- `suggestions.alias_render_preferred=true`

Dismissal:
- `suggestions.dismissal_learned_threshold=3`
- `suggestions.dismissal_learned_halflife_hours=720`
- `suggestions.dismissal_temporary_halflife_ms=1800000`

Directory scope:
- `suggestions.directory_scoping_enabled=true`
- `suggestions.directory_scope_max_depth=3`

Explainability:
- `suggestions.explain_enabled=true`
- `suggestions.explain_max_reasons=3`
- `suggestions.explain_min_contribution=0.05`

Extended playbook:
- `suggestions.task_playbook_extended_enabled=true`
- `suggestions.task_playbook_after_boost=0.30`
- `suggestions.task_playbook_workflow_seed_count=100`

Discovery:
- `suggestions.discovery_enabled=true`
- `suggestions.discovery_cooldown_hours=24`
- `suggestions.discovery_max_confidence_threshold=0.3`
- `suggestions.discovery_source_project_type=true`
- `suggestions.discovery_source_playbook=true`
- `suggestions.discovery_source_tool_common=true`

Describe search:
- `suggestions.search_describe_enabled=true`
- `suggestions.search_tag_vocabulary_path=""`
- `suggestions.search_auto_mode_merge=true`
