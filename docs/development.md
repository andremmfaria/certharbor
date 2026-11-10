# Development

Local dev loop, tests, lint gates, regenerating the API contract, and the project layout.

## Local dev loop

The backend runs only in its container: running the Go binary on the host would need postgres, step-ca and the secret files reachable there, which this repository does not support.

```bash
make up            # build both images and start all four containers
make dev           # postgres, step-ca and certharbor-control as already built, then Vite on :5173
make preview       # build both images, run the full stack healthy, smoke-test the origin
```

`make dev` starts the three backend services with `--wait` and never `--build`, so after a Go change run `make up-backend` first. It brings the backend up with `SESSION_SECURE=false`, because Vite serves plain HTTP on `localhost` and the `__Host-` cookie prefix needs HTTPS. The backend publishes no host port, so Vite's proxy reaches it at its address on `step-network`, which works on a Linux host and not under Docker Desktop. Vite serves the SPA under `/app/` and proxies only the prefixes in `frontend/proxy-prefixes.json`, so the server-rendered pages are not reachable through it. It exercises none of nginx's headers, CSP, caching or body limits: `make preview` is the check for any change to `nginx.conf.template`, `vite.config.ts`, the CSP or the prefix list.

`make up-web` rebuilds and restarts nginx alone against the running backend. `make nginx-test` runs `nginx -t` in the running container and `make web-reload` validates and reloads it. `make logs-ui` and `make logs-web` stream one service's logs.

When submitting changes:

- Run `make fmt` and `make lint` before pushing.
- Update relevant tests.
- Keep commits focused and descriptive, following this repository's actual convention: `area: lowercase summary` (`docs(plans):`, `ci:`, `e2e:`, and so on, one area per commit). There is no `CONTRIBUTING` file, the convention is inferred from `git log`.

## Tests

### Unit tests

```bash
make test   # go test -race ./... in backend/
make cover  # go test -coverprofile=coverage.out ./..., then
            # COVERPROFILE=coverage.out THRESHOLD=15 backend/scripts/coverage-gate.sh
```

`ci.yml`'s `build-test-lint` job runs `gofmt -l`, `go vet`, `go build`, `go test -race -coverprofile=coverage.out`, the coverage gate, and `golangci-lint` v2.12.2 via `golangci-lint-action@v9.2.1`, then asserts the depguard fixture in `scripts/lint-fixtures.sh` actually gets rejected. Lint runs against the whole tree, there is no `new-from-rev` or `only-new-issues` baseline that would let a pre-existing finding through. The ruleset itself lives in `backend/.golangci.yml`: `errcheck`, `govet`, `staticcheck`, `ineffassign`, `unused`, `misspell`, `revive`, `gocritic`, `errorlint`, `bodyclose`, `noctx`, `gosec`, `unconvert`, `unparam`, `sloglint`, `depguard` and `gomodguard_v2`.

The coverage gate itself is a floor, not a ratchet against the previous run's number: `THRESHOLD=15` is a fixed value in CI today (`backend/scripts/coverage-gate.sh`'s own comment calls it "the honest measured baseline set in PR-22"). Package coverage from a `go test -coverprofile` run (`backend/coverage.out`) is uneven: `security` 97.0%, `middleware` 90.5%, `config` 85.7% are well covered by unit tests. `le` sits at 45.8% and `handlers` at 17.0%, both leaning on integration and e2e tests rather than unit mocks for a lot of their behaviour. `db` is at 0.8% under a plain unit run because its real coverage lives behind the `integration` build tag and only shows up under `go test -tags=integration`.

### DB integration tests

`ci.yml`'s `db-integration` job runs against a real `postgres:16-alpine` service container:

```bash
go test -tags=integration -race -count=1 ./db/...
```

### End-to-end tests (Playwright)

`test/e2e/` has 31 spec files under `specs/`, organized by domain (`auth/`, `admin/`, `certs/`, `csrf/`, `health/`, `rbac/`, and so on). `playwright.config.ts` defines three projects:

| Project | Matches | Notes |
|---|---|---|
| `api` | `*.api.spec.ts` | Runs as a container on the compose network. Dials nginx at `BASE_URL` (default `https://certharbor-console:8443`), and the backend directly at `BACKEND_URL` (default `https://certharbor-control:8443`) only where a test must bypass the origin, the health tier for one |
| `ui` | `*.ui.spec.ts` | Same network, drives a real Chromium via Playwright against `BASE_URL` |
| `infra` | `*.infra.spec.ts` | Runs on the host, brings up its own stack without nginx and dials the backend at its `step-network` address (`backendAddress()` in `helpers/compose.ts`), since the backend publishes no host port. Five spec files under `specs/boot/` cover E2E-BOOT-01 to -07 and -09 (see below). `make e2e-bootstrap` runs `--project=infra` filtered to whichever IDs the chosen `SCENARIO` names |

