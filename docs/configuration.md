# Configuration

Startup sequence, TLS bootstrap, and the full environment variable reference, all sourced from `backend/config/config.go`, `backend/entrypoint.sh`, `backend/main.go`, `docker-compose.yml` and `frontend/entrypoint.sh`.

All configuration lives in `.env`. `make setup` creates it from `.env.example` and generates `secrets/` (see [docs/deployment.md](deployment.md)). `.env.example` carries the same reference this page does, inline as comments.

## Startup sequence

`backend/entrypoint.sh` runs before the Go binary starts:

1. **Secret files.** Read `POSTGRES_PASSWORD`, `SECRET_KEY`, `PROVISIONER_PASSWORD` from `secrets/` (`*_FILE` path, default `/run/secrets/<name>`), falling back to a plain-env value if already set.
2. **`DATABASE_URL` construction.** Assembled from the `POSTGRES_*` parts if `DATABASE_URL` is not already set. Aborts if neither `DATABASE_URL` nor a postgres password is available.
3. **PostgreSQL wait.** Host and port are parsed out of `DATABASE_URL` (works with any backend, not just the compose service name) and probed with `nc` up to 60 times, 1 second apart. Bounded and non-fatal: if postgres is not yet reachable, startup continues and the Go app's own connection retry takes over.
4. **Provisioner password file.** Writes `PROVISIONER_PASSWORD` to `$PASSWORD_FILE` if that file does not already exist.
5. **`exec /opt/certharbor/certharbor`.**

Everything past that point, root CA trust and the UI's own TLS certificate, is handled **in-process** by the Go binary (`backend/main.go`, `backend/tlsbootstrap.go`), not by the entrypoint script. It runs synchronously before the HTTPS listener starts.

`main.go` also runs a set of startup checks before any of this: it refuses to start if `SECRET_KEY` is weak, if `TRUST_PROXY=true` with a malformed `TRUSTED_PROXY_CIDRS` (or with that list empty and `WEB_UPSTREAM_HOST` empty too), if `WEB_TLS_MODE` is not `stepca` or `provided`, if `WEB_TLS_MODE=stepca` and `WEB_HOSTNAME` is empty, or if `OIDC_ENABLED=true` and `OIDC_DEFAULT_ROLE` names something other than `viewer`, `manager`, `admin` or empty (the check is skipped entirely when `OIDC_ENABLED` is false). It logs (but does not fail on) an unset `ALLOWED_DOMAIN_SUFFIXES`, a `UI_CERT_DURATION` under 10 minutes, and an `LE_ACME_DIRECTORY_URL` that isn't Let's Encrypt's production directory.

## Root CA trust

`main.go` establishes the root certificate the Go app uses to verify step-ca, through `github.com/smallstep/certificates/ca`. No `step` binary is involved. Three methods, applied in this precedence order:

1. **`CA_ROOT_CERT_PEM` set.** The inline PEM is written to `$ROOT_CERT`. Use this for ECS/Kubernetes deployments where a Docker volume mount is impractical.
2. **`CA_FINGERPRINT` set and `$ROOT_CERT` absent or empty.** An unauthenticated, fingerprint-verified fetch against step-ca, retried up to 30 times, 1 second apart. On failure a warning is logged and startup continues.
3. **Neither set.** The file at `$ROOT_CERT` is assumed to already exist (for example, mounted read-only from the step-ca container, which is what `docker-compose.yml` does).

`CA_FINGERPRINT` is the SHA-256 hex digest of the root certificate's DER encoding. It is public and non-secret, it only authenticates which certificate to trust on download.

## TLS certificate modes

`UI_TLS_MODE` controls how the UI's own serving certificate is obtained:

