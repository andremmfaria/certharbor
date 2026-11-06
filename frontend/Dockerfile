# Build context is the repository root (docker build -f frontend/Dockerfile .),
# because the SPA is built against the client generated from
# backend/openapi/openapi.json. The COPYs are narrow and in dependency order so
# a backend-only commit does not invalidate npm ci or the Vite build.

# ── Stage 1: client and SPA build ────────────────────────────────────────────
FROM node:25-alpine3.23 AS build

WORKDIR /src

COPY frontend/package.json frontend/package-lock.json frontend/
COPY clients/ts/package.json clients/ts/package-lock.json clients/ts/
WORKDIR /src/clients/ts
RUN npm ci
WORKDIR /src/frontend
RUN npm ci
WORKDIR /src

COPY VERSION ./
COPY backend/openapi/openapi.json backend/openapi/openapi.json
COPY clients/ts/ clients/ts/
# The in-image client is exempt from the commit-sha provenance check and
# carries the -dev fallback version (D8): there is no git history here.
WORKDIR /src/clients/ts
RUN npm run generate && npm run build \
 && npm version "$(tr -d '[:space:]' < ../../VERSION)-dev" --no-git-tag-version --allow-same-version \
 && npm pack --pack-destination dist \
 && mv dist/andremmfaria-step-ca-webui-client-*.tgz dist/client.tgz

COPY frontend/ /src/frontend/
WORKDIR /src/frontend
# CONTRACT_SHA defaults to the digest of the spec the client was generated
# from; a skew test overrides it to force a mismatch (5.10).
ARG CONTRACT_SHA=""
SHELL ["/bin/ash", "-o", "pipefail", "-c"]
RUN npm install --no-save ../clients/ts/dist/client.tgz \
 && CONTRACT_SHA="${CONTRACT_SHA:-$(sha256sum ../backend/openapi/openapi.json | cut -d' ' -f1)}" npm run build

# ── Stage 2: nginx with the config, no SPA ───────────────────────────────────
# Pinned to a minor. The master process must run as root: it reads web.key,
# which the backend writes 0600 as uid 10001, before dropping to the worker
# user. nginx-unprivileged or a USER line breaks TLS startup.
# /etc/step-webui/ssl is deliberately absent from the image: an empty named
# volume takes its ownership from the first image that has the mount path, and
# that must be the backend's uid 10001 directory, never a root-owned one here.
FROM nginx:1.29-alpine AS runtime-base
# hadolint ignore=DL3018
RUN apk add --no-cache curl openssl \
 && rm -f /etc/nginx/conf.d/default.conf \
 && rm -rf /usr/share/nginx/html/* \
 && mkdir -p /etc/step-webui/ephemeral
COPY frontend/nginx.conf.template frontend/nginx.waiting.conf.template frontend/conf.d/doc-headers.conf.template /etc/step-webui/
COPY frontend/conf.d/proxy-headers.conf /etc/nginx/conf.d/
COPY --chmod=0755 frontend/entrypoint.sh /usr/local/bin/step-webui-web-entrypoint
EXPOSE 8443
# The SNI must be WEB_HOSTNAME: any other name is refused at the handshake.
HEALTHCHECK --interval=10s --timeout=3s --start-period=10s --retries=3 \
  CMD curl -fsk --max-time 2 --resolve "${WEB_HOSTNAME}:8443:127.0.0.1" "https://${WEB_HOSTNAME}:8443/nginx-health" || exit 1
ENTRYPOINT ["/usr/local/bin/step-webui-web-entrypoint"]

# ── make nginx-lint's fixture: the real config over a placeholder bundle ─────
# PHASE9-DELETE: both bundles go to /usr/share/nginx/html/ once the SPA owns
# the root. Until then it lives under /app/, Vite's base.
FROM runtime-base AS nginx-fixture
COPY frontend/nginx-lint/fixture-dist/ /usr/share/nginx/html/app/

# ── Final image ──────────────────────────────────────────────────────────────
FROM runtime-base
COPY --from=build /src/frontend/dist/ /usr/share/nginx/html/app/
