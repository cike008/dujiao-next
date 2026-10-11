#!/usr/bin/env bash

set -euo pipefail
umask 077

SITE_FILE="${SITE_FILE:-/etc/nginx/sites-available/share.aimosh.com}"
BACKUP_ROOT="${BACKUP_ROOT:-/opt/backups}"
STAMP="$(date -u +%Y%m%d%H%M%S)"
BACKUP_DIR="$BACKUP_ROOT/dujiao-next-browser-headers-$STAMP"
APPLY=0
case "${1:-}" in
  ""|--dry-run) ;;
  --apply) APPLY=1 ;;
  *) echo "Usage: $0 [--dry-run|--apply]" >&2; exit 2 ;;
esac
[[ $# -le 1 ]] || exit 2
if [[ $(id -u) -ne 0 ]]; then
  echo "Run with sudo/root; default mode only prepares a candidate." >&2
  exit 1
fi
test -f "$SITE_FILE"
if [[ -L "$SITE_FILE" || -L "$BACKUP_ROOT" || "$BACKUP_ROOT" != /* || "$BACKUP_ROOT" == / ]]; then
  echo "Refusing unsafe target or backup root." >&2
  exit 1
fi
if grep -Eq 'add_header|include[[:space:]]' "$SITE_FILE"; then
  echo "Existing headers/includes require manual review before changing header inheritance." >&2
  exit 1
fi
install -d -m 0700 "$BACKUP_DIR"
install -m 0600 "$SITE_FILE" "$BACKUP_DIR/site.before"
candidate="$BACKUP_DIR/site.candidate"
awk '
  { print }
  /listen 443 ssl; server_name share\.aimosh\.com;/ {
    matches++
    print "  # Browser hardening: CSP is observation-only; HSTS excludes subdomains."
    print "  add_header X-Content-Type-Options nosniff always;"
    print "  add_header X-Frame-Options SAMEORIGIN always;"
    print "  add_header Referrer-Policy strict-origin-when-cross-origin always;"
    print "  add_header Strict-Transport-Security \"max-age=300\" always;"
    print "  add_header Content-Security-Policy \"frame-ancestors '\''self'\''\" always;"
    print "  add_header Content-Security-Policy-Report-Only \"default-src '\''self'\''; script-src '\''self'\''; style-src '\''self'\'' '\''unsafe-inline'\''; img-src '\''self'\'' data: https:; font-src '\''self'\'' data:; connect-src '\''self'\'' https: wss:; frame-src '\''self'\'' https:; object-src '\''none'\''; base-uri '\''self'\''; form-action '\''self'\'' https:\" always;"
  }
  END { if (matches != 1) exit 1 }
' "$SITE_FILE" > "$candidate"
echo "backup_dir=$BACKUP_DIR"
echo "candidate=$candidate"
if [[ $APPLY -eq 0 ]]; then
  echo "Dry run completed; live Nginx configuration was not changed."
  exit 0
fi

changed=0
rollback() {
  status=$?
  if [[ $status -ne 0 && $changed -eq 1 ]]; then
    echo "Validation/reload failed; restoring the original site configuration." >&2
    cp "$BACKUP_DIR/site.before" "$SITE_FILE"
    if nginx -t; then systemctl reload nginx || true; fi
  fi
}
trap rollback EXIT
nginx -t
changed=1
cp "$candidate" "$SITE_FILE"
nginx -t
systemctl reload nginx
changed=0
trap - EXIT
echo "Browser headers deployed. CSP reports are visible in the browser console; no report collector is configured."
