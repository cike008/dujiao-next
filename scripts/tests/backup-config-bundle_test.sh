#!/usr/bin/env bash
# Run as root inside a disposable Linux container, never on a production host.
set -euo pipefail
[[ -f /.dockerenv && $(id -u) == 0 ]] || exit 1
script="$(cd "$(dirname "$0")/.." && pwd)/backup-config-bundle.sh"
work=$(mktemp -d)
trap 'rm -rf -- "$work"' EXIT
chmod 755 "$work"
useradd -M backupfixture
mkdir -p "$work/app/config" "$work/output"
printf 'fixture: true\n' >"$work/app/config/config.yml"
touch "$work/app/.env" "$work/app/docker-compose.yml"
chmod 700 "$work/app"
chown backupfixture "$work/output"
run_bundle() {
  APP_DIR="$work/app" OUT_DIR="$work/output" OWNER=backupfixture KEEP=1 bash "$script"
}
run_bundle
bundle=$(find "$work/output" -name '*.tar.gz' -type f)
test "$(stat -c %U "$bundle")" = backupfixture
test "$(stat -c %a "$bundle")" = 600
(cd "$work/output" && sha256sum -c "$(basename "$bundle").sha256")
tar tzf "$bundle" >"$work/list"
grep -Fx "${work#/}/app/config/config.yml" "$work/list"
# A recipient-controlled destination symlink must be replaced, not followed.
printf 'unchanged\n' >"$work/sentinel"
chmod 600 "$work/sentinel"
for offset in $(seq 0 15); do
  name="dujiao-config-$(date -u -d "+$offset seconds" +%Y%m%d%H%M%S).tar.gz"
  rm -f -- "$work/output/$name"
  ln -s "$work/sentinel" "$work/output/$name"
done
run_bundle
test "$(cat "$work/sentinel")" = unchanged
if APP_DIR="$work/app" OUT_DIR="$work/output" OWNER=backupfixture KEEP=0 bash "$script"; then
  echo 'Invalid retention unexpectedly accepted' >&2
  exit 1
fi
echo 'PASS: private source, unprivileged publication, checksum, modes, symlink target preserved, invalid retention rejected'