| `UI_TLS_MODE` | Behaviour |
|---|---|
| `self-signed` (`config.go`'s default, unreachable through compose, which defaults to `stepca`) | Generates a 10-year self-signed EC P-256 cert on first boot via `crypto/x509`, if `$SSL_CERT` is absent. SANs: `IP:$HOST_IP`, `DNS:localhost`, and `DNS:$UI_HOSTNAME` when set |
| `provided` | Does nothing. Expects a cert and key to already exist at `$SSL_CERT` / `$SSL_KEY` |
| `stepca` | Requests a signed leaf from the step-ca this UI manages, via the smallstep library. Retried up to 30 times, 1 second apart. On failure it falls back to a self-signed leaf **and writes the marker `/opt/certharbor/ssl/.fallback`**, which makes `/ready` answer 503 naming it and makes the `certharbor` compose healthcheck fail, holding `certharbor-web` off, because nginx verifies this leaf against the CA root and cannot proxy to a self-signed one. A background renewer (`startTLSRenewer` in `backend/tlsbootstrap.go`) runs whenever this or `WEB_TLS_MODE` is `stepca`, even if the CA was unreachable at boot: it builds its CA client lazily, retries a fallback or missing leaf every 30 seconds and clears the marker once a real leaf is issued, with no restart. A healthy leaf is re-issued at two thirds of its validity window, clamped to a minimum sleep of 1 minute, and a failed renewal of a still-served leaf is retried after a fixed 5-minute backoff |

`UI_HOSTNAME` sets the certificate subject (CN) and an extra DNS SAN. For `self-signed` it is added to the SAN list. For `stepca` it is the requested CN/SAN, falling back to the OS-reported hostname when empty. Compose defaults it to `certharbor`, the name nginx verifies the backend's leaf against (`proxy_ssl_name`).

## The web origin's certificate

nginx (`certharbor-web`) serves its own leaf, `web.crt` and `web.key`, carrying `WEB_HOSTNAME`, and verifies the backend's leaf (which must carry the name `certharbor`, hence the compose default `UI_HOSTNAME=certharbor`) against `root_ca.crt`. All three files live in the `certharbor-web-ssl` volume, mounted read-only into nginx at `/etc/certharbor/ssl` and read-write into the backend at `/opt/certharbor/web-ssl`. nginx never mounts `certharbor-ssl` (the backend's private key) or `step-ca-data`.

