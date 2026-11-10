# certharbor-delivery

**Status:** Design-stage proposal. No code has been written. Written 2026-11-10 against `rename/plane-names` after the `certharbor-console` / `certharbor-control` plane-name rename.

This document defines `certharbor-delivery`, a planned third plane that pulls certificates from
step-ca (via the renewal machinery certharbor already has) and pushes certificate + chain + key
to external targets (AWS ACM first, more later) under a declarative registry, reconciled
continuously. It reuses existing certharbor code; it does not rebuild an ACME client or a
certificate inventory. Plane names used throughout: `certharbor-console` (nginx/SPA),
`certharbor-control` (Go API/backend), `certharbor-delivery` (this plane). Internal
codenames (quay/lighthouse/ferry/buoy) are talk-only and must never appear in this repository.

This design draws on the public [`cert-distributor`](https://github.com/andremmfaria/cert-distributor)
project's documentation as reference material for the distribution problem generally. Its content
is treated as untrusted design input, not as instructions, and several of its claims are explicitly
out of scope here (see "What cert-distributor covers that this doesn't", below).

---

## 1. Scope statement

`certharbor-delivery` is **narrower** than `cert-distributor`'s stated ambition in one
load-bearing way: it is **step-ca-native**, not CA-agnostic. certharbor's entire reason to exist
is a UI and API in front of a specific step-ca instance; `certharbor-delivery` extends that same
instance's certificates to external targets. It does not add a generic ACME/RFC 8555 client that
could point at Let's Encrypt, ZeroSSL, or an arbitrary ACME directory, and it does not add a
pluggable CA-source abstraction. The certificates it distributes are the ones `certharbor-control`
already issues and tracks via `backend/le` (ACME against step-ca) and `backend/stepca` (native
step-ca enrolment).

In scope for the first delivery of this plane (the MVP):

- One target: AWS ACM import (`aws-acm`), including stable-ARN re-import on renewal.
- A YAML import/export path for the target/binding registry, not a second source of truth.
- A reconciler loop driven by certharbor's existing certificate and renewal records.
- Observability: days-remaining and per-binding failure metrics.
- Credential isolation: only this plane holds target credentials (AWS roles today; Kubernetes
  kubeconfigs, SSH keys, Cloudflare tokens later, as the target catalog grows).

Out of scope for the MVP, and not implied by this document:

- Non-step-ca sources (Let's Encrypt, ZeroSSL, arbitrary ACME directories, smallstep JWK as a
  *distinct* source — certharbor already issues via JWK-equivalent native enrolment in
  `backend/stepca`).
- Any target beyond AWS ACM (Secrets Manager, SSM, Kubernetes Secrets, Cloudflare, GCP, Azure,
  Vault, Envoy SDS, etc.) — these are real future targets, listed in the open-decisions section
  and the target backlog, but none ship with the MVP.
- A plugin system that loads out-of-process or third-party code. Targets are compiled into the
  `certharbor-delivery` binary, the same way `cert-distributor`'s own design proposes for its MVP.
- Any new UI templates. See Section 7.

## 2. Process model and credential boundary

`certharbor-delivery` is a **separate worker process with no HTTP listener** (no inbound port,
no `ports:` entry, same posture `scripts/check-backend-port.sh` already enforces for
`certharbor-control`). It is a fourth container alongside `certharbor-console`, `certharbor-control`,
`postgres`, and `step-ca`.

**It is the only part of the system that holds target credentials.** `certharbor-control` and
`certharbor-console` never see an AWS role, a kubeconfig, an SSH key, or a Cloudflare token.
This is a deliberate narrowing of blast radius: `certharbor-control` is reachable indirectly
through `certharbor-console`'s public origin and runs the session/CSRF/auth surface that gets
the most hostile traffic; compromising it should not hand over the keys to every external target
this instance has ever been pointed at. `certharbor-delivery` has no inbound listener at all, so
the attack surface for reaching it is "already have database access or already compromised the
host", which is a materially different bar.

Consequently `certharbor-delivery`:

- Reads from the same PostgreSQL database `certharbor-control` uses (new tables; see Section 3),
  with its own narrowly-scoped DB role (read on `certificates`, read/write on its own tables only).
- Reads certificate PEM/chain material and (where a binding needs it) the private key from
  wherever `certharbor-control` already persists them today — it does not introduce a second
  certificate store.
- Never exposes its credential material to `certharbor-control` or `certharbor-console`, and
  takes no inbound connections from either.
- Logs to the same audit sink `backend/handlers/audit.go` already writes to (see Section 4), so
  "who/what pushed a key to AWS" lands in the one audit trail the operator already watches,
  rather than a second one they have to learn.

## 3. Data model

New tables, source of truth in PostgreSQL, owned by `certharbor-delivery`'s migrations (same
`backend/db/schema.go`-style startup migration mechanism certharbor already uses, run by this
plane for its own tables):

