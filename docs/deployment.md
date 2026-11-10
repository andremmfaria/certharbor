# Deployment

Requirements, the compose stack, secrets, the container image, and the Makefile.

## Requirements

- Docker Engine with the Compose plugin (v2). `docker-compose.yml` has no top-level `version:` key, which is what actually marks it as a version-less Compose Spec file, not the top-level `secrets:` with file references or `depends_on: condition: service_healthy` it also uses, both of which exist in the older, versioned Compose formats too.
- Ports: `443/tcp` by default for the UI (configurable via `UI_HTTPS_PORT`, published by the nginx origin `certharbor-web`, the backend publishes none), and `9443/tcp` for the step-ca API, which the stock `docker-compose.yml` publishes unconditionally (`ports: ["9443:9443"]` on the `step-ca` service). Remove that mapping yourself if you don't want step-ca reachable directly from outside the host.
- A reachable PostgreSQL database if you're not using the bundled `postgres` container (the bundled container pins `postgres:16-alpine`, no other version is tested against this codebase).

This page intentionally does not give CPU/RAM/disk sizing numbers or a supported-OS list: nothing in the repository benchmarks or states them, so a table here would be invented rather than verified.

## Quick start

```bash
git clone https://github.com/andremmfaria/certharbor.git
cd certharbor
make setup   # copies .env.example → .env and generates secrets/
```

