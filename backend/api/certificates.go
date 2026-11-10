package api

import (
	"context"
	"errors"
	"mime/multipart"
	"net/http"
	"time"

	"certharbor/apitypes"
	"certharbor/handlers"
	appmw "certharbor/middleware"

	"github.com/danielgtaylor/huma/v2"
)

// certificateSummaryBody is CertificateSummary's wire shape: the row list
// and detail share (5.11's getCertificate shape, extended with the fields
// Section 6's note on the certificate DTOs asks for). expiresAt is a
// pointer because models.Certificate's is: a certificate with no recorded
// expiry renders null rather than the Unix epoch.
type certificateSummaryBody struct {
	ID           int        `json:"id"`
	Name         string     `json:"name"`
	Domain       string     `json:"domain"`
	Status       string     `json:"status"`
	KeyType      string     `json:"keyType"`
	Serial       string     `json:"serial"`
	IssuedAt     *time.Time `json:"issuedAt,omitempty"`
	ExpiresAt    *time.Time `json:"expiresAt,omitempty"`
	ExpiryStatus string     `json:"expiryStatus" enum:"ok,warning,expired"`
}

func newCertificateSummaryBody(s *handlers.CertificateSummary) certificateSummaryBody {
	return certificateSummaryBody{
		ID: s.ID, Name: s.Name, Domain: s.Domain, Status: s.Status,
		KeyType: s.KeyType, Serial: s.Serial, IssuedAt: s.IssuedAt, ExpiresAt: s.ExpiresAt,
		ExpiryStatus: handlers.CertExpiryStatus(s.ExpiresAt),
	}
}

// ─── GET /api/v1/certificates ───────────────────────────────────────────────

type listCertificatesInput struct {
	Page     int    `query:"page" minimum:"1" default:"1"`
	PageSize int    `query:"pageSize" minimum:"1" maximum:"200" default:"25"`
	Q        string `query:"q" maxLength:"255" doc:"Case-insensitive substring match against name and domain."`
	Status   string `query:"status" maxLength:"64" doc:"Certificate status filter, e.g. active or revoked."`
}

type listCertificatesOutput struct {
	Body apitypes.Page[certificateSummaryBody]
}

// listCertificates implements GET /api/v1/certificates, replacing GET
// /certificates (5.6, Section 6). The `tab` parameter the old route carried
// is deliberately dropped (Section 6's note on the certificates table): a
// UI tab is not a certificate property and has no place in this query.
//
// Pagination and the `q` filter are applied in process rather than in SQL
// (handlers.ListCertificateSummaries), matching what the template route
// already does by loading every row and leaving the template to iterate.
// Moving that to SQL, and the total-order fix 5.6 otherwise asks for, is
// Phase 5's explicit scope, not this one's.
func listCertificates(h *handlers.Handler) func(context.Context, *listCertificatesInput) (*listCertificatesOutput, error) {
	return func(_ context.Context, in *listCertificatesInput) (*listCertificatesOutput, error) {
		page, pageSize := in.Page, in.PageSize
		if page < 1 {
			page = 1
		}
		if pageSize < 1 {
			pageSize = apitypes.DefaultPageSize
		}
		if page*pageSize > apitypes.MaxOffset {
			return nil, newProblem(http.StatusUnprocessableEntity, "Unprocessable Entity", problemTypeBase+"validation",
				"page*pageSize exceeds the maximum offset", "")
		}

		all, err := h.ListCertificateSummaries(in.Status, in.Q)
		if err != nil {
			return nil, err
		}

		total := len(all)
		start := (page - 1) * pageSize
		items := []certificateSummaryBody{}
		if start < total {
			end := start + pageSize
			if end > total {
				end = total
			}
			for i := range all[start:end] {
				items = append(items, newCertificateSummaryBody(&all[start:end][i]))
			}
		}

		return &listCertificatesOutput{Body: apitypes.NewPage(items, page, pageSize, total)}, nil
	}
}

// ─── GET /api/v1/certificates/{id} ──────────────────────────────────────────

type getCertificateInput struct {
	ID int `path:"id" doc:"Certificate id."`
}