nginx refuses a TLS handshake for any name but `WEB_HOSTNAME`, so an in-network run needs `WEB_HOSTNAME=certharbor-console` in `.env`, which is what `e2e.yml` writes.

`.github/workflows/e2e.yml`'s `image` job builds the backend and frontend images on separate cache scopes (`e2e-backend`, `e2e-frontend`), asserts neither contains Node, and ships both in one `docker save | gzip` artifact. `e2e-main` loads them, materialises the Playwright-plus-docker-CLI harness image from its layer cache, brings up `docker-compose.yml` plus `compose.e2e-image.yml`, asserts the backend has no host binding, and runs the `api` project and then the `ui` project's `specs/spa/` files. The rest of the `ui` project runs locally via `make e2e-main` (which runs `api` then `ui`).

`specs/spa/spa-shell.ui.spec.ts` drives the SPA through nginx: login under `/app/` with `__Host-` cookies, navigation, a hard refresh of a deep link, logout, a refresh mid-MFA, a CSP smoke over every route in `frontend/src/routes/routeList.ts` against the policy read off nginx's response, and the unavailable state with the backend stopped. `specs/origin/origin-01.api.spec.ts` asserts every server-rendered GET route registered in `backend/main.go` and every file under `backend/static/` still answers through the origin.

Several compose overlays exist purely to reach test scenarios the stock stack can't. `test/e2e/scenario.sh` (invoked by `make e2e-bootstrap SCENARIO=...`) is the driver behind the bootstrap scenarios. It composes `compose.e2e-noweb.yml` into every scenario, plus, chosen per scenario, `compose.e2e-image.yml`, `compose.e2e-nodeps.yml`, `compose.e2e-fingerprint.yml`, and `compose.e2e-fatals.yml` (with `compose.e2e-nodeps.yml` for the `fatals` case). `scripts/step-ca-bootstrap.sh` is unrelated to any of this, it is the CA init script `docker-compose.yml` runs inside the `step-ca` container itself.

| Overlay | Purpose | Applied by |
|---|---|---|
| `compose.e2e-fatals.yml` | Turns off `restart: unless-stopped` so an intended crash stays exited and its exit code can be read | `scenario.sh` (`fatals`) |
| `compose.e2e-fingerprint.yml` | Drops the read-only `step-ca-data` mount so `CA_FINGERPRINT`'s fetch path is actually exercised | `scenario.sh` (`fingerprint`) |
| `compose.e2e-image.yml` | Runs both `certharbor-control` and `certharbor-console` from the images the CI `image` job already built, instead of `up -d --build` | `e2e.yml`, and `scenario.sh` when `E2E_USE_PREBUILT_IMAGE=1` |
| `compose.e2e-noweb.yml` | Gives `certharbor-console` a profile nothing enables, so nginx never exists for the `infra` tier | `scenario.sh`, every scenario |
| `compose.e2e-nodeps.yml` | Lets `certharbor-control` start against a stopped step-ca or postgres | `scenario.sh` (`ca-down`, `fatals`) |
| `compose.e2e-config.yml` | Passes `USE_HTTPS` and `ALLOWED_DOMAIN_SUFFIXES` through, neither of which the stock compose file wires up | the nightly `oidc-mail` leg ([plans/e2e-tests.md](../plans/e2e-tests.md), section 2.7.1), not `scenario.sh` |
| `compose.e2e-le.yml` | A local ACME server (pebble) for the Let's Encrypt leg | the nightly `oidc-mail` leg |
| `compose.e2e-mail.yml` | A mail catcher for reset-link and notification tests | the nightly `oidc-mail` leg |
| `compose.e2e-oidc.yml` | A mock OIDC IdP, with `OIDC_*` defaults pointing the backend at it and the redirect URI on nginx's origin | the nightly `oidc-mail` leg |

