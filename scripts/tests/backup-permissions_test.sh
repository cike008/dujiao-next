#!/usr/bin/env bash

set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
mkdir -p "$TMP/backups/dujiao-next-test/nested" "$TMP/backups/unrelated" \
  "$TMP/app/security-rollout-test" "$TMP/app/cutover-vtest" "$TMP/app/config" "$TMP/bin"
printf 'fixture\n' > "$TMP/backups/dujiao-next-test/nested/postgres.dump"
printf 'fixture\n' > "$TMP/outside"
printf 'fixture\n' > "$TMP/app/config/config.yml"
printf 'fixture\n' > "$TMP/app/config/config.yml.bak-test"
printf 'fixture\n' > "$TMP/app/security-rollout-test/docker-compose.yml.before"
chmod 0755 "$TMP/backups/dujiao-next-test" "$TMP/backups/dujiao-next-test/nested"
chmod 0644 "$TMP/backups/dujiao-next-test/nested/postgres.dump" "$TMP/outside" "$TMP/app/config/"*
ln -s "$TMP/outside" "$TMP/backups/dujiao-next-test/link"
export BACKUP_ROOT="$TMP/backups" APP_DIR="$TMP/app"

mode() {
  if stat -c '%a' "$1" >/dev/null 2>&1; then stat -c '%a' "$1"; else stat -f '%Lp' "$1"; fi
}

# This helper uses GNU find on the production Linux host.
if ! find "$TMP" -maxdepth 0 -printf '' >/dev/null 2>&1; then
  echo 'SKIP: GNU find is required; run this test on Linux.'
  exit 0
fi
bash "$ROOT/scripts/harden-backup-permissions.sh" > "$TMP/dry-run.txt"
[[ $(mode "$TMP/backups/dujiao-next-test/nested/postgres.dump") == 644 ]]
printf '#!/usr/bin/env bash\nprintf "0\\n"\n' > "$TMP/bin/id"
chmod 0700 "$TMP/bin/id"
PATH="$TMP/bin:$PATH" bash "$ROOT/scripts/harden-backup-permissions.sh" --apply > "$TMP/apply.txt"
[[ $(mode "$TMP/backups/dujiao-next-test") == 700 ]]
[[ $(mode "$TMP/backups/dujiao-next-test/nested") == 700 ]]
[[ $(mode "$TMP/backups/dujiao-next-test/nested/postgres.dump") == 600 ]]
[[ $(mode "$TMP/app/security-rollout-test/docker-compose.yml.before") == 600 ]]
[[ $(mode "$TMP/app/config/config.yml.bak-test") == 600 ]]
[[ $(mode "$TMP/app/config/config.yml") == 644 ]]
[[ $(mode "$TMP/outside") == 644 ]]
[[ -L "$TMP/backups/dujiao-next-test/link" ]]
if BACKUP_ROOT=/ bash "$ROOT/scripts/harden-backup-permissions.sh" > /dev/null 2>&1; then
  echo 'FAIL: unsafe root was accepted' >&2
  exit 1
fi
printf '#!/usr/bin/env bash\nprintf "1000\\n"\n' > "$TMP/bin/id"
if PATH="$TMP/bin:$PATH" bash "$ROOT/scripts/harden-backup-permissions.sh" --apply > /dev/null 2>&1; then
  echo 'FAIL: non-root apply was accepted' >&2
  exit 1
fi
echo 'PASS: private archives, dry-run, symlink safety, live config preservation, and root guard'

mkdir -p "$TMP/deploy/config" "$TMP/deploy-backup"
printf 'services:\n  backend:\n    image: fixture-old\n' > "$TMP/deploy/docker-compose.yml"
printf 'fixture\n' > "$TMP/deploy/.env"
printf 'fixture\n' > "$TMP/deploy/config/config.yml"
chmod 0644 "$TMP/deploy/".env "$TMP/deploy/config/config.yml"
printf 'old fixture\n' > "$TMP/deploy-backup/postgres.dump"
chmod 0755 "$TMP/deploy-backup"
chmod 0644 "$TMP/deploy-backup/postgres.dump"
cat > "$TMP/bin/docker" <<'MOCK'
#!/usr/bin/env bash
set -euo pipefail
case "$*" in
  *'pg_dump'*) printf 'fixture dump\n' ;;
  *'pg_restore'*) cat > /dev/null ;;
  *'Config.Image'*) printf 'fixture-new\n' ;;
  'inspect '*) printf 'healthy\n' ;;
esac
MOCK
cat > "$TMP/bin/curl" <<'MOCK'
#!/usr/bin/env bash
printf '{"status":"ok","app_version":"fixture-version"}\n'
MOCK
chmod 0700 "$TMP/bin/docker" "$TMP/bin/curl"
PATH="$TMP/bin:$PATH" APP_DIR="$TMP/deploy" BACKUP_DIR="$TMP/deploy-backup" \
  OLD_IMAGE=fixture-old NEW_IMAGE=fixture-new EXPECTED_VERSION=fixture-version \
  bash "$ROOT/scripts/deploy-security-hardening-v1.4.9.sh" > "$TMP/deploy.txt"
[[ $(mode "$TMP/deploy-backup") == 700 ]]
[[ $(mode "$TMP/deploy-backup/postgres.dump") == 600 ]]
[[ $(mode "$TMP/deploy-backup/.env") == 600 ]]
[[ $(mode "$TMP/deploy/.env") == 644 ]]
[[ $(find "$TMP/deploy-backup" -perm /077 -printf x) == '' ]]
[[ $(find "$TMP/deploy" -name 'security-rollout-*' -exec find {} -perm /077 -printf x \;) == '' ]]
echo 'PASS: deployment backup and rollout permissions, including existing files'

for script in backup-production-v1.4.9-preflight.sh prepare-v1.4.9-production-cutover.sh patch-v1.4.9-production-config.sh; do
  awk '/^#!/ {if (started) print; next} /<<\x27REMOTE\x27$/ {started=1; next} /^REMOTE$/ {started=0} started {print}' \
    "$ROOT/scripts/$script" > "$TMP/embedded.sh"
  bash -n "$TMP/embedded.sh"
  grep -qx 'umask 077' "$TMP/embedded.sh"
done
echo 'PASS: uploaded helper syntax and restrictive umask'
