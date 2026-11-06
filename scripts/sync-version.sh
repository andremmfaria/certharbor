#!/usr/bin/env bash
# VERSION is the single source of the project version. Without arguments this
# writes it into every file that carries a version. With --check it changes
# nothing and fails if any of them disagrees.
set -euo pipefail

cd "$(dirname "$0")/.."

VERSION=$(tr -d '[:space:]' < VERSION)
[[ "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo "VERSION must be MAJOR.MINOR.PATCH, got '$VERSION'" >&2; exit 1; }

CHECK=0
[[ "${1:-}" == "--check" ]] && CHECK=1
FAILED=0

report() {
  if [[ "$2" != "$VERSION" ]]; then
    echo "$1 is at '$2', VERSION is '$VERSION'" >&2
    FAILED=1
  fi
}

GO_FILE=backend/handlers/handler.go
SPEC=backend/openapi/openapi.json
PACKAGES=(clients/ts frontend test/e2e)

if [[ "$CHECK" == 1 ]]; then
  report "$GO_FILE" "$(sed -nE 's/^[[:space:]]*Version[[:space:]]*=[[:space:]]*"([^"]*)".*/\1/p' "$GO_FILE")"
  report "$SPEC" "$(jq -r '.info.version' "$SPEC")"
  for dir in "${PACKAGES[@]}"; do
    report "$dir/package.json" "$(jq -r '.version' "$dir/package.json")"
    report "$dir/package-lock.json" "$(jq -r '.version' "$dir/package-lock.json")"
    report "$dir/package-lock.json (root package)" "$(jq -r '.packages[""].version' "$dir/package-lock.json")"
  done
  [[ "$FAILED" == 0 ]] || { echo "Run scripts/sync-version.sh to fix." >&2; exit 1; }
  echo "every version matches VERSION ($VERSION)"
  exit 0
fi

sed -i -E "s/^([[:space:]]*Version[[:space:]]*=[[:space:]]*)\"[^\"]*\"/\1\"$VERSION\"/" "$GO_FILE"
for dir in "${PACKAGES[@]}"; do
  (cd "$dir" && npm version "$VERSION" --no-git-tag-version --allow-same-version >/dev/null)
done
(cd backend && go run ./cmd/openapi -out openapi/openapi.json)
echo "synced to $VERSION"
