# Architecture

What runs in production today, the request path, and the state of the SPA migration.

## Components today

Four containers, wired by `docker-compose.yml`:

| Container | Image / build | Role |
|---|---|---|
| `certharbor-console` | built from `frontend/Dockerfile` | nginx, the only public origin. Terminates TLS for `WEB_HOSTNAME` with its own leaf, serves the React SPA at the root, and proxies everything else to `certharbor-control` over TLS, verifying the backend's leaf against the CA root. Holds no session or database access |
| `certharbor-control` | built from `backend/Dockerfile` | Go binary, publishing no host port. API-only (chi router): a small versioned JSON API, no server-rendered HTML, no `html/template`. Owns sessions, CSRF, rate limiting, and all step-ca/database access |
| `postgres` | `postgres:16-alpine` | Users, certificate history, Let's Encrypt state, notification settings, password-reset tokens |
| `step-ca` | `smallstep/step-ca:0.30.2` (pinned via `STEP_CA_IMAGE`) | The actual PKI. certharbor-control never runs the `step` CLI against it: `backend/stepca/` wraps `github.com/smallstep/certificates/ca` and `github.com/smallstep/certificates/api` directly (pinned to the same v0.30.2), so certificate issuance and revocation are Go library calls, not shelled-out subprocesses. Renewal has no separate call path in `backend/stepca/`, it is a re-issue through the same `issueCert` helper handlers use for a fresh certificate (`backend/handlers/cert_ops.go:118`, `backend/handlers/certs.go:225-250`) |
| `certharbor-delivery` | **planned, not yet built** | A separate worker process (no HTTP listener) that pulls renewed certificates from the existing inventory and pushes cert+chain+key to external targets (AWS ACM first) under a declarative registry. The only plane that would hold target credentials (AWS roles, kubeconfigs, SSH keys, Cloudflare tokens); `certharbor-console` and `certharbor-control` never would. See [plans/distribution.md](../plans/distribution.md) |

`backend/Dockerfile` ships `openssl` only because the admin diagnostic console has an "OpenSSL version" entry (`openssl.version`, see [docs/security.md](security.md)). No `step` binary is installed in the image.

## Request flow

```
                       ┌────────────────┐            ┌──────────────┐
   Browser ── HTTPS ──►│ certharbor-console │── HTTPS ──►│ certharbor-control │  Go, chi router, port 8443
                       │ (nginx)        │            │              │
                       └────────────────┘            └──┬───────┬───┘
                        / : the SPA bundle               │       │
                        everything else : proxied  SQL ◄┘       └► HTTPS API (ca library)
                                                     ┌──▼──┐ ┌────▼──────┐
                                                     │ pg  │ │ step-ca   │  port 9443
                                                     │ 16  │ │ (PKI)     │
                                                     └─────┘ └───────────┘

   certharbor-console publishes :443 (UI_HTTPS_PORT) → listens on :8443
   certharbor-control publishes nothing, nginx reaches it as certharbor-control:8443 on step-network
   step-ca exposes :9443, published on the host by default (docker-compose.yml
   maps "9443:9443") → remove that port mapping yourself to keep it internal-only
```

nginx sends the prefixes in `frontend/proxy-prefixes.json` (`/api`, `/auth`, `/health`, `/ready`, `/openapi.json`) to the backend, and serves everything else from the SPA bundle at the root with an `index.html` fallback for deep links. Every proxying block includes `frontend/conf.d/proxy-headers.conf`, which replaces `X-Forwarded-For` and `X-Real-IP` with the real peer and clears `True-Client-IP` and `Forwarded`. The backend trusts that header only from the address `WEB_UPSTREAM_HOST` resolves to. nginx re-resolves `certharbor-control` per request, so a restarted backend is followed without restarting nginx, and answers a problem document with type `upstream-unavailable` when the backend is unreachable.

Inside `certharbor-control`, one `chi.Router` serves the API only, on a single port: a versioned JSON API mounted at `/api/v1` by `humaapi.Mount` (`backend/api/`), described by an OpenAPI 3.1 document at `backend/openapi/openapi.json`. There is no server-rendered UI, no `backend/templates/`, no `html/template` — the SPA (served by `certharbor-console`/nginx, see above) is the only UI.

