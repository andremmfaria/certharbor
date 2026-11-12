package api

import (
	"context"
	"net/http"
	"time"

	"certharbor/apitypes"
	"certharbor/handlers"
)

// acmeCertificateBody is the wire shape GET/POST/PATCH all share (5.11's
// getCertificate shape, applied to ACME certificates). CertPath/KeyPath are
// deliberately absent, same banned-property-name reasoning as
// certificateSummaryBody (Section 2).
type acmeCertificateBody struct {
	ID           int        `json:"id"`
	Domain       string     `json:"domain"`
	Email        string     `json:"email"`
	Provider     string     `json:"provider"`
	Status       string     `json:"status" enum:"active,expired,pending,error"`
	LastError    string     `json:"lastError,omitempty"`
	AutoRenew    bool       `json:"autoRenew"`
	IssuedAt     *time.Time `json:"issuedAt,omitempty"`
	ExpiresAt    *time.Time `json:"expiresAt,omitempty"`
	ExpiryStatus string     `json:"expiryStatus" enum:"ok,warning,expired"`
}

func newACMECertificateBody(s *handlers.ACMECertificateSummary) acmeCertificateBody {
	return acmeCertificateBody{
		ID: s.ID, Domain: s.Domain, Email: s.Email, Provider: s.Provider,
		Status: s.Status, LastError: s.LastError, AutoRenew: s.AutoRenew,
		IssuedAt: s.IssuedAt, ExpiresAt: s.ExpiresAt,
		ExpiryStatus: handlers.CertExpiryStatus(s.ExpiresAt),
	}
}

var errACMECertificateNotFound = newProblem(http.StatusNotFound, "Not Found", "", "no ACME certificate with that id", "")

// ─── GET /api/v1/acme/certificates ──────────────────────────────────────────

type listACMECertificatesInput struct {
	Page     int `query:"page" minimum:"1" default:"1"`
	PageSize int `query:"pageSize" minimum:"1" maximum:"200" default:"25"`
}

type listACMECertificatesOutput struct {
	Body apitypes.Page[acmeCertificateBody]
}

// listACMECertificates implements GET /api/v1/acme/certificates, replacing
// GET /le (Section 6 "### ACME"). Pagination is applied in process, the
// same convention listCertificates already uses (5.6): ACMEDashboard loads
// every row today and leaves the template to iterate, and moving that to
// SQL is not this step's scope.
func listACMECertificates(h *handlers.Handler) func(context.Context, *listACMECertificatesInput) (*listACMECertificatesOutput, error) {
	return func(ctx context.Context, in *listACMECertificatesInput) (*listACMECertificatesOutput, error) {
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

		all, err := h.ListACMECertificateSummaries(ctx)
		if err != nil {
			return nil, err
		}

		total := len(all)
		start := (page - 1) * pageSize
		items := []acmeCertificateBody{}
		if start < total {
			end := start + pageSize
			if end > total {
				end = total
			}
			for i := range all[start:end] {
				items = append(items, newACMECertificateBody(&all[start:end][i]))
			}
		}

		return &listACMECertificatesOutput{Body: apitypes.NewPage(items, page, pageSize, total)}, nil
	}
}

// ─── GET /api/v1/acme/options ───────────────────────────────────────────────

type acmeProviderOptionBody struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Note  string `json:"note,omitempty"`
}

type acmeOptionsBody struct {
	Providers             []acmeProviderOptionBody `json:"providers"`
	DefaultProvider       string                   `json:"defaultProvider"`
	DefaultEmail          string                   `json:"defaultEmail,omitempty"`
	AllowedDomainSuffixes []string                 `json:"allowedDomainSuffixes"`
	CloudflareConfigured  bool                     `json:"cloudflareConfigured"`
	Route53Configured     bool                     `json:"route53Configured"`
}

type getACMEOptionsOutput struct {
	Body acmeOptionsBody
}

