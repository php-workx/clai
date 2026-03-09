#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
STATE_DIR="${TEST_SERVER_STATE_DIR:-$ROOT_DIR/.tmp/e2e-test-server}"
PID_FILE="$STATE_DIR/gotty.pid"
RUNTIME_FILE="$STATE_DIR/runtime.env"

status_only=0
if [[ "${1:-}" == "--status" ]]; then
	status_only=1
fi

if [[ ! -f "$PID_FILE" ]]; then
	if [[ "$status_only" -eq 1 ]]; then
		echo "test server: stopped"
		exit 0
	fi
	echo "test server not running"
	exit 0
fi

pid="$(cat "$PID_FILE" 2>/dev/null || true)"
if [[ -z "$pid" ]]; then
	rm -f "$PID_FILE"
	echo "test server pid file was empty; cleaned up"
	exit 0
fi

if ! kill -0 "$pid" 2>/dev/null; then
	rm -f "$PID_FILE"
	echo "test server: stale pid file removed"
	exit 0
fi

if [[ "$status_only" -eq 1 ]]; then
	url="(unknown)"
	if [[ -f "$RUNTIME_FILE" ]]; then
		url="$(grep '^URL=' "$RUNTIME_FILE" | sed 's/^URL=//' || true)"
	fi
	echo "test server: running (pid=$pid, url=$url)"
	exit 0
fi

kill "$pid" 2>/dev/null || true
for _ in $(seq 1 30); do
	if ! kill -0 "$pid" 2>/dev/null; then
		break
	fi
	sleep 0.1
done

if kill -0 "$pid" 2>/dev/null; then
	kill -9 "$pid" 2>/dev/null || true
fi

rm -f "$PID_FILE"
echo "test server stopped (pid=$pid)"
