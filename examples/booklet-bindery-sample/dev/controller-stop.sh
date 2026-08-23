#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PIDFILE="$SCRIPT_DIR/.controller.pid"

if [[ ! -f "$PIDFILE" ]]; then
  echo "No controller pidfile found."
  exit 0
fi

pid="$(cat "$PIDFILE" 2>/dev/null || true)"
rm -f "$PIDFILE"

if [[ -z "${pid:-}" ]]; then
  echo "Controller pidfile was empty."
  exit 0
fi

if kill -0 "$pid" 2>/dev/null; then
  echo "Stopping controller (pid=$pid)..."
  kill "$pid" || true
  for _ in $(seq 1 20); do
    if kill -0 "$pid" 2>/dev/null; then
      sleep 0.2
      continue
    fi
    echo "Controller stopped."
    exit 0
  done
  echo "Controller still running; sending SIGKILL."
  kill -9 "$pid" || true
fi

echo "Controller not running."

