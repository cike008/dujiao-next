#!/usr/bin/env bash

set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
mkdir -p "$TMP/bin" "$TMP/backups"
cat > "$TMP/site.original" <<'FIXTURE'
server { listen 80; server_name share.aimosh.com; location / { return 301 https://$host$request_uri; } }
server {
  listen 443 ssl; server_name share.aimosh.com;
  location / { proxy_pass http://127.0.0.1:8083; }
}
FIXTURE
cp "$TMP/site.original" "$TMP/site.conf"
cat > "$TMP/bin/id" <<'MOCK'
#!/usr/bin/env bash
echo 0
MOCK
cat > "$TMP/bin/nginx" <<'MOCK'
#!/usr/bin/env bash
if [[ ${FAIL_CANDIDATE:-0} == 1 ]] && grep -q 'add_header' "$SITE_FILE"; then exit 1; fi
MOCK
cat > "$TMP/bin/systemctl" <<'MOCK'
#!/usr/bin/env bash
[[ "$*" == 'reload nginx' ]]
MOCK
chmod 0700 "$TMP/bin/"*
export SITE_FILE="$TMP/site.conf" BACKUP_ROOT="$TMP/backups"
PATH="$TMP/bin:$PATH" bash "$ROOT/scripts/harden-storefront-headers.sh" > "$TMP/dry-run.txt"
cmp "$TMP/site.original" "$SITE_FILE"
PATH="$TMP/bin:$PATH" bash "$ROOT/scripts/harden-storefront-headers.sh" --apply > "$TMP/apply.txt"
grep -q 'Content-Security-Policy-Report-Only' "$SITE_FILE"
grep -q 'frame-ancestors' "$SITE_FILE"
grep -q 'max-age=300' "$SITE_FILE"
if grep -q 'includeSubDomains\|preload' "$SITE_FILE"; then exit 1; fi
if PATH="$TMP/bin:$PATH" bash "$ROOT/scripts/harden-storefront-headers.sh" --apply > /dev/null 2>&1; then
  echo 'FAIL: existing headers were not rejected' >&2; exit 1
fi
cp "$TMP/site.original" "$SITE_FILE"
if PATH="$TMP/bin:$PATH" FAIL_CANDIDATE=1 bash "$ROOT/scripts/harden-storefront-headers.sh" --apply > "$TMP/rollback.txt" 2>&1; then
  echo 'FAIL: invalid candidate was accepted' >&2; exit 1
fi
cmp "$TMP/site.original" "$SITE_FILE"
echo 'PASS: dry-run, header generation, inheritance guard, and rollback'

if command -v docker > /dev/null && docker image inspect nginx:alpine > /dev/null 2>&1; then
  candidate="$(find "$BACKUP_ROOT" -name site.candidate -type f | head -1)"
  {
    printf 'events {}\nhttp { server { listen 8080;\n'
    awk '/^[[:space:]]*add_header/ {print}' "$candidate"
    printf 'location / { return 200 "ok"; }\n} }\n'
  } | docker run --rm -i --network none nginx:alpine sh -ec 'cat > /tmp/headers-test.conf; nginx -t -c /tmp/headers-test.conf'
  echo 'PASS: real Nginx header syntax'
else
  echo 'SKIP: real Nginx syntax test requires the existing nginx:alpine Docker image'
fi
