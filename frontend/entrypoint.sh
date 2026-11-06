#!/bin/sh
# Entrypoint for step-webui-web. It replaces the stock nginx entrypoint instead
# of adding a /docker-entrypoint.d script because it must stay running as the
# parent of nginx: it forwards the stop signal so nginx drains rather than
# dies, and it runs the certificate reload watcher beside it. The stock
# entrypoint execs nginx and keeps nothing running, so this script does its
# own envsubst rendering.
set -eu

SSL_DIR=/etc/step-webui/ssl
CERT="$SSL_DIR/web.crt"
KEY="$SSL_DIR/web.key"
ROOT="$SSL_DIR/root_ca.crt"
TEMPLATE=/etc/step-webui/nginx.conf.template
WAITING_TEMPLATE=/etc/step-webui/nginx.waiting.conf.template
NGINX_CONF=/etc/nginx/nginx.conf
HSTS_CONF=/etc/nginx/conf.d/hsts.conf
DOC_HEADERS_TEMPLATE=/etc/step-webui/doc-headers.conf.template
DOC_HEADERS_CONF=/etc/nginx/conf.d/doc-headers.conf
# PHASE9-DELETE: /usr/share/nginx/html/index.html once the SPA owns the root.
INDEX_HTML=/usr/share/nginx/html/app/index.html
# Seconds to let a renewal finish writing both files before hashing them.
SETTLE=1

STATE=serving
APPLIED=

log() {
	printf 'step-webui-web: %s\n' "$*" >&2
}

# missing prints the space-separated subset of its arguments that are absent
# or empty.
missing() {
	_out=
	for _f in "$@"; do
		[ -s "$_f" ] || _out="${_out:+$_out }$_f"
	done
	printf '%s' "$_out"
}

# render substitutes only the named placeholders, so nginx's own $variables
# survive.
render() {
	# shellcheck disable=SC2016 # the single quotes are the point: envsubst gets the names, not values
	envsubst '${WEB_HOSTNAME} ${WEB_RESOLVER} ${WEB_RATE_LIMIT} ${WEB_RATE_BURST} ${WEB_MISSING} ${WEB_CSP_SCRIPT_HASHES} ${WEB_CSP_STYLE_HASHES}' <"$1" >"$2.tmp"
	mv "$2.tmp" "$2"
}

write_hsts() {
	if [ -z "${WEB_HSTS_MAX_AGE:-}" ]; then
		: >"$HSTS_CONF"
		return 0
	fi
	case "$WEB_HSTS_MAX_AGE" in
	*[!0-9]*)
		log "WEB_HSTS_MAX_AGE=$WEB_HSTS_MAX_AGE is not a whole number of seconds"
		return 1
		;;
	esac
	printf 'add_header Strict-Transport-Security "max-age=%s" always;\n' "$WEB_HSTS_MAX_AGE" >"$HSTS_CONF"
}

