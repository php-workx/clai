echo confidence_seed_beta
clai suggest echo --format=json --limit=3
git status
git log --oneline
clai suggest git --format=fzf --limit=3
kubectl get pods -n production
clai suggest kubectl --format=fzf --limit=3
mkdir -p /tmp/reason-test
cd /tmp/reason-test
echo '{"scripts":{"lint":"eslint"}}' > package.json
cd /tmp/reason-test && clai suggest npm --format=json --limit=5
echo api_json_seed_one
echo api_json_seed_two
clai suggest echo --format=json --limit=5
echo 'setup command 1'
echo 'setup command 2'
ls -la
echo apple
echo banana
echo apricot
git status
command one
command two
command three
echo 'selected command'
selected
echo unique_session_test_command_12345
echo session_specific_command
echo this_is_a_very_long_command_that_should_be_truncated_in_the_middle_with_an_ellipsis_indicator_to_show_both_start_and_end
echo UNIQUE_START_marker_this_is_a_very_long_command_with_lots_of_text_in_the_middle_UNIQUE_END_marker
UNIQUE_START
echo CLIPBOARD_START_this_is_a_very_long_command_for_clipboard_test_CLIPBOARD_END
source ~/.clai/shell/bash.sh
echo $CLAI_SESSION_ID
echo first
echo second
echo third
history
clai status
echo test_history_cmd_1
echo test_history_cmd_2
clai history --session=$CLAI_SESSION_ID
clai history --global --limit 10
cd /tmp && echo from_tmp_dir
cd ~ && echo from_home_dir
if clai history --cwd=/tmp | grep -q from_tmp_dir && ! clai history --cwd=/tmp | grep -q from_home_dir; then echo HISTORY_CWD_FILTER_PASS; else echo HISTORY_CWD_FILTER_FAIL; fi
echo success_command
false
if clai history --status=success | grep -q success_command && ! clai history --status=success | grep -q '^false$'; then echo HISTORY_SUCCESS_FILTER_PASS; else echo HISTORY_SUCCESS_FILTER_FAIL; fi
echo will_succeed
false
if clai history --status=failure | grep -q '^false$' && ! clai history --status=failure | grep -q will_succeed; then echo HISTORY_FAILURE_FILTER_PASS; else echo HISTORY_FAILURE_FILTER_FAIL; fi
clai off
clai off
clai on
echo 'hello world'
ls --color=always
sleep 60
`list all files in current directory
echo 'this is a very long command that should wrap properly in the terminal without causing any display issues or breaking the shell integration features'
echo "hello 'world' $HOME"
mkdir -p '/tmp/test dir with spaces'
cd '/tmp/test dir with spaces'
echo 'command in spaced dir'
ls -la
mkdir -p "/tmp/path'with\"quotes"
cd "/tmp/path'with\"quotes"
echo 'special path command'
clai incognito on
clai incognito on
echo 'SECRET_INCOGNITO_COMMAND_12345'
clai incognito off
if clai history --global | grep -q SECRET_INCOGNITO; then echo INCOGNITO_PERSISTED_FAIL; else echo INCOGNITO_PERSISTED_PASS; fi
clai incognito on
clai incognito off
export CLAI_NO_RECORD=1
echo 'NO_RECORD_TEST_CMD'
unset CLAI_NO_RECORD
if clai history --session=$CLAI_SESSION_ID | grep -q NO_RECORD_TEST_CMD; then echo NO_RECORD_FAIL; else echo NO_RECORD_PASS; fi
true
if clai history --session=$CLAI_SESSION_ID --format json | grep -q '"exit_code":0'; then echo EXIT_SUCCESS_PASS; else echo EXIT_SUCCESS_FAIL; fi
false
if clai history --session=$CLAI_SESSION_ID --format json | grep -q '"exit_code":1'; then echo EXIT_FAILURE_PASS; else echo EXIT_FAILURE_FAIL; fi
cd /tmp && echo 'ingestion_cwd_test'
clai history --cwd=/tmp --global
echo "hello | world" > /dev/null && echo 'done'
clai history --session=$CLAI_SESSION_ID
echo '日本語 émoji 🎉'
clai history --session=$CLAI_SESSION_ID
clai daemon status
clai daemon start
clai daemon status
clai daemon start
clai daemon stop
clai daemon status
clai daemon restart
clai daemon status
echo suggest_text_seed_one
echo suggest_text_seed_two
clai suggest echo --limit=3
echo suggest_json_seed_one
echo suggest_json_seed_two
clai suggest echo --format=json --limit=3
docker ps
clai suggest --format=fzf
cmd1
cmd2
cmd3
clai suggest --limit=2
git status
git log --oneline
npm test
make build
clai suggest git
echo freq_reason_seed
echo freq_reason_seed
echo freq_reason_seed
echo freq_reason_other
clai suggest echo --format=json --limit=5
echo confidence_seed_alpha
echo confidence_seed_beta
clai suggest echo --format=json --limit=3
git status
clai suggest git --format=json --limit=3
git status
git log --oneline
clai suggest git --format=fzf --limit=3
docker ps -a
npm install
git status
kubectl get pods -n production
clai suggest kubectl --format=fzf --limit=3
echo test1
git status
echo 'searchable_unique_term_abc123'
git status
clai search 'searchable_unique'
echo 'repo_specific_search_term'
clai search --repo 'repo_specific'
echo search_limit_1
echo search_limit_2
echo search_limit_3
echo search_limit_4
echo search_limit_5
clai search --limit 2 'search_limit'
echo json_search_test
clai search --json 'json_search'
clai search 'xyznonexistent98765'
echo 'test-with-dashes_and_underscores'
clai search 'test-with-dashes'
mkdir -p /tmp/node-test-project
cd /tmp/node-test-project
echo '{"scripts":{"test":"jest","build":"tsc"}}' > package.json
cd /tmp/node-test-project && clai suggest
mkdir -p /tmp/make-test-project
cd /tmp/make-test-project
printf 'build:\n\techo build\ntest:\n\techo test\n' > Makefile
cd /tmp/make-test-project && clai suggest
mkdir -p /tmp/reason-test
cd /tmp/reason-test
echo '{"scripts":{"lint":"eslint"}}' > package.json
cd /tmp/reason-test && clai suggest npm --format=json --limit=5
curl -s http://localhost:8765/debug/scores 2>/dev/null || clai debug scores
curl -s 'http://localhost:8765/debug/scores?limit=5' 2>/dev/null || clai debug scores --limit=5
git status
git add .
git commit -m 'test'
curl -s http://localhost:8765/debug/transitions 2>/dev/null || clai debug transitions
mkdir -p /tmp/debug-task-test
cd /tmp/debug-task-test
echo '{"scripts":{"test":"jest"}}' > package.json
clai suggest
curl -s http://localhost:8765/debug/tasks 2>/dev/null || clai debug tasks
curl -s http://localhost:8765/debug/discovery-errors 2>/dev/null || clai debug discovery-errors
curl -s http://localhost:8765/debug/cache 2>/dev/null | jq . || echo 'curl failed'
clai incognito on
clai incognito on
echo 'ephemeral_session_cmd_12345'
clai suggest
clai incognito on
echo 'incognito_only_cmd_xyz'
echo 'incognito_only_cmd_xyz'
echo 'incognito_only_cmd_xyz'
clai incognito off
clai suggest --format=json
export CLAI_EPHEMERAL=1
echo 'ephemeral_env_test_cmd'
unset CLAI_EPHEMERAL
clai suggest --format=json
echo api_json_seed_one
echo api_json_seed_two
clai suggest echo --format=json --limit=5
echo 'setup command 1'
echo 'setup command 2'
ls -la
command one
command two
command three
echo 'selected command'
selected
echo unique_session_test_command_12345
echo session_specific_command
echo this_is_a_very_long_command_that_should_be_truncated_in_the_middle_with_an_ellipsis_indicator_to_show_both_start_and_end
echo UNIQUE_START_marker_this_is_a_very_long_command_with_lots_of_text_in_the_middle_UNIQUE_END_marker
UNIQUE_START
echo CLIPBOARD_START_this_is_a_very_long_command_for_clipboard_test_CLIPBOARD_END
source ~/.clai/shell/bash.sh
echo $CLAI_SESSION_ID
echo first
echo second
echo third
history
clai status
echo test_history_cmd_1
echo test_history_cmd_2
clai history --session=$CLAI_SESSION_ID
clai history --global --limit 10
cd /tmp && echo from_tmp_dir
cd ~ && echo from_home_dir
if clai history --cwd=/tmp | grep -q from_tmp_dir && ! clai history --cwd=/tmp | grep -q from_home_dir; then echo HISTORY_CWD_FILTER_PASS; else echo HISTORY_CWD_FILTER_FAIL; fi
echo success_command
false
if clai history --status=success | grep -q success_command && ! clai history --status=success | grep -q '^false$'; then echo HISTORY_SUCCESS_FILTER_PASS; else echo HISTORY_SUCCESS_FILTER_FAIL; fi
echo will_succeed
false
if clai history --status=failure | grep -q '^false$' && ! clai history --status=failure | grep -q will_succeed; then echo HISTORY_FAILURE_FILTER_PASS; else echo HISTORY_FAILURE_FILTER_FAIL; fi
clai off
clai off
clai on
echo 'hello world'
ls --color=always
sleep 60
`list all files in current directory
echo 'this is a very long command that should wrap properly in the terminal without causing any display issues or breaking the shell integration features'
echo "hello 'world' $HOME"
mkdir -p "/tmp/path'with\"quotes"
cd "/tmp/path'with\"quotes"
echo 'special path command'
clai incognito on
clai incognito on
echo 'SECRET_INCOGNITO_COMMAND_12345'
clai incognito off
if clai history --global | grep -q SECRET_INCOGNITO; then echo INCOGNITO_PERSISTED_FAIL; else echo INCOGNITO_PERSISTED_PASS; fi
clai incognito on
clai incognito off
export CLAI_NO_RECORD=1
echo 'NO_RECORD_TEST_CMD'
unset CLAI_NO_RECORD
if clai history --session=$CLAI_SESSION_ID | grep -q NO_RECORD_TEST_CMD; then echo NO_RECORD_FAIL; else echo NO_RECORD_PASS; fi
true
if clai history --session=$CLAI_SESSION_ID --format json | grep -q '"exit_code":0'; then echo EXIT_SUCCESS_PASS; else echo EXIT_SUCCESS_FAIL; fi
false
if clai history --session=$CLAI_SESSION_ID --format json | grep -q '"exit_code":1'; then echo EXIT_FAILURE_PASS; else echo EXIT_FAILURE_FAIL; fi
cd /tmp && echo 'ingestion_cwd_test'
clai history --cwd=/tmp --global
echo "hello | world" > /dev/null && echo 'done'
clai history --session=$CLAI_SESSION_ID
echo '日本語 émoji 🎉'
clai history --session=$CLAI_SESSION_ID
clai daemon status
clai daemon start
clai daemon status
clai daemon start
clai daemon stop
clai daemon status
clai daemon restart
clai daemon status
echo suggest_text_seed_one
echo suggest_text_seed_two
clai suggest echo --limit=3
echo suggest_json_seed_one
echo suggest_json_seed_two
clai suggest echo --format=json --limit=3
docker ps
clai suggest --format=fzf
cmd1
cmd2
cmd3
clai suggest --limit=2
git status
git log --oneline
npm test
make build
clai suggest git
echo freq_reason_seed
echo freq_reason_seed
echo freq_reason_seed
echo freq_reason_other
clai suggest echo --format=json --limit=5
echo confidence_seed_alpha
echo confidence_seed_beta
clai suggest echo --format=json --limit=3
git status
clai suggest git --format=json --limit=3
git status
git log --oneline
clai suggest git --format=fzf --limit=3
docker ps -a
npm install
git status
kubectl get pods -n production
clai suggest kubectl --format=fzf --limit=3
echo 'searchable_unique_term_abc123'
git status
clai search 'searchable_unique'
echo 'repo_specific_search_term'
clai search --repo 'repo_specific'
echo search_limit_1
echo search_limit_2
echo search_limit_3
echo search_limit_4
echo search_limit_5
clai search --limit 2 'search_limit'
echo json_search_test
clai search --json 'json_search'
clai search 'xyznonexistent98765'
echo 'test-with-dashes_and_underscores'
clai search 'test-with-dashes'
mkdir -p /tmp/node-test-project
cd /tmp/node-test-project
echo '{"scripts":{"test":"jest","build":"tsc"}}' > package.json
cd /tmp/node-test-project && clai suggest
mkdir -p /tmp/make-test-project
cd /tmp/make-test-project
printf 'build:\n\techo build\ntest:\n\techo test\n' > Makefile
cd /tmp/make-test-project && clai suggest
mkdir -p /tmp/reason-test
cd /tmp/reason-test
echo '{"scripts":{"lint":"eslint"}}' > package.json
cd /tmp/reason-test && clai suggest npm --format=json --limit=5
curl -s http://localhost:8765/debug/scores 2>/dev/null || clai debug scores
curl -s 'http://localhost:8765/debug/scores?limit=5' 2>/dev/null || clai debug scores --limit=5
git status
git add .
git commit -m 'test'
curl -s http://localhost:8765/debug/transitions 2>/dev/null || clai debug transitions
mkdir -p /tmp/debug-task-test
cd /tmp/debug-task-test
echo '{"scripts":{"test":"jest"}}' > package.json
clai suggest
curl -s http://localhost:8765/debug/tasks 2>/dev/null || clai debug tasks
curl -s http://localhost:8765/debug/discovery-errors 2>/dev/null || clai debug discovery-errors
curl -s http://localhost:8765/debug/cache 2>/dev/null | jq . || echo 'curl failed'
clai incognito on
clai incognito on
echo 'ephemeral_session_cmd_12345'
clai suggest
clai incognito on
echo 'incognito_only_cmd_xyz'
echo 'incognito_only_cmd_xyz'
echo 'incognito_only_cmd_xyz'
clai incognito off
clai suggest --format=json
export CLAI_EPHEMERAL=1
echo 'ephemeral_env_test_cmd'
unset CLAI_EPHEMERAL
clai suggest --format=json
echo api_json_seed_one
echo api_json_seed_two
clai suggest echo --format=json --limit=5
echo 'setup command 1'
echo 'setup command 2'
ls -la
command one
command two
command three
echo 'selected command'
selected
echo unique_session_test_command_12345
echo session_specific_command
echo this_is_a_very_long_command_that_should_be_truncated_in_the_middle_with_an_ellipsis_indicator_to_show_both_start_and_end
echo UNIQUE_START_marker_this_is_a_very_long_command_with_lots_of_text_in_the_middle_UNIQUE_END_marker
UNIQUE_START
echo CLIPBOARD_START_this_is_a_very_long_command_for_clipboard_test_CLIPBOARD_END
source ~/.clai/shell/bash.sh
echo $CLAI_SESSION_ID
echo first
echo second
echo third
history
clai status
echo test_history_cmd_1
echo test_history_cmd_2
clai history --session=$CLAI_SESSION_ID
clai history --global --limit 10
cd /tmp && echo from_tmp_dir
cd ~ && echo from_home_dir
if clai history --cwd=/tmp | grep -q from_tmp_dir && ! clai history --cwd=/tmp | grep -q from_home_dir; then echo HISTORY_CWD_FILTER_PASS; else echo HISTORY_CWD_FILTER_FAIL; fi
echo success_command
false
if clai history --status=success | grep -q success_command && ! clai history --status=success | grep -q '^false$'; then echo HISTORY_SUCCESS_FILTER_PASS; else echo HISTORY_SUCCESS_FILTER_FAIL; fi
echo will_succeed
false
if clai history --status=failure | grep -q '^false$' && ! clai history --status=failure | grep -q will_succeed; then echo HISTORY_FAILURE_FILTER_PASS; else echo HISTORY_FAILURE_FILTER_FAIL; fi
clai off
clai off
clai on
echo 'hello world'
ls --color=always
sleep 60
`list all files in current directory
echo 'this is a very long command that should wrap properly in the terminal without causing any display issues or breaking the shell integration features'
echo "hello 'world' $HOME"
mkdir -p "/tmp/path'with\"quotes"
cd "/tmp/path'with\"quotes"
echo 'special path command'
clai incognito on
clai incognito on
echo 'SECRET_INCOGNITO_COMMAND_12345'
clai incognito off
if clai history --global | grep -q SECRET_INCOGNITO; then echo INCOGNITO_PERSISTED_FAIL; else echo INCOGNITO_PERSISTED_PASS; fi
clai incognito on
clai incognito off
export CLAI_NO_RECORD=1
echo 'NO_RECORD_TEST_CMD'
unset CLAI_NO_RECORD
if clai history --session=$CLAI_SESSION_ID | grep -q NO_RECORD_TEST_CMD; then echo NO_RECORD_FAIL; else echo NO_RECORD_PASS; fi
true
if clai history --session=$CLAI_SESSION_ID --format json | grep -q '"exit_code":0'; then echo EXIT_SUCCESS_PASS; else echo EXIT_SUCCESS_FAIL; fi
false
if clai history --session=$CLAI_SESSION_ID --format json | grep -q '"exit_code":1'; then echo EXIT_FAILURE_PASS; else echo EXIT_FAILURE_FAIL; fi
cd /tmp && echo 'ingestion_cwd_test'
clai history --cwd=/tmp --global
echo "hello | world" > /dev/null && echo 'done'
clai history --session=$CLAI_SESSION_ID
echo '日本語 émoji 🎉'
clai history --session=$CLAI_SESSION_ID
clai daemon status
clai daemon start
clai daemon status
clai daemon start
clai daemon stop
clai daemon status
clai daemon restart
clai daemon status
echo suggest_text_seed_one
echo suggest_text_seed_two
clai suggest echo --limit=3
echo suggest_json_seed_one
echo suggest_json_seed_two
clai suggest echo --format=json --limit=3
docker ps
clai suggest --format=fzf
cmd1
cmd2
cmd3
clai suggest --limit=2
git status
git log --oneline
npm test
make build
clai suggest git
echo freq_reason_seed
echo freq_reason_seed
echo freq_reason_seed
echo freq_reason_other
clai suggest echo --format=json --limit=5
echo confidence_seed_alpha
echo confidence_seed_beta
clai suggest echo --format=json --limit=3
git status
clai suggest git --format=json --limit=3
git status
git log --oneline
clai suggest git --format=fzf --limit=3
docker ps -a
npm install
git status
kubectl get pods -n production
clai suggest kubectl --format=fzf --limit=3
echo 'searchable_unique_term_abc123'
git status
clai search 'searchable_unique'
echo 'repo_specific_search_term'
clai search --repo 'repo_specific'
echo search_limit_1
echo search_limit_2
echo search_limit_3
echo search_limit_4
echo search_limit_5
clai search --limit 2 'search_limit'
echo json_search_test
clai search --json 'json_search'
clai search 'xyznonexistent98765'
echo 'test-with-dashes_and_underscores'
clai search 'test-with-dashes'
mkdir -p /tmp/node-test-project
cd /tmp/node-test-project
echo '{"scripts":{"test":"jest","build":"tsc"}}' > package.json
cd /tmp/node-test-project && clai suggest
mkdir -p /tmp/make-test-project
cd /tmp/make-test-project
printf 'build:\n\techo build\ntest:\n\techo test\n' > Makefile
cd /tmp/make-test-project && clai suggest
mkdir -p /tmp/reason-test
cd /tmp/reason-test
echo '{"scripts":{"lint":"eslint"}}' > package.json
cd /tmp/reason-test && clai suggest npm --format=json --limit=5
curl -s http://localhost:8765/debug/scores 2>/dev/null || clai debug scores
curl -s 'http://localhost:8765/debug/scores?limit=5' 2>/dev/null || clai debug scores --limit=5
