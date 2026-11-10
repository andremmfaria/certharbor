# FAQ

Only entries still true against the current codebase.

<details>
<summary><b>How do I change the HTTPS port from 443?</b></summary>

Set `UI_HTTPS_PORT` in `.env`. It is the host port nginx (`certharbor-console`) publishes, the only published port of the UI:

```env
UI_HTTPS_PORT=8443
```

Then restart: `make restart`.
</details>

<details>
<summary><b>How do I back up and restore the data?</b></summary>

Use the admin UI: **Admin > Backup > Download backup bundle**, or the CLI:

```bash
make backup
```

Both produce a SHA-256-checksummed manifest, through two separate implementations that cover different ground. The admin UI (`backend/handlers/backup.go`) archives a `pg_dump`, `/home/step` (the step-ca-data mount), `/opt/certharbor/data`, certificates and uploads. `make backup` archives all seven named Docker volumes, including `postgres-data`, `certharbor-control-ssl` (the backend's TLS material) and `certharbor-console-ssl` (nginx's), which the admin-UI export does not cover, alongside its own `pg_dump`. Restore is manual by design: unpack the archive, restore the PostgreSQL dump, and restore the Docker volumes.
</details>

<details>
<summary><b>How do I reset the admin password?</b></summary>

**Self-service (requires SMTP and `PUBLIC_BASE_URL` configured):** use the **Forgot password?** link on the login page. SMTP is configured as a database setting from the admin UI's notifications page (host, from address, and so on, `backend/handlers/password_reset.go:94`), not an environment variable, only `PUBLIC_BASE_URL` is env. A single-use reset link is emailed and expires after 30 minutes.

**Database reset (no email required):**

```bash
docker compose exec postgres psql -U certharbor -d certharbor -c \
  "CREATE EXTENSION IF NOT EXISTS pgcrypto; \
   UPDATE users SET password_hash = crypt('newpass', gen_salt('bf', 10)), \
     totp_enabled = false, totp_secret = '', is_active = true \
   WHERE username='admin';"
```

Log in with `admin` / `newpass` and change it from the UI. The `crypt` call writes a bcrypt hash at the application's default cost, which `VerifyPassword` accepts as is.

The `totp_enabled`/`totp_secret` reset and `is_active = true` matter even if you only meant to reset the password: login checks `is_active` before the password at all (`backend/handlers/auth.go:102`), and a stale `totp_enabled=true` sends a successful password check straight to the 2FA step (`backend/handlers/auth.go:114`) which you may no longer be able to complete.
</details>

<details>
<summary><b>The browser does not trust the certificate. How do I use my own?</b></summary>

The browser sees nginx's leaf, `web.crt`, never the backend's. By default (`WEB_TLS_MODE=stepca`) the backend issues it for `WEB_HOSTNAME` from the bundled step-ca and renews it, and nginx reloads on change with no restart, so it chains to this stack's private root. Two approaches:

**Option 1, trust the root:** import step-ca's root certificate (`/home/step/certs/root_ca.crt` in the `step-ca` container, or `/download/ca` in the UI) into the browsers and operating systems that use the UI.

**Option 2, operator-provided cert:** set `WEB_TLS_MODE=provided` and mount `web.crt`, `web.key` and the root that signed the backend's leaf as `root_ca.crt`, read-only at `/etc/certharbor/ssl` in `certharbor-console`, for example by replacing its `certharbor-console-ssl` mount with a bind mount. nginx exits at once naming any of the three that is missing. See [docs/configuration.md](configuration.md#the-web-origins-certificate).

The backend's own leaf (`UI_TLS_MODE`, `certharbor-control-ssl`) serves only the hop from nginx, which verifies it against the CA root as `certharbor-control`, so leave `UI_TLS_MODE=stepca` unless you provide a backend leaf for that name chaining to the root nginx is given.
</details>

<details>
<summary><b>Can I run this behind Cloudflare / Caddy / nginx?</b></summary>

The stack already runs one: `certharbor-console` is an nginx origin with its own certificate in front of the backend, and the backend trusts it by resolving its name, see [docs/configuration.md](configuration.md#web-origin-nginx).

To put another proxy in front of that, point it at nginx's published port and add nginx's realip module (`set_real_ip_from`, `real_ip_header`, `real_ip_recursive on`) for the new hop, so nginx's `$remote_addr` stays the true client. Widening `TRUSTED_PROXY_CIDRS` instead collapses the login rate limiter to one bucket for everyone. For a proxy that replaces nginx, set `TRUST_PROXY=true` and `TRUSTED_PROXY_CIDRS` to the proxy's own address (a `/32` or `/128` unless it's a pool) in `.env`. Both reach the container through `docker-compose.yml`'s `environment:` block already. Only `X-Forwarded-For` is read, so the proxy must replace it with the address it sees rather than append to it. A malformed `TRUSTED_PROXY_CIDRS` entry is a startup fatal, by design. With the list empty, the app trusts whatever `WEB_UPSTREAM_HOST` (default `certharbor-console`) resolves to, see [docs/security.md](security.md#application-security-controls).

Set `PUBLIC_BASE_URL` to the origin your users actually reach, e.g. `https://ca.example.com`. Emailed password-reset links are built from it, deliberately never from `Host` or `X-Forwarded-Proto`, so forwarding those headers does not affect the links.
</details>

<details>
<summary><b>How do I update?</b></summary>

```bash
make backup  # snapshot first
make update  # docker compose pull + up -d --build
```

Database migrations run automatically on startup.
</details>

<details>
<summary><b>Why doesn't setting OIDC_ENABLED in .env do anything?</b></summary>

The stock `docker-compose.yml` does not pass any `OIDC_*` variable, or `LOCAL_LOGIN_ENABLED`, through to the `certharbor-control` container. You need to add those `environment:` lines to the `certharbor-control` service yourself, see [docs/configuration.md](configuration.md#oidc-sso) for the full list and [docs/authentication.md](authentication.md#oidc-sso) for the setup checklist.
</details>

<details>
<summary><b>The container won't start: "No admin user exists and CERTHARBOR_ADMIN_PASSWORD is not set". What now?</b></summary>

This is a first-boot fatal, not a bug: `backend/db/schema.go` refuses to seed an admin user with a made-up password (`schema.go:187-193`). Set `CERTHARBOR_ADMIN_PASSWORD` in `.env` to a strong password and start the stack again. Once the admin user exists, remove the variable from `.env`, it is only consulted while the `users` table is empty.
</details>

See also: [docs/configuration.md](configuration.md), [docs/authentication.md](authentication.md), [docs/deployment.md](deployment.md).
