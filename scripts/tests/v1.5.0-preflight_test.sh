#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
mkdir -p "$TMP/bin"

for script in backup-production-v1.5.0-preflight.sh prepare-v1.5.0-staging.sh resume-v1.5.0-staging.sh start-v1.5.0-staging-api.sh check-v1.5.0-production-cutover.sh build-v1.5.0-teamgenie-image-remote.sh; do
  bash -n "$ROOT/scripts/$script"
done
printf '#!/usr/bin/env bash\nprintf "1000\\n"\n' > "$TMP/bin/id"
chmod 700 "$TMP/bin/id"
for script in backup-production-v1.5.0-preflight.sh prepare-v1.5.0-staging.sh resume-v1.5.0-staging.sh start-v1.5.0-staging-api.sh check-v1.5.0-production-cutover.sh; do
  if PATH="$TMP/bin:$PATH" bash "$ROOT/scripts/$script" > "$TMP/output" 2>&1; then
    echo "FAIL: $script accepted a non-root caller" >&2
    exit 1
  fi
  grep -q 'Run with sudo' "$TMP/output"
done
printf '#!/usr/bin/env bash\nprintf "0\\n"\n' > "$TMP/bin/id"
if PATH="$TMP/bin:$PATH" bash "$ROOT/scripts/prepare-v1.5.0-staging.sh" /opt/dujiao-next fixture fixture > "$TMP/output" 2>&1; then
  echo 'FAIL: staging accepted a production path or invalid image' >&2
  exit 1
fi
grep -q 'Usage:' "$TMP/output"

STAGE="$ROOT/scripts/prepare-v1.5.0-staging.sh"
BACKUP="$ROOT/scripts/backup-production-v1.5.0-preflight.sh"
grep -qx 'umask 077' "$STAGE"
grep -qx 'umask 077' "$BACKUP"
grep -q 'internal: true' "$STAGE"
if grep -q 'ports:' "$STAGE"; then
  echo 'FAIL: isolated staging publishes host ports' >&2
  exit 1
fi
grep -A1 '^queue:' "$STAGE" | grep -q 'enabled: false'
grep -A1 '^teamgenie_sync:' "$STAGE" | grep -q 'enabled: false'
grep -A1 '^email:' "$STAGE" | grep -q 'enabled: false'
grep -q -- '--single-transaction --exit-on-error' "$STAGE"
grep -q 'refusing to overwrite or delete' "$STAGE"
if grep -Eq 'rm -rf|docker compose down|config.yml.*cp|cp .*config.yml' "$STAGE"; then
  echo 'FAIL: staging includes destructive reset or production config copy' >&2
  exit 1
fi
grep -q 'pg_restore --list' "$BACKUP"
grep -q 'sha256sum --check' "$BACKUP"
grep -q 'COMPLETE' "$BACKUP"
grep -q 'pg_isready -h 127.0.0.1' "$STAGE"
RESUME="$ROOT/scripts/resume-v1.5.0-staging.sh"
grep -q 'pg_isready -h 127.0.0.1' "$RESUME"
grep -q 'refusing to overwrite' "$RESUME"
grep -q 'com.docker.compose.project.working_dir' "$RESUME"
grep -q -- '--single-transaction --exit-on-error' "$RESUME"
grep -Fq 'command: ["./dujiao-next", "-mode", "api"]' "$STAGE"
START="$ROOT/scripts/start-v1.5.0-staging-api.sh"
grep -Fq 'command: ["./dujiao-next", "-mode", "api"]' "$START"
grep -q 'wget -qO- -T 10 http://127.0.0.1:8080/health' "$START"
if grep -q 'curl.*18081' "$START"; then
  echo 'FAIL: health checks depend on unpublished host ports' >&2
  exit 1
fi
if grep -Eq 'pg_restore|rm -rf' "$START"; then
  echo 'FAIL: API startup helper modifies or restores the database' >&2
  exit 1
fi
echo 'PASS: syntax, root/path guards, private backups, isolated staging, fail-fast restore'
