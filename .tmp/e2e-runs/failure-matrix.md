# E2E Failure Matrix

Generated from .tmp/e2e-runs/results-*.json

| comment                                    | Test                                            | Bash | Zsh | Fish | Expected (from failure assertion) | Triage |
|--------------------------------------------|-------------------------------------------------|---|---|---|---|---|
| need                                       | Alt+Right accepts next token | FAIL | FAIL | FAIL | expected screen not to contain: pods | runner_or_test_visual |
| need                                       | CLAI_EPHEMERAL affects suggestions              | FAIL | FAIL | FAIL | expected screen not to contain: ephemeral_env_test_cmd | test_expectation_state_leak |
| need                                       | Discovery shows project_task reason          | FAIL | FAIL | FAIL | expected screen to contain: project_task | likely_product_gap |
| need                                       | Escape clears suggestion                     | FAIL | FAIL | FAIL | expected screen not to contain: nstall | runner_or_test_visual |
| need                                       | Ghost text suggestion appears                | FAIL | FAIL | FAIL | expected screen to contain: atus | runner_or_test_visual |
| need                                       | Incognito commands not in global suggestions | FAIL | FAIL | FAIL | expected screen not to contain: incognito_only_cmd_xyz | test_expectation_state_leak |
| not sure                                   | Suggest API includes cache status            | FAIL | FAIL | FAIL | expected screen to match: (cache_hit\|cache_miss\|from_cache) | runner_artifact_skip_true |
| need                                       | Suggest API returns expected JSON structure  | FAIL | FAIL | FAIL | expected screen to match: (reason\|reasons) | likely_product_gap |
| need                                       | Suggestion context includes last_cmd_norm    | FAIL | FAIL | FAIL | expected screen to match: (context\|last_cmd) | likely_product_gap |
| need                                       | Suggestion includes confidence score         | FAIL | FAIL | FAIL | expected screen to contain: confidence | likely_product_gap |
| need                                       | Suggestion shows freq_repo reason            | FAIL | FAIL | FAIL | expected screen to match: (freq_repo\|freq_global) | likely_product_gap |
| need                                       | Suggestion shows transition reason           | FAIL | FAIL | FAIL | expected screen to contain: transition | likely_product_gap |
| need                                       | Typo correction respects similarity threshold | FAIL | FAIL | FAIL | expected screen not to contain: xyzabc123 | runner_artifact_skip_true |
| need                                       | clai search basic query                      | FAIL | FAIL | FAIL | expected screen to contain: searchable_unique_term_abc123 | investigate_product_or_timing |
| need                                       | clai suggest with prefix filters results     | FAIL | FAIL | FAIL | expected screen not to contain: npm \| expected screen not to contain: make | test_expectation_buffer_scope |
| need                                       | clai suggest JSON format                     | PASS | FAIL | FAIL | expected screen to contain: { | investigate_shell_format |
| need                                       | CLAI_NO_RECORD prevents all ingestion        | FAIL | - | - | expected screen not to contain: NO_RECORD_TEST_CMD | test_expectation_buffer_scope |
| need                                       | CWD scope filters correctly with spaces in path | - | FAIL | - | expected screen not to contain: in tmp root | test_expectation_buffer_scope |
| need                                       | CWD scope works with paths containing spaces | FAIL | - | - | expected screen to contain: CWD | test_expectation_ui_text |
| need                                       | Command ingestion captures exit code failure | FAIL | - | - | expected screen to contain: exit_code | investigate_product_or_output_format |
| need                                       | Command ingestion captures exit code success | FAIL | - | - | expected screen to contain: exit_code | investigate_product_or_output_format |
| need                                       | Commands in incognito mode not persisted     | FAIL | - | - | expected screen not to contain: SECRET_INCOGNITO_COMMAND_12345 | test_expectation_buffer_scope |
| need                                       | Ctrl+C copies full untruncated command       | FAIL | - | - | expected screen to contain: Copied! | test_expectation_ui_toast |
| Alt/Opt+H or double arrow UP               | Ctrl+Space opens suggestion picker (bash)    | FAIL | - | - | keyboard.press: Unknown key: "Ctrl" | runner_artifact_key_mapping |
| Alt/Opt+H or double arrow UP               | Ctrl+Space opens suggestion picker (zsh)       | - | FAIL | - | keyboard.press: Unknown key: "Ctrl" | runner_artifact_key_mapping |
| need                                       | Debug discovery-errors endpoint                | PASS | FAIL | PASS | expected screen to match: (errors\|count) | test_expectation_data_dependent |
| need                                       | Debug scores endpoint returns data             | PASS | FAIL | PASS | expected screen to contain: scores | test_expectation_data_dependent |
| need                                       | Debug transitions endpoint returns data        | FAIL | PASS | PASS | expected screen to match: (transitions\|prev\|next) | test_expectation_data_dependent |
| need                                       | Global history contains cross-session commands | FAIL | - | - | expected screen to contain: Global | test_expectation_ui_text |
| need                                       | History picker Enter inserts command           | FAIL | - | - | expected screen not to contain: history | test_expectation_ui_text |
| need                                       | History picker Escape cancels                  | FAIL | - | - | expected screen not to contain: history | test_expectation_ui_text |
| need                                       | History picker fuzzy search filters results    | FAIL | - | - | expected screen not to contain: banana \| expected screen not to contain: git status | test_expectation_buffer_scope |
| need                                       | History picker navigation with arrow keys      | FAIL | - | - | expected visible element: picker-selection | runner_artifact_dom_assertion |
| need                                       | History scope switching with Tab               | FAIL | - | - | expected screen to contain: Global | test_expectation_ui_text |
| need                                       | Long command shows truncation indicator        | FAIL | - | - | expected screen to contain: … | test_expectation_ui_glyph |
| need                                       | Session history is isolated from global        | FAIL | - | - | expected screen to contain: Session | test_expectation_ui_text |
| for session filtered, global is unfiltered | clai history filters by CWD                    | FAIL | - | - | expected screen not to contain: from_home_dir | test_expectation_buffer_scope |
| for session filtered, global is unfiltered | clai history filters failed commands     | FAIL | - | - | expected screen not to contain: will_succeed | test_expectation_buffer_scope |
| only duplicate commands on output          | clai history filters successful commands | FAIL | - | - | expected screen not to contain: false | test_expectation_buffer_scope |
| not sure why needed                        | clai search with JSON output      | PASS | FAIL | PASS | expected screen to contain: { \| expected screen to contain: results | test_expectation_shell_output |
| not sure why needed                        | clai suggest fzf format           | FAIL | PASS | PASS | expected screen not to contain: ( | test_expectation_buffer_scope |
