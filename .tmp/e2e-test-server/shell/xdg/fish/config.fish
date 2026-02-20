set -gx PATH "/Users/runger/.claude-worktrees/clai/spec-reviews/bin" $PATH
set -gx CLAI_HOME "/Users/runger/.claude-worktrees/clai/spec-reviews/.tmp/e2e-test-server/clai"
set -gx CLAI_DISABLE_UPDATE_CHECK 1
set -gx CLAI_AUTO_EXTRACT true
set -gx TERM "xterm-256color"
"/Users/runger/.claude-worktrees/clai/spec-reviews/bin/clai" init fish | source
function fish_prompt
    echo -n 'TEST> '
end
