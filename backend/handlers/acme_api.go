package handlers

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"certharbor/acme"
	"certharbor/models"

	appdb "certharbor/db"
)

// ACMECertificateSummary is one ACME certificate row, stripped of filesystem
// paths (CertPath/KeyPath are on Section 2's banned-property-name list,
// exactly as CertificateSummary already strips them for managed
// certificates). api/ builds its wire body from this instead of converting
// certharbor/models.ACMECertificate directly, which it is not allowed to
// import (Section 2's depguard rule).
type ACMECertificateSummary struct {
	ID        int
	Domain    string
	Email     string
	Provider  string
	Status    string
	LastError string
	AutoRenew bool
	IssuedAt  *time.Time
	ExpiresAt *time.Time
}

func summarizeACMECert(c *models.ACMECertificate) ACMECertificateSummary {
	return ACMECertificateSummary{
		ID: c.ID, Domain: c.Domain, Email: c.Email, Provider: c.Provider,
		Status: c.Status, LastError: c.LastError, AutoRenew: c.AutoRenew,
		IssuedAt: c.IssuedAt, ExpiresAt: c.ExpiresAt,
	}
}

// ListACMECertificateSummaries is the data half of GET /api/v1/acme/certificates,
// replacing GET /le (ACMEDashboard's appdb.GetACMECerts call, Section 6).
func (h *Handler) ListACMECertificateSummaries(ctx context.Context) ([]ACMECertificateSummary, error) {
	certs, err := appdb.GetACMECerts(ctx, h.db)
	if err != nil {
		return nil, err
	}
	out := make([]ACMECertificateSummary, 0, len(certs))
	for _, c := range certs {
		out = append(out, summarizeACMECert(c))
	}
	return out, nil
}

// GetACMECertificateSummary fetches one ACME certificate, returning (nil,
// nil) when id names none, the same convention FetchCertificateDetail
// already uses for managed certificates.
func (h *Handler) GetACMECertificateSummary(ctx context.Context, id int) (*ACMECertificateSummary, error) {
	c, err := appdb.GetACMECert(ctx, h.db, id)
	if err != nil || c == nil {
		return nil, err
	}
	s := summarizeACMECert(c)
	return &s, nil
}

// ACMEIssueOptions is the data GET /api/v1/acme/options composes, replacing
// GET /le/issue (Section 6). None of this is schema constraint (D3 rule 1):
// the provider list, the default email/provider carried over from settings
// and the allowed-domain-suffix policy are all response data a client
// reads, mirroring getCertificateOptions' own reasoning for the same kind
// of operation.
type ACMEIssueOptions struct {
	Providers             []ACMEProviderOption
	DefaultProvider       string
	DefaultEmail          string
	AllowedDomainSuffixes []string
	CloudflareConfigured  bool
	Route53Configured     bool
}

// ACMEProviderOption is one selectable challenge provider, matching the
// fixed three options templates/le_issue.html's <select> already offers.
type ACMEProviderOption struct {
	ID    string
	Label string
	Note  string
}

var acmeProviderOptions = []ACMEProviderOption{
	{ID: "http01", Label: "HTTP-01", Note: "Port 80 must be open and reachable from the internet."},
	{ID: "cloudflare", Label: "DNS-01 — Cloudflare", Note: "Creates a TXT record via the Cloudflare API."},
	{ID: "route53", Label: "DNS-01 — AWS Route53", Note: "Creates a TXT record via AWS Route53."},
}

// FetchACMEIssueOptions loads the current ACME settings to seed defaults,
// the same read ACMEIssueGet already performs (handlers/acme.go), without
// touching the settings PATCH surface this step deliberately does not own.
func (h *Handler) FetchACMEIssueOptions(ctx context.Context) (*ACMEIssueOptions, error) {
	settings, err := appdb.GetACMESettings(ctx, h.db)
	if err != nil {
		return nil, err
	}
	opts := &ACMEIssueOptions{
		Providers:             acmeProviderOptions,
		DefaultProvider:       "http01",
		AllowedDomainSuffixes: h.cfg.AllowedDomainSuffixes,
	}
	if settings != nil {
		opts.DefaultEmail = settings.Email
		if settings.Provider != "" {
			opts.DefaultProvider = settings.Provider
		}
		opts.CloudflareConfigured = settings.CFToken != "" || settings.CFZoneID != ""
		opts.Route53Configured = settings.R53KeyID != "" || settings.R53SecretKey != ""
	}
	return opts, nil
}

