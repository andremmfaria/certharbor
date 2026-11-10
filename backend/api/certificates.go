package api

import (
	"context"
	"net/http"
	"time"

	"certharbor/apitypes"
	"certharbor/handlers"
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
