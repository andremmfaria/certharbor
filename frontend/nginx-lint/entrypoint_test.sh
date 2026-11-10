#!/bin/sh
# The variables below are read by the sourced entrypoint, and each check is a
# one-line pass-or-fail by design.
# shellcheck disable=SC2034,SC2015,SC2016
# Unit tests for frontend/entrypoint.sh's reload watcher and its config
# helpers, run under the image's own busybox sh by make nginx-lint. nginx is
# replaced by a fake on PATH that records each invocation.
set -eu

T=$(mktemp -d)
trap 'rm -rf "$T"' EXIT
mkdir -p "$T/bin" "$T/ssl"
cat >"$T/bin/nginx" <<'EOF'
#!/bin/sh
echo "$*" >>"$FAKE_NGINX_LOG"
case "$*" in
-t) exit "${FAKE_NGINX_T_RC:-0}" ;;
"-s reload") exit "${FAKE_NGINX_RELOAD_RC:-0}" ;;
esac
EOF
chmod +x "$T/bin/nginx"
PATH="$T/bin:$PATH"
export FAKE_NGINX_LOG="$T/calls"

STEP_WEBUI_WEB_ENTRYPOINT_LIB=1
# shellcheck source=/dev/null
. /usr/local/bin/certharbor-web-entrypoint
set +e

SSL_DIR="$T/ssl"
CERT="$SSL_DIR/web.crt"
KEY="$SSL_DIR/web.key"
ROOT="$SSL_DIR/root_ca.crt"
NGINX_CONF="$T/nginx.conf"
HSTS_CONF="$T/hsts.conf"
TEMPLATE="$T/tpl"
SETTLE=0
WEB_RELOAD_INTERVAL=1

fails=0
ok() { echo "  ok    $*"; }
no() {
	echo "  FAIL  $*"
	fails=$((fails + 1))
}
calls() { tr '\n' ',' <"$FAKE_NGINX_LOG" 2>/dev/null; }
reset_calls() { : >"$FAKE_NGINX_LOG"; }

echo one >"$CERT"
echo key-one >"$KEY"
echo root >"$ROOT"
STATE=serving
APPLIED=$(digest)

reset_calls
watch_once 2>/dev/null
[ "$(calls)" = "" ] && ok "unchanged certificate: nginx is not called" || no "unchanged certificate called nginx: $(calls)"

echo two >"$CERT"
reset_calls
watch_once 2>/dev/null
[ "$(calls)" = "-t,-s reload," ] && ok "changed certificate: nginx -t runs before nginx -s reload" || no "call order was $(calls)"
[ "$APPLIED" = "$(digest)" ] && ok "the applied digest advances after a reload" || no "applied digest did not advance"

echo three >"$CERT"
reset_calls
export FAKE_NGINX_T_RC=1
out=$(watch_once 2>&1)
watch_once 2>/dev/null
[ "$(calls)" = "-t,-t," ] && ok "failed nginx -t never reloads" || no "after a failed -t the calls were $(calls)"
case "$out" in *"web-reload: nginx -t FAILED"*) ok "failed nginx -t logs the FAILED line" ;; *) no "no FAILED line: $out" ;; esac
[ "$APPLIED" != "$(digest)" ] && ok "a failed check leaves the digest unapplied" || no "digest applied despite failure"

export FAKE_NGINX_T_RC=0
reset_calls
watch_once 2>/dev/null
[ "$(calls)" = "-t,-s reload," ] && ok "the watcher re-arms and reloads once nginx -t passes" || no "re-arm calls were $(calls)"

echo four >"$CERT"
export FAKE_NGINX_RELOAD_RC=1
reset_calls
watch_once 2>/dev/null
export FAKE_NGINX_RELOAD_RC=0
watch_once 2>/dev/null
[ "$(calls)" = "-t,-s reload,-t,-s reload," ] && ok "a failed reload is retried on the next tick" || no "reload retry calls were $(calls)"

# Waiting state: nothing happens until all three files exist, then one
# render and one reload.
rm -f "$ROOT"
printf 'server_name ${WEB_HOSTNAME}; resolver ${WEB_RESOLVER}; $remote_addr\n' >"$TEMPLATE"
export WEB_HOSTNAME=ca.example.com WEB_RESOLVER=127.0.0.11 WEB_MISSING=
STATE=waiting
reset_calls
watch_once 2>/dev/null
[ "$(calls)" = "" ] && [ ! -e "$NGINX_CONF" ] && ok "waiting: no reload while root_ca.crt is missing" || no "waiting state acted early: $(calls)"
echo root >"$ROOT"
watch_once 2>/dev/null
[ "$(calls)" = "-t,-s reload," ] && [ "$STATE" = serving ] && ok "waiting: the arrival renders and reloads once" || no "arrival calls were $(calls), state $STATE"
[ "$(cat "$NGINX_CONF")" = 'server_name ca.example.com; resolver 127.0.0.11; $remote_addr' ] && ok "render substitutes only the named placeholders" || no "rendered: $(cat "$NGINX_CONF")"

[ "$(missing "$CERT" "$T/nope" "$KEY" "$T/nada")" = "$T/nope $T/nada" ] && ok "missing lists exactly the absent paths" || no "missing printed $(missing "$CERT" "$T/nope" "$KEY" "$T/nada")"

unset WEB_HSTS_MAX_AGE
write_hsts && [ ! -s "$HSTS_CONF" ] && ok "unset WEB_HSTS_MAX_AGE writes no HSTS header" || no "HSTS written while unset"
WEB_HSTS_MAX_AGE=300 write_hsts && grep -q 'max-age=300' "$HSTS_CONF" && ok "WEB_HSTS_MAX_AGE=300 writes the header" || no "HSTS 300 not written"
if WEB_HSTS_MAX_AGE='1; includeSubDomains' write_hsts 2>/dev/null; then no "a non-numeric WEB_HSTS_MAX_AGE was accepted"; else ok "a non-numeric WEB_HSTS_MAX_AGE is refused"; fi

[ "$fails" -eq 0 ] || exit 1
