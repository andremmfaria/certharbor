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
		OperationID: "importCertificateUpload",
		Method:      http.MethodPost,
		Path:        BasePath + "/certificates/import/upload",
		Tags:        []string{"certificates"},
		Summary:     "Import a certificate by uploading its files",
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
}
