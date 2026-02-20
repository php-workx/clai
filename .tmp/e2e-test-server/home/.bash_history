echo 'setup command 1'
echo 'setup command 2'
ls -la
clai status
echo $CLAI_SESSION_ID
clai daemon status
echo first_hist_test
echo second_hist_test
echo third_hist_test
clear
history
clear
echo 'this is a very long command that should wrap properly in the terminal without causing any display issues or breaking the shell integration features'
clear
echo "hello 'world' test123"
clear
clai history --global --limit 10
clear
true
clai history --session=$CLAI_SESSION_ID --json | tail -1
clear
false
clai history --session=$CLAI_SESSION_ID --json | tail -1
clear
clai off
clear
clai on
clear
clai history --global --limit 10
clear
echo investigate_test_cmd
clai history --session=$CLAI_SESSION_ID --json 2>&1 | tail -5
clear
clai history --json --limit 3 2>&1
clear
clai history --global --limit 10
clear
true
clai history --session=$CLAI_SESSION_ID --format json --limit 3 2>&1 | tail -3
clear
false
clai history --session=$CLAI_SESSION_ID --format json --limit 1 2>&1
clear
echo test_history_session_cmd_1
echo test_history_session_cmd_2
clear
clai history --session=$CLAI_SESSION_ID
clear
cd /tmp && echo 'ingestion_cwd_test_xyz' && cd -
clai history --cwd=/tmp --global
clear
echo "hello | world" > /dev/null && echo done_special
clai history --session=$CLAI_SESSION_ID
clear
echo 'unicode_test_日本語'
clai history --session=$CLAI_SESSION_ID
clear
echo SESSION=$CLAI_SESSION_ID
clear
clai history --help
clear
echo debug_session_test_xyz
clai history --session=$CLAI_SESSION_ID 2>&1
clear
clai history --global --limit 5 2>&1
clear
clai history --limit 5 2>&1
clear
clai history --global --limit 10
clear
echo retest_session_cmd_A
echo retest_session_cmd_B
clear
clai history --session=$CLAI_SESSION_ID
clear
true
clai history --format json --limit 1 2>&1
clear
false
clai history --format json --limit 1 2>&1
clear
echo success_filter_test
false
clai history --status=success --global 2>&1
clear
echo will_succeed_test
false
clai history --status=failure --global 2>&1
echo 'picker_apple_cmd'
echo 'picker_banana_cmd'
echo 'picker_apricot_cmd'
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
clai cmd 'list all files in current directory'
clai cmd 'show git log with one line per commit'
clai suggest
clai ask 'How do I find large files?'
clai ask --context 'error: permission denied' 'What went wrong?'
clai status
echo test_history_cmd_1
echo test_history_cmd_2
clai history --session=$CLAI_SESSION_ID
clai history --global --limit 10
cd /tmp && echo from_tmp_dir
cd ~ && echo from_home_dir
clai history --cwd=/tmp --global
echo success_command
false
clai history --status=success --global
echo will_succeed
false
clai history --status=failure --global
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
clai history --global | grep SECRET_INCOGNITO
clai incognito on
clai incognito off
export CLAI_NO_RECORD=1
echo 'NO_RECORD_TEST_CMD'
unset CLAI_NO_RECORD
clai history --session=$CLAI_SESSION_ID
true
clai history --session=$CLAI_SESSION_ID --json | tail -1
false
clai history --session=$CLAI_SESSION_ID --json | tail -1
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
echo setup1
git status
make build
npm test
clai suggest --format=json
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
git add .
git commit -m 'test'
make build
git status
clai suggest --format=json
git status
clai suggest --format=json
git status
git log --oneline
docker ps -a
npm install
git status
kubectl get pods -n production
git status
git log --oneline -1
gti status
clai suggest
xyzabc123nonexistent
clai suggest
echo common_cmd
echo common_cmd
echo common_cmd
echo rare_cmd
commo_cmd
clai suggest
kubectl get pods -n production
kubectl get pods -n production
kubectl get pods -n staging
clai suggest kubectl
docker run -it alpine
docker run -it alpine
docker run -it alpine
docker run -it ubuntu
clai suggest 'docker run'
cd /tmp && docker run -it ubuntu && cd -
docker run -it alpine
docker run -it alpine
clai suggest --format=json 'docker run'
echo cache_warmup_cmd
clai suggest --format=json
echo cache_test_cmd
clai suggest --format=json
clai suggest --format=json
echo warmup
clai suggest --format=json
echo new_command
clai suggest --format=json
curl -s http://localhost:8765/debug/cache 2>/dev/null || clai debug cache
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
cd /tmp/reason-test && clai suggest --format=json
mkdir -p /tmp/rust-test-project
cd /tmp/rust-test-project
echo '[package]\nname = "test"\nversion = "0.1.0"' > Cargo.toml
cd /tmp/rust-test-project && clai suggest
mkdir -p /tmp/python-test-project
cd /tmp/python-test-project
echo '[project.scripts]\ntest = "pytest"' > pyproject.toml
cd /tmp/python-test-project && clai suggest
curl -s http://localhost:8765/debug/scores 2>/dev/null || clai debug scores
curl -s 'http://localhost:8765/debug/scores?limit=5' 2>/dev/null || clai debug scores --limit=5
git status
git add .
git commit -m 'test'
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
git status
make build
echo warmup
clai suggest --format=json
echo warmup
clai suggest --format=json