// IssueACMECertificate is ACMEIssuePost's data half: policy checks, the
// database record and the background issuance goroutine, with no form
// parsing, no flash message and no redirect (Section 6, replacing POST
// /le/issue).
func (h *Handler) IssueACMECertificate(ctx context.Context, domain, email, provider string, autoRenew bool) (*ACMECertificateSummary, error) {
	domain = trimStr(domain)
	email = trimStr(email)
	if domain == "" || email == "" {
		return nil, NewValidationError("domain and email are required")
	}
	// A different issuer, but the same question of authority over the name (V6).
	if err := checkDomainPolicy(domain, h.cfg.AllowedDomainSuffixes); err != nil {
		return nil, NewValidationError(err.Error())
	}
	if appdb.ACMECertExists(ctx, h.db, domain) {
		return nil, NewValidationError("a certificate for this domain already exists")
	}

	settings, err := appdb.GetACMESettings(ctx, h.db)
	if err != nil {
		return nil, err
	}

	id, err := appdb.CreateACMECert(ctx, h.db, domain, email, provider, autoRenew)
	if err != nil {
		return nil, fmt.Errorf("failed to create record: %w", err)
	}

	appdb.AddACMELog(ctx, h.db, domain, "issue",
		fmt.Sprintf("Certificate issuance started (directory: %s)", h.cfg.ACMEDirectoryURL))

	// Issue in the background, use Background so the goroutine outlives the request.
	bgCtx := context.Background()
	safeGo("acme-issue:"+domain, func() {
		result, issueErr := acme.IssueCert(&acme.ACMEConfig{
			Email:        email,
			Domain:       domain,
			Provider:     provider,
			CFToken:      settings.CFToken,
			CFZoneID:     settings.CFZoneID,
			R53KeyID:     settings.R53KeyID,
			R53Secret:    settings.R53SecretKey,
			R53Region:    settings.R53Region,
			DirectoryURL: h.cfg.ACMEDirectoryURL,
		})
		if issueErr != nil {
			_ = appdb.UpdateACMECertStatus(bgCtx, h.db, id, "error", issueErr.Error())
			appdb.AddACMELog(bgCtx, h.db, domain, "error", fmt.Sprintf("Error: %v", issueErr))
			return
		}
		_ = appdb.UpdateACMECertPaths(bgCtx, h.db, id, result.CertPath, result.KeyPath, result.IssuedAt, result.ExpiresAt)
		appdb.AddACMELog(bgCtx, h.db, domain, "issue", "Certificate issued successfully")
	})

	c, err := appdb.GetACMECert(ctx, h.db, id)
	if err != nil || c == nil {
		return nil, err
	}
	s := summarizeACMECert(c)
	return &s, nil
}

