#!/usr/bin/env bash
# certharbor-console is the one public origin. A host port on certharbor-control is a
# second path to the backend with no rate limit, no body ceiling and a peer
# outside the trusted-proxy list, so the same request would record a different
# client depending on which way it came in. Nothing breaks when one is added,
# which is why this check exists.
#
#   check-backend-port.sh            every compose file, as compose merges it
#   check-backend-port.sh --running  the running stack: no binding on the
#                                    backend, and the origin answers
set -euo pipefail

cd "$(dirname "$0")/.."

status=0

static_check() {
  local files=(docker-compose.yml compose.e2e-*.yml)
  for f in "${files[@]}"; do
    local args=(-f docker-compose.yml)
    [[ "$f" == docker-compose.yml ]] || args+=(-f "$f")
    local ports
    ports=$(docker compose "${args[@]}" config --format json --no-interpolate | jq -c '.services["certharbor-control"].ports // empty')
    if [[ -n "$ports" ]]; then
      echo "$f: certharbor-control publishes $ports; publish on certharbor-console only"
      status=1
    fi
    # certharbor-delivery has no HTTP listener at all (plans/distribution.md
    # Section 2), so it must never carry a ports: entry either.
    local delivery_ports
    delivery_ports=$(docker compose "${args[@]}" config --format json --no-interpolate | jq -c '.services["certharbor-delivery"].ports // empty')
    if [[ -n "$delivery_ports" ]]; then
      echo "$f: certharbor-delivery publishes $delivery_ports; it has no HTTP listener and must publish nothing"
      status=1
    fi
  done
  if [[ $status -eq 0 ]]; then
    echo "backend port: no compose file publishes a host port for certharbor-control or certharbor-delivery (${#files[@]} checked)"
  fi
}

running_check() {
  local bindings
  bindings=$(docker inspect -f '{{json .NetworkSettings.Ports}}' certharbor-control | jq -c '[.[]? | select(. != null) | .[]]')
  if [[ "$bindings" != "[]" ]]; then
    echo "certharbor-control: running with host port bindings $bindings"
    status=1
  fi

  local cfg host port
  cfg=$(docker compose config --format json)
  host=$(jq -r '.services["certharbor-console"].environment.WEB_HOSTNAME' <<<"$cfg")
  port=$(jq -r '.services["certharbor-console"].ports[0].published' <<<"$cfg")
  # The origin refuses any SNI but WEB_HOSTNAME, so the name is pinned to
  # loopback rather than resolved.
  origin() { curl -sk --max-time 10 --resolve "$host:$port:127.0.0.1" "$@"; }

  local want path code
  for want in "/app/ 200" "/app/certificates/1 200" "/login 200" "/static/css/pages.css 200" "/api/v1/session 200" "/health 200" "/ready 200"; do
    path=${want% *}
    code=$(origin -o /dev/null -w '%{http_code}' "https://$host:$port$path")
    if [[ "$code" != "${want#* }" ]]; then
      echo "https://$host:$port$path: $code, want ${want#* }"
      status=1
    fi
  done
  if ! origin "https://$host:$port/app/" | grep -q '<div id="root">'; then
    echo "https://$host:$port/app/ is not the SPA shell"
    status=1
  fi
  if [[ $status -eq 0 ]]; then
    echo "preview: certharbor-control has no host binding, and https://$host:$port serves the SPA under /app/, the server-rendered pages and the API"
  fi
}

case "${1:-}" in
  "") static_check ;;
  --running) running_check ;;
  *) echo "usage: $0 [--running]" >&2; exit 2 ;;
esac
exit "$status"