| `WEB_TLS_MODE` | Behaviour |
|---|---|
| `stepca` (default) | The backend issues `web.crt` for `WEB_HOSTNAME` from step-ca at boot, copies its `ROOT_CERT` into the volume as `root_ca.crt`, and renews the web leaf in the same loop as its own leaf. It never self-signs the web leaf: if step-ca is unreachable the file is simply absent, `/ready` reports `webCert: missing`, and nginx waits up to `WEB_CERT_WAIT_SECONDS` before serving a 503 that names the missing path, switching to the real config as soon as the file arrives |
| `provided` | The backend writes nothing into the web volume. The operator mounts `web.crt`, `web.key` and `root_ca.crt` (the root that signed the **backend's** leaf) at `/etc/certharbor/ssl`, for example by replacing the `certharbor-web-ssl` mount with a read-only bind mount. nginx does not wait: if any of the three is missing it exits non-zero at once, naming the paths |

Every write of a certificate, key or root goes to a `.tmp` sibling that is then renamed into place, key before certificate, so neither reader can see a half-written PEM. nginx's entrypoint hashes `web.crt` plus `web.key` every `WEB_RELOAD_INTERVAL` seconds and on a change runs `nginx -t` and then `nginx -s reload`, logging `web-reload: reloaded` on success and a line containing `FAILED` on either failure, and retries on the next tick. `/ready` reports `backendCertNotAfter` and `webCertNotAfter`, and goes non-200 when a leaf the backend manages is more than one 5-minute backoff past its renewal point, so a web leaf that keeps failing to renew is visible before browsers start rejecting it.

The nginx master process runs as root because it reads `web.key`, which the backend writes `0600` as uid 10001. A `USER` line or an unprivileged nginx base image breaks TLS startup. The volume takes its ownership from the backend image's `/opt/certharbor/web-ssl` the first time it is mounted, so a volume that was created some other way and is owned by root shows up as `permission denied` in the backend log: remove it with `docker volume rm certharbor_certharbor-web-ssl` and recreate the stack.

## TLS hot-reload

`backend/tlsreload.go` re-stats both `SSL_CERT` and `SSL_KEY` on every TLS handshake. When either file's modification time changes, it reloads the pair in place via `tls.LoadX509KeyPair`. If the reload fails **and a certificate has already loaded successfully once**, that last-good certificate keeps serving and no handshake is dropped, a failure on the very first load (no cached certificate yet) is returned as an error instead. This works for the `stepca` renewal goroutine, a manual file replacement in the `certharbor-ssl` volume, or any external cert-manager writing to the mounted path.

## Environment variable reference

Every variable below is read in `backend/config/config.go` unless noted otherwise. "In compose" says whether the stock `docker-compose.yml` actually passes the variable through to the `certharbor` container, since setting a key only in `.env` has no effect unless `docker-compose.yml`'s `environment:` block references it.

### Core deployment

| Variable | Default | In compose | Description |
|---|---|---|---|
| `HOST_IP` | `127.0.0.1` (config default, `.env.example` ships `192.168.1.100` instead) | Yes | SAN in the self-signed cert and DNS name for step-ca |
| `UI_HTTPS_PORT` | `443` | Yes (host port mapping only) | Host port mapped to `certharbor-web`'s `8443`, the only published port of the UI. `certharbor` publishes none |
| `PORT` | `8443` | Yes (hardcoded, not from `.env`) | Internal port the Go app listens on |
| `TZ` | `UTC` | Yes | Container timezone, applied to all four containers |
| `STEP_NET_SUBNET` | `10.213.0.0/24` | Yes (compose bridge network only) | Subnet for the `step-network` bridge network. No service carries a static address in it. The default sits outside `172.16.0.0/12`, where many corporate VPNs route and Docker allocates its own networks. **Failure mode:** a collision with a LAN or VPN route shows up as unrelated network failures on the host. Set another subnet in `.env` and run `docker compose down && docker network rm step-network` before recreating |
| `CERTHARBOR_ADMIN_PASSWORD` | none, **required on first boot** | Yes | Seeds the initial admin user when no user rows exist yet. `backend/db/schema.go`'s `resolveAdminPassword` returns an error if no admin exists and this is empty, its caller, `InitSchema`, is what calls `log.Fatal` on that error. There is no `Admin123!`-style implicit default in the code, despite what `.env.example`'s comment says (see the note below). Remove from `.env` after first login |

`.env.example`'s comment claims an empty value defaults to a placeholder password. That does not match `backend/db/schema.go`: if no admin user exists yet and `CERTHARBOR_ADMIN_PASSWORD` is empty, the database migration step calls `log.Fatal` and the container will not come up. Set `CERTHARBOR_ADMIN_PASSWORD` before the first `make up`.

### step-ca connection

| Variable | Default | In compose | Description |
|---|---|---|---|
| `CA_URL` | `https://step-ca:9443` | Yes (hardcoded) | Base URL of the step-ca API |
| `ROOT_CERT` | `/home/step/certs/root_ca.crt` | Yes (hardcoded) | Path to the step-ca root certificate inside the container |
| `CA_FINGERPRINT` | empty | Yes | SHA-256 hex fingerprint of the root cert DER. Public, non-secret. See [Root CA trust](#root-ca-trust) |
| `CA_ROOT_CERT_PEM` | empty | Yes | Inline PEM of the root CA certificate, highest-precedence root-trust method |
| `PROVISIONER` | `admin` | Yes | step-ca provisioner identifier used for certificate requests |
| `PASSWORD_FILE` | `/opt/certharbor/data/provisioner_password` | Yes (hardcoded) | Path to the provisioner password file inside the container |
| `PROVISIONER_PASSWORD` | empty | entrypoint.sh only | Plain-env fallback, written to `$PASSWORD_FILE` by the entrypoint when the file does not exist. Prefer `secrets/ca_password` |
| `PROVISIONER_PASSWORD_FILE` | `/run/secrets/ca_password` | Yes (as `PROVISIONER_PASSWORD_FILE`, entrypoint.sh only) | Docker secret path for the provisioner password. The `/run/secrets/ca_password` default is `entrypoint.sh`'s own shell fallback, not something `config.go` reads |
| `STEP_CA_IMAGE` | `smallstep/step-ca:0.30.2` | Yes | step-ca image tag, also used for CA integrity verification |
| `STEPCA_DEFAULT_TLS_CERT_DURATION` | `8760h` | Yes, on the `step-ca` service only | Read by `scripts/step-ca-bootstrap.sh` and the step-ca container directly, not by the Go app. Default cert lifetime applied during step-ca initialisation |
| `STEPCA_MAX_TLS_CERT_DURATION` | `87600h` | Yes, on the `step-ca` service only | Same script/container. Maximum cert lifetime applied during step-ca initialisation |

### TLS: UI serving certificate

| Variable | Default | In compose | Description |
|---|---|---|---|
| `UI_TLS_MODE` | `stepca` in compose (`self-signed` in `config.go`) | Yes | `self-signed`, `provided`, or `stepca`. See [TLS certificate modes](#tls-certificate-modes). Behind nginx only `stepca`, or `provided` with a leaf chaining to `root_ca.crt`, can work: nginx verifies this leaf |
| `UI_HOSTNAME` | `certharbor` in compose (empty in `config.go`) | Yes | Hostname added as DNS SAN (self-signed) or used as CN (stepca). nginx verifies the backend's leaf for `certharbor`, so change it only together with `frontend/nginx.conf.template` |
| `SSL_CERT` | `/opt/certharbor/ssl/server.crt` | n/a, hardcoded in config.go | Path to the serving TLS certificate. `config.go:109` hardcodes this path, setting `SSL_CERT` in `.env` has no effect |
| `SSL_KEY` | `/opt/certharbor/ssl/server.key` | n/a, hardcoded in config.go | Path to the serving TLS private key. `config.go:110` hardcodes this path, setting `SSL_KEY` in `.env` has no effect |
| `USE_HTTPS` | empty (auto-detect) | No, only in `compose.e2e-config.yml` | Force TLS on (`true`), or leave auto-detect (empty, probes whether `$SSL_CERT` exists). `USE_HTTPS=false` is a no-op: `main.go` ORs the flag with the same `$SSL_CERT` existence probe (`main.go:410-412`, default `false` in `config.go:128`), so an explicit `false` behaves identically to leaving the variable unset |
| `UI_CERT_DURATION` | `8760h` | Yes | Validity requested for both leaves issued from step-ca, the backend's under `UI_TLS_MODE=stepca` and nginx's under `WEB_TLS_MODE=stepca`. Unparseable or non-positive values fall back to the default with a warning |

### Database

| Variable | Default | In compose | Description |
|---|---|---|---|
| `DATABASE_URL` | constructed from parts, or `postgres://certharbor:certharbor@postgres:5432/certharbor?sslmode=disable` (`config.go:91`'s own fallback if nothing constructs one) | No (constructed by entrypoint.sh) | Full PostgreSQL DSN. Overrides the `POSTGRES_*` parts when set. In the stock stack `entrypoint.sh` normally builds and exports this from the `POSTGRES_*` parts before the Go app ever sees `config.go`'s own default |
| `POSTGRES_HOST` | `postgres` | Yes (hardcoded, not from `.env`) | Used only when `DATABASE_URL` is not set |
| `POSTGRES_PORT` | `5432` | Yes (hardcoded, not from `.env`) | Used only when `DATABASE_URL` is not set |
| `POSTGRES_USER` | `certharbor` | Yes (hardcoded, not from `.env`) | Used only when `DATABASE_URL` is not set |
| `POSTGRES_DB` | `certharbor` | Yes (hardcoded, not from `.env`) | Used only when `DATABASE_URL` is not set |
| `POSTGRES_PASSWORD` | empty | entrypoint.sh only | Plain-env fallback for the database password. Prefer `secrets/postgres_password` |
| `POSTGRES_PASSWORD_FILE` | `/run/secrets/postgres_password` | Yes, entrypoint.sh only | Docker secret path for the database password. The `/run/secrets/postgres_password` default is `entrypoint.sh`'s own shell fallback, not something `config.go` reads |

### Application security

| Variable | Default | In compose | Description |
|---|---|---|---|
| `SECRET_KEY` | none, **required** | No plain fallback in compose (file-based only) | Signs session cookies and CSRF tokens. Minimum 32 characters and must not equal the built-in placeholder, or the app refuses to start |
| `SECRET_KEY_FILE` | `/run/secrets/secret_key` | Yes | Docker secret path for `SECRET_KEY`, read by both `entrypoint.sh` and, redundantly, `config.go`'s own `*_FILE` fallback. The `/run/secrets/secret_key` default shown here is `entrypoint.sh`'s shell fallback and `docker-compose.yml`'s literal value, `config.go` itself supplies no default path for `SECRET_KEY_FILE` |
| `SESSION_SECURE` | `true` | Yes | Sets the `Secure` flag (and the `__Host-` cookie name prefix, see [docs/authentication.md](authentication.md)) on session and CSRF cookies |
| `TRUST_PROXY` | `true` in compose (`false` in `config.go`) | Yes | Rewrites the client IP from `X-Forwarded-For` only, never from `X-Real-IP`, `True-Client-IP` or `Forwarded`, and only when the immediate TCP peer is a trusted proxy. **Failure mode:** off behind nginx, every client is recorded as nginx's own address, so five wrong passwords from anyone lock everyone out of login for five minutes |
| `TRUSTED_PROXY_CIDRS` | empty | Yes | Comma-separated CIDR blocks of trusted proxies, used verbatim when set. When empty, the backend resolves `WEB_UPSTREAM_HOST` instead and trusts exactly the addresses it returns, re-resolving every 5 seconds. **Failure mode:** a malformed entry is a startup fatal. A list wider than the proxy's own address lets anything inside it choose its recorded address |
| `WEB_UPSTREAM_HOST` | `certharbor-web` | Yes | The proxy's name to resolve when `TRUSTED_PROXY_CIDRS` is empty. Resolution is not a startup fatal, because nginx only starts once the backend is healthy, so its name never resolves at boot. **Failure mode:** while the name does not resolve, forwarding headers are ignored (a warning is logged on the first failure and every minute after), so behind a misnamed proxy every client is recorded as the proxy's address |
| `ALLOWED_DOMAIN_SUFFIXES` | empty (unrestricted) | **No**, only in `compose.e2e-config.yml` | Comma-separated domain suffixes the UI will request a signature for (internal issuance, renewal and the Let's Encrypt path). Matched on label boundaries. This is not an X.509 name constraint: a caller talking to step-ca directly bypasses it |
| `PUBLIC_BASE_URL` | unset | Yes | Externally reachable origin, e.g. `https://ca.example.com`. **Required for password recovery**, emailed links are built from this and never from the request. Behind nginx it must name nginx's published origin, the same host as `WEB_HOSTNAME`, with no path: the reset link targets `/reset-password` at the root, never under `/app/`. **Failure mode:** unset means reset emails are not sent, and a path under `/app/` would break every link already emailed once the SPA moves to the root |

### Web origin (nginx)

`frontend/nginx.conf.template` may hold routing, TLS, headers, caching and limits, and never a decision that depends on a user, a role or a resource: those belong in the backend, where they are typed and tested.

These keys are read by `backend/config/config.go` (the `WEB_TLS_MODE`, `WEB_HOSTNAME` and `WEB_UPSTREAM_HOST` rows), by `frontend/entrypoint.sh`, or by `docker-compose.yml` itself.

nginx serves the SPA under `/app/` and proxies `/api/`, `/auth/`, `/health`, `/ready` and `/openapi.json` (the list in `frontend/proxy-prefixes.json`, which the Vite dev proxy reads too) to the backend. Every other path, the server-rendered pages and `/static/` included, goes to the backend through a passthrough block marked `PHASE9-DELETE`, which is removed once the SPA owns the root. `/reset-password` and the OIDC callback stay at the root after that too. Response headers from the backend may total 16 KiB (`proxy_buffer_size`), enough for three 4 KiB cookies on one redirect.

| Variable | Default | In compose | Description |
|---|---|---|---|
| `WEB_TLS_MODE` | `stepca` | Yes, both services | How nginx gets `web.crt`, see [The web origin's certificate](#the-web-origins-certificate). **Failure mode:** any other value is fatal in both containers, and `provided` with a file missing makes nginx exit non-zero at once |
| `WEB_HOSTNAME` | none, **required** | Yes, both services | The public name browsers use: nginx's `server_name`, the only name on `web.crt`, and the SNI its healthcheck sends. There is no fallback to the container hostname, because a certificate naming a container id would survive on the volume. **Failure mode:** empty is fatal in nginx and, under `WEB_TLS_MODE=stepca`, in the backend. A handshake for any other name is refused |
| `WEB_HSTS_MAX_AGE` | unset (no HSTS) | Yes, `certharbor-web` | Seconds of `Strict-Transport-Security` nginx sends on the SPA document and its assets, never on API responses. The backend sends no HSTS at all. **Failure mode:** a non-integer value makes nginx exit non-zero. A long value on a name whose certificate chains to a private root leaves browsers unable to reach the name if that certificate ever stops validating, with no way to undo it from the server |
| `WEB_RESOLVER` | `127.0.0.11` | Yes, `certharbor-web` | DNS server nginx re-resolves `certharbor` against on every request, which is what lets a restarted backend container be followed without restarting nginx. **Failure mode:** a wrong resolver makes every API path answer the `upstream-unavailable` 503 |
| `WEB_RELOAD_INTERVAL` | `60` | Yes, `certharbor-web` | Seconds between the reload watcher's checks of `web.crt` and `web.key` |
| `WEB_RATE_LIMIT` | `20r/s` | Yes, `certharbor-web` | Sustained request rate nginx allows per client address, in nginx `limit_req_zone` rate syntax. A client over it gets a 429. **Failure mode:** a value nginx cannot parse makes it exit non-zero |
| `WEB_RATE_BURST` | `40` | Yes, `certharbor-web` | Requests a client address may send above `WEB_RATE_LIMIT` before nginx answers 429. **Failure mode:** a non-integer value makes nginx exit non-zero |
| `WEB_CERT_WAIT_SECONDS` | `120` | No | How long nginx waits for the web leaf under `WEB_TLS_MODE=stepca` before serving the 503 that names the missing file |
| `IMAGE_TAG` | `local` | Yes, both services | Tag for both application images, `certharbor-backend:<tag>` and `certharbor-web:<tag>`, so an accidental partial upgrade cannot happen. `BACKEND_IMAGE` and `WEB_IMAGE` override one half each for a deliberate rollback. **Failure mode:** the two halves at different contract versions make the SPA show its contract-skew banner and calls to changed operations fail |
| `BACKEND_IMAGE` | `certharbor-backend:${IMAGE_TAG}` | Yes | Full image reference for `certharbor`, overriding `IMAGE_TAG` for that half only |
| `WEB_IMAGE` | `certharbor-web:${IMAGE_TAG}` | Yes | Full image reference for `certharbor-web`, overriding `IMAGE_TAG` for that half only |

### Let's Encrypt

| Variable | Default | In compose | Description |
|---|---|---|---|
| `LE_ACME_DIRECTORY_URL` | `https://acme-v02.api.letsencrypt.org/directory` | **No**, only in `compose.e2e-le.yml` | ACME directory issuance dials. Env-only on purpose, a manager-editable database setting must not be able to repoint where issuance actually goes |

### Local password login

| Variable | Default | In compose | Description |
|---|---|---|---|
| `LOCAL_LOGIN_ENABLED` | `true` | Yes | Shows the username/password form. See below |

### OIDC SSO

| Variable | Default | In compose | Description |
|---|---|---|---|
| `OIDC_ENABLED` | `false` | Yes | Enable OIDC SSO |
| `OIDC_ISSUER_URL` | empty | Yes | Provider issuer URL |
| `OIDC_CLIENT_ID` | empty | Yes | OAuth2 client ID from the IdP |
| `OIDC_CLIENT_SECRET` | empty | Yes | OAuth2 client secret from the IdP |
| `OIDC_REDIRECT_URL` | empty | Yes | Must be `https://<WEB_HOSTNAME>[:<UI_HTTPS_PORT>]/auth/oidc/callback`, on nginx's published origin and never under `/app/`, since the IdP holds the registered value |
| `OIDC_GROUP_CLAIM` | `groups` | Yes | ID token claim that carries group membership |
| `OIDC_GROUP_ADMIN` | empty | Yes | IdP group name mapped to `admin` |
| `OIDC_GROUP_MANAGER` | empty | Yes | IdP group name mapped to `manager` |
| `OIDC_GROUP_VIEWER` | empty | Yes | IdP group name mapped to `viewer` |
| `OIDC_DEFAULT_ROLE` | empty (deny) | Yes | Role assigned when no group matches. Must be `viewer`, `manager`, `admin` or empty, or startup fails, **but only when `OIDC_ENABLED=true`**: the check is skipped otherwise |
| `OIDC_SYNC_ROLE` | `true` | Yes | Re-syncs role from IdP groups on every login |

Every OIDC variable and `LOCAL_LOGIN_ENABLED` reach the `certharbor` service from `.env` through `docker-compose.yml`. `compose.e2e-oidc.yml` points them at a mock IdP for the e2e suite. See [docs/authentication.md](authentication.md) for the login and group-mapping behaviour these variables configure.

After changing `.env` (for a variable the compose file does pass through), recreate the containers:

```bash
docker compose up -d --force-recreate
```

See also: [docs/authentication.md](authentication.md), [docs/security.md](security.md), [docs/deployment.md](deployment.md).