// RenewACMECertificate is ACMERenew's data half, replacing POST
// /le/{id}/renew. Returns (nil, nil) when id names no certificate.
func (h *Handler) RenewACMECertificate(ctx context.Context, id int) (*ACMECertificateSummary, error) {
	cert, err := appdb.GetACMECert(ctx, h.db, id)
	if err != nil || cert == nil {
		return nil, err
	}
	// A name that has fallen outside policy must not renew its way around it.
	if err := checkDomainPolicy(cert.Domain, h.cfg.AllowedDomainSuffixes); err != nil {
		return nil, NewValidationError(err.Error())
	}

	settings, err := appdb.GetACMESettings(ctx, h.db)
	if err != nil {
		return nil, err
	}
	_ = appdb.UpdateACMECertStatus(ctx, h.db, id, "pending", "")
	appdb.AddACMELog(ctx, h.db, cert.Domain, "renew", "Manual renewal started")

	bgCtx := context.Background()
	safeGo("acme-renew:"+cert.Domain, func() {
		result, issueErr := acme.IssueCert(&acme.ACMEConfig{
			Email:        cert.Email,
			Domain:       cert.Domain,
			Provider:     cert.Provider,
			CFToken:      settings.CFToken,
			CFZoneID:     settings.CFZoneID,
			DirectoryURL: h.cfg.ACMEDirectoryURL,
		})
		if issueErr != nil {
			_ = appdb.UpdateACMECertStatus(bgCtx, h.db, id, "error", issueErr.Error())
			appdb.AddACMELog(bgCtx, h.db, cert.Domain, "error", fmt.Sprintf("Renewal error: %v", issueErr))
			return
		}
		_ = appdb.UpdateACMECertPaths(bgCtx, h.db, id, result.CertPath, result.KeyPath, result.IssuedAt, result.ExpiresAt)
		appdb.AddACMELog(bgCtx, h.db, cert.Domain, "renew", "Certificate renewed successfully")
	})

	c, err := appdb.GetACMECert(ctx, h.db, id)
	if err != nil || c == nil {
		return nil, err
	}
	s := summarizeACMECert(c)
	return &s, nil
}

// DeleteACMECertificate is ACMEDelete's data half, replacing POST
// /le/{id}/delete. Deleting an id that does not exist is a no-op success,
// the same semantics the template route already has.
func (h *Handler) DeleteACMECertificate(ctx context.Context, id int) error {
	cert, err := appdb.GetACMECert(ctx, h.db, id)
	if err != nil {
		return err
	}
	if cert == nil {
		return nil
	}
	appdb.AddACMELog(ctx, h.db, cert.Domain, "delete", "Certificate removed from the system")
	return appdb.DeleteACMECert(ctx, h.db, cert.ID)
}

// UpdateACMECertificateAutoRenew is ACMEToggleAutoRenew's data half,
// replacing POST /le/{id}/autorenew, folded into PATCH per 5.8: one way to
// model a toggle. Returns (nil, nil) when id names no certificate.
func (h *Handler) UpdateACMECertificateAutoRenew(ctx context.Context, id int, autoRenew bool) (*ACMECertificateSummary, error) {
	cert, err := appdb.GetACMECert(ctx, h.db, id)
	if err != nil || cert == nil {
		return nil, err
	}
	if err := appdb.UpdateACMECertAutoRenew(ctx, h.db, id, autoRenew); err != nil {
		return nil, err
	}
	c, err := appdb.GetACMECert(ctx, h.db, id)
	if err != nil || c == nil {
		return nil, err
	}
	s := summarizeACMECert(c)
	return &s, nil
}

// ACMECertificateFile returns the filename and raw bytes of an ACME
// certificate's certificate file, for GET /api/v1/acme/certificates/{id}/certificate
// (replacing GET /le/download/cert/{id}). Unlike the retired handler
// (handlers/acme.go's former ACMEDownloadCert), the domain is passed
// through safeName at serve time rather than interpolated unquoted into
// the filename (plans/frontend-backend-split.md 5.7): a stored row is
// still untrusted input as far as a filename is concerned.
func (h *Handler) ACMECertificateFile(ctx context.Context, id int) (filename string, data []byte, err error) {
	c, err := appdb.GetACMECert(ctx, h.db, id)
	if err != nil {
		return "", nil, err
	}
	if c == nil || c.CertPath == "" {
		return "", nil, nil
	}
	//nolint:gosec // G304: CertPath is a DB-stored ACME certificate path, not user input
	data, err = os.ReadFile(c.CertPath)
	if err != nil {
		return "", nil, err
	}
	fn := "certificate.crt"
	if safe, nameErr := safeName(c.Domain); nameErr == nil {
		fn = safe + ".crt"
	}
	return fn, data, nil
}

