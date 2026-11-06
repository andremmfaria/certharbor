#!/usr/bin/env bash
# The client version is the project version from VERSION plus the commit it was
# generated from, so a consumer can prove which commit a client came from.
set -euo pipefail

cd "$(dirname "$0")/.."

VERSION=$(tr -d '[:space:]' < VERSION)

if SHA=$(git rev-parse --short HEAD 2>/dev/null) && [[ -n "$SHA" ]]; then
  echo "${VERSION}-sha.${SHA}"
else
  echo "${VERSION}-dev"
fi