type certValidationBody struct {
	Name   string `json:"name"`
	Status string `json:"status" enum:"ok,warn,err"`
	Detail string `json:"detail"`
}

type certificateDetailBody struct {
	certificateSummaryBody
	Subject           string               `json:"subject,omitempty"`
	Issuer            string               `json:"issuer,omitempty"`
	SerialHex         string               `json:"serialHex,omitempty"`
	FingerprintSha1   string               `json:"fingerprintSha1,omitempty"`
	FingerprintSha256 string               `json:"fingerprintSha256,omitempty"`
	NotBefore         *time.Time           `json:"notBefore,omitempty"`
	NotAfter          *time.Time           `json:"notAfter,omitempty"`
	DaysLeft          *int                 `json:"daysLeft,omitempty"`
	PublicKey         string               `json:"publicKey,omitempty"`
	SignatureAlg      string               `json:"signatureAlgorithm,omitempty"`
	DNSNames          []string             `json:"dnsNames,omitempty"`
	IPAddresses       []string             `json:"ipAddresses,omitempty"`
	KeyUsage          []string             `json:"keyUsage,omitempty"`
	ExtKeyUsage       []string             `json:"extKeyUsage,omitempty"`
	Validations       []certValidationBody `json:"validations"`
}

type getCertificateOutput struct {
	Body certificateDetailBody
}

// getCertificate implements GET /api/v1/certificates/{id}, replacing GET
// /certificates/{id} (5.11's worked example, extended with the x509 detail
// and validation fields the template page also shows). CertPath and KeyPath
// are deliberately absent: both are on Section 2's banned-property-name
// list, so no response may carry a filesystem path for a managed
// certificate's files.
func getCertificate(h *handlers.Handler) func(context.Context, *getCertificateInput) (*getCertificateOutput, error) {
	return func(_ context.Context, in *getCertificateInput) (*getCertificateOutput, error) {
		summary, detail, validations, err := h.FetchCertificateDetail(in.ID)
		if err != nil {
			return nil, err
		}
		if summary == nil {
			return nil, newProblem(http.StatusNotFound, "Not Found", "", "no certificate with that id", "")
		}

		body := certificateDetailBody{certificateSummaryBody: newCertificateSummaryBody(summary)}
		if detail != nil {
			days := detail.DaysLeft
			body.Subject = detail.Subject
			body.Issuer = detail.Issuer
			body.SerialHex = detail.SerialHex
			body.FingerprintSha1 = detail.FingerprintSHA1
			body.FingerprintSha256 = detail.Fingerprint256
			body.NotBefore = &detail.NotBefore
			body.NotAfter = &detail.NotAfter
			body.DaysLeft = &days
			body.PublicKey = detail.PublicKey
			body.SignatureAlg = detail.Signature
			body.DNSNames = detail.DNSNames
			body.IPAddresses = detail.IPAddresses
			body.KeyUsage = detail.KeyUsage
			body.ExtKeyUsage = detail.ExtKeyUsage
		}
		body.Validations = make([]certValidationBody, 0, len(validations))
		for _, v := range validations {
			body.Validations = append(body.Validations, certValidationBody{Name: v.Name, Status: v.Status, Detail: v.Detail})
		}

		return &getCertificateOutput{Body: body}, nil
	}
}

// ─── GET /api/v1/certificates/options ───────────────────────────────────────

type certTemplateOptionBody struct {
	ID              string `json:"id"`
	Label           string `json:"label"`
	Description     string `json:"description"`
	DefaultDuration string `json:"defaultDuration"`
	DefaultKeyType  string `json:"defaultKeyType"`
}

type certKeyTypeOptionBody struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Note  string `json:"note,omitempty"`
}

type certDurationOptionBody struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type certProvisionerOptionBody struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type certificateOptionsBody struct {
	Templates             []certTemplateOptionBody    `json:"templates"`
	KeyTypes              []certKeyTypeOptionBody     `json:"keyTypes"`
	Durations             []certDurationOptionBody    `json:"durations"`
	Provisioners          []certProvisionerOptionBody `json:"provisioners"`
	AllowedDomainSuffixes []string                    `json:"allowedDomainSuffixes"`
	DefaultTemplate       string                      `json:"defaultTemplate"`
}

