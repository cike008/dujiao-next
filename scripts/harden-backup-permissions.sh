#!/usr/bin/env bash

set -euo pipefail
umask 077

BACKUP_ROOT="${BACKUP_ROOT:-/opt/backups}"
APP_DIR="${APP_DIR:-/opt/dujiao-next}"
APPLY=0
case "${1:-}" in
  ""|--dry-run) ;;
  --apply) APPLY=1 ;;
  *) echo "Usage: $0 [--dry-run|--apply]" >&2; exit 2 ;;
esac
if [[ $# -gt 1 ]]; then
  echo "Too many arguments." >&2
  exit 2
fi
if [[ $APPLY -eq 1 && $(id -u) -ne 0 ]]; then
  echo "Run with sudo/root to apply backup permissions." >&2
  exit 1
fi

for root in "$BACKUP_ROOT" "$APP_DIR"; do
  if [[ "$root" != /* || "$root" == / || "$root" == */../* || "$root" == */.. || -L "$root" ]]; then
    echo "Refusing unsafe archive root: $root" >&2
    exit 1
  fi
done

harden_archive() {
  local archive="$1"
  echo "archive=$archive"
  # Do not follow symlinks or cross into another mounted filesystem.
  if [[ $APPLY -eq 1 ]]; then
    find -P "$archive" -xdev -type d -exec chmod 0700 -- {} +
    find -P "$archive" -xdev -type f -exec chmod 0600 -- {} +
  fi
  local exposed
  exposed="$(find -P "$archive" -xdev \( -type d -o -type f \) -perm /077 -printf 'x\n' | wc -l)"
  echo "group_or_other_access_entries=$exposed"
  if [[ $APPLY -eq 1 && $exposed -ne 0 ]]; then
    echo "Permission verification failed: $archive" >&2
    exit 1
  fi
}

LIST_FILE="$(mktemp)"
trap 'rm -f "$LIST_FILE"' EXIT
echo "mode=$([[ $APPLY -eq 1 ]] && echo apply || echo dry-run)"
if [[ -d "$BACKUP_ROOT" ]]; then
  find -P "$BACKUP_ROOT" -mindepth 1 -maxdepth 1 -type d -name 'dujiao-next*' -print0 >> "$LIST_FILE"
fi
if [[ -d "$APP_DIR" ]]; then
  find -P "$APP_DIR" -mindepth 1 -maxdepth 1 -type d \( -name 'security-rollout-*' -o -name 'cutover-v*' \) -print0 >> "$LIST_FILE"
fi
while IFS= read -r -d '' archive; do
  harden_archive "$archive"
done < "$LIST_FILE"
if [[ -d "$APP_DIR" ]]; then
  if [[ -d "$APP_DIR/config" && ! -L "$APP_DIR/config" ]]; then
    find -P "$APP_DIR/config" -mindepth 1 -maxdepth 1 -type f -name 'config.yml.bak-*' -print0 > "$LIST_FILE"
    while IFS= read -r -d '' backup; do
      if [[ $APPLY -eq 1 ]]; then
        chmod 0600 -- "$backup"
      fi
      echo "config_backup=$backup"
    done < "$LIST_FILE"
  fi
fi
echo "Only archive permissions were processed; live configuration and database were not changed."
