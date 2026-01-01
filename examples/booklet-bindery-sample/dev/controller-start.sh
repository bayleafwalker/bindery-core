#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../../.." && pwd)"

PIDFILE="$SCRIPT_DIR/.controller.pid"
LOGFILE="$SCRIPT_DIR/.controller.log"

if [[ -f "$PIDFILE" ]]; then
  pid="$(cat "$PIDFILE" 2>/dev/null || true)"
  if [[ -n "${pid:-}" ]] && kill -0 "$pid" 2>/dev/null; then
    echo "Controller already running (pid=$pid)."
    exit 0
  fi
  rm -f "$PIDFILE"
fi

cd "$REPO_ROOT"

echo "Starting controller manager (logs: $LOGFILE)"

if command -v nohup >/dev/null 2>&1; then
  nohup make run-controller >"$LOGFILE" 2>&1 &
else
  make run-controller >"$LOGFILE" 2>&1 &
fi

echo $! >"$PIDFILE"
echo "Controller started (pid=$(cat "$PIDFILE"))."

