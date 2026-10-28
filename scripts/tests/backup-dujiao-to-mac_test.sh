#!/usr/bin/env bash
# Local fixture test; SSH and rsync are replaced, no remote host is contacted.
set -euo pipefail
[[ $(uname) == Darwin ]] || exit 1
script="$(cd "$(dirname "$0")/.." && pwd)/backup-dujiao-to-mac.sh"
work=$(mktemp -d)
trap 'rm -rf -- "$work"' EXIT
mkdir -p "$work/bin" "$work/source/db" "$work/source/config" "$work/source/uploads" "$work/tree/opt/dujiao-next/config"
printf 'fixture\n' >"$work/tree/opt/dujiao-next/config/config.yml"
touch "$work/tree/opt/dujiao-next/.env" "$work/tree/opt/dujiao-next/docker-compose.yml"
tar czf "$work/source/config/dujiao-config-fixture.tar.gz" -C "$work/tree" opt/dujiao-next
printf 'database fixture\n' >"$work/source/db/postgres-fixture.dump"
shasum -a 256 "$work/source/db/postgres-fixture.dump" >"$work/source/db/postgres-fixture.dump.sha256"
shasum -a 256 "$work/source/config/dujiao-config-fixture.tar.gz" >"$work/source/config/dujiao-config-fixture.tar.gz.sha256"
cat >"$work/bin/rsync" <<'MOCK'
#!/bin/bash
args=("$@")
src=${args[${#args[@]}-2]}
dest=${args[${#args[@]}-1]}
cp -pR "${src#*:}". "$dest"
MOCK
chmod +x "$work/bin/rsync"
export PATH="$work/bin:$PATH" PG_RESTORE=/usr/bin/true
export REMOTE_DB_DIR="$work/source/db" REMOTE_CONFIG_DIR="$work/source/config" REMOTE_UPLOADS="$work/source/uploads"
export BACKUP_DEST="$work/dest"
bash "$script"
grep -q '"ok":true' "$BACKUP_DEST/status.json"
touch -t 202001010000 "$work/source/config/dujiao-config-fixture.tar.gz"
if bash "$script"; then echo 'Stale config accepted' >&2; exit 1; fi
grep -q '"ok":false' "$BACKUP_DEST/status.json"
touch "$work/source/config/dujiao-config-fixture.tar.gz"
printf 'corruption' >>"$work/source/config/dujiao-config-fixture.tar.gz"
if bash "$script"; then echo 'Corrupt config accepted' >&2; exit 1; fi
rm "$work/source/config/dujiao-config-fixture.tar.gz.sha256" "$BACKUP_DEST/config/dujiao-config-fixture.tar.gz.sha256"
if bash "$script"; then echo 'Missing checksum accepted' >&2; exit 1; fi
echo 'PASS: valid fixture, stale configuration, corrupted configuration, missing checksum'
