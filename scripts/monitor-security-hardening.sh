#!/usr/bin/env bash

set -euo pipefail

OPS_DIR="${OPS_DIR:-/home/deploy/ops/dujiao-security}"
VERIFY_SCRIPT="${VERIFY_SCRIPT:-$OPS_DIR/verify-security-hardening-v1.4.8.sh}"
LOG_FILE="$OPS_DIR/security-monitor.log"
FAILURE_FILE="$OPS_DIR/security-monitor.failed"
LOCK_FILE="$OPS_DIR/security-monitor.lock"
MAX_LOG_LINES="${MAX_LOG_LINES:-2000}"

mkdir -p "$OPS_DIR"
exec 9>"$LOCK_FILE"
flock -n 9 || exit 0

output_file="$(mktemp)"
trimmed_log="$(mktemp)"
cleanup() {
  rm -f "$output_file" "$trimmed_log"
}
trap cleanup EXIT

started_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
if bash "$VERIFY_SCRIPT" >"$output_file" 2>&1; then
  {
    echo "[$started_at] status=ok"
    cat "$output_file"
  } >>"$LOG_FILE"
  rm -f "$FAILURE_FILE"
else
  status=$?
  {
    echo "[$started_at] status=failed exit_code=$status"
    cat "$output_file"
  } >>"$LOG_FILE"
  cp "$output_file" "$FAILURE_FILE"
  logger -t dujiao-security-monitor "security verification failed; see $FAILURE_FILE" || true
fi

tail -n "$MAX_LOG_LINES" "$LOG_FILE" >"$trimmed_log"
mv "$trimmed_log" "$LOG_FILE"