Edit `.env`: set `HOST_IP`, `WEB_HOSTNAME`, `UI_HTTPS_PORT`, `PROVISIONER`, `TZ`, and **`CERTHARBOR_ADMIN_PASSWORD`**. Two are not optional: if no admin user exists yet and the admin password is empty, the backend fails to start (see [docs/configuration.md](configuration.md#core-deployment)), and an empty `WEB_HOSTNAME` stops both the backend and nginx, because it is the only name on nginx's certificate and nginx refuses a handshake for any other name. Then:

```bash
make up      # docker compose up -d --build
```

The UI is then at `https://<WEB_HOSTNAME>:<UI_HTTPS_PORT>/`. The SPA is served under `/app/`, and every other path, including `/login`, `/reset-password`, `/auth/oidc/callback` and `/static/`, is proxied to the backend's server-rendered pages. That split is temporary until the SPA owns the root, except that `/reset-password` and the OIDC callback stay at the root for good, because reset links are emailed and the redirect URI is registered with the IdP.

Run `make help` to see every target.

## Docker Compose services

Four services, defined in `docker-compose.yml`:

| Service | Image | Notes |
|---|---|---|
| `postgres` | `postgres:16-alpine` | `postgres-data` volume, healthcheck via `pg_isready` |
| `step-ca` | `${STEP_CA_IMAGE:-smallstep/step-ca:0.30.2}` | `step-ca-data` volume, bootstrapped by `scripts/step-ca-bootstrap.sh`, publishes `9443` |
| `certharbor` | `${BACKEND_IMAGE:-certharbor-backend:${IMAGE_TAG:-local}}`, built from `backend/Dockerfile` | The Go backend. `certharbor-certs`, `certharbor-ssl`, `certharbor-web-ssl`, `certharbor-data`, `certharbor-uploads` volumes, plus a read-only mount of `step-ca-data` at `/home/step`. Publishes **no** host port. Its healthcheck is `curl -fsk https://localhost:8443/health` plus the absence of `/opt/certharbor/ssl/.fallback`, so a backend that fell back to a self-signed leaf reports unhealthy |
| `certharbor-web` | `${WEB_IMAGE:-certharbor-web:${IMAGE_TAG:-local}}`, built from `frontend/Dockerfile` with the repository root as context | nginx, the only public origin: publishes `${UI_HTTPS_PORT:-443}` mapped to its `8443`, serves the SPA and proxies everything else to `certharbor`, verifying the backend's leaf. Mounts only `certharbor-web-ssl`, read-only. `init: true`, and starts only once `certharbor` is healthy (`depends_on: condition: service_healthy`). Its healthcheck is the image's own |

`certharbor-web-ssl` carries nginx's `web.crt`, `web.key` and the CA's `root_ca.crt`: the backend writes it, nginx reads it. `certharbor-ssl`, which holds the backend's `server.key`, and `step-ca-data` are never mounted into nginx. Debug the backend with `docker compose exec certharbor ...` rather than by publishing its port: `make preview` and CI fail on any compose file that gives `certharbor` a `ports:` entry (`scripts/check-backend-port.sh`).

All four share a bridge network named `step-network` (compose service key `step-net`), pinned to `${STEP_NET_SUBNET:-10.213.0.0/24}`, which sits outside `172.16.0.0/12`. No service carries a static address: the backend trusts nginx by resolving its name. If the subnet collides with a LAN or VPN route, set `STEP_NET_SUBNET` in `.env` and run `docker compose down && docker network rm step-network` before bringing the stack back.

Several `compose.e2e-*.yml` overlay files exist for CI and local e2e runs only, they are not part of a normal deployment, see [docs/development.md](development.md#end-to-end-tests-playwright) for what each one does.

## Secrets

`make setup` creates `secrets/` (mode 700) and three files, each mode 644 on the host (not 600: a plain, non-Swarm `docker compose` bind-mount preserves the host file's own owner and mode rather than remapping it the way Swarm secrets do, and `certharbor` runs as a non-root uid, so a 600 file owned by the host user would be unreadable inside the container):

| File | Used for |
|---|---|
| `secrets/postgres_password` | PostgreSQL password |
| `secrets/secret_key` | Session/CSRF signing key |
| `secrets/ca_password` | step-ca provisioner password |

Each is mounted read-only at `/run/secrets/<name>` and never appears in `docker inspect`'s environment output or `/proc/<pid>/environ`. Regenerate them with `make setup FORCE=1`.

## Container images

Two images, built by `.github/workflows/docker-build.yml` as a matrix:

| Image | Built from |
|---|---|
| `ghcr.io/andremmfaria/certharbor` | `backend/Dockerfile`, context `./backend` |
| `ghcr.io/andremmfaria/certharbor-web` | `frontend/Dockerfile`, context `.` |

| Trigger | Pushed? | Tags |
|---|---|---|
| Push to `main` | Yes | `main` (branch ref), short commit SHA |
| Push of a `v*` tag | Yes | full semver, `{major}.{minor}`, short commit SHA |
| `workflow_dispatch` | Yes | same rules as above, applied to whatever ref the dispatch runs against |
| Pull request | No, build only | PR ref (computed, never pushed) |

Both are built for `linux/amd64` only, `docker-build.yml` sets no `platforms:` input on `docker/build-push-action`. Compose names them `certharbor-backend` and `certharbor-web` locally, both tagged `${IMAGE_TAG:-local}`, and `BACKEND_IMAGE` and `WEB_IMAGE` pin one half for a deliberate rollback, see [docs/configuration.md](configuration.md#web-origin-nginx).

`backend/Dockerfile` is a two-stage build: `golang:1.26.10-alpine3.23` compiles a static (`CGO_ENABLED=0`) binary, then `alpine:3.23` runs it as a non-root user (uid/gid `10001`). It declares `EXPOSE 8443`, a `HEALTHCHECK` that curls `https://localhost:8443/health`, `USER certharbor`, and `ENTRYPOINT ["/entrypoint.sh"]`, which builds `DATABASE_URL` from the secret file before exec-ing the Go binary. The runtime image installs `curl`, `openssl`, `ca-certificates`, `netcat-openbsd`, `tzdata` and `postgresql-client` (the last for `pg_dump` in the backup handler), and nothing named `step`: certificate issuance, renewal and revocation go through `github.com/smallstep/certificates/ca` and `.../api` directly. `openssl` stays only because the admin diagnostic console has an `openssl.version` entry.

`frontend/Dockerfile` builds the TypeScript client from the committed `backend/openapi/openapi.json` and then the SPA in a `node:24-alpine3.23` stage, and copies the bundle into `nginx:1.29-alpine` at `/usr/share/nginx/html/app/`. The runtime image contains no Node. Its nginx master runs as root because it reads `web.key`, which the backend writes `0600` as uid `10001`. The root `.dockerignore` keeps `secrets/`, `.env`, `backups/` and every `node_modules/` out of that build context.

```bash
docker pull ghcr.io/andremmfaria/certharbor:main
docker pull ghcr.io/andremmfaria/certharbor-web:main
```

To build locally without pushing:

```bash
docker build -f backend/Dockerfile backend
docker build -f frontend/Dockerfile .
```

Running the backend standalone (without Compose) needs a reachable PostgreSQL instance, a running step-ca and nginx in front of it for the SPA. For most deployments, use `docker-compose.yml` via `make up`.

## Makefile targets

From `make help` (`.DEFAULT_GOAL := help`, self-documenting via `## ` comments):

| Target | Description |
|---|---|
| `help` | Show this help |
| `setup` | Bootstrap: copy `.env.example` and generate `secrets/` |
| `up` | Build images and start all services in detached mode |
| `down` | Stop and remove containers (volumes preserved) |
| `restart` | Stop then start all services |
| `logs` | Stream logs from all services |
| `ps` | Show container status |
| `update` | Pull latest images and rebuild |
| `backup` | Dump the database and named volumes into `backups/<timestamp>/`, with a SHA-256 manifest |
| `test` | Run Go tests with the race detector |
| `build` | Build the Go binary |
| `fmt` | Format Go source with gofumpt |
| `lint` | Run golangci-lint and check formatting |
| `cover` | Run the coverage gate |
| `openapi` | Regenerate `backend/openapi/openapi.json` from the huma-registered operations |
| `hooks` | Register the `openapi.json` merge driver in this clone's git config |
| `client` | Generate, build and pack the TS client to `clients/ts/dist/client.tgz` |
| `frontend-install`, `frontend-build`, `frontend-lint`, `frontend-typecheck`, `frontend-test`, `frontend-sdk-surface` | The SPA's install, build, lint, typecheck, Vitest and dead-SDK-surface steps |
| `e2e-install` | Install the e2e harness deps (plus Chromium for a host-side run) |
| `e2e-fresh` | Destroy every e2e volume and bring the stack back up healthy |
| `e2e-main` | PR-tier suite: `api` then `ui`, against the long-lived stack |
| `e2e-quick` | Pre-push subset, `api` only, roughly 2 minutes |
| `e2e-bootstrap` | Run one bootstrap scenario: `make e2e-bootstrap SCENARIO=fingerprint` |
| `e2e-restart-ui` | Restart certharbor, clearing both process-local rate limiters |
| `e2e-reset-ssl` | Remove both leaf volumes (`certharbor-ssl` and `certharbor-web-ssl`), leaving users and CA state, then bring the whole stack back |
| `e2e-seed-history` | Insert N synthetic `cert_history` rows: `make e2e-seed-history N=25` |
| `e2e-le-certs` | Generate the local ACME server's TLS material for the Let's Encrypt leg |
| `clean` | Remove build artifacts, the generated client, the SPA bundle and old backups (`secrets/` and `.env` are untouched) |
| `nginx-lint` | Fixture nginx against a stub upstream: routing, headers, cache, failure documents (no compose) |
| `proxy-routes-check` | Assert nginx and the Vite dev proxy both derive from `frontend/proxy-prefixes.json`, and the SPA base agrees across Vite, nginx, the image and the entrypoint |
| `config-keys-check` | Assert every web-origin key has a `docs/configuration.md` row and an `.env.example` line |
| `backend-port-check` | Assert no compose file, base or override, publishes a host port for `certharbor` |
| `preview` | Build both images, run all four containers healthy, smoke-test the origin |
| `dev` | Start postgres, step-ca and certharbor as already built (`--wait`, never `--build`), then Vite with hot reload on the host |
| `up-backend` | Rebuild and start postgres, step-ca and certharbor only |
| `up-web` | Rebuild and restart certharbor-web alone against the running backend |
| `logs-ui`, `logs-web` | Stream certharbor's or certharbor-web's logs |
| `nginx-test` | Run `nginx -t` inside the running certharbor-web |
| `web-reload` | Validate and reload nginx in the running certharbor-web, no restart |

`make backup`'s manifest format differs from the admin UI's own backup export (`backend/handlers/backup.go`), the two are separate implementations that both produce a SHA-256-checksummed manifest.

## `make update`

```bash
make backup   # snapshot first
make update   # docker compose pull + up -d --build
```

Database migrations run automatically on startup (`backend/db/schema.go`). `make backup` archives every named volume, `certharbor-web-ssl` included.

See also: [docs/configuration.md](configuration.md), [docs/development.md](development.md), [docs/security.md](security.md).