type getCertificateOptionsOutput struct {
	Body certificateOptionsBody
}

// getCertificateOptions implements GET /api/v1/certificates/options,
// replacing GET /issue (Section 6). None of this response is schema
// constraint (D3 rule 1): templates, key types, durations, provisioners and
// the allowed-domain-suffix policy are all response data a client reads,
// never an enum, maximum or default on the request schema, which is exactly
// where this operation would otherwise leak configuration into the spec.
func getCertificateOptions(h *handlers.Handler) func(context.Context, *struct{}) (*getCertificateOptionsOutput, error) {
	return func(ctx context.Context, _ *struct{}) (*getCertificateOptionsOutput, error) {
		opts := h.CertificateIssueOptions(ctx)

		body := certificateOptionsBody{
			AllowedDomainSuffixes: opts.AllowedDomainSuffixes,
			DefaultTemplate:       opts.DefaultTemplate,
		}
		for _, t := range opts.Templates {
			body.Templates = append(body.Templates, certTemplateOptionBody{
				ID: t.ID, Label: t.Label, Description: t.Description,
				DefaultDuration: t.DefaultDuration, DefaultKeyType: t.DefaultKeyType,
			})
		}
		for _, k := range opts.KeyTypes {
			body.KeyTypes = append(body.KeyTypes, certKeyTypeOptionBody{ID: k.ID, Label: k.Label, Note: k.Note})
		}
		for _, d := range opts.Durations {
			body.Durations = append(body.Durations, certDurationOptionBody{ID: d.ID, Label: d.Label})
		}
		for _, p := range opts.Provisioners {
			body.Provisioners = append(body.Provisioners, certProvisionerOptionBody{Name: p.Name, Type: p.Type})
		}
		if body.AllowedDomainSuffixes == nil {
			body.AllowedDomainSuffixes = []string{}
		}

		return &getCertificateOptionsOutput{Body: body}, nil
	}
}

// ─── Phase 4, second batch: issue, import, renew, revoke ───────────────────
//
// R23's observed-cost record for this batch, which 8's "do the first three
// before scoping the rest" asked for: list, detail and options (above) each
// had a predecessor in handlers/ that already isolated the data from the
// rendering. None of these four do. IssuePost, Renew, Revoke and ImportPost
// compute and write their response in the same function body, so every one
// of handlers.IssueCertificate, RenewCertificate, RevokeCertificate,
// ImportCertificateUpload and ImportCertificateByPath is new code, not an
// extraction of existing code — the real cost R23 warned this phase would
// carry, now priced: roughly the size of the template handler each replaces,
// once per operation, because "separate the data from the response write" and
// "write the first version of the data half" turn out to be the same amount
// of work when there is nothing to separate.

// userIdentity reads the username and role off the request context for the
// certificate_history row, without naming certharbor/models: the type of
// appmw.UserFrom's result is inferred by :=, exactly as session.go's
// sessionUserBody construction already relies on (Section 2's depguard rule).
func userIdentity(ctx context.Context) (username, role string) {
	if user, ok := appmw.UserFrom(ctx); ok {
		username, role = user.Username, user.Role
	}
	return username, role
}

// mapCertError maps a handlers.ValidationError to 422 (5.2: caller-input
// errors, not malformed JSON, so this is the application-level case the
// rule reserves 400 from). Every other error is returned unchanged and
// surfaces as huma's default 500, same as getCertificate already does for a
// database failure.
func mapCertError(err error) error {
	var verr *handlers.ValidationError
	if errors.As(err, &verr) {
		return newProblem(http.StatusUnprocessableEntity, "Unprocessable Entity", problemTypeBase+"validation", err.Error(), "")
	}
	return err
}

type certificateOutput struct {
	Body certificateSummaryBody
}

// ─── POST /api/v1/certificates ──────────────────────────────────────────────

