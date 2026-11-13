#!/usr/bin/env bash
# Assertions run as bash -c '...' with positional arguments on purpose.
# shellcheck disable=SC2016,SC2001
# make nginx-lint: the fast rung under nginx.conf. It builds only the fixture
# target of frontend/Dockerfile (the real config and entrypoint over a
# committed placeholder bundle, no Node stage), runs it against a stub
# upstream with throwaway certificates on a throwaway network, and asserts the
# routing, the header set, the cache policy and the failure documents. No
# postgres, no step-ca, no compose project.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
lint_dir="$repo_root/frontend/nginx-lint"
image="certharbor-console:nginx-lint"
stub_image="nginx:1.29-alpine"
run_id="nginx-lint-$$"
net="$run_id"
host="web.test"

work="$(mktemp -d)"
containers=()
failures=0

cleanup() {
  for c in "${containers[@]}"; do
    docker rm -f "$c" >/dev/null 2>&1 || true
  done
  docker network rm "$net" >/dev/null 2>&1 || true
  rm -rf "$work"
}
trap cleanup EXIT

pass() { printf '  ok    %s\n' "$*"; }
fail() { printf '  FAIL  %s\n' "$*"; failures=$((failures + 1)); }
check() {
  local what="$1"
  shift
  if "$@"; then pass "$what"; else fail "$what"; fi
}

# ── Throwaway PKI ───────────────────────────────────────────────────────────
gen_root() {
  openssl req -x509 -nodes -newkey ec -pkeyopt ec_paramgen_curve:P-256 -days 2 \
    -subj "/CN=$2" -keyout "$1.key" -out "$1.crt" 2>/dev/null
}
gen_leaf() { # out-prefix root-prefix dns-name
  openssl req -nodes -newkey ec -pkeyopt ec_paramgen_curve:P-256 \
    -subj "/CN=$3" -keyout "$1.key" -out "$1.csr" 2>/dev/null
  printf 'subjectAltName=DNS:%s\nextendedKeyUsage=serverAuth\n' "$3" >"$1.ext"
  openssl x509 -req -in "$1.csr" -CA "$2.crt" -CAkey "$2.key" -CAcreateserial \
    -days 2 -extfile "$1.ext" -out "$1.crt" 2>/dev/null
}

pki="$work/pki"
mkdir -p "$pki" "$work/web" "$work/stub" "$work/untrusted-stub"
gen_root "$pki/root" "nginx-lint root"
gen_root "$pki/evil" "nginx-lint untrusted root"
gen_leaf "$pki/web" "$pki/root" "$host"
gen_leaf "$pki/web2" "$pki/root" "$host"
gen_leaf "$pki/upstream" "$pki/root" "certharbor-control"
gen_leaf "$pki/evil-upstream" "$pki/evil" "certharbor-control"
cp "$pki/web.crt" "$work/web/web.crt"
cp "$pki/web.key" "$work/web/web.key"
cp "$pki/root.crt" "$work/web/root_ca.crt"
# Three 4 KiB cookies, the most a redirect sets at once, under an API prefix
# so nginx actually proxies the request to this stub location.
printf 'location = /api/v1/login-big-headers {\n  add_header Set-Cookie "a=%s";\n  add_header Set-Cookie "b=%s";\n  add_header Set-Cookie "c=%s";\n  return 303 /api/v1/x;\n}\n' \
  "$(head -c 3000 /dev/zero | tr '\0' a)" "$(head -c 3000 /dev/zero | tr '\0' b)" "$(head -c 3000 /dev/zero | tr '\0' c)" \
  >"$work/stub/big-headers.conf"
cp "$pki/upstream.crt" "$work/stub/upstream.crt"
cp "$pki/upstream.key" "$work/stub/upstream.key"
cp "$pki/evil-upstream.crt" "$work/untrusted-stub/upstream.crt"
cp "$pki/evil-upstream.key" "$work/untrusted-stub/upstream.key"
chmod -R a+rX "$work"

# ── Static checks: the shared prefix source and the template ────────────────
echo "static"
routes="$repo_root/frontend/proxy-prefixes.json"
template="$repo_root/frontend/nginx.conf.template"
alternation=$(jq -r '[.[] | ltrimstr("/") | gsub("\\."; "\\.")] | join("|")' "$routes")
want_regex="^/($alternation)(/|\$)"
check "proxy location regex is derived from frontend/proxy-prefixes.json ($want_regex)" \
  grep -qF "location ~ $want_regex {" "$template"