`make nginx-lint` tests the nginx config without compose: it builds the `nginx-fixture` target of `frontend/Dockerfile` (the real config and entrypoint over the placeholder bundle in `frontend/nginx-lint/fixture-dist/`, no Node stage), runs it against a stub upstream with throwaway certificates on a throwaway network, and asserts the proxied prefixes, the `/app/` fallback, the legacy passthrough with its forgery table, the `PHASE9-DELETE` markers, the header set, the cache policy, the failure documents and the entrypoint's modes, with a 60-second budget measured after the image build. `make proxy-routes-check` asserts nginx's proxy location and the Vite dev proxy both derive from `frontend/proxy-prefixes.json` and that the SPA base agrees across `vite.config.ts`, nginx, the image and the entrypoint. `make config-keys-check` asserts every web-origin key has a row in the configuration reference, and `make backend-port-check` that no compose file publishes a host port for `certharbor-control`.

The migration plan ([plans/frontend-backend-split.md](../plans/frontend-backend-split.md), section 10) describes the e2e suite as **active**, not paused. Four `ui` spec files assert against the server-rendered markup the migration eventually deletes and will need rewriting or retiring once that markup changes.

`specs/boot/` adds the `infra` project's coverage of the bootstrap matrix (Section 3.1 of [plans/e2e-tests.md](../plans/e2e-tests.md)): `boot-fingerprint.infra.spec.ts` (E2E-BOOT-01, -05, -06, one file since the three share a CA identity across the job), `boot-ca-down.infra.spec.ts` (E2E-BOOT-02, -09), `boot-provided.infra.spec.ts` (E2E-BOOT-03), `boot-selfsigned.infra.spec.ts` (E2E-BOOT-04), and `boot-fatals.infra.spec.ts` (E2E-BOOT-07's five cases plus its positive control). The first four files were verified with real `make e2e-bootstrap SCENARIO=<fingerprint|ca-down|provided|selfsigned>` runs on 2026-09-02; `boot-fatals.infra.spec.ts` is marked `test.fixme` (see the file's own comment) because case (c)'s timing against a stopped-not-absent postgres was not re-confirmed after a same-day fix to how the other cases reach postgres. No CI workflow invokes the `infra` project, only `lint-meta.yml`'s harness `tsc`/`eslint` steps cover these files, see `plans/e2e-implementation-status.md`.

## Regenerating the API contract

```bash
cd backend
go run ./cmd/openapi -out openapi/openapi.json   # or: make openapi
```

`ci.yml`'s `client` job fails if this drifts from the committed `openapi.json`. `make hooks` registers a git merge driver that reruns this automatically on a local merge or rebase (note: GitHub's own merge button, squash-merge, and merge queues never invoke local git config, so this only helps a local `git merge`/`git rebase`).

To regenerate the TypeScript client package (`clients/ts`):

```bash
cd clients/ts
npm ci
npm run generate   # @hey-api/openapi-ts against backend/openapi/openapi.json
npm run build
```