// getACMEOptions implements GET /api/v1/acme/options, replacing GET
// /le/issue (Section 6). Mirrors getCertificateOptions' reasoning (D3 rule
// 1): the provider list, the carried-over defaults and the domain policy
// are response data, never request-schema constraints.
func getACMEOptions(h *handlers.Handler) func(context.Context, *struct{}) (*getACMEOptionsOutput, error) {
	return func(ctx context.Context, _ *struct{}) (*getACMEOptionsOutput, error) {
		opts, err := h.FetchACMEIssueOptions(ctx)
		if err != nil {
			return nil, err
		}
		body := acmeOptionsBody{
			DefaultProvider:       opts.DefaultProvider,
			DefaultEmail:          opts.DefaultEmail,
			AllowedDomainSuffixes: opts.AllowedDomainSuffixes,
			CloudflareConfigured:  opts.CloudflareConfigured,
			Route53Configured:     opts.Route53Configured,
		}
		for _, p := range opts.Providers {
			body.Providers = append(body.Providers, acmeProviderOptionBody{ID: p.ID, Label: p.Label, Note: p.Note})
		}
		if body.AllowedDomainSuffixes == nil {
			body.AllowedDomainSuffixes = []string{}
		}
		return &getACMEOptionsOutput{Body: body}, nil
	}
}

// ─── POST /api/v1/acme/certificates ─────────────────────────────────────────

type issueACMECertificateInput struct {
	Body struct {
		Domain    string `json:"domain" minLength:"1" maxLength:"255" doc:"Domain the certificate covers. Must be publicly accessible."`
		Email     string `json:"email" minLength:"1" maxLength:"255" doc:"Used for Let's Encrypt notifications."`
		Provider  string `json:"provider,omitempty" maxLength:"32" doc:"Challenge provider, from GET /api/v1/acme/options."`
		AutoRenew bool   `json:"autoRenew"`
	}
}

type acmeCertificateOutput struct {
	Body acmeCertificateBody
}

// createACMECertificate implements POST /api/v1/acme/certificates,
// replacing POST /le/issue (Section 6). Issuance stays asynchronous, as
// ACMEIssuePost already is: unlike managed-certificate issuance, an ACME
// challenge can involve DNS propagation delay, so the returned record is
// status "pending" and the caller polls listACMECertificates/getACMECertificate.
func createACMECertificate(h *handlers.Handler) func(context.Context, *issueACMECertificateInput) (*acmeCertificateOutput, error) {
	return func(ctx context.Context, in *issueACMECertificateInput) (*acmeCertificateOutput, error) {
		summary, err := h.IssueACMECertificate(ctx, in.Body.Domain, in.Body.Email, in.Body.Provider, in.Body.AutoRenew)
		if err != nil {
			return nil, mapCertError(err)
		}
		return &acmeCertificateOutput{Body: newACMECertificateBody(summary)}, nil
	}
}

// ─── POST /api/v1/acme/certificates/{id}/renew ──────────────────────────────

type acmeCertificateIDInput struct {
	ID int `path:"id" doc:"ACME certificate id."`
}

// renewACMECertificate implements POST /api/v1/acme/certificates/{id}/renew,
// replacing POST /le/{id}/renew (Section 6).
func renewACMECertificate(h *handlers.Handler) func(context.Context, *acmeCertificateIDInput) (*acmeCertificateOutput, error) {
	return func(ctx context.Context, in *acmeCertificateIDInput) (*acmeCertificateOutput, error) {
		summary, err := h.RenewACMECertificate(ctx, in.ID)
		if err != nil {
			return nil, mapCertError(err)
		}
		if summary == nil {
			return nil, errACMECertificateNotFound
		}
		return &acmeCertificateOutput{Body: newACMECertificateBody(summary)}, nil
	}
}

// ─── DELETE /api/v1/acme/certificates/{id} ──────────────────────────────────

type deleteACMECertificateOutput struct {
	Status int
}

