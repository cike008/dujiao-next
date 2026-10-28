#!/usr/bin/env bash
set -euo pipefail

SOURCE_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
REMOTE_HOST="${REMOTE_HOST:-dujiao-vps}"
REV="$(git -C "$SOURCE_DIR" rev-parse --short=8 HEAD)"
IMAGE_TAG="dujiaonext/dujiao-next:teamgenie-v1.5.1-security-$REV"
APP_VERSION="v1.5.1-teamgenie-security-$REV"
REMOTE_PATH="/home/deploy/services/dujiao-next-v1.5.1-$REV"

if [[ $(git -C "$SOURCE_DIR" status --porcelain | wc -l) -ne 0 ]] ||
   [[ -f "$(git -C "$SOURCE_DIR" rev-parse --git-path MERGE_HEAD)" ]]; then
  echo 'Commit the reviewed merge and scripts before building a release image.' >&2
  exit 1
fi
git -C "$SOURCE_DIR" merge-base --is-ancestor v1.5.1 HEAD
echo "Building $IMAGE_TAG from committed source $REV (no production switch)."
git -C "$SOURCE_DIR" archive --format=tar.gz HEAD | ssh "$REMOTE_HOST" \
  "set -eu; umask 077; test ! -e '$REMOTE_PATH'; mkdir -p '$REMOTE_PATH'; tar -xzf - -C '$REMOTE_PATH'"
ssh "$REMOTE_HOST" "cd '$REMOTE_PATH' && docker build --build-arg APP_VERSION='$APP_VERSION' -t '$IMAGE_TAG' ."
ssh "$REMOTE_HOST" "docker image inspect '$IMAGE_TAG' --format 'image={{.RepoTags}} id={{.Id}}'"
echo "image=$IMAGE_TAG"
echo "expected_version=$APP_VERSION"