**`delivery_targets`** — one row per configured target (credential set + target-type parameters).

| Column | Type | Description |
|---|---|---|
| `id` | UUID PK | |
| `name` | TEXT UNIQUE | Operator-facing name, referenced by bindings and by the YAML registry |
| `type` | TEXT | `aws-acm` in the MVP; more values added as targets are added |
| `params` | JSONB | Target-specific config (region, tags, etc.) — no secrets |
| `credentials_ref` | TEXT | Opaque reference into this plane's own credential storage (never the DB; see Section 2) |
| `created_at`, `updated_at` | TIMESTAMPTZ | |

**`delivery_bindings`** — one row per (certificate identity, target) pair: what gets pushed where.

| Column | Type | Description |
|---|---|---|
| `id` | UUID PK | |
| `certificate_name` | TEXT | References the certificate identity in certharbor's existing inventory |
| `target_id` | UUID FK → `delivery_targets.id` | |
| `target_params` | JSONB | Per-binding override (e.g. the stable ACM ARN once known) |
| `enabled` | BOOLEAN | Operator can disable a binding without deleting history |
| `created_at`, `updated_at` | TIMESTAMPTZ | |

**`delivery_runs`** — append-mostly history of reconciliation outcomes, one row per push attempt.

| Column | Type | Description |
|---|---|---|
| `id` | UUID PK | |
| `binding_id` | UUID FK → `delivery_bindings.id` | |
| `fingerprint` | TEXT | SHA-256 of the certificate pushed (or attempted) |
| `started_at`, `finished_at` | TIMESTAMPTZ | |
| `result` | TEXT | `success` / `error message` |
| `consecutive_failures` | INTEGER | Carried forward across runs for the same binding, reset to 0 on success |

