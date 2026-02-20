#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
STATE_DIR="${TEST_SERVER_STATE_DIR:-$ROOT_DIR/.tmp/e2e-test-server}"
TEST_SHELL="${TEST_SHELL:-${E2E_SHELL:-bash}}"
ADDRESS="${ADDRESS:-127.0.0.1}"
PORT="${PORT:-8080}"
TERM_VALUE="${TERM_VALUE:-xterm-256color}"

mkdir -p "$STATE_DIR"
PID_FILE="$STATE_DIR/gotty.pid"
LOG_FILE="$STATE_DIR/gotty.log"
RUNTIME_FILE="$STATE_DIR/runtime.env"
HOME_DIR="$STATE_DIR/home"
CLAI_HOME="${CLAI_HOME:-$STATE_DIR/clai}"
SHELL_DIR="$STATE_DIR/shell"

if ! command -v gotty >/dev/null 2>&1; then
	echo "error: gotty is not installed or not in PATH" >&2
	echo "hint: go install github.com/sorenisanerd/gotty@latest" >&2
	exit 1
fi

if [[ -x "$ROOT_DIR/bin/clai" ]]; then
	CLAI_CMD="$ROOT_DIR/bin/clai"
elif command -v clai >/dev/null 2>&1; then
	CLAI_CMD="$(command -v clai)"
else
	echo "error: clai binary not found (expected ./bin/clai or PATH)" >&2
	echo "hint: run 'make build' first" >&2
	exit 1
fi

if [[ -f "$PID_FILE" ]]; then
	pid="$(cat "$PID_FILE" 2>/dev/null || true)"
	if [[ -n "$pid" ]] && kill -0 "$pid" 2>/dev/null; then
		echo "test server already running (pid=$pid)"
		echo "url: http://$ADDRESS:$PORT"
		exit 0
	fi
	rm -f "$PID_FILE"
fi

mkdir -p "$HOME_DIR" "$CLAI_HOME" "$SHELL_DIR"
export HOME="$HOME_DIR"
export CLAI_HOME

write_bash_rc() {
	local rc_file="$1"
	cat >"$rc_file" <<EOF
export PATH="$ROOT_DIR/bin:\$PATH"
export CLAI_HOME="$CLAI_HOME"
export CLAI_DISABLE_UPDATE_CHECK=1
export CLAI_AUTO_EXTRACT=true
export TERM="$TERM_VALUE"
eval "\$("$CLAI_CMD" init bash)"
PS1='TEST> '
EOF
}

write_zsh_rc() {
	local zdir="$1"
	mkdir -p "$zdir"
	cat >"$zdir/.zshrc" <<EOF
export PATH="$ROOT_DIR/bin:\$PATH"
export CLAI_HOME="$CLAI_HOME"
export CLAI_DISABLE_UPDATE_CHECK=1
export CLAI_AUTO_EXTRACT=true
export TERM="$TERM_VALUE"
eval "\$("$CLAI_CMD" init zsh)"
PROMPT='TEST> '
EOF
}

write_fish_config() {
	local xdg_config_home="$1"
	local fish_dir="$xdg_config_home/fish"
	mkdir -p "$fish_dir"
	cat >"$fish_dir/config.fish" <<EOF
set -gx PATH "$ROOT_DIR/bin" \$PATH
set -gx CLAI_HOME "$CLAI_HOME"
set -gx CLAI_DISABLE_UPDATE_CHECK 1
set -gx CLAI_AUTO_EXTRACT true
set -gx TERM "$TERM_VALUE"
"$CLAI_CMD" init fish | source
function fish_prompt
    echo -n 'TEST> '
end
EOF
}

declare -a shell_cmd
case "$TEST_SHELL" in
bash)
	BASH_RC="$SHELL_DIR/bashrc"
	write_bash_rc "$BASH_RC"
	shell_cmd=(bash --noprofile --rcfile "$BASH_RC" -i)
	;;
zsh)
	ZDOTDIR_PATH="$SHELL_DIR/zdotdir"
	write_zsh_rc "$ZDOTDIR_PATH"
	shell_cmd=(env ZDOTDIR="$ZDOTDIR_PATH" zsh -i)
	;;
fish)
	XDG_CONFIG_HOME_PATH="$SHELL_DIR/xdg"
	write_fish_config "$XDG_CONFIG_HOME_PATH"
	shell_cmd=(env XDG_CONFIG_HOME="$XDG_CONFIG_HOME_PATH" fish --interactive)
	;;
*)
	echo "error: unsupported TEST_SHELL '$TEST_SHELL' (expected bash|zsh|fish)" >&2
	exit 1
	;;
esac

: >"$LOG_FILE"
cd "$ROOT_DIR"
nohup gotty -w -a "$ADDRESS" -p "$PORT" --title-format "clai e2e ($TEST_SHELL)" "${shell_cmd[@]}" >>"$LOG_FILE" 2>&1 < /dev/null &
pid="$!"
echo "$pid" >"$PID_FILE"

ready=0
for _ in $(seq 1 80); do
	if ! kill -0 "$pid" 2>/dev/null; then
		echo "error: gotty exited unexpectedly; see $LOG_FILE" >&2
		exit 1
	fi
	if grep -q "HTTP server is listening at:" "$LOG_FILE" 2>/dev/null; then
		ready=1
		break
	fi
	sleep 0.25
done

if [[ "$ready" -ne 1 ]]; then
	echo "error: timed out waiting for test server on http://$ADDRESS:$PORT" >&2
	echo "see $LOG_FILE for details" >&2
	exit 1
fi

cat >"$RUNTIME_FILE" <<EOF
PID=$pid
TEST_SHELL=$TEST_SHELL
ADDRESS=$ADDRESS
PORT=$PORT
URL=http://$ADDRESS:$PORT
STATE_DIR=$STATE_DIR
CLAI_HOME=$CLAI_HOME
LOG_FILE=$LOG_FILE
EOF

echo "test server started"
echo "shell: $TEST_SHELL"
echo "url: http://$ADDRESS:$PORT"
echo "pid: $pid"
echo "log: $LOG_FILE"
