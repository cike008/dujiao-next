#!/usr/bin/env bash

set -euo pipefail

SOURCE_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
REMOTE_HOST="${REMOTE_HOST:-dujiao-vps}"
REMOTE_PATH="${REMOTE_PATH:-/home/deploy/services/dujiao-next-api-custom/}"
IMAGE_TAG="${IMAGE_TAG:-dujiaonext/api:teamgenie-sync}"
COMMIT_TAG="${COMMIT_TAG:-}"

if [[ ! -f "$SOURCE_DIR/go.mod" || ! -f "$SOURCE_DIR/Dockerfile" ]]; then
  echo "Source directory does not look like a dujiao-next API tree: $SOURCE_DIR" >&2
  exit 1
fi

if [[ -z "$COMMIT_TAG" ]] && command -v git >/dev/null 2>&1; then
  COMMIT_TAG="$(git -C "$SOURCE_DIR" rev-parse --short HEAD 2>/dev/null || true)"
fi

echo "Syncing API source:"
echo "  local : $SOURCE_DIR"
echo "  remote: ${REMOTE_HOST}:${REMOTE_PATH}"
rsync -az --delete \
  --exclude '.git/' \
  --exclude '.github/' \
  --exclude '.DS_Store' \
  --exclude '.gocache/' \
  --exclude '.gomodcache/' \
  "$SOURCE_DIR/" "${REMOTE_HOST}:${REMOTE_PATH}"

build_cmd="cd ${REMOTE_PATH%/} && docker build -t ${IMAGE_TAG}"
if [[ -n "$COMMIT_TAG" ]]; then
  build_cmd="${build_cmd} -t ${IMAGE_TAG}-${COMMIT_TAG}"
fi
build_cmd="${build_cmd} ."

echo "Building image on $REMOTE_HOST:"
echo "  $build_cmd"
ssh "$REMOTE_HOST" "$build_cmd"

echo "Recreating API service:"
ssh "$REMOTE_HOST" "cd /opt/dujiao-next && docker compose up -d --force-recreate dujiaonext-api"

echo "Checking API health:"
ssh "$REMOTE_HOST" "sleep 20; docker inspect dujiaonext-api --format 'api_image={{.Config.Image}} status={{.State.Status}} health={{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}'; curl -fsSL http://127.0.0.1:8081/health; echo"

