package api

import (
	"net/http"

	"certharbor/handlers"

	"github.com/danielgtaylor/huma/v2"
)

// Register wires Phase 0's operations onto humaAPI against h. Kept separate
// from construction (Mount, NewForSpec) so cmd/openapi can call it against a
// Handler built with no database, no CA client and no environment (D3).
func Register(humaAPI huma.API, h *handlers.Handler) {
	// The one auth: optional operation in the API (5.5): it validates a
	// session when present, never answers 401, and is exempt from the
	// sliding-window renewal so an idling open tab cannot keep a session alive.
	huma.Register(humaAPI, optionalAuthOp(huma.Operation{
		OperationID: "getSession",
		Method:      http.MethodGet,
		Path:        BasePath + "/session",
		Tags:        []string{"session"},
		Summary:     "Get the current session state",
	}), getSession(h))

	// Public because there is no session to validate yet: the handler reads the
	// pending state itself. CSRF applies, and the login rate limiter is scoped
	// to exactly these two operations (5.5).
	huma.Register(humaAPI, rateLimited(publicOp(huma.Operation{
		OperationID: "createSession",
		Method:      http.MethodPost,
		Path:        BasePath + "/session",
		Tags:        []string{"session"},
		Summary:     "Log in with a username and password",
	})), createSession(h))

	huma.Register(humaAPI, rateLimited(publicOp(huma.Operation{
		OperationID: "submitMfa",
		Method:      http.MethodPost,
		Path:        BasePath + "/session/mfa",
		Tags:        []string{"session"},
		Summary:     "Complete a login with a second factor",
	})), submitMfa(h))

	// Public: logging out must work from a session the server has already
	// rejected, and the only effect is expiring the caller's own cookies.
	huma.Register(humaAPI, csrfWhenSession(publicOp(huma.Operation{
		OperationID:   "deleteSession",
		Method:        http.MethodDelete,
		Path:          BasePath + "/session",
		Tags:          []string{"session"},
		Summary:       "Log out and expire the session and CSRF cookies",
		DefaultStatus: http.StatusNoContent,
	})), deleteSession(h))

	huma.Register(humaAPI, publicOp(huma.Operation{
		OperationID: "getConfig",
		Method:      http.MethodGet,
		Path:        BasePath + "/config",
		Tags:        []string{"system"},
		Summary:     "Get public runtime configuration",
	}), getConfig(h))

	huma.Register(humaAPI, roleOp("viewer", huma.Operation{
		OperationID: "getStatus",
		Method:      http.MethodGet,
		Path:        BasePath + "/status",
		Tags:        []string{"dashboard"},
		Summary:     "Get active/expiring certificate counts",
	}), getStatus(h))

	// The two spike operations prove the octet-stream and multipart mechanics
	// Phase 4 reuses (5.7). They carry a real role rather than staying public:
	// an unauthenticated multipart upload on a certificate authority is not
	// something to leave reachable for the length of a migration.
	huma.Register(humaAPI, roleOp("viewer", huma.Operation{
		OperationID: "getSpikeBlob",
		Method:      http.MethodGet,
		Path:        BasePath + "/_spike/blob",
		Tags:        []string{"system"},
		Summary:     "Phase 0 spike: binary download mechanic",
		Responses:   spikeBlobResponses,
	}), getSpikeBlob)

	huma.Register(humaAPI, roleOp("manager", huma.Operation{
		OperationID: "postSpikeUpload",
		Method:      http.MethodPost,
		Path:        BasePath + "/_spike/upload",
		Tags:        []string{"system"},
		Summary:     "Phase 0 spike: multipart upload mechanic",
	}), postSpikeUpload)

	// Phase 4, first three operations (plans/frontend-backend-split.md
	// Section 8): list, detail and options. The remaining certificate
	// operations follow once the observed cost of this first three is
	// recorded (R23).
	huma.Register(humaAPI, roleOp("viewer", huma.Operation{
		OperationID: "listCertificates",
		Method:      http.MethodGet,
		Path:        BasePath + "/certificates",
		Tags:        []string{"certificates"},
		Summary:     "List certificates",
	}), listCertificates(h))

	huma.Register(humaAPI, roleOp("viewer", huma.Operation{
		OperationID: "getCertificate",
		Method:      http.MethodGet,
		Path:        BasePath + "/certificates/{id}",
		Tags:        []string{"certificates"},
		Summary:     "Fetch one certificate",
	}), getCertificate(h))

	huma.Register(humaAPI, roleOp("manager", huma.Operation{
		OperationID: "getCertificateOptions",
		Method:      http.MethodGet,
		Path:        BasePath + "/certificates/options",
		Tags:        []string{"certificates"},
		Summary:     "Get certificate issuance options",
	}), getCertificateOptions(h))

	// Phase 4, second batch (R23's observed-cost record on these five):
	// issue, import (upload and paths), renew and revoke. Every one is
	// manager except revoke, which Section 6 calls out as admin-only.
	huma.Register(humaAPI, roleOp("manager", huma.Operation{
		OperationID:   "createCertificate",
		Method:        http.MethodPost,
		Path:          BasePath + "/certificates",
		Tags:          []string{"certificates"},
		Summary:       "Issue a new certificate",
		DefaultStatus: http.StatusCreated,
	}), createCertificate(h))

	huma.Register(humaAPI, roleOp("manager", huma.Operation{
		OperationID: "listCertificateImportCandidates",
		Method:      http.MethodGet,
		Path:        BasePath + "/certificates/import-candidates",
		Tags:        []string{"certificates"},
		Summary:     "List unregistered certificates found on disk",
	}), listCertificateImportCandidates(h))

	huma.Register(humaAPI, roleOp("manager", huma.Operation{
		OperationID:   "importCertificateUpload",
		Method:        http.MethodPost,
		Path:          BasePath + "/certificates/import/upload",
		Tags:          []string{"certificates"},
		Summary:       "Import a certificate by uploading its files",
		DefaultStatus: http.StatusCreated,
	}), importCertificateUpload(h))

	huma.Register(humaAPI, roleOp("manager", huma.Operation{
		OperationID: "importCertificatesByPaths",
		Method:      http.MethodPost,
		Path:        BasePath + "/certificates/import/paths",
		Tags:        []string{"certificates"},
		Summary:     "Import certificates by server-side path, with a per-item result",
	}), importCertificatesByPaths(h))

	huma.Register(humaAPI, roleOp("manager", huma.Operation{
		OperationID: "renewCertificate",
		Method:      http.MethodPost,
		Path:        BasePath + "/certificates/{id}/renew",
		Tags:        []string{"certificates"},
		Summary:     "Renew a certificate",
	}), renewCertificate(h))

	huma.Register(humaAPI, roleOp("admin", huma.Operation{
		OperationID: "revokeCertificate",
		Method:      http.MethodPost,
		Path:        BasePath + "/certificates/{id}/revoke",
		Tags:        []string{"certificates"},
		Summary:     "Revoke a certificate",
	}), revokeCertificate(h))

	// Phase 4, third and final batch: the two certificate downloads, the
	// new GET /api/v1/ca resource, and the three CA chain downloads. All
	// five downloads reuse Phase 0's binaryResponses mechanic; the role
	// split matches Section 6 exactly (manager for the certificate's own
	// files, admin for everything naming the CA itself, viewer for the
	// CA's read-only status).
	huma.Register(humaAPI, roleOp("manager", huma.Operation{
		OperationID: "getCertificateCertificate",
		Method:      http.MethodGet,
		Path:        BasePath + "/certificates/{id}/certificate",
		Tags:        []string{"certificates"},
		Summary:     "Download a certificate's certificate file",
		Responses:   binaryResponses("The certificate's PEM-encoded certificate file."),
	}), getCertificateCertificate(h))

	huma.Register(humaAPI, roleOp("manager", huma.Operation{
		OperationID: "getCertificateKey",
		Method:      http.MethodGet,
		Path:        BasePath + "/certificates/{id}/key",
		Tags:        []string{"certificates"},
		Summary:     "Download a certificate's private key file",
		Responses:   binaryResponses("The certificate's PEM-encoded private key file."),
	}), getCertificateKey(h))

	huma.Register(humaAPI, roleOp("viewer", huma.Operation{
		OperationID: "getCA",
		Method:      http.MethodGet,
		Path:        BasePath + "/ca",
		Tags:        []string{"ca"},
		Summary:     "Get the certificate authority's subject, fingerprint, validity and reachability",
	}), getCAInfo(h))

	huma.Register(humaAPI, roleOp("admin", huma.Operation{
		OperationID: "getCARoot",
		Method:      http.MethodGet,
		Path:        BasePath + "/ca/root",
		Tags:        []string{"ca"},
		Summary:     "Download the root CA certificate",
		Responses:   binaryResponses("The root CA's PEM-encoded certificate."),
	}), getCARoot(h))

	huma.Register(humaAPI, roleOp("admin", huma.Operation{
		OperationID: "getCAIntermediate",
		Method:      http.MethodGet,
		Path:        BasePath + "/ca/intermediate",
		Tags:        []string{"ca"},
		Summary:     "Download the intermediate CA certificate",
		Responses:   binaryResponses("The intermediate CA's PEM-encoded certificate."),
	}), getCAIntermediate(h))

	huma.Register(humaAPI, roleOp("admin", huma.Operation{
		OperationID: "getCAChain",
		Method:      http.MethodGet,
		Path:        BasePath + "/ca/chain",
		Tags:        []string{"ca"},
		Summary:     "Download the full certificate chain (intermediate followed by root)",
		Responses:   binaryResponses("The intermediate certificate followed by the root certificate, PEM-encoded."),
	}), getCAChain(h))

	// Phase 5, step 1 (plans/frontend-backend-split.md Section 8): the
	// list envelope, the total-order fix and the first two of this
	// phase's five operations. security/events, audit-events and
	// dashboard follow in later steps.
	huma.Register(humaAPI, roleOp("viewer", huma.Operation{
		OperationID: "listHistory",
		Method:      http.MethodGet,
		Path:        BasePath + "/history",
		Tags:        []string{"system"},
		Summary:     "List certificate lifecycle history entries",
	}), listHistory(h))

	huma.Register(humaAPI, roleOp("viewer", huma.Operation{
		OperationID: "listProvisioners",
		Method:      http.MethodGet,
		Path:        BasePath + "/provisioners",
		Tags:        []string{"provisioners"},
		Summary:     "List the certificate authority's provisioners",
	}), listProvisioners(h))

	// Phase 5, step 2 (plans/frontend-backend-split.md Section 8): the
	// remaining two GETs and the one DELETE of this phase's five
	// operations (dashboard follows in step 3). security/events and
	// audit-events both read the same auth_log table, one filtered and
	// one not (Section 6; there is no separate audit_log table).
	huma.Register(humaAPI, roleOp("admin", huma.Operation{
		OperationID: "listSecurityEvents",
		Method:      http.MethodGet,
		Path:        BasePath + "/security/events",
		Tags:        []string{"security"},
		Summary:     "List authentication and security events",
	}), listSecurityEvents(h))

	huma.Register(humaAPI, roleOp("admin", huma.Operation{
		OperationID: "listAuditEvents",
		Method:      http.MethodGet,
		Path:        BasePath + "/audit-events",
		Tags:        []string{"security"},
		Summary:     "List the full administrative audit trail",
	}), listAuditEvents(h))

	huma.Register(humaAPI, roleOp("admin", huma.Operation{
		OperationID:   "deleteIPBlock",
		Method:        http.MethodDelete,
		Path:          BasePath + "/security/ip-blocks/{ip}",
		Tags:          []string{"security"},
		Summary:       "Clear a rate-limited IP's block",
		DefaultStatus: http.StatusNoContent,
	}), deleteIPBlock(h))

	// Phase 5, step 3: the dashboard aggregate, replacing GET / and GET
	// /dashboard (D4 decision 8, D10). viewer, same as every source it
	// draws from (listCertificates, getCA) — D10 rule 2. No ACME field yet:
	// see dashboard.go's comment.
	huma.Register(humaAPI, roleOp("viewer", huma.Operation{
		OperationID: "getDashboard",
		Method:      http.MethodGet,
		Path:        BasePath + "/dashboard",
		Tags:        []string{"dashboard"},
		Summary:     "Get the dashboard aggregate: certificate counts and CA status",
	}), getDashboard(h))

	// Phase 6, step 2 (plans/frontend-backend-split.md Section 8): admin
	// console, about and integrity. The console's two operations split the
	// old single GET/POST /admin/console: listing the allowlist (DTO-narrowed
	// to id/label/description) and running one command by id are now
	// separate operations, matching the table at Section 6.
	huma.Register(humaAPI, roleOp("admin", huma.Operation{
		OperationID: "listAdminConsoleCommands",
		Method:      http.MethodGet,
		Path:        BasePath + "/admin/console/commands",
		Tags:        []string{"admin"},
		Summary:     "List the allowlisted diagnostic console commands",
	}), listAdminConsoleCommands(h))

	huma.Register(humaAPI, roleOp("admin", huma.Operation{
		OperationID: "runAdminConsoleCommand",
		Method:      http.MethodPost,
		Path:        BasePath + "/admin/console/run",
		Tags:        []string{"admin"},
		Summary:     "Run one allowlisted diagnostic console command",
	}), postAdminConsoleRun(h))

	huma.Register(humaAPI, roleOp("admin", huma.Operation{
		OperationID: "getAdminAbout",
		Method:      http.MethodGet,
		Path:        BasePath + "/admin/about",
		Tags:        []string{"admin"},
		Summary:     "Get system build info and preflight health checks",
	}), getAdminAbout(h))

	huma.Register(humaAPI, roleOp("admin", huma.Operation{
		OperationID: "getAdminIntegrity",
		Method:      http.MethodGet,
		Path:        BasePath + "/admin/integrity",
		Tags:        []string{"admin"},
		Summary:     "Get CA integrity and update guardrail checks",
	}), getAdminIntegrity(h))

	// Phase 6, step 1 (plans/frontend-backend-split.md Section 8): the
	// five-way POST /admin/users multiplexer becomes five single-purpose
	// operations (unblock_ip left already, in Phase 5 step 2, as
	// deleteIPBlock above), plus the list and detail GETs. All admin: the
	// template chain gates the whole /admin/users tree at admin today.
	huma.Register(humaAPI, roleOp("admin", huma.Operation{
		OperationID: "listUsers",
		Method:      http.MethodGet,
		Path:        BasePath + "/users",
		Tags:        []string{"users"},
		Summary:     "List user accounts",
	}), listUsers(h))

	huma.Register(humaAPI, roleOp("admin", huma.Operation{
		OperationID: "getUser",
		Method:      http.MethodGet,
		Path:        BasePath + "/users/{id}",
		Tags:        []string{"users"},
		Summary:     "Get one user account",
	}), getUser(h))

	huma.Register(humaAPI, roleOp("admin", huma.Operation{
		OperationID:   "createUser",
		Method:        http.MethodPost,
		Path:          BasePath + "/users",
		Tags:          []string{"users"},
		Summary:       "Create a user account",
		DefaultStatus: http.StatusCreated,
	}), createUser(h))

	huma.Register(humaAPI, roleOp("admin", huma.Operation{
		OperationID: "updateUser",
		Method:      http.MethodPatch,
		Path:        BasePath + "/users/{id}",
		Tags:        []string{"users"},
		Summary:     "Update a user's role and/or active state",
	}), updateUser(h))

	huma.Register(humaAPI, roleOp("admin", huma.Operation{
		OperationID:   "deleteUser",
		Method:        http.MethodDelete,
		Path:          BasePath + "/users/{id}",
		Tags:          []string{"users"},
		Summary:       "Delete a user account",
		DefaultStatus: http.StatusNoContent,
	}), deleteUser(h))

	huma.Register(humaAPI, roleOp("admin", huma.Operation{
		OperationID: "resetUserPassword",
		Method:      http.MethodPost,
		Path:        BasePath + "/users/{id}/password-reset",
		Tags:        []string{"users"},
		Summary:     "Reset a user's password",
	}), resetUserPassword(h))

	// Temporary users (handlers/admin_temp.go), same tag and role as the
	// permanent-user operations above: Section 6's endpoint map.
	huma.Register(humaAPI, roleOp("admin", huma.Operation{
		OperationID: "listTemporaryUsers",
		Method:      http.MethodGet,
		Path:        BasePath + "/temporary-users",
		Tags:        []string{"users"},
		Summary:     "List temporary user accounts",
	}), listTemporaryUsers(h))

	huma.Register(humaAPI, roleOp("admin", huma.Operation{
		OperationID:   "createTemporaryUser",
		Method:        http.MethodPost,
		Path:          BasePath + "/temporary-users",
		Tags:          []string{"users"},
		Summary:       "Create a temporary user account",
		DefaultStatus: http.StatusCreated,
	}), createTemporaryUser(h))

	huma.Register(humaAPI, roleOp("admin", huma.Operation{
		OperationID:   "deleteTemporaryUser",
		Method:        http.MethodDelete,
		Path:          BasePath + "/temporary-users/{id}",
		Tags:          []string{"users"},
		Summary:       "Delete a temporary user account",
		DefaultStatus: http.StatusNoContent,
	}), deleteTemporaryUser(h))

	// Phase 6, step 3 (plans/frontend-backend-split.md Section 8): backups
	// with the create-then-navigate pattern (5.7) and notifications as
	// PATCH with writeOnly secrets (5.8). Every row here is admin.
	huma.Register(humaAPI, roleOp("admin", huma.Operation{
		OperationID: "listBackups",
		Method:      http.MethodGet,
		Path:        BasePath + "/admin/backups",
		Tags:        []string{"admin"},
		Summary:     "List persisted backup bundles",
	}), listBackups(h))

	huma.Register(humaAPI, roleOp("admin", huma.Operation{
		OperationID:   "createBackup",
		Method:        http.MethodPost,
		Path:          BasePath + "/admin/backups",
		Tags:          []string{"admin"},
		Summary:       "Create a new backup bundle",
		DefaultStatus: http.StatusCreated,
	}), createBackup(h))

	huma.Register(humaAPI, roleOp("admin", huma.Operation{
		OperationID: "downloadBackup",
		Method:      http.MethodGet,
		Path:        BasePath + "/admin/backups/{id}/download",
		Tags:        []string{"admin"},
		Summary:     "Download a backup bundle",
		Responses:   binaryResponses("The backup bundle, gzip-compressed tar."),
	}), downloadBackup(h))

	huma.Register(humaAPI, roleOp("admin", huma.Operation{
		OperationID: "getNotifications",
		Method:      http.MethodGet,
		Path:        BasePath + "/admin/notifications",
		Tags:        []string{"admin"},
		Summary:     "Get notification settings",
	}), getNotifications(h))

	huma.Register(humaAPI, roleOp("admin", huma.Operation{
		OperationID: "patchNotifications",
		Method:      http.MethodPatch,
		Path:        BasePath + "/admin/notifications",
		Tags:        []string{"admin"},
		Summary:     "Update notification settings",
	}), patchNotifications(h))

	huma.Register(humaAPI, roleOp("admin", huma.Operation{
		OperationID:   "testNotifications",
		Method:        http.MethodPost,
		Path:          BasePath + "/admin/notifications/test",
		Tags:          []string{"admin"},
		Summary:       "Send a test webhook notification",
		DefaultStatus: http.StatusNoContent,
	}), testNotifications(h))

	// Phase 7, step 2 (plans/frontend-backend-split.md Section 8 "### ACME"
	// / Section 6 "### ACME"): certificates CRUD, options and downloads.
	// Every row here is manager. ACME settings (PATCH) and events follow
	// immediately below (step 3).
	huma.Register(humaAPI, roleOp("manager", huma.Operation{
		OperationID: "listACMECertificates",
		Method:      http.MethodGet,
		Path:        BasePath + "/acme/certificates",
		Tags:        []string{"acme"},
		Summary:     "List ACME-managed certificates",
	}), listACMECertificates(h))

	huma.Register(humaAPI, roleOp("manager", huma.Operation{
		OperationID: "getACMEOptions",
		Method:      http.MethodGet,
		Path:        BasePath + "/acme/options",
		Tags:        []string{"acme"},
		Summary:     "Get ACME issuance options and defaults",
	}), getACMEOptions(h))

	huma.Register(humaAPI, roleOp("manager", huma.Operation{
		OperationID:   "createACMECertificate",
		Method:        http.MethodPost,
		Path:          BasePath + "/acme/certificates",
		Tags:          []string{"acme"},
		Summary:       "Issue a new ACME certificate",
		DefaultStatus: http.StatusCreated,
	}), createACMECertificate(h))

	huma.Register(humaAPI, roleOp("manager", huma.Operation{
		OperationID: "renewACMECertificate",
		Method:      http.MethodPost,
		Path:        BasePath + "/acme/certificates/{id}/renew",
		Tags:        []string{"acme"},
		Summary:     "Manually renew an ACME certificate",
	}), renewACMECertificate(h))

	huma.Register(humaAPI, roleOp("manager", huma.Operation{
		OperationID:   "deleteACMECertificate",
		Method:        http.MethodDelete,
		Path:          BasePath + "/acme/certificates/{id}",
		Tags:          []string{"acme"},
		Summary:       "Delete an ACME certificate record",
		DefaultStatus: http.StatusNoContent,
	}), deleteACMECertificate(h))

	huma.Register(humaAPI, roleOp("manager", huma.Operation{
		OperationID: "patchACMECertificate",
		Method:      http.MethodPatch,
		Path:        BasePath + "/acme/certificates/{id}",
		Tags:        []string{"acme"},
		Summary:     "Toggle auto-renewal for an ACME certificate",
	}), patchACMECertificate(h))

	huma.Register(humaAPI, roleOp("manager", huma.Operation{
		OperationID: "getACMECertificateCertificate",
		Method:      http.MethodGet,
		Path:        BasePath + "/acme/certificates/{id}/certificate",
		Tags:        []string{"acme"},
		Summary:     "Download an ACME certificate's certificate file",
		Responses:   binaryResponses("The ACME certificate file, PEM-encoded."),
	}), getACMECertificateCertificate(h))

	huma.Register(humaAPI, roleOp("manager", huma.Operation{
		OperationID: "getACMECertificateKey",
		Method:      http.MethodGet,
		Path:        BasePath + "/acme/certificates/{id}/key",
		Tags:        []string{"acme"},
		Summary:     "Download an ACME certificate's private key",
		Responses:   binaryResponses("The ACME certificate's private key, PEM-encoded."),
	}), getACMECertificateKey(h))

	// Phase 7, step 3 (plans/frontend-backend-split.md Section 8, "### ACME"):
	// settings as PATCH with writeOnly provider secrets, and events with
	// the same pagination convention history/security-events/audit-events
	// already proved. Every row here is manager, matching the le_* chi
	// group's current gate.
	huma.Register(humaAPI, roleOp("manager", huma.Operation{
		OperationID: "getAcmeSettings",
		Method:      http.MethodGet,
		Path:        BasePath + "/acme/settings",
		Tags:        []string{"acme"},
		Summary:     "Get ACME provider settings",
	}), getAcmeSettings(h))

	huma.Register(humaAPI, roleOp("manager", huma.Operation{
		OperationID: "patchAcmeSettings",
		Method:      http.MethodPatch,
		Path:        BasePath + "/acme/settings",
		Tags:        []string{"acme"},
		Summary:     "Update ACME provider settings",
	}), patchAcmeSettings(h))

	huma.Register(humaAPI, roleOp("manager", huma.Operation{
		OperationID: "listAcmeEvents",
		Method:      http.MethodGet,
		Path:        BasePath + "/acme/events",
		Tags:        []string{"acme"},
		Summary:     "List ACME certificate lifecycle events",
	}), listAcmeEvents(h))

	// Phase 8, step A (plans/frontend-backend-split.md Section 8 "Profile"):
	// GET/PATCH profile, PATCH preferences, POST password (reauth). Every
	// row is viewer, acting on the session's own account: no {id} parameter
	// anywhere in this group (5.5). The old /profile chi group sat with no
	// RequireRole wrapper (any authenticated user), so viewer is the correct
	// floor, not a guess.
	huma.Register(humaAPI, roleOp("viewer", huma.Operation{
		OperationID: "getProfile",
		Method:      http.MethodGet,
		Path:        BasePath + "/profile",
		Tags:        []string{"profile"},
		Summary:     "Get the signed-in user's own profile",
	}), getProfile(h))

	huma.Register(humaAPI, roleOp("viewer", huma.Operation{
		OperationID: "patchProfile",
		Method:      http.MethodPatch,
		Path:        BasePath + "/profile",
		Tags:        []string{"profile"},
		Summary:     "Update the signed-in user's display name and email",
	}), patchProfile(h))

	huma.Register(humaAPI, roleOp("viewer", huma.Operation{
		OperationID: "patchProfilePreferences",
		Method:      http.MethodPatch,
		Path:        BasePath + "/profile/preferences",
		Tags:        []string{"profile"},
		Summary:     "Update the signed-in user's theme preference",
	}), patchProfilePreferences(h))

	huma.Register(humaAPI, roleOp("viewer", huma.Operation{
		OperationID:   "changePassword",
		Method:        http.MethodPost,
		Path:          BasePath + "/profile/password",
		Tags:          []string{"profile"},
		Summary:       "Change the signed-in user's password",
		DefaultStatus: http.StatusNoContent,
	}), changePassword(h))

	// Phase 8 (plans/frontend-backend-split.md Section 8 "Phase 8. Profile
	// and MFA", Section 6): the three password-reset operations, public and
	// rate-limited the same way createSession/submitMfa are above. See
	// api/password_reset.go's doc comments for the per-operation rationale.
	huma.Register(humaAPI, rateLimited(publicOp(huma.Operation{
		OperationID:   "requestPasswordReset",
		Method:        http.MethodPost,
		Path:          BasePath + "/password-reset/requests",
		Tags:          []string{"session"},
		Summary:       "Request a password reset email",
		DefaultStatus: http.StatusAccepted,
	})), requestPasswordReset(h))

	huma.Register(humaAPI, rateLimited(publicOp(huma.Operation{
		OperationID:   "validatePasswordReset",
		Method:        http.MethodPost,
		Path:          BasePath + "/password-reset/validate",
		Tags:          []string{"session"},
		Summary:       "Validate a password reset token without consuming it",
		DefaultStatus: http.StatusNoContent,
	})), validatePasswordReset(h))

	huma.Register(humaAPI, rateLimited(publicOp(huma.Operation{
		OperationID:   "confirmPasswordReset",
		Method:        http.MethodPost,
		Path:          BasePath + "/password-reset/confirm",
		Tags:          []string{"session"},
		Summary:       "Set a new password using a password reset token",
		DefaultStatus: http.StatusNoContent,
	})), confirmPasswordReset(h))
}
