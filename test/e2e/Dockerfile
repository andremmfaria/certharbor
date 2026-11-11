# Harness image for the api and ui projects. They run as a container on
# step-network, not on the host: both rate limiters key on the client IP, so a
# host harness is one gateway address and per-test rate-limit isolation becomes
# impossible (Section 4.1.2).
#
# The tag must match @playwright/test in package.json, or the reporter and the
# runtime disagree. The digest pins the browser build; an unpinned image changes
# browser version between runs.
ARG PLAYWRIGHT_IMAGE=mcr.microsoft.com/playwright:v1.65.0-noble@sha256:6e3fb709361521eb27161606e6da6d2674e5e89fa16516cb37d6fdd91372273d
# hadolint ignore=DL3006
FROM ${PLAYWRIGHT_IMAGE}

# docker-ce-cli and the compose plugin are the single mechanism every
# container- or database-touching assertion uses over the mounted socket.
# hadolint ignore=DL3008,DL3015
RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates curl gnupg openssl postgresql-client \
 && install -m 0755 -d /etc/apt/keyrings \
 && curl -fsSL https://download.docker.com/linux/ubuntu/gpg -o /etc/apt/keyrings/docker.asc \
 && chmod a+r /etc/apt/keyrings/docker.asc \
 && echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/ubuntu noble stable" \
      > /etc/apt/sources.list.d/docker.list \
 && apt-get update \
 && apt-get install -y --no-install-recommends docker-ce-cli docker-compose-plugin \
 && apt-get purge -y gnupg \
 && apt-get autoremove -y \
 && rm -rf /var/lib/apt/lists/*

# Build the TS client in-image, same approach as frontend/Dockerfile: the
# harness consumes the generated client through its packed tarball, never a
# file: dependency on ../../clients/ts (that path doesn't exist inside this
# image's build context once it's narrowed to test/e2e, and widening the
# context to the repo root to copy source directly would invalidate this
# layer on every unrelated backend/frontend change instead of only on an
# openapi.json or clients/ts change).
COPY clients/ts/package.json clients/ts/package-lock.json /clients/ts/
WORKDIR /clients/ts
RUN npm ci
COPY VERSION /VERSION
COPY backend/openapi/openapi.json /backend/openapi/openapi.json
COPY clients/ts/ /clients/ts/
# The in-image client is exempt from the commit-sha provenance check and
# carries the -dev fallback version (D8): there is no git history here.
RUN npm run generate && npm run build \
 && npm version "$(tr -d '[:space:]' < /VERSION)-dev" --no-git-tag-version --allow-same-version \
 && npm pack --pack-destination dist \
 && mv dist/certharbor-client-*.tgz dist/client.tgz

WORKDIR /e2e

# node_modules is baked into its own layer, so no job pays a cold npm ci.
COPY test/e2e/package.json test/e2e/package-lock.json ./
RUN npm ci \
 && npm install --no-save /clients/ts/dist/client.tgz

COPY test/e2e/ .

# No ENTRYPOINT: each job passes a full command, and an entrypoint would prefix
# it, turning `npx playwright test --project=api` into a nonsense argv.
CMD ["npx", "playwright", "test"]
