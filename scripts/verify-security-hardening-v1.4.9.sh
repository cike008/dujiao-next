#!/usr/bin/env bash

set -euo pipefail

EXPECTED_IMAGE="${EXPECTED_IMAGE:-dujiaonext/dujiao-next:teamgenie-v1.4.9-security-20261006}"
EXPECTED_VERSION="${EXPECTED_VERSION:-v1.4.9-teamgenie-security-20261006}"
LOCAL_BASE_URL="${LOCAL_BASE_URL:-http://127.0.0.1:8081}"
PUBLIC_BASE_URL="${PUBLIC_BASE_URL:-https://share.aimosh.com}"
ORDER_NO="DJ000000000000000000000000"
EMAIL="security-audit-invalid@example.com"
PASSWORD="invalid-password-20260930"
TOKEN="$(printf '%s\n%s' "$EMAIL" "$PASSWORD" | openssl base64 -A | tr '+/' '-_' | tr -d '=')"
TMP_DIR="$(mktemp -d)"

cleanup() {
  rm -rf "$TMP_DIR"
}
trap cleanup EXIT

request_guest_order() {
  local name=$1
  local url=$2
  local headers="$TMP_DIR/$name.headers"
  local body="$TMP_DIR/$name.body"
  local status

  status="$(curl -sS --max-time 15 -D "$headers" -o "$body" -w '%{http_code}' \
    -H "Authorization: Guest $TOKEN" "$url/api/v1/guest/orders/$ORDER_NO")"

  # The API uses HTTP 200 envelopes and carries the domain status in JSON.
  [[ "$status" == "200" ]]
  grep -Fq '"status_code":404' "$body"
  grep -Eiq '^Cache-Control:.*no-store' "$headers"
  grep -Eiq '^Pragma:[[:space:]]*no-cache' "$headers"
  grep -Eiq '^Referrer-Policy:[[:space:]]*no-referrer' "$headers"
  ! grep -Fq "$EMAIL" "$body"
  ! grep -Fq "$PASSWORD" "$body"
  echo "$name=ok status=$status no_store=yes"
}

echo "== runtime =="
runtime_image="$(docker inspect dujiaonext --format '{{.Config.Image}}')"
runtime_health="$(docker inspect dujiaonext --format '{{.State.Health.Status}}')"
[[ "$runtime_image" == "$EXPECTED_IMAGE" ]]
[[ "$runtime_health" == "healthy" ]]
curl -fsS --max-time 10 "$LOCAL_BASE_URL/health" | grep -q '"status":"ok"'
curl -fsS --max-time 10 "$LOCAL_BASE_URL/api/v1/public/config" | grep -Fq "\"app_version\":\"$EXPECTED_VERSION\""
echo "image=$runtime_image"
echo "health=$runtime_health"
echo "version=$EXPECTED_VERSION"

echo "== storefront =="
curl -fsS --max-time 15 "$PUBLIC_BASE_URL/" >/dev/null
echo "frontend=ok"

echo "== guest order protections =="
request_guest_order local "$LOCAL_BASE_URL"
request_guest_order public "$PUBLIC_BASE_URL"

echo "== backup =="
latest_backup="$(find /opt/backups -maxdepth 1 -type d -name 'dujiao-next-security-*' -printf '%T@ %p\n' | sort -nr | head -1 | cut -d' ' -f2-)"
test -n "$latest_backup"
test -s "$latest_backup/postgres.dump"
docker exec -i dujiaonext-postgres sh -ec 'pg_restore --list >/dev/null' <"$latest_backup/postgres.dump"
echo "backup_restore_list=ok"
echo "backup_dir=$latest_backup"

echo "Security hardening verification completed."
