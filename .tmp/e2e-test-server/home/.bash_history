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