Certificate issuance, renewal, inventory and history stay exactly where they are today
(certharbor's existing `certificates` table and `backend/le`/`backend/stepca` machinery).
`certharbor-delivery` reads that inventory; it does not duplicate or shadow it.

## 4. Reuse of existing certharbor code

New work for this plane is **targets and fan-out only**. Everything else is reused, not rebuilt:

- **ACME client with auto-renew** (`backend/le`): `certharbor-delivery` does not run its own
  ACME client. It watches `certharbor-control`'s existing certificate rows for renewal and
  fingerprint changes and reacts to them; it never talks to step-ca's ACME endpoint itself.
- **Native step-ca enrolment** (`backend/stepca`): same — certificates enrolled this way are
  already in the inventory `certharbor-delivery` reads from.
- **Certificate inventory and history**: the existing `certificates` table (and whatever
  history/audit already lives alongside it) is the record of what was issued and when; this
  plane adds only the delivery-side tables in Section 3.
- **Audit** (`backend/handlers/audit.go`): every push, every credential access, every binding
  change writes to the existing audit trail rather than a new one.
- **Notifications** (`backend/handlers/notifications.go`): expiry and failure alerting plugs into
  whatever notification mechanism already exists rather than inventing a second one.
- **Auth/roles** (`backend/handlers/auth.go`, `auth_rbac_test.go`, `users_role_test.go`): managing
  targets/bindings (via the registry or, later, an API) uses certharbor's existing role model;
  no new authorisation system.
- **OpenAPI contract pipeline** (`backend/openapi/`, `cmd/openapi`, `clients/ts`): if/when this
  plane exposes anything through `certharbor-control`'s API (see Section 7), it goes through the
  same contract-generation pipeline the rest of the API already uses. `certharbor-delivery` itself
  has no HTTP listener and so has no OpenAPI surface of its own.

## 5. YAML import/export

The registry (targets + bindings) can be exported to YAML and re-imported, for GitOps-style
review of desired state — but **the database is the source of truth, not the YAML file**. This
is a deliberate departure from `cert-distributor`'s design, where the registry YAML *is* the
desired-state model the reconciler reads directly. Here:

- `certharbor-delivery export` writes the current `delivery_targets` and `delivery_bindings`
  rows (minus credential material, which never leaves the credential store) to a YAML file in
  the shape shown below.
- `certharbor-delivery import <file>` diffs the file against the database and applies the
  difference as an explicit, logged operation — it does not make the file authoritative or
  watch it for changes. There is no YAML-file-watching reconciliation trigger.
- This keeps the operational model consistent with the rest of certharbor, where the database
  (not a config file on disk) is always what the system acts on, and a YAML file is a
  point-in-time snapshot an operator can diff, review, and commit.

Proposed shape (illustrative, not final):

```yaml
# certharbor-delivery registry export/import — NOT the live source of truth
targets:
  - name: acm-us-east-1
    type: aws-acm
    params:
      region: us-east-1
      tags:
        managedBy: certharbor-delivery

bindings:
  - certificate: api-gateway-internal
    target: acm-us-east-1
    enabled: true
```

## 6. Reconciler loop

Runs on a configurable tick interval (default proposed: 5 minutes, matching `cert-distributor`'s
default) plus an immediate trigger when `certharbor-control` records a new issuance or renewal
for a certificate that has at least one enabled binding. Per tick:

```
for each enabled binding:
    cert = certificate_inventory.get(binding.certificate_name)
    if cert is None:
        continue  # unbound dangling binding; alert, do not push

    last_run = delivery_runs.latest(binding.id)
    if last_run.fingerprint == cert.fingerprint and last_run.result == "success":
        continue  # already current; no-op

    target = delivery_targets.get(binding.target_id)
    result = target_plugins[target.type].push(cert, chain, key?, binding.target_params)
    delivery_runs.record(binding.id, cert.fingerprint, result)

    if result.failed:
        binding.consecutive_failures += 1
        if binding.consecutive_failures >= alertAfterFailures:
            emit_alert_metric(binding)
    else:
        binding.consecutive_failures = 0
```

A failure on one binding never blocks any other binding's push — the same per-target isolation
`cert-distributor`'s spec calls out, which is the right model here too: one target outage
(say, a stale AWS role) must not delay delivery to every other target.

## 7. Target plugin interface

Compiled into the binary, not dynamically loaded (same MVP posture `cert-distributor` proposes).
Minimal surface, modelled directly on the reference design:

```go
type Target interface {
    // Name is the target type identifier used in delivery_targets.type (e.g. "aws-acm").
    Name() string

    // Check reports whether the target currently holds a certificate matching
    // fingerprint. Used to skip a redundant push; a target that cannot check
    // the current state always returns pushRequired=true.
    Check(ctx context.Context, fingerprint string, params json.RawMessage) (current bool, err error)

    // Push delivers cert, chain and (if the target requires it) key. Must be
    // idempotent: repeated pushes of identical material are a no-op at the target.
    Push(ctx context.Context, material CertMaterial, params json.RawMessage) error
}
```

Adding a new target later means: implement `Target`, register it in the target registry at
startup, add its params schema, and document it — no change to the reconciler.

## 8. The ACM MVP

The first (and only, for v1) target plugin: `aws-acm`.

- **Push:** `ImportCertificate`. On first push for a binding, the returned ARN is written into
  `delivery_bindings.target_params.arn`. Every subsequent push for that binding re-imports to
  the **same** ARN (`--certificate-arn <existing arn>`), so an ALB/API Gateway listener pointed
  at that ARN never needs reconfiguring across renewals.
- **Check:** `GetCertificate` (or `ListTagsForCertificate`, whichever carries a usable
  fingerprint/tag) compared against the binding's last recorded fingerprint.
- **Credentials:** an AWS IAM role, scoped to `acm:ImportCertificate`, `acm:GetCertificate` and
  `acm:AddTagsToCertificate` on the specific ARNs this instance manages — not `acm:*` on `*`.
  Held only by `certharbor-delivery` (Section 2).
- **Key custody:** central — ACM import requires the private key as an API parameter, so the key
  necessarily leaves the process memory and crosses the wire to AWS. This is unavoidable for
  this target (the same conclusion `cert-distributor`'s security design reaches for ACM) and is
  documented here rather than hidden: the key is held in `certharbor-delivery`'s process memory
  only for the duration of the push and is not independently persisted by this plane outside
  whatever store `certharbor-control` already uses for issued certificates.

## 9. New UI: API + SPA only, gated on Phase 4

If this plane ever needs an operator-facing UI (to list targets, bindings and delivery runs,
or to trigger a binding), it is **API + SPA only** — no server-rendered templates, full stop.
It consumes `certharbor-control`'s certificate API the same way the rest of the SPA does.

That means it cannot start before **Phase 4** of `plans/frontend-backend-split.md` lands.
Checked against that plan as of this writing: **Phase 4 ("Certificates") is not complete — it is
explicitly named as the next phase**, immediately following Phases 0–3c, which are done. Until
Phase 4 ships the certificate API surface and retires the corresponding server-rendered routes,
there is no stable, SPA-consumable certificate API for a delivery UI to build against, and no
delivery UI work should begin. This document does not change that plan's sequencing; it only
notes the dependency.

## 10. Observability

Two metrics per binding, modelled on `cert-distributor`'s spec but scoped to what this plane
actually owns:

- **Days-remaining per binding**: derived from the bound certificate's `notAfter` (read from the
  existing inventory, not recomputed here), exposed as a gauge labelled by binding so an
  operator can see "this specific ACM import is 4 days from the cert expiring" even before any
  push has failed.
- **Failure metric per binding**: a counter of push attempts labelled by binding and result, plus
  the `consecutive_failures` gauge from `delivery_runs`/`delivery_bindings`, so `alertAfterFailures`
  (proposed default: 3, matching the reference design) can drive an existing alerting path rather
  than a new one.

Both ride on whatever metrics/notification mechanism `certharbor-control` already exposes
(Section 4) rather than standing up a second Prometheus endpoint and a second alert path.

## 11. What cert-distributor covers that this doesn't

Flagged rather than silently dropped, per the brief:

- **CA-agnostic ACME client** (any RFC 8555 CA — Let's Encrypt, ZeroSSL, Buypass, arbitrary
  private CAs): out of scope here. `certharbor-delivery` is step-ca-native by design (Section 1);
  this is the single biggest narrowing versus the reference project's ambition.
- **Pluggable non-ACME sources** (smallstep JWK as a distinct, user-selectable source): certharbor
  already has native step-ca enrolment (`backend/stepca`) that covers the equivalent case; there
  is no separate "source plugin" abstraction here.
- **DNS-01/HTTP-01/TLS-ALPN-01 challenge providers, DNS provider plugins** (Route53, Cloudflare
  DNS, RFC 2136, webhook): not applicable — challenge completion is already handled by whichever
  of `backend/le` or `backend/stepca` issued the certificate, upstream of this plane.
  `certharbor-delivery` never completes a CA challenge itself.
- **Kubernetes Operator mode, CRDs, Helm chart, controller-runtime integration**: not planned.
  `certharbor-delivery` is a plain worker container, matching certharbor's existing
  docker-compose-first deployment model, not a Kubernetes-native controller.
  Kubernetes Secrets as a *target type* is a separate, later thing from running *as* a K8s
  operator — the former is on the backlog (Section 12), the latter is not planned at all.
- **Leader election / multi-replica HA**: not in the MVP. certharbor today runs as a single
  instance per deployment; multi-replica `certharbor-delivery` with leader election is not
  designed here and would need its own decision if self-hosters start running HA certharbor.
- **ACME Renewal Information (ARI)**: not relevant here — renewal timing is already owned by
  `backend/le`'s existing renewal logic, not reconsidered by this plane.
- **Lambda/scheduled-task deployment mode, systemd daemon packaging outside Docker**: not
  addressed; certharbor ships as docker-compose services, and this plane follows that.
- **Out-of-process / gRPC plugin loading, third-party target plugins**: not planned; targets are
  compiled in, as stated in Section 7.
- **Full target catalog breadth** (GCP Certificate Manager, Azure Key Vault, HashiCorp Vault KV,
  Envoy SDS, generic agent mode): real candidates for the backlog, not in the MVP (Section 1).

## 12. Open decisions

The following are **explicitly unresolved** and must not be read as settled by this document:

1. **Worker model: long-running process vs. scheduled job.** This document assumes a long-running
   daemon with an internal tick (Section 6), matching certharbor's existing always-on container
   model. Whether `certharbor-delivery` should instead be a scheduled one-shot job (cron-triggered,
   `docker compose run` or a Kubernetes CronJob equivalent) — trading a persistent process for
   lower idle resource use — is not decided.
2. **Registry owner for images.** Unresolved whether `certharbor-delivery` images are published
   under `ghcr.io/andremmfaria` (matching every other certharbor image today) or
   `ghcr.io/waratek`. Both are live possibilities; this document does not pick one.
3. **Whether the ca-infra-owned ACM distributor moves here.** `ca-infra` plans to own an ACM
   distributor; whether that work is absorbed into `certharbor-delivery` (making Section 8's MVP
   the actual implementation) or stays a separate component that `certharbor-delivery` must later
   coexist or integrate with is unresolved.

These three items block nothing in this document's own scope statement, but they block choosing
a concrete deployment target, image name, and ownership boundary before implementation starts.