type issueCertificateBody struct {
	Name     string `json:"name" minLength:"1" maxLength:"255" doc:"Certificate name, used as its managed directory name."`
	Domain   string `json:"domain" minLength:"1" maxLength:"255" doc:"Domain or IP address the certificate covers."`
	Template string `json:"template,omitempty" maxLength:"64" doc:"Certificate template id, from GET /api/v1/certificates/options."`
	Duration string `json:"duration,omitempty" maxLength:"16" doc:"Issuance duration, e.g. 8760h."`
	KeyType  string `json:"keyType,omitempty" maxLength:"16" doc:"Key type, e.g. EC:P-256."`
}

type issueCertificateInput struct {
	Body issueCertificateBody
}

// createCertificate implements POST /api/v1/certificates, replacing POST
// /issue (Section 6). The field is name, not cert_name (handlers/certs.go's
// old IssuePost read r.FormValue("name") into a local also called name, so
// only the wire name changes here, not the Go-side one).
func createCertificate(h *handlers.Handler) func(context.Context, *issueCertificateInput) (*certificateOutput, error) {
	return func(ctx context.Context, in *issueCertificateInput) (*certificateOutput, error) {
		username, role := userIdentity(ctx)
		summary, err := h.IssueCertificate(ctx, username, role, in.Body.Name, in.Body.Domain, in.Body.Template, in.Body.Duration, in.Body.KeyType)
		if err != nil {
			return nil, mapCertError(err)
		}
		return &certificateOutput{Body: newCertificateSummaryBody(summary)}, nil
	}
}

// ─── GET /api/v1/certificates/import-candidates ─────────────────────────────

// importCandidateBody is deliberately not named with CertPath/KeyPath
// fields: see handlers.ImportCandidate's comment. certFile and keyFile here
// are plain response data, not writeOnly, because nothing about this
// response is a secret: it is the fact an admin importing a certificate
// needs to choose between.
type importCandidateBody struct {
	Name     string `json:"name"`
	CertFile string `json:"certFile"`
	KeyFile  string `json:"keyFile,omitempty"`
}

type listImportCandidatesOutput struct {
	Body []importCandidateBody
}

// listCertificateImportCandidates implements GET
// /api/v1/certificates/import-candidates, replacing GET /import (Section 6).
// It reuses scanExistingCerts via handlers.ImportCandidates, which already
// does the filesystem walk and the already-registered check.
func listCertificateImportCandidates(h *handlers.Handler) func(context.Context, *struct{}) (*listImportCandidatesOutput, error) {
	return func(_ context.Context, _ *struct{}) (*listImportCandidatesOutput, error) {
		candidates := h.ImportCandidates()
		body := make([]importCandidateBody, 0, len(candidates))
		for _, c := range candidates {
			body = append(body, importCandidateBody{Name: c.Name, CertFile: c.CertFile, KeyFile: c.KeyFile})
		}
		return &listImportCandidatesOutput{Body: body}, nil
	}
}

// ─── POST /api/v1/certificates/import/upload ────────────────────────────────

type importUploadFields struct {
	Name     string        `form:"name" required:"true" maxLength:"255"`
	Domain   string        `form:"domain" required:"true" maxLength:"255"`
	CertFile huma.FormFile `form:"certFile" contentType:"application/octet-stream" required:"true"`
	KeyFile  huma.FormFile `form:"keyFile" contentType:"application/octet-stream"`
}

type importUploadInput struct {
	RawBody huma.MultipartFormFiles[importUploadFields]
}

// importCertificateUpload implements POST
// /api/v1/certificates/import/upload, the file-upload half of POST /import
// (Section 6: "two operations by payload"). It reuses Phase 0's proven
// multipart mechanic (huma.MultipartFormFiles, humachi.MultipartMaxMemory).
func importCertificateUpload(h *handlers.Handler) func(context.Context, *importUploadInput) (*certificateOutput, error) {
	return func(ctx context.Context, in *importUploadInput) (*certificateOutput, error) {
		username, role := userIdentity(ctx)
		data := in.RawBody.Data()
		var keyFile multipart.File
		if data.KeyFile.IsSet {
			keyFile = data.KeyFile.File
		}
		summary, err := h.ImportCertificateUpload(username, role, data.Name, data.Domain, data.CertFile.File, keyFile)
		if err != nil {
			return nil, mapCertError(err)
		}
		return &certificateOutput{Body: newCertificateSummaryBody(summary)}, nil
	}
}

