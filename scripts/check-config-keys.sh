#!/usr/bin/env bash
# Every key the web origin introduced must have a row in the configuration
# reference: the operator is whoever self-hosts this repository and cannot
# ask what a key does. A row is a table line starting with the key in
# backticks.
set -euo pipefail

cd "$(dirname "$0")/.."

table=docs/configuration.md
keys=(WEB_TLS_MODE WEB_HOSTNAME WEB_UPSTREAM_HOST TRUST_PROXY TRUSTED_PROXY_CIDRS
  STEP_NET_SUBNET WEB_HSTS_MAX_AGE PUBLIC_BASE_URL IMAGE_TAG)
missing=0
for key in "${keys[@]}"; do
  if ! grep -qE "^\| \`$key\` \|" "$table"; then
    echo "$table: no configuration-table row for $key"
    missing=1
  fi
  if ! grep -qE "^#? ?$key=" .env.example; then
    echo ".env.example: no line for $key"
    missing=1
  fi
done
if [[ $missing -eq 0 ]]; then
  echo "config keys: all ${#keys[@]} have a row in $table and a line in .env.example"
fi
exit "$missing"