# The SPA owns the root: exactly one proxy_pass location exists, the prefix
# regex above, and the config names no base path under the document root,
# which left with the migration window.
check "exactly one proxy_pass location, the prefix regex" test "$(grep -cE '^[[:space:]]*proxy_pass ' "$template")" -eq 1
check "the config names no SPA base path" bash -c '! grep -qF "html/app" "$1"' _ "$template"

# ── Images and network ──────────────────────────────────────────────────────
docker build -q --target nginx-fixture -t "$image" -f "$repo_root/frontend/Dockerfile" "$repo_root" >/dev/null
# The budget is the checks', not a cold runner's image pulls.
started=$(date +%s)
docker network create "$net" >/dev/null

start_stub() { # cert-dir
  containers+=("$run_id-stub")
  docker run -d --name "$run_id-stub" --network "$net" --network-alias certharbor-control \
    -v "$1:/stub:ro" -v "$lint_dir/stub-upstream.conf:/etc/nginx/nginx.conf:ro" \
    "$stub_image" >/dev/null
}

start_web() { # name cert-dir extra-docker-args...
  local name="$1" dir="$2"
  shift 2
  containers+=("$name")
  docker run -d --name "$name" --network "$net" -p 127.0.0.1::8443 \
    -e WEB_HOSTNAME="$host" -v "$dir:/etc/certharbor/ssl" "$@" "$image" >/dev/null
}

port_of() { docker port "$1" 8443/tcp | head -n1 | sed 's/.*://'; }

wait_healthy() { # name
  for _ in $(seq 1 40); do
    if [[ "$(docker inspect -f '{{.State.Health.Status}}' "$1" 2>/dev/null)" == healthy ]]; then
      return 0
    fi
    if [[ "$(docker inspect -f '{{.State.Running}}' "$1")" != true ]]; then
      docker logs "$1" >&2
      return 1
    fi
    sleep 0.5
  done
  docker logs "$1" >&2
  return 1
}

start_stub "$work/stub"
start_web "$run_id-web" "$work/web" -e WEB_TLS_MODE=provided --health-interval=1s
wait_healthy "$run_id-web"
port=$(port_of "$run_id-web")

req() { # extra curl args..., last is the path
  local path="${*: -1}"
  curl -sS --max-time 5 --resolve "$host:$port:127.0.0.1" --cacert "$pki/root.crt" \
    "${@:1:$#-1}" "https://$host:$port$path"
}
status_of() { req -o /dev/null -w '%{http_code}' "$@"; }
headers_of() { req -o /dev/null -D - "$@" | tr -d '\r'; }
header() { # name headers-text
  printf '%s\n' "$2" | awk -v n="$(printf '%s' "$1" | tr '[:upper:]' '[:lower:]')" \
    'BEGIN{FS=": "} tolower($1)==n {sub(/^[^:]*: /,""); print; exit}'
}