// ACMEKeyFile is ACMECertificateFile's private-key equivalent, for GET
// /api/v1/acme/certificates/{id}/key (replacing GET /le/download/key/{id}).
// Logging the download is the caller's responsibility (AuditACMEKeyDownload
// below), the same split getCertificateKey already uses.
func (h *Handler) ACMEKeyFile(ctx context.Context, id int) (filename string, data []byte, err error) {
	c, err := appdb.GetACMECert(ctx, h.db, id)
	if err != nil {
		return "", nil, err
	}
	if c == nil || c.KeyPath == "" {
		return "", nil, nil
	}
	//nolint:gosec // G304: KeyPath is a DB-stored ACME certificate path, not user input
	data, err = os.ReadFile(c.KeyPath)
	if err != nil {
		return "", nil, err
	}
	fn := "private.key"
	if safe, nameErr := safeName(c.Domain); nameErr == nil {
		fn = safe + ".key"
	}
	return fn, data, nil
}

// AuditACMEKeyDownload is ACMEDownloadKey's h.auditSecurity call site,
// adapted so the api layer's getACMECertificateKey can log the same line a
// key download through the retired template route would. It takes the
// *http.Request directly, the same split getCertificateKey's AuditKeyDownload
// already uses, since the api layer (not this package) recovers it from the
// context the unwrap middleware stashed (5.5).
func (h *Handler) AuditACMEKeyDownload(r *http.Request, id int, domain string) {
	if r == nil {
		return
	}
	h.auditSecurity(r, fmt.Sprintf("le.key_download id=%d domain=%s", id, domain))
}

// ─── Settings ─────────────────────────────────────────────────────────────────

// ACMESettingsRow is the handler-layer view of models.ACMESettings: GET and
// PATCH /api/v1/acme/settings build their response from this, never from
// the models type directly (api/ never imports certharbor/models,
// certharbor/handlers.NotificationSettingsRow's comment). CFToken and
// R53SecretKey are deliberately absent: they are writeOnly on the way in
// (ACMESettingsPatch) and must never round-trip into any response, exactly
// as NotificationSettingsRow omits SMTPPassword (5.8).
type ACMESettingsRow struct {
	Email     string
	Provider  string
	CFZoneID  string
	R53KeyID  string
	R53Region string
}

// ACMESettingsPatch is PATCH /api/v1/acme/settings' merge-style body
// (5.8): a nil field means absent, which UpdateACMESettings leaves
// unchanged. CFToken and R53SecretKey are the ACME provider account
// material this phase's exit criterion ("Settings PATCH preserves account
// material") is written against: a PATCH omitting either must leave the
// stored credential unchanged, never zeroed.
type ACMESettingsPatch struct {
	Email, Provider, CFZoneID, R53KeyID, R53Region *string
	CFToken, R53SecretKey                          *string
}

func toACMESettingsRow(s *models.ACMESettings) *ACMESettingsRow {
	return &ACMESettingsRow{
		Email: s.Email, Provider: s.Provider,
		CFZoneID: s.CFZoneID, R53KeyID: s.R53KeyID, R53Region: s.R53Region,
	}
}

// applyACMESettingsPatch merges patch onto current and returns a new value
// (5.8: absent means unchanged). Pure and database-free, mirroring
// applyNotificationPatch, so the "omitting a secret leaves it unchanged"
// claim is unit-testable without a database.
func applyACMESettingsPatch(current *models.ACMESettings, patch *ACMESettingsPatch) *models.ACMESettings {
	merged := *current
	if patch.Email != nil {
		merged.Email = *patch.Email
	}
	if patch.Provider != nil {
		merged.Provider = *patch.Provider
	}
	if patch.CFZoneID != nil {
		merged.CFZoneID = *patch.CFZoneID
	}
	if patch.R53KeyID != nil {
		merged.R53KeyID = *patch.R53KeyID
	}
	if patch.R53Region != nil {
		merged.R53Region = *patch.R53Region
	}
	// Omitted (nil): merged.CFToken / merged.R53SecretKey keep current's
	// value, carried over by the `merged := *current` copy above. No
	// branch here means no code path can zero the account material.
	if patch.CFToken != nil {
		merged.CFToken = *patch.CFToken
	}
	if patch.R53SecretKey != nil {
		merged.R53SecretKey = *patch.R53SecretKey
	}
	return &merged
}

