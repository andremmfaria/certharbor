# ── Stage 1: Build ───────────────────────────────────────────────────────────
# Mirrors backend/Dockerfile's layout: build context is ./backend (same as
# certharbor-control's), this Dockerfile just builds a different cmd/ binary.
FROM golang:1.27.3-alpine3.23 AS builder

WORKDIR /build

# hadolint ignore=DL3018
RUN apk add --no-cache git ca-certificates

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o certharbor-delivery ./cmd/certharbor-delivery

# ── Stage 2: Runtime ──────────────────────────────────────────────────────────
FROM alpine:3.23

WORKDIR /opt/certharbor-delivery

# Same non-root uid/gid convention as backend/Dockerfile, a different name
# so `docker inspect`/`ps` output is unambiguous about which plane a shell
# landed in.
RUN addgroup -g 10001 -S certharbor-delivery && \
    adduser -u 10001 -S -G certharbor-delivery certharbor-delivery

# ca-certificates: outbound TLS to AWS ACM. No curl/openssl/postgresql-client
# here — this plane has no HTTP listener to healthcheck and ships no admin
# diagnostics page, so it doesn't need the tooling certharbor-control's
# image installs for those.
# hadolint ignore=DL3018
RUN apk add --no-cache ca-certificates tzdata

COPY --from=builder /build/certharbor-delivery .
RUN chown -R certharbor-delivery:certharbor-delivery /opt/certharbor-delivery

# Deliberately NO EXPOSE and NO HEALTHCHECK: this binary never listens on a
# port (plans/distribution.md Section 2), so there is nothing to probe over
# the network. Liveness is observed via `docker compose ps`/process exit,
# the same way any non-HTTP worker container is, not via a health endpoint.

USER certharbor-delivery

ENTRYPOINT ["/opt/certharbor-delivery/certharbor-delivery"]
CMD ["run"]