// ─── POST /api/v1/certificates/import/paths ─────────────────────────────────

// importPathItem is the request shape Section 6 specifies verbatim:
// {name, domain, certPath, keyPath}. certPath and keyPath are allowlisted in
// openapi/secret-allowlist.txt and writeOnly, so they can appear in this
// request body and nowhere in any response.
type importPathItem struct {
	Name     string `json:"name" minLength:"1" maxLength:"255"`
	Domain   string `json:"domain" minLength:"1" maxLength:"255"`
	CertPath string `json:"certPath" minLength:"1" writeOnly:"true" doc:"Path to the certificate file, resolved against the managed certificates directory."`
	KeyPath  string `json:"keyPath,omitempty" writeOnly:"true" doc:"Path to the private key file, resolved against the managed certificates directory."`
}

type importPathsInput struct {
	Body []importPathItem
}

type importPathsOutput struct {
	Body apitypes.BulkResponse
}

// importCertificatesByPaths implements POST /api/v1/certificates/import/paths,
// serving both the manual-path case and the scan-import case (Section 6):
// the SPA submits a candidate's certFile/keyFile from
// listCertificateImportCandidates through here exactly as it would a
// hand-typed path. Unlike the old import/scan, which swallowed per-item
// failures into a count, every item gets its own result (5.2's bulk-result
// convention): this always answers 200, never a problem document for a
// partial failure.
func importCertificatesByPaths(h *handlers.Handler) func(context.Context, *importPathsInput) (*importPathsOutput, error) {
	return func(ctx context.Context, in *importPathsInput) (*importPathsOutput, error) {
		username, role := userIdentity(ctx)
		results := make([]apitypes.BulkResult, 0, len(in.Body))
		for _, item := range in.Body {
			ref := item.Name
			if ref == "" {
				ref = item.Domain
			}
			if _, err := h.ImportCertificateByPath(username, role, item.Name, item.Domain, item.CertPath, item.KeyPath); err != nil {
				results = append(results, apitypes.BulkResult{Ref: ref, Status: "error", Error: err.Error()})
				continue
			}
			results = append(results, apitypes.BulkResult{Ref: ref, Status: "ok"})
		}
		return &importPathsOutput{Body: apitypes.BulkResponse{Results: results}}, nil
	}
}

// ─── POST /api/v1/certificates/{id}/renew ───────────────────────────────────

// renewCertificate implements POST /api/v1/certificates/{id}/renew,
// replacing POST /renew/{id} (Section 6).
func renewCertificate(h *handlers.Handler) func(context.Context, *getCertificateInput) (*certificateOutput, error) {
	return func(ctx context.Context, in *getCertificateInput) (*certificateOutput, error) {
		username, role := userIdentity(ctx)
		summary, err := h.RenewCertificate(ctx, username, role, in.ID)
		if err != nil {
			return nil, mapCertError(err)
		}
		if summary == nil {
			return nil, newProblem(http.StatusNotFound, "Not Found", "", "no certificate with that id", "")
		}
		return &certificateOutput{Body: newCertificateSummaryBody(summary)}, nil
	}
}

// ─── POST /api/v1/certificates/{id}/revoke ──────────────────────────────────

// revokeCertificate implements POST /api/v1/certificates/{id}/revoke,
// replacing POST /revoke/{id}. Section 6 flags the role split here is not
// visible from the page layout: this is admin, every other certificate
// mutation in this file is manager.
func revokeCertificate(h *handlers.Handler) func(context.Context, *getCertificateInput) (*certificateOutput, error) {
	return func(ctx context.Context, in *getCertificateInput) (*certificateOutput, error) {
		username, role := userIdentity(ctx)
		summary, err := h.RevokeCertificate(ctx, username, role, in.ID)
		if err != nil {
			return nil, mapCertError(err)
		}
		if summary == nil {
			return nil, newProblem(http.StatusNotFound, "Not Found", "", "no certificate with that id", "")
		}
		return &certificateOutput{Body: newCertificateSummaryBody(summary)}, nil
	}
}