// acmeValidProviders mirrors the provider vocabulary acme.ACMEConfig.Provider
// and the le_issue.html form already use.
var acmeValidProviders = map[string]bool{
	"http01": true, "cloudflare": true, "route53": true, "manual": true,
}

// ACMESettingsAPI implements GET /api/v1/acme/settings, replacing GET
// /le/settings (Section 6, Phase 7).
func (h *Handler) ACMESettingsAPI(ctx context.Context) (*ACMESettingsRow, error) {
	s, err := appdb.GetACMESettings(ctx, h.db)
	if err != nil {
		return nil, err
	}
	return toACMESettingsRow(s), nil
}

// UpdateACMESettingsAPI implements PATCH /api/v1/acme/settings, replacing
// POST /le/settings (Section 6, 5.8): PUT would clear CFToken/R53SecretKey
// on the first save of an unrelated field, exactly the defect a
// merge-style PATCH against the loaded current row avoids.
func (h *Handler) UpdateACMESettingsAPI(ctx context.Context, patch *ACMESettingsPatch) (*ACMESettingsRow, error) {
	current, err := appdb.GetACMESettings(ctx, h.db)
	if err != nil {
		return nil, err
	}
	merged := applyACMESettingsPatch(current, patch)

	if merged.Provider != "" && !acmeValidProviders[merged.Provider] {
		return nil, NewValidationError("provider must be one of: http01, cloudflare, route53, manual")
	}
	if merged.Provider == "cloudflare" && merged.CFToken == "" {
		return nil, NewValidationError("cloudflare provider requires an API token")
	}
	if merged.Provider == "route53" && (merged.R53KeyID == "" || merged.R53SecretKey == "") {
		return nil, NewValidationError("route53 provider requires an access key id and secret key")
	}

	if err := appdb.SaveACMESettings(ctx, h.db, merged); err != nil {
		return nil, err
	}
	return toACMESettingsRow(merged), nil
}

// AuditACMESettingsSave is UpdateACMESettingsAPI's h.auditSecurity call
// site, adapted for the api layer exactly as AuditNotificationsSave is
// (handlers/notifications.go's comment): the huma session middleware never
// populates the gorilla-session "username" key h.auditSecurity reads.
func (h *Handler) AuditACMESettingsSave(username string) {
	if username == "" || h.db == nil {
		return
	}
	_ = appdb.LogAuth(h.db, username, "", true, auditPrefix+"acme.settings.save (api)")
}

// ─── Events ───────────────────────────────────────────────────────────────────

// ACMEEvent is the handler-layer view of models.ACMELog: GET
// /api/v1/acme/events builds its response from this.
type ACMEEvent struct {
	ID        int
	Domain    string
	Action    string
	Message   string
	CreatedAt string
}

// ListACMEEvents implements GET /api/v1/acme/events's data half, replacing
// GET /le/logs (Section 6, Phase 7). Pagination follows 5.6's envelope and
// the same (created_at DESC, id DESC) total order db/authlog.go and
// db/history.go already carry, proven against this table by
// db.ListACMELogsPaged's own tied-timestamp test.
func (h *Handler) ListACMEEvents(domain string, page, pageSize int) ([]ACMEEvent, int, error) {
	logs, total, err := appdb.ListACMELogsPaged(context.Background(), h.db, domain, page, pageSize)
	if err != nil {
		return nil, 0, err
	}
	out := make([]ACMEEvent, 0, len(logs))
	for _, l := range logs {
		out = append(out, ACMEEvent{
			ID: l.ID, Domain: l.Domain, Action: l.Action, Message: l.Message,
			CreatedAt: l.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
		})
	}
	return out, total, nil
}