# ── The dumped config ───────────────────────────────────────────────────────
echo "nginx -T"
dump=$(docker exec "$run_id-web" nginx -T 2>/dev/null)
directives=$(sed 's/#.*//' <<<"$dump")
locations_ok() {
  printf '%s\n' "$dump" | awk '
    /^# configuration file / { infile = ($4 == "/etc/nginx/nginx.conf:"); next }
    !infile { next }
    {
      line = $0; sub(/#.*/, "", line)
      if (!inloc && line ~ /^[ \t]*location[ \t]/) { inloc = 1; depth = 0; pp = 0; inc = 0; ver = 0; name = line }
      if (inloc) {
        if (line ~ /proxy_pass[ \t]/) pp = 1
        if (line ~ /proxy_ssl_verify[ \t]+on;/) ver = 1
        if (line ~ /include[ \t]+conf\.d\/proxy-headers\.conf;/) inc = 1
        o = gsub(/\{/, "{", line); c = gsub(/\}/, "}", line); depth += o - c
        if ((o || c) && depth == 0) {
          if (pp) { proxied++; if (!inc) { print "missing include in:" name; bad = 1 }; if (!ver) { print "missing proxy_ssl_verify on in:" name; bad = 1 } }
          inloc = 0
        }
      }
    }
    END { if (proxied != 1) { print "want 1 proxying location, found " proxied; bad = 1 } exit bad }'
}
check "every proxy_pass location includes conf.d/proxy-headers.conf and verifies the upstream" locations_ok
for directive in "proxy_ssl_verify on;" "proxy_ssl_verify_depth 2;" "proxy_ssl_name certharbor-control;" \
  "proxy_ssl_trusted_certificate /etc/certharbor/ssl/root_ca.crt;" "server_tokens off;" \
  "proxy_read_timeout 180s;" "ssl_reject_handshake on;"; do
  check "nginx -T contains '$directive'" grep -qF "$directive" <<<"$directives"
done
for absent in "proxy_intercept_errors" "proxy_http_version" "upstream " "keepalive " "Access-Control-"; do
  check "nginx -T has no '$absent' directive" bash -c '! grep -qE "(^|[[:space:]])$1" <<<"$2"' _ "$absent" "$directives"
done

# ── Routing ─────────────────────────────────────────────────────────────────
echo "routing"
forged=(-H "X-Forwarded-For: 1.2.3.4" -H "X-Real-IP: 5.6.7.8" -H "True-Client-IP: 9.10.11.12"
  -H "Forwarded: for=13.14.15.16" -H "X-Request-Id: forged-request-id" -H "Origin: https://evil.example")
probe_paths=()
while IFS= read -r p; do probe_paths+=("$p" "$p/lint-probe"); done < <(jq -r '.[]' "$routes")
for path in "${probe_paths[@]}"; do
  req "${forged[@]}" -D "$work/hdrs" -o "$work/body" "$path"
  body=$(<"$work/body")
  hdrs=$(tr -d '\r' <"$work/hdrs")
  check "$path is proxied to the backend" grep -q '"stub":"certharbor-control"' <<<"$body"
  IFS=$'\t' read -r xff xri tci fwd rid fhost < <(jq -r '[.xff, .xri, .tci, .fwd, .rid, .host] | map(if . == "" then "-" else . end) | @tsv' <<<"$body")
  check "$path: X-Forwarded-For replaced with the peer ($xff)" bash -c '[[ "$1" =~ ^[0-9.]+$ && "$1" != 1.2.3.4 ]]' _ "$xff"
  check "$path: X-Real-IP set to the same peer" test "$xri" = "$xff"
  check "$path: True-Client-IP cleared" test "$tci" = -
  check "$path: Forwarded cleared" test "$fwd" = -
  check "$path: X-Request-Id overwritten with nginx's id" bash -c '[[ "$1" =~ ^[0-9a-f]{32}$ ]]' _ "$rid"
  check "$path: Host passed through" test "$fhost" = "$host"
  check "$path: exactly one nosniff on the proxied response" test "$(grep -ci '^x-content-type-options: nosniff$' <<<"$hdrs")" -eq 1
  check "$path: no document CSP on the proxied response" test -z "$(header Content-Security-Policy "$hdrs")"
  check "$path: no HSTS on the proxied response" test -z "$(header Strict-Transport-Security "$hdrs")"
  check "$path: no Access-Control-* header" bash -c '! grep -qi "^access-control-" <<<"$1"' _ "$hdrs"
done

check "a backend response with 9 KiB of cookies passes through, not a 502" test "$(status_of /api/v1/login-big-headers)" = 303
check "/api/v1/x reaches the backend unchanged" test "$(jq -r .uri <<<"$(req /api/v1/x?x=1)")" = "/api/v1/x?x=1"
for path in / /certificates/12 /reset-password /admin/users/1; do
  body=$(req "$path")
  check "$path falls back to the SPA shell" grep -q 'data-testid="nginx-lint-shell"' <<<"$body"
done
for path in /docs /docs/x /openapi.yaml /schemas/Thing /assets/missing-0000.js; do
  check "$path is 404 with no SPA fallback" test "$(status_of "$path")" = 404
done
check "/nginx-health is 200" test "$(status_of /nginx-health)" = 200

# ── Header set and cache policy on the document ─────────────────────────────
echo "headers"
doc=$(headers_of -H "Origin: https://evil.example" /certificates/12)
asset=$(headers_of /assets/index-a1b2c3d4.js)
check "document is no-store" test "$(header Cache-Control "$doc")" = no-store
check "asset is immutable" test "$(header Cache-Control "$asset")" = "public, max-age=31536000, immutable"
for h in "$doc" "$asset"; do
  check "CSP has no unsafe-inline and sets default-src 'self'" bash -c '[[ "$1" == *"default-src '"'"'self'"'"'"* && "$1" != *unsafe-inline* ]]' _ "$(header Content-Security-Policy "$h")"
  check "X-Frame-Options DENY" test "$(header X-Frame-Options "$h")" = DENY
  check "nosniff" test "$(header X-Content-Type-Options "$h")" = nosniff
  check "Referrer-Policy set" test "$(header Referrer-Policy "$h")" = strict-origin-when-cross-origin
done
# Hashed independently of the entrypoint's awk, straight off the fixture file.
for kind in script style; do
  want_hash=$(python3 -c '
import base64, hashlib, re, sys
html = open(sys.argv[1], encoding="utf-8").read()
for attrs, body in re.findall(r"<%s([^>]*)>(.*?)</%s>" % (sys.argv[2], sys.argv[2]), html, re.S):
    if "src=" not in attrs:
        print(base64.b64encode(hashlib.sha256(body.encode()).digest()).decode())
' "$lint_dir/fixture-dist/index.html" "$kind")
  check "CSP $kind-src carries the hash of the inline $kind ($want_hash)" bash -c \
    '[[ -n "$2" && "$1" == *"$3-src '"'"'self'"'"' '"'"'sha256-$2'"'"'"* ]]' _ "$(header Content-Security-Policy "$doc")" "$want_hash" "$kind"
done
check "no HSTS while WEB_HSTS_MAX_AGE is unset" test -z "$(header Strict-Transport-Security "$doc")"
check "Server header discloses no version" bash -c '[[ "$1" == nginx ]]' _ "$(header Server "$doc")"
check "no Access-Control-* on the document" bash -c '! grep -qi "^access-control-" <<<"$1"' _ "$doc"
check "a name other than WEB_HOSTNAME is refused at the handshake" bash -c \
  '! curl -sk --max-time 3 --resolve "other.test:$1:127.0.0.1" "https://other.test:$1/" -o /dev/null'  _ "$port"
check "a client with no SNI is refused at the handshake" bash -c \
  '! curl -sk --max-time 3 "https://127.0.0.1:$1/" -o /dev/null' _ "$port"

# ── Failure documents ───────────────────────────────────────────────────────
echo "failures"
check "the backend's own 503 passes through with its body" bash -c \
  '[[ "$1" == *backend-own* ]]' _ "$(req /api/v1/lint-backend-503)"
apidown_ok() {
  local body hdrs
  body=$(req /api/v1/session)
  hdrs=$(headers_of /api/v1/session)
  [[ "$(status_of /api/v1/session)" == 503 ]] &&
    [[ "$(jq -r .type <<<"$body")" == */upstream-unavailable ]] &&
    [[ "$(header Content-Type "$hdrs")" == application/problem+json ]] &&
    [[ "$(header Cache-Control "$hdrs")" == no-store ]]
}
docker rm -f "$run_id-stub" >/dev/null
start_stub "$work/untrusted-stub"
sleep 0.5
check "an upstream leaf from an untrusted root gets the @apidown 503" apidown_ok
docker rm -f "$run_id-stub" >/dev/null
check "a stopped upstream gets the @apidown 503" apidown_ok
start_stub "$work/stub"

# ── HSTS opt-in ─────────────────────────────────────────────────────────────
start_web "$run_id-hsts" "$work/web" -e WEB_TLS_MODE=provided -e WEB_HSTS_MAX_AGE=600 --health-interval=1s
wait_healthy "$run_id-hsts"
hsts_port=$(port_of "$run_id-hsts")
hsts_doc=$(curl -sS --resolve "$host:$hsts_port:127.0.0.1" --cacert "$pki/root.crt" -o /dev/null -D - "https://$host:$hsts_port/" | tr -d '\r')
hsts_api=$(curl -sS --resolve "$host:$hsts_port:127.0.0.1" --cacert "$pki/root.crt" -o /dev/null -D - "https://$host:$hsts_port/api/v1/x" | tr -d '\r')
check "WEB_HSTS_MAX_AGE=600 sets HSTS on the document" test "$(header Strict-Transport-Security "$hsts_doc")" = "max-age=600"
check "and not on the proxied response" test -z "$(header Strict-Transport-Security "$hsts_api")"

# ── Entrypoint modes ────────────────────────────────────────────────────────
echo "entrypoint"
mkdir -p "$work/empty"
t0=$(date +%s)
set +e
provided_out=$(timeout 20 docker run --rm -e WEB_HOSTNAME="$host" -e WEB_TLS_MODE=provided \
  -v "$work/empty:/etc/certharbor/ssl:ro" "$image" 2>&1)
provided_rc=$?
set -e
t1=$(date +%s)
check "provided mode with no files exits non-zero" test "$provided_rc" -ne 0
check "within five seconds ($((t1 - t0))s)" test $((t1 - t0)) -le 5
check "naming both missing paths" bash -c '[[ "$1" == */etc/certharbor/ssl/web.crt* && "$1" == */etc/certharbor/ssl/web.key* ]]' _ "$provided_out"
set +e
nohost_out=$(timeout 20 docker run --rm -e WEB_TLS_MODE=provided -v "$work/web:/etc/certharbor/ssl:ro" "$image" 2>&1)
nohost_rc=$?
set -e
check "an empty WEB_HOSTNAME is a non-zero exit naming it" bash -c '[[ $1 -ne 0 && "$2" == *WEB_HOSTNAME* ]]' _ "$nohost_rc" "$nohost_out"

# stepca mode: nginx waits, then serves a 503 naming the file, then picks the
# files up and later a renewal, each with one reload and no restart.
mkdir -p "$work/late"
chmod 0777 "$work/late"
start_web "$run_id-late" "$work/late" -e WEB_TLS_MODE=stepca -e WEB_CERT_WAIT_SECONDS=1 -e WEB_RELOAD_INTERVAL=1
late_port=$(port_of "$run_id-late")
late() { curl -sk --max-time 3 --resolve "$host:$late_port:127.0.0.1" "$@"; }
waiting_ok() { [[ "$(late "https://$host:$late_port/api/v1/x")" == *"waiting for /etc/certharbor/ssl/web.crt"* ]]; }
for _ in $(seq 1 20); do waiting_ok && break; sleep 0.3; done
check "stepca mode serves a 503 naming the missing web.crt" waiting_ok
check "and /nginx-health is not 200 while waiting" bash -c '[[ "$1" != 200 ]]' _ "$(late -o /dev/null -w '%{http_code}' "https://$host:$late_port/nginx-health")"
cp "$pki/web.key" "$pki/web.crt" "$pki/root.crt" "$work/late/"
mv "$work/late/root.crt" "$work/late/root_ca.crt"
serial_served() {
  openssl s_client -connect "127.0.0.1:$late_port" -servername "$host" </dev/null 2>/dev/null |
    openssl x509 -noout -serial 2>/dev/null
}
want1=$(openssl x509 -noout -serial -in "$pki/web.crt")
for _ in $(seq 1 30); do [[ "$(serial_served)" == "$want1" ]] && break; sleep 0.3; done
check "the arriving web leaf is served without a restart" test "$(serial_served)" = "$want1"
cp "$pki/web2.key" "$work/late/web.key.tmp" && mv "$work/late/web.key.tmp" "$work/late/web.key"
cp "$pki/web2.crt" "$work/late/web.crt.tmp" && mv "$work/late/web.crt.tmp" "$work/late/web.crt"
want2=$(openssl x509 -noout -serial -in "$pki/web2.crt")
for _ in $(seq 1 30); do [[ "$(serial_served)" == "$want2" ]] && break; sleep 0.3; done
sleep 2.5
check "a renewed web leaf is served without a restart" test "$(serial_served)" = "$want2"
reloads=$(docker logs "$run_id-late" 2>&1 | grep -c 'web-reload: reloaded' || true)
check "exactly two reloads: the arrival and the renewal ($reloads)" test "$reloads" -eq 2
check "the container never restarted" test "$(docker inspect -f '{{.RestartCount}}' "$run_id-late")" -eq 0

# Unit tests for the watcher, inside the same image so busybox sh runs them.
check "entrypoint unit tests" docker run --rm --entrypoint sh \
  -v "$lint_dir/entrypoint_test.sh:/t/entrypoint_test.sh:ro" "$image" /t/entrypoint_test.sh

elapsed=$(($(date +%s) - started))
echo
if ((failures > 0)); then
  echo "nginx-lint: $failures check(s) failed in ${elapsed}s"
  exit 1
fi
echo "nginx-lint: all checks passed in ${elapsed}s"
if ((elapsed > 60)); then
  echo "nginx-lint: over the 60s budget"
  exit 1
fi
