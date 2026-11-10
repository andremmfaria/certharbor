<div align="center">

<img src="assets/logo.svg" alt="Certharbor logo" width="140" height="140" />

# Certharbor

**Self-hosted web interface for [Smallstep step-ca](https://smallstep.com/docs/step-ca/), manage your private PKI from a browser.**

[![License: MIT](https://img.shields.io/badge/License-MIT-green.svg)](https://opensource.org/licenses/MIT)
[![Made with Go](https://img.shields.io/badge/Made%20with-Go-00ADD8.svg)](https://go.dev)
[![Docker](https://img.shields.io/badge/Docker-Compose-2496ED.svg)](https://docs.docker.com/compose/)

</div>

---

A small-team-friendly web UI on top of `smallstep/step-ca`. No SaaS, no telemetry, no vendor lock-in, runs on your own server in four Docker containers (the nginx origin certharbor-web, the Go backend certharbor, PostgreSQL, step-ca). The UI talks to step-ca through the Go `smallstep/certificates` library, not the `step` CLI.

## Features

- Certificate management: issue, renew, revoke and import X.509 certificates, with Server TLS / Internal service / Wildcard / Client identity templates
- Role-based access (`admin` / `manager` / `viewer`) plus short-lived temporary users with automatic expiry
- TOTP 2FA, OIDC SSO with group-to-role mapping, and self-service password recovery
- Let's Encrypt / ACME issuance and renewal from within the UI
- Built-in security: CSRF tokens, per-IP rate limiting, security and admin audit logs, a read-only diagnostics console
- Backup export (admin UI and `make backup`) with SHA-256 manifest checksums, CA integrity checks, webhook and SMTP notifications
- 3 colour themes plus an auto (system) mode, a custom date picker, and a responsive layout

## Quick Start

```bash
git clone https://github.com/andremmfaria/certharbor.git
cd certharbor
make setup   # copies .env.example → .env and generates secrets/
# edit .env: set HOST_IP, WEB_HOSTNAME, UI_HTTPS_PORT, PROVISIONER, TZ, and CERTHARBOR_ADMIN_PASSWORD
make up      # docker compose up -d --build
```

`CERTHARBOR_ADMIN_PASSWORD` is not optional on a first boot, and neither is `WEB_HOSTNAME`, the name browsers use for the nginx origin, see [docs/deployment.md](docs/deployment.md). Run `make help` for every other target.

## Documentation

- [docs/architecture.md](docs/architecture.md): components today, request flow, roles, the SPA migration's state, and the generated TypeScript client
- [docs/configuration.md](docs/configuration.md): startup sequence, TLS bootstrap, and the full environment variable reference
- [docs/authentication.md](docs/authentication.md): local login, OIDC SSO, session/CSRF cookies, 2FA, and password reset
- [docs/deployment.md](docs/deployment.md): requirements, compose services, secrets, the container image, and Makefile targets
- [docs/security.md](docs/security.md): application security controls and the supply-chain gates in CI
- [docs/development.md](docs/development.md): local dev loop, tests, lint gates, regenerating the API contract, and project structure
- [docs/faq.md](docs/faq.md): frequently asked questions
- [plans/frontend-backend-split.md](plans/frontend-backend-split.md): the in-progress React SPA / Go backend-for-frontend migration plan

## License

MIT License, see [LICENSE](LICENSE).