# inline_hashes prints a CSP source list entry (" 'sha256-...'") for each
# inline <script> or <style> element ($1) in $2, hashing the exact bytes
# between the tags as the browser does. Elements with a src are skipped.
inline_hashes() {
	_dir=$(mktemp -d)
	awk -v tag="$1" -v dir="$_dir" '
		{ doc = doc $0 "\n" }
		END {
			n = 0
			while ((i = index(doc, "<" tag)) > 0) {
				doc = substr(doc, i)
				close_tag = index(doc, ">")
				open_tag = substr(doc, 1, close_tag)
				doc = substr(doc, close_tag + 1)
				j = index(doc, "</" tag ">")
				if (j == 0) break
				body = substr(doc, 1, j - 1)
				doc = substr(doc, j)
				if (open_tag ~ /[[:space:]]src=/) continue
				n++
				printf "%s", body > (dir "/" n)
				close(dir "/" n)
			}
		}' "$2"
	for _f in "$_dir"/*; do
		[ -e "$_f" ] || continue
		printf " 'sha256-%s'" "$(sha256sum "$_f" | cut -d' ' -f1 | xxd -r -p | base64)"
	done
	rm -rf "$_dir"
}

render_doc_headers() {
	WEB_CSP_SCRIPT_HASHES=
	WEB_CSP_STYLE_HASHES=
	if [ -f "$INDEX_HTML" ]; then
		WEB_CSP_SCRIPT_HASHES=$(inline_hashes script "$INDEX_HTML")
		WEB_CSP_STYLE_HASHES=$(inline_hashes style "$INDEX_HTML")
	fi
	export WEB_CSP_SCRIPT_HASHES WEB_CSP_STYLE_HASHES
	render "$DOC_HEADERS_TEMPLATE" "$DOC_HEADERS_CONF"
}

digest() {
	cat "$CERT" "$KEY" 2>/dev/null | sha256sum | cut -d' ' -f1
}

# watch_once is one watcher tick. nginx -t always runs before nginx -s reload,
# and a failure of either leaves APPLIED unchanged, so the next tick retries:
# the watcher re-arms instead of exiting. The FAILED lines are what an
# operator greps for when a renewal never reaches the browser.
watch_once() {
	if [ "$STATE" = waiting ]; then
		_m=$(missing "$CERT" "$KEY" "$ROOT")
		[ -z "$_m" ] || return 0
		log "certificate files arrived, switching from the waiting config"
		render "$TEMPLATE" "$NGINX_CONF"
	fi
	_d=$(digest)
	if [ "$STATE" = serving ] && [ "$_d" = "$APPLIED" ]; then
		return 0
	fi
	sleep "$SETTLE"
	_d=$(digest)
	if ! _out=$(nginx -t 2>&1); then
		log "web-reload: nginx -t FAILED, still serving the previous config, retrying in ${WEB_RELOAD_INTERVAL}s: $(printf '%s\n' "$_out" | tail -n 1)"
		return 0
	fi
	if ! nginx -s reload; then
		log "web-reload: nginx -s reload FAILED, retrying in ${WEB_RELOAD_INTERVAL}s"
		return 0
	fi
	APPLIED=$_d
	STATE=serving
	log "web-reload: reloaded, certificate sha256 $_d"
}

watch_loop() {
	while :; do
		sleep "$WEB_RELOAD_INTERVAL"
		watch_once
	done
}

# wait_for_certs applies to stepca mode only: the backend writes the files
# into the shared volume, possibly after this container starts, for example
# after a host reboot restarts containers in no particular order.
wait_for_certs() {
	_elapsed=0
	while :; do
		_m=$(missing "$CERT" "$KEY" "$ROOT")
		if [ -z "$_m" ]; then
			render "$TEMPLATE" "$NGINX_CONF"
			STATE=serving
			return 0
		fi
		if [ "$_elapsed" -ge "$WEB_CERT_WAIT_SECONDS" ]; then
			break
		fi
		if [ $((_elapsed % 10)) -eq 0 ]; then
			log "waiting for $_m (${_elapsed}s of ${WEB_CERT_WAIT_SECONDS}s)"
		fi
		sleep 1
		_elapsed=$((_elapsed + 1))
	done
	log "still missing after ${WEB_CERT_WAIT_SECONDS}s: $_m. Serving 503 naming it until it arrives"
	WEB_MISSING=$_m
	export WEB_MISSING
	render "$WAITING_TEMPLATE" "$NGINX_CONF"
	STATE=waiting
}

stop() {
	log "stopping: nginx drains in-flight requests"
	[ -z "${WATCH_PID:-}" ] || kill "$WATCH_PID" 2>/dev/null || true
	kill -QUIT "$NGINX_PID" 2>/dev/null || true
}

main() {
	: "${WEB_TLS_MODE:=stepca}"
	: "${WEB_RESOLVER:=127.0.0.11}"
	: "${WEB_CERT_WAIT_SECONDS:=120}"
	: "${WEB_RELOAD_INTERVAL:=60}"
	: "${WEB_RATE_LIMIT:=20r/s}"
	: "${WEB_RATE_BURST:=40}"
	export WEB_HOSTNAME="${WEB_HOSTNAME:-}" WEB_RESOLVER WEB_RATE_LIMIT WEB_RATE_BURST WEB_MISSING=

	if [ -z "$WEB_HOSTNAME" ]; then
		log "WEB_HOSTNAME is required: it is the server_name and the name browsers check on web.crt"
		exit 1
	fi
	write_hsts || exit 1
	render_doc_headers

	case "$WEB_TLS_MODE" in
	provided)
		# No wait: nothing will ever write these files, so their absence is a
		# deployment error to report now rather than a 503 to serve for ever.
		_m=$(missing "$CERT" "$KEY" "$ROOT")
		if [ -n "$_m" ]; then
			log "WEB_TLS_MODE=provided but these are missing or empty: $_m. Mount web.crt, web.key and the backend's root_ca.crt read-only at $SSL_DIR"
			exit 1
		fi
		render "$TEMPLATE" "$NGINX_CONF"
		;;
	stepca)
		wait_for_certs
		;;
	*)
		log "WEB_TLS_MODE=$WEB_TLS_MODE is not one of stepca, provided"
		exit 1
		;;
	esac

	nginx -t
	APPLIED=$(digest)

	nginx -g 'daemon off;' &
	NGINX_PID=$!
	trap stop TERM INT QUIT

	watch_loop &
	WATCH_PID=$!

	# wait returns early when a trapped signal arrives; the second wait is the
	# drain.
	_rc=0
	wait "$NGINX_PID" || _rc=$?
	if kill -0 "$NGINX_PID" 2>/dev/null; then
		_rc=0
		wait "$NGINX_PID" || _rc=$?
	fi
	kill "$WATCH_PID" 2>/dev/null || true
	exit "$_rc"
}

if [ "${STEP_WEBUI_WEB_ENTRYPOINT_LIB:-0}" != 1 ]; then
	main "$@"
fi