The published version string is computed by `scripts/client-version.sh`, never hand-edited, see [docs/architecture.md](architecture.md#the-generated-typescript-client). `ci.yml`'s `client` job does more than build it: it stamps the computed version onto the generated package with `npm version --no-git-tag-version`, runs `publint` and `attw --pack . --profile esm-only --ignore-rules internal-resolution-error` against it, then `npm pack`s the result for the `frontend` job to install (`ci.yml:174-194`).

## Frontend

`frontend/` is the Vite, React 19 and TypeScript single-page app. It needs Node 22.22 or newer. The generated client is not in its `package.json`: `make client` packs it to `clients/ts/dist/client.tgz` and `make frontend-install` runs `npm ci` and then `npm install --no-save` of that tarball (D8).

```bash
make client frontend-install
make frontend-typecheck frontend-lint frontend-test frontend-sdk-surface frontend-build
```

`make dev` runs Vite on `:5173`, serving the SPA under `/app/` (Vite's `base`, which the router's `basename` follows) and proxying the prefixes in `frontend/proxy-prefixes.json` to `DEV_BACKEND_URL`. `src/api/client.ts` is the only file allowed to touch `fetch`, and ESLint bans `fetch`, `XMLHttpRequest`, `EventSource`, `axios`, `dangerouslySetInnerHTML`, `eval` and `new Function` everywhere else under `src/`. `npm run check:sdk-surface` fails when a spec operation is imported nowhere in `src/` and absent from `clients/ts/unused-allowlist.txt`, and also when an allowlist entry is stale.

`src/zodConfig.ts` is the first module `main.tsx` imports and sets zod's `jitless`, because zod otherwise probes for `eval` with `new Function`, which nginx's `script-src 'self'` reports as a CSP violation. `src/routes/routeList.ts` is the committed route list. The router is built from it and the route tests are table-driven from it. `src/styles/` is a copy of `backend/static/css/` plus `spa.css`, and `lint-meta.yml` runs stylelint on both.

## Lint gates

| Workflow | Job(s) |
|---|---|
| `ci.yml` | `build-test-lint`, `db-integration`, `client`, `frontend` (provenance, typecheck, lint, test, dead-surface check, build), `origin` (`proxy-routes-check`, `config-keys-check`, `backend-port-check`, the `collect.sh` service-list assertion, `nginx-lint`), `contract-negative`, `ci-gate` (fails unless the first four and `origin` all succeeded) |
| `lint-meta.yml` | `hadolint` (a matrix over `backend/Dockerfile` and `frontend/Dockerfile`), `actionlint`, `yamllint`, `style` (stylelint/eslint/djlint plus the e2e harness's own typecheck/eslint) |
| `security.yml` | `gosec`, `govulncheck`, `gitleaks`, `trivy-fs`, `trivy-image` (both images), see [docs/security.md](security.md#supply-chain-security) |
| `codeql.yml` | `analyze` (Go, and JavaScript and TypeScript) |
| `e2e.yml` | `image`, `e2e-main` (the `api` project and the `ui` project's `specs/spa/`), `e2e-gate` |
| `docker-build.yml` | Builds both images and, on `main` or a `v*` tag, publishes them |

There is no markdown linter wired into CI (no markdownlint config or job exists in this repository), and `.pre-commit-config.yaml` covers whitespace/EOL hygiene, gofumpt, golangci-lint, stylelint, eslint and djlint, run locally via `pre-commit`, not in CI.

## Project structure

```
.
├── docker-compose.yml         # 4 services: postgres, step-ca, certharbor-control, certharbor-console
├── compose.e2e-*.yml          # e2e-only overlays, see above
├── .dockerignore              # for root-context builds (frontend/Dockerfile)
├── .env.example               # configuration template
├── Makefile                   # setup, up, down, backup, test, lint, e2e-*, ...
├── secrets/                   # generated by `make setup`, not committed
├── assets/                    # logo.svg, used by README.md
├── scripts/
│   ├── check-backend-port.sh  # asserts certharbor-control publishes no host port, statically or running
│   ├── check-config-keys.sh   # asserts each web-origin key has a configuration row
│   ├── check-proxy-routes.sh  # asserts nginx and Vite derive from one prefix list and one SPA base
│   ├── client-version.sh      # derives the TS client's published version
│   ├── contract-gate.sh       # fails a breaking change with no contract-changes.md row
│   ├── lint-fixtures.sh       # asserts the depguard negative fixture is rejected
│   ├── step-ca-bootstrap.sh   # step-ca init script
│   └── test_deploy.sh         # simulates a fresh install end to end
├── docs/                      # this documentation set
├── plans/                     # frontend-backend-split.md and its companions
├── clients/ts/                # generated TypeScript client package
├── frontend/                  # React SPA, nginx config and the certharbor-console image
├── test/e2e/                  # Playwright suite
├── LICENSE                    # MIT
├── README.md                  # short index
└── backend/
    ├── main.go                # entry point, router setup
    ├── api/                   # huma-registered /api/v1 operations
    ├── apitypes/              # shared API types, incl. BasePath
    ├── cmd/openapi/           # OpenAPI spec generator
    ├── config/                # env-based config loader
    ├── db/                    # SQL queries and schema migrations
    ├── handlers/              # HTTP handlers (one file per area)
    ├── middleware/            # auth, security headers, CSRF, real-IP
    ├── models/                # data structs
    ├── security/              # password hashing, rate limiting, CSRF tokens
    ├── stepca/                # wraps github.com/smallstep/certificates
    ├── le/                    # Let's Encrypt / ACME
    ├── openapi/               # committed openapi.json plus embed glue
    ├── templates/             # HTML templates (Go html/template)
    ├── static/                # CSS, JS, and a favicon (no other images)
    ├── scripts/               # coverage-gate.sh
    ├── testdata/              # golden files for the api/handlers test suites
    ├── Dockerfile              # multi-stage Alpine build
    ├── entrypoint.sh           # startup sequence: secrets → DB wait → provisioner → exec app
    ├── tlsbootstrap.go         # in-process root CA trust and UI cert issuance
    └── tlsreload.go            # hot-reloading TLS cert reloader (mtime-based, zero-restart)
```

See also: [docs/architecture.md](architecture.md), [docs/security.md](security.md), [docs/deployment.md](deployment.md).
