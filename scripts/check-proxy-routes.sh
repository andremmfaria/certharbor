#!/usr/bin/env bash
# The proxied path list is one routing table used twice: nginx's proxy
# location in production and Vite's dev proxy in development. Both must come
# from frontend/proxy-prefixes.json, or a new prefix works in one environment
# and 404s in the other.
set -euo pipefail

cd "$(dirname "$0")/.."

routes=frontend/proxy-prefixes.json
alternation=$(jq -r '[.[] | ltrimstr("/") | gsub("\\."; "\\.")] | join("|")' "$routes")
want="location ~ ^/($alternation)(/|\$) {"
status=0

if ! grep -qF "$want" frontend/nginx.conf.template; then
  echo "frontend/nginx.conf.template: proxy location does not match $routes; want: $want"
  status=1
fi
if ! grep -qF "proxy-prefixes.json" frontend/vite/proxy.ts; then
  echo "frontend/vite/proxy.ts: does not import $routes, so the dev proxy list is written by hand"
  status=1
fi
if ! grep -qE "proxy: *devProxy\(" frontend/vite.config.ts; then
  echo "frontend/vite.config.ts: server.proxy is not built by devProxy from $routes"
  status=1
fi

# The SPA's base path is one setting in vite.config.ts. nginx's static blocks,
# the image's bundle directory and the file the entrypoint hashes for the CSP
# must all name the same path, or the shell loads with no assets or no hashes.
base=$(sed -nE "s/^[[:space:]]*base: '([^']*)',.*/\1/p" frontend/vite.config.ts)
if [[ -z "$base" ]]; then
  echo "frontend/vite.config.ts: no base: '...' line"
  status=1
else
  for want in "location ${base} {" "location ${base}assets/ {" "try_files \$uri ${base}index.html;"; do
    if ! grep -qF "$want" frontend/nginx.conf.template; then
      echo "frontend/nginx.conf.template: SPA base $base needs: $want"
      status=1
    fi
  done
  if [[ $(grep -cF "/usr/share/nginx/html${base}" frontend/Dockerfile) -ne 2 ]]; then
    echo "frontend/Dockerfile: both bundles must be copied to /usr/share/nginx/html${base}"
    status=1
  fi
  if ! grep -qF "INDEX_HTML=/usr/share/nginx/html${base}index.html" frontend/entrypoint.sh; then
    echo "frontend/entrypoint.sh: INDEX_HTML must be the index.html served under $base"
    status=1
  fi
fi

if [[ $status -eq 0 ]]; then
  echo "proxy routes: nginx and vite both derive from $routes, and the SPA base $base agrees across vite, nginx, the image and the entrypoint"
fi
exit "$status"
