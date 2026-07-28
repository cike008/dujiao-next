#!/usr/bin/env bash

set -euo pipefail

SOURCE_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
REMOTE_HOST="${REMOTE_HOST:-dujiao-vps}"
REMOTE_PATH="${REMOTE_PATH:-/home/deploy/services/dujiao-next-v1.4.1-teamgenie/}"
IMAGE_TAG="${IMAGE_TAG:-dujiaonext/dujiao-next:teamgenie-v1.4.1}"
APP_VERSION="${APP_VERSION:-v1.4.1-teamgenie}"

if [[ ! -f "$SOURCE_DIR/go.mod" || ! -f "$SOURCE_DIR/Dockerfile" || ! -d "$SOURCE_DIR/frontend/admin" ]]; then
  echo "Source directory does not look like a v1.4 fullstack Dujiao-Next tree: $SOURCE_DIR" >&2
  exit 1
fi

echo "Syncing fullstack source:"
echo "  local : $SOURCE_DIR"
echo "  remote: ${REMOTE_HOST}:${REMOTE_PATH}"
ssh "$REMOTE_HOST" "mkdir -p '${REMOTE_PATH%/}'"
rsync -az --delete \
  --exclude '.git/' \
  --exclude '.github/' \
  --exclude '.DS_Store' \
  --exclude 'server' \
  --exclude '.gocache/' \
  --exclude '.gomodcache/' \
  "$SOURCE_DIR/" "${REMOTE_HOST}:${REMOTE_PATH}"

echo "Building fullstack image on $REMOTE_HOST:"
echo "  image: ${IMAGE_TAG}"
echo "  app_version: ${APP_VERSION}"
ssh "$REMOTE_HOST" "cd ${REMOTE_PATH%/} && docker build --build-arg APP_VERSION='${APP_VERSION}' -t '${IMAGE_TAG}' ."

echo "Image build completed:"
ssh "$REMOTE_HOST" "docker image inspect '${IMAGE_TAG}' --format 'image={{.RepoTags}} id={{.Id}} created={{.Created}} size={{.Size}}'"