`/health` (liveness, always 200 while the process is up) and `/ready` (readiness, checks a database ping, step-ca reachability and both TLS leaves, reporting each leaf's `notAfter`, and answers 503 with a body naming what failed, including the self-signed fallback marker, a missing web leaf, or a leaf overdue for renewal) sit outside the versioned API, unauthenticated.

## Roles

| Role | View | Issue / Import | Revoke | Manage users |
|---|---|---|---|---|
| `viewer` | Yes | No | No | No |
| `manager` | Yes | Yes | No | No |
| `admin` | Yes | Yes | Yes | Yes |

Temporary users (created in `backend/handlers/admin_temp.go`) can hold any of the three roles and are auto-deactivated once their `expires_at` timestamp passes: a background goroutine in `backend/main.go` (started around line 354) calls `db.ExpireOverdueTempUsers` every 60 seconds.

## The `/api/v1` surface today

This is genuinely small. `backend/api/register.go` registers exactly:

| Method | Path | Auth | Purpose |
|---|---|---|---|
| POST | `/api/v1/session` | public (CSRF-checked, rate-limited) | Log in with a username and password |
| POST | `/api/v1/session/mfa` | public (CSRF-checked, rate-limited) | Complete a login with a second factor |
| GET | `/api/v1/session` | optional | Current session state, exempt from idle-timeout renewal so an open tab cannot keep a session alive forever |
| DELETE | `/api/v1/session` | public (CSRF-checked only when a session is present) | Log out, expiring the session and CSRF cookies |
| GET | `/api/v1/config` | public | Public runtime flags: `oidcEnabled`, `oidcButtonLabel`, `acmeEnabled`, `appVersion`, `contractSha`, `roleLevels`, `sessionIdleTimeoutSeconds`, `expiringSoonDays`. Note `oidcButtonLabel` and `acmeEnabled` are spike placeholders with no backing config field yet |
| GET | `/api/v1/status` | `viewer` | Active/expiring certificate counts |
| GET | `/api/v1/_spike/blob` | `viewer` | Proves the binary-download response mechanic a later phase reuses |
| POST | `/api/v1/_spike/upload` | `manager` | Proves the multipart-upload mechanic a later phase reuses |

An unmatched path under `/api/v1` gets an RFC 9457 problem document instead of a 404 page.

## Migration state

The repository has completed [plans/frontend-backend-split.md](../plans/frontend-backend-split.md)'s removal and hardening phase: the React SPA, talking to `certharbor-control` only through the versioned JSON API, owns the root. `base` in `frontend/vite.config.ts` is `/`, the router's `basename` follows it, and nginx serves the bundle from `/` with no legacy passthrough. `/reset-password` and the OIDC callback stay at the root, as they always have, because reset links are emailed and the OIDC redirect URI is registered with the IdP.

`frontend/` is the Vite, React and TypeScript app (React 19.3.1, Vite 8.3.5, `@vitejs/plugin-react` 6.2.1), its nginx config (`nginx.conf.template`, `conf.d/`), its entrypoint (`entrypoint.sh`: certificate wait, reload watcher, CSP hashes of the served `index.html`'s inline blocks) and `frontend/Dockerfile`.

### The generated TypeScript client

`clients/ts` (`@kantharos-ai/certharbor-client`) is generated from `backend/openapi/openapi.json` by `@hey-api/openapi-ts` in the `client` job of `.github/workflows/ci.yml`, which runs on a push to `main`, a `client-v*` tag push, a pull request, and `workflow_dispatch`. It is published to GitHub Packages only on a `client-v*` tag.

The version is never hand-edited. The `VERSION` file at the repository root is the single source of the project version, and `scripts/sync-version.sh` writes it into the backend, the OpenAPI document and the three Node packages (`make version`). The `version` job of `lint-meta.yml` fails if any of them disagrees. `scripts/client-version.sh` derives the client's published version as `<VERSION>-sha.<short-sha>`. The appended short commit SHA tells two builds of the same version apart and lets a consumer prove which commit the client was generated from. The `frontend` CI job asserts the installed client's version string ends in the current commit's short SHA before it will typecheck or build (this is what `docs/contract-proof.md` demonstrates end to end with a deliberately regressed commit).

`frontend` never declares the client as a package.json dependency (D8 of the plan): CI installs the freshly packed tarball with `npm install --no-save` so the client under test is always the one generated from the commit being built, never a stale published version from an npm cache.

### Contract documentation

- [docs/contract-changes.md](contract-changes.md) is an append-only log of every breaking API change, gated by `scripts/contract-gate.sh` (backed by `oasdiff`) so a breaking change without a matching row fails CI.
- [docs/contract-proof.md](contract-proof.md) is a two-commit worked proof that the provenance check above actually rejects a client that doesn't match the commit under test, not just that the check exists.

See also: [docs/configuration.md](configuration.md), [docs/deployment.md](deployment.md), [docs/development.md](development.md).