// deleteACMECertificate implements DELETE /api/v1/acme/certificates/{id},
// replacing POST /le/{id}/delete (Section 6).
func deleteACMECertificate(h *handlers.Handler) func(context.Context, *acmeCertificateIDInput) (*deleteACMECertificateOutput, error) {
	return func(ctx context.Context, in *acmeCertificateIDInput) (*deleteACMECertificateOutput, error) {
		if err := h.DeleteACMECertificate(ctx, in.ID); err != nil {
			return nil, err
		}
		return &deleteACMECertificateOutput{Status: http.StatusNoContent}, nil
	}
}

// ─── PATCH /api/v1/acme/certificates/{id} ───────────────────────────────────

type patchACMECertificateInput struct {
	ID   int `path:"id"`
	Body struct {
		AutoRenew *bool `json:"autoRenew,omitempty"`
	}
}

// patchACMECertificate implements PATCH /api/v1/acme/certificates/{id},
// replacing POST /le/{id}/autorenew (Section 6: "one way to model a
// toggle", 5.8, the same pattern PATCH /users/{id} already uses for
// toggle_active). Merge-style (5.8): an absent autoRenew leaves the value
// unchanged, which here is a documented no-op rather than an error, since
// it is the only field this operation carries.
func patchACMECertificate(h *handlers.Handler) func(context.Context, *patchACMECertificateInput) (*acmeCertificateOutput, error) {
	return func(ctx context.Context, in *patchACMECertificateInput) (*acmeCertificateOutput, error) {
		if in.Body.AutoRenew == nil {
			summary, err := h.GetACMECertificateSummary(ctx, in.ID)
			if err != nil {
				return nil, err
			}
			if summary == nil {
				return nil, errACMECertificateNotFound
			}
			return &acmeCertificateOutput{Body: newACMECertificateBody(summary)}, nil
		}
		summary, err := h.UpdateACMECertificateAutoRenew(ctx, in.ID, *in.Body.AutoRenew)
		if err != nil {
			return nil, err
		}
		if summary == nil {
			return nil, errACMECertificateNotFound
		}
		return &acmeCertificateOutput{Body: newACMECertificateBody(summary)}, nil
	}
}

// ─── GET /api/v1/acme/certificates/{id}/certificate, .../key ───────────────

// getACMECertificateCertificate implements GET
// /api/v1/acme/certificates/{id}/certificate, replacing GET
// /le/download/cert/{id} (Section 6). Reuses binaryFileOutput/
// binaryResponses from certificates.go (5.7's single mechanic, same
// package).
func getACMECertificateCertificate(h *handlers.Handler) func(context.Context, *acmeCertificateIDInput) (*binaryFileOutput, error) {
	return func(ctx context.Context, in *acmeCertificateIDInput) (*binaryFileOutput, error) {
		filename, data, err := h.ACMECertificateFile(ctx, in.ID)
		if err != nil {
			return nil, err
		}
		if data == nil {
			return nil, errACMECertificateNotFound
		}
		return newBinaryFileOutput(filename, data), nil
	}
}

// getACMECertificateKey implements GET /api/v1/acme/certificates/{id}/key,
// replacing GET /le/download/key/{id} (Section 6). ACMEDownloadKey's audit
// side effect (a key download is always logged) is preserved via
// AuditACMEKeyDownload, the same split getCertificateKey already uses.
func getACMECertificateKey(h *handlers.Handler) func(context.Context, *acmeCertificateIDInput) (*binaryFileOutput, error) {
	return func(ctx context.Context, in *acmeCertificateIDInput) (*binaryFileOutput, error) {
		filename, data, err := h.ACMEKeyFile(ctx, in.ID)
		if err != nil {
			return nil, err
		}
		if data == nil {
			return nil, errACMECertificateNotFound
		}
		if summary, sErr := h.GetACMECertificateSummary(ctx, in.ID); sErr == nil && summary != nil {
			r, _ := httpFrom(ctx)
			h.AuditACMEKeyDownload(r, summary.ID, summary.Domain)
		}
		return newBinaryFileOutput(filename, data), nil
	}
}
