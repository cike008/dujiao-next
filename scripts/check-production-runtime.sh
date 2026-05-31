#!/usr/bin/env bash

set -euo pipefail

HOST="${1:-dujiao-vps}"
SSH="ssh $HOST"

echo "=== Host: $HOST ==="

echo
echo "--- compose ps ---"
$SSH "cd /opt/dujiao-next && docker compose ps"

echo
echo "--- env tag ---"
$SSH "cd /opt/dujiao-next && sed -n '1,20p' .env"

echo
echo "--- image versions ---"
$SSH "docker inspect dujiaonext-api --format 'api_image={{.Config.Image}} status={{.State.Status}} health={{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}' && docker inspect dujiaonext-admin --format 'admin_image={{.Config.Image}} status={{.State.Status}}'"

echo
echo "--- public version ---"
$SSH "curl -fsSL http://127.0.0.1:8081/api/v1/public/config | grep -o '\"app_version\":\"[^\"]*\"' || true"

echo
echo "--- api health ---"
$SSH "curl -fsSL http://127.0.0.1:8081/health"

echo
echo "--- admin health ---"
$SSH "curl -fsSI http://127.0.0.1:8082/ | sed -n '1,8p'"

echo
echo "--- site connections ---"
$SSH "docker exec -i dujiaonext-postgres psql -U dujiao -d dujiao -t -A -F '|' -c \"select id, name, status, last_ping_ok, last_ping_at from site_connections order by id;\""

echo
echo "--- teamgenie health ---"
$SSH "docker exec teamgenie-sync wget -qO- http://127.0.0.1:8788/health"

echo
echo "--- recent upstream errors ---"
$SSH "docker exec dujiaonext-api sh -lc \"tail -n 300 /app/logs/app.log 2>/dev/null | grep -Ei 'invalid_api_key|upstream_request_error|sync_connection_stock|worker_upstream_sync_stock_failed' | tail -n 80 || true\""

echo
echo "--- recent callback/teamgenie/fulfillment logs ---"
$SSH "docker logs --tail=300 dujiaonext-api 2>&1 | grep -E 'callback|teamgenie_sync|fulfillment' | tail -n 80 || true"

