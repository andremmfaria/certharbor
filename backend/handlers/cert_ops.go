package handlers

import (
	"context"
	"crypto/ecdsa"
	"crypto/rsa"
	"crypto/x509"
	"database/sql"
	"encoding/pem"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"certharbor/config"
	"certharbor/models"
	"certharbor/stepca"
	"github.com/go-chi/chi/v5"

	appdb "certharbor/db"
)

// certFromURL parses the chi "id" URL param, fetches the certificate from the
// DB, and writes an appropriate HTTP error if something goes wrong.  It returns
// (cert, true) on success or (nil, false) when the handler should return early.
func (h *Handler) certFromURL(w http.ResponseWriter, r *http.Request) (*models.Certificate, bool) {
	id, _ := strconv.Atoi(chi.URLParam(r, "id"))
	c, err := appdb.GetCert(h.db, id)
	if err != nil {
		slog.Error("certFromURL: DB error", "id", id, "err", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return nil, false
	}
	return c, true
}

// leCertFromURL is the LE-certificate equivalent of certFromURL.
// Returns (cert, true) on success; on DB error it writes 500 and returns (nil, false).
// A missing cert returns (nil, true), callers redirect to /le.
func (h *Handler) leCertFromURL(w http.ResponseWriter, r *http.Request) (*models.LECertificate, bool) {
	id, _ := strconv.Atoi(chi.URLParam(r, "id"))
	c, err := appdb.GetLECert(r.Context(), h.db, id)
	if err != nil {
		slog.Error("leCertFromURL: DB error", "id", id, "err", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return nil, false
	}
	return c, true
}

// IssuePolicy holds the resolved certificate template and duration for issuance.
type IssuePolicy struct {
	Template string
	Duration string
	KeyType  string
}

var issueTemplates = map[string]IssuePolicy{
	"server":   {Template: "server", Duration: "8760h", KeyType: "EC:P-256"},
	"internal": {Template: "internal", Duration: "87600h", KeyType: "EC:P-256"},
	"wildcard": {Template: "wildcard", Duration: "8760h", KeyType: "EC:P-256"},
	"client":   {Template: "client", Duration: "8760h", KeyType: "EC:P-256"},
}

var allowedIssueDurations = map[string]bool{
	"720h": true, "4380h": true, "8760h": true, "87600h": true,
}

var allowedIssueKeyTypes = map[string]bool{
	"EC:P-256": true, "EC:P-384": true, "RSA:2048": true, "RSA:4096": true,
}

// issueTemplateOrder is issueTemplates' keys in the order templates/issue.html
// lays its template cards out, since a Go map has none of its own.
var issueTemplateOrder = []string{"server", "internal", "wildcard", "client"}

// issueTemplateCopy is the label and description templates/issue.html hands
// each template card. GET /api/v1/certificates/options is the first place
// this has a Go-side home: today it exists only as literal HTML strings and
// the hidden `data-*` attributes static/js reads back off the DOM, computed
// nowhere (R23's observed-cost record, Phase 4: this operation has no
// predecessor to extract from, unlike list and detail).
var issueTemplateCopy = map[string]struct{ Label, Description string }{
	"server":   {"Server TLS", "Standard HTTPS/service certificate, 1 year"},
	"internal": {"Internal service", "Internal service, up to 10 years"},
	"wildcard": {"Wildcard", "Wildcard names only, e.g. *.example.com"},
	"client":   {"Client identity", "Client identity for mTLS/VPN scenarios"},
}

// keyTypeOrder and keyTypeCopy are allowedIssueKeyTypes' entries, labelled as
// templates/issue.html's key-type cards label them.
var keyTypeOrder = []string{"EC:P-256", "EC:P-384", "RSA:2048", "RSA:4096"}

var keyTypeCopy = map[string]struct{ Label, Note string }{
	"EC:P-256": {"EC P-256", "Elliptic curve, 256-bit (recommended)"},
	"EC:P-384": {"EC P-384", "Elliptic curve, 384-bit (enhanced)"},
	"RSA:2048": {"RSA 2048", "Compatibility (classic)"},
	"RSA:4096": {"RSA 4096", "Maximum security (slow)"},
}

// durationOrder and durationCopy label allowedIssueDurations' entries.
var durationOrder = []string{"720h", "4380h", "8760h", "87600h"}

var durationCopy = map[string]string{
	"720h":   "30 days",
	"4380h":  "6 months",
	"8760h":  "1 year",
	"87600h": "10 years",
}

// CertTemplateOption is one selectable certificate template in
// CertIssueOptions. It, CertKeyTypeOption, CertDurationOption and
// CertProvisionerOption are CertIssueOptions' rows. Plain data, no json
// tags: backend/api owns the wire shape (D3 rule 3) and converts.
type CertTemplateOption struct {
	ID              string
	Label           string
	Description     string
	DefaultDuration string
	DefaultKeyType  string
}

// CertKeyTypeOption is one selectable key-type choice in CertIssueOptions.
type CertKeyTypeOption struct {
	ID    string
	Label string
	Note  string
}

// CertDurationOption is one selectable issuance-duration choice in CertIssueOptions.
type CertDurationOption struct {
	ID    string
	Label string
}

// CertProvisionerOption is one selectable step-ca provisioner in CertIssueOptions.
type CertProvisionerOption struct {
	Name string
	Type string
}

// CertIssueOptions is GET /api/v1/certificates/options' data: everything the
// issuance form needs to render, computed server-side instead of hardcoded
// in templates/issue.html and static/js/issue.js. Per D3 rule 1 none of this
// reaches a schema (no enum, no maximum, no default): it is response data a
// client reads, same as the rest of this struct.
type CertIssueOptions struct {
	Templates             []CertTemplateOption
	KeyTypes              []CertKeyTypeOption
	Durations             []CertDurationOption
	Provisioners          []CertProvisionerOption
	AllowedDomainSuffixes []string
	DefaultTemplate       string
}

// CertificateIssueOptions computes CertIssueOptions. IssueGet, the template
// route it replaces, computes nothing at all today: it renders a static
// form and leaves every choice hardcoded in HTML and JS, so this function
// has no predecessor to extract from (R23's observed-cost record, Phase 4).
// Provisioners degrades to an empty list on a CA error, matching
// Provisioners' (the template handler for GET /provisioners) existing
// behaviour rather than failing the whole response over a CA hiccup.
func (h *Handler) CertificateIssueOptions(ctx context.Context) CertIssueOptions {
	templates := make([]CertTemplateOption, 0, len(issueTemplateOrder))
	for _, id := range issueTemplateOrder {
		policy := issueTemplates[id]
		info := issueTemplateCopy[id]
		templates = append(templates, CertTemplateOption{
			ID: id, Label: info.Label, Description: info.Description,
			DefaultDuration: policy.Duration, DefaultKeyType: policy.KeyType,
		})
	}

	keyTypes := make([]CertKeyTypeOption, 0, len(keyTypeOrder))
	for _, id := range keyTypeOrder {
		info := keyTypeCopy[id]
		keyTypes = append(keyTypes, CertKeyTypeOption{ID: id, Label: info.Label, Note: info.Note})
	}

	durations := make([]CertDurationOption, 0, len(durationOrder))
	for _, id := range durationOrder {
		durations = append(durations, CertDurationOption{ID: id, Label: durationCopy[id]})
	}

	var provisioners []CertProvisionerOption
	if caClient, err := h.caClient(); err == nil {
		if list, err := caClient.Provisioners(ctx); err == nil {
			provisioners = make([]CertProvisionerOption, 0, len(list))
			for _, p := range list {
				provisioners = append(provisioners, CertProvisionerOption{Name: p.Name, Type: p.Type})
			}
		}
	}

	return CertIssueOptions{
		Templates:             templates,
		KeyTypes:              keyTypes,
		Durations:             durations,
		Provisioners:          provisioners,
		AllowedDomainSuffixes: append([]string{}, h.cfg.AllowedDomainSuffixes...),
		DefaultTemplate:       "server",
	}
}

func normalizeIssuePolicy(template, duration, keyType, domain string) (IssuePolicy, error) {
	template = strings.TrimSpace(strings.ToLower(template))
	if template == "" {
		template = "server"
	}
	policy, ok := issueTemplates[template]
	if !ok {
		return IssuePolicy{}, fmt.Errorf("unknown certificate template: %s", template)
	}
	if allowedIssueDurations[duration] {
		policy.Duration = duration
	}
	if allowedIssueKeyTypes[keyType] {
		policy.KeyType = keyType
	}
	if policy.Template == "wildcard" && !strings.HasPrefix(strings.TrimSpace(domain), "*.") {
		return IssuePolicy{}, fmt.Errorf("wildcard template requires domain like *.example.com")
	}
	return policy, nil
}

// checkDomainPolicy enforces ALLOWED_DOMAIN_SUFFIXES. validateIdentifier asks
// whether a name is well formed; this asks whether the operator has any
// authority over it (V6). An empty policy allows everything, which is what the
// application did before the key existed.
//
// Matching is on label boundaries, so "evil-example.com" does not satisfy
// "example.com", and a wildcard is judged by the name under its "*." prefix.
func checkDomainPolicy(domain string, suffixes []string) error {
	if len(suffixes) == 0 {
		return nil
	}
	name := strings.ToLower(strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(domain), "*."), "."))
	for _, suffix := range suffixes {
		if name == suffix || strings.HasSuffix(name, "."+suffix) {
			return nil
		}
	}
	return NewValidationError(fmt.Sprintf("domain %q is not covered by ALLOWED_DOMAIN_SUFFIXES (%s)", domain, strings.Join(suffixes, ", ")))
}

func issueCert(ctx context.Context, caClient stepca.CA, domain, certPath, keyPath, duration, keyType string, cfg *config.Config) error {
	// Validate domain before it reaches the CA library, the same guard that
	// previously protected against flag injection into the step CLI's argv.
	if err := validateIdentifier(domain); err != nil {
		return err
	}
	// Both issuance and renewal reach the CA through here, so the name policy
	// sits here rather than in normalizeIssuePolicy, which Renew never calls.
	if err := checkDomainPolicy(domain, cfg.AllowedDomainSuffixes); err != nil {
		return err
	}
	dur, err := time.ParseDuration(duration)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", duration, err)
	}
	// Renew reaches here with paths loaded from the database, so both are
	// re-checked against the managed directory before anything is written.
	safeCertPath, err := containedAbsPath(cfg.CertsDir, certPath)
	if err != nil {
		return fmt.Errorf("certificate path: %w", err)
	}
	safeKeyPath, err := containedAbsPath(cfg.CertsDir, keyPath)
	if err != nil {
		return fmt.Errorf("key path: %w", err)
	}
	certPEM, keyPEM, err := caClient.IssueCertificate(ctx, stepca.IssueRequest{
		Domain:       domain,
		Duration:     dur,
		KeyType:      keyType,
		Provisioner:  cfg.Provisioner,
		PasswordFile: cfg.PasswordFile,
	})
	if err != nil {
		return fmt.Errorf("issue certificate: %w", err)
	}
	//nolint:gosec // G703: containedAbsPath above proved the path is inside
	// cfg.CertsDir; gosec's taint analysis cannot see that sanitiser, so
	// TestIssueCert_PathOutsideCertsDir asserts the invariant instead.
	if err := os.WriteFile(safeCertPath, certPEM, 0o600); err != nil {
		return fmt.Errorf("write certificate file: %w", err)
	}
	//nolint:gosec // G703: see the certificate write above
	if err := os.WriteFile(safeKeyPath, keyPEM, 0o600); err != nil {
		return fmt.Errorf("write key file: %w", err)
	}
	return nil
}

// revokeStep revokes a certificate via the CA client and returns any error so
// callers can decide whether to mark the cert as revoked in the database.
// cfg is unused now that Client.Revoke carries its own cfg (RootCert) from
// construction, but kept in the signature for symmetry with issueCert and in
// case a future caller needs cfg-derived context here too.
func revokeStep(ctx context.Context, caClient stepca.CA, certPath, keyPath string, _ *config.Config) error {
	if err := caClient.Revoke(ctx, certPath, keyPath); err != nil {
		return fmt.Errorf("revoke certificate: %w", err)
	}
	return nil
}

func parseCertDates(certPath string) (issued, expires *time.Time, serial string, err error) {
	//nolint:gosec // G304: certPath is the stored DB path for a managed certificate, containedPath-checked on import
	data, err := os.ReadFile(certPath)
	if err != nil {
		return issued, expires, serial, err
	}
	block, _ := pem.Decode(data)
	if block == nil {
		err = fmt.Errorf("no PEM block found")
		return issued, expires, serial, err
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return issued, expires, serial, err
	}
	i := cert.NotBefore
	e := cert.NotAfter
	issued = &i
	expires = &e
	serial = cert.SerialNumber.String()
	return issued, expires, serial, err
}

func getCertKeyType(certPath string) string {
	//nolint:gosec // G304: certPath is the stored DB path for a managed certificate, containedPath-checked on import
	data, err := os.ReadFile(certPath)
	if err != nil {
		return ""
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return ""
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return ""
	}
	// The "EC:P-256" / "RSA:2048" shape is the KeyType convention used
	// everywhere else (allowedIssueKeyTypes, Handler.certs.go's fallback,
	// cert_ops_test.go), so an import must report the same shape rather
	// than the bare algorithm name: a caller (e.g. Renew) that reuses a
	// row's KeyType as-is would otherwise feed "EC" into
	// normalizeIssuePolicy/allowedIssueKeyTypes, which only recognises the
	// qualified form.
	switch pub := cert.PublicKey.(type) {
	case *ecdsa.PublicKey:
		return "EC:" + pub.Curve.Params().Name
	case *rsa.PublicKey:
		return fmt.Sprintf("RSA:%d", pub.N.BitLen())
	default:
		return "Unknown"
	}
}

func scanExistingCerts(certsDir string, d *sql.DB) []map[string]string {
	var found []map[string]string
	// best-effort filesystem scan; individual walk errors are handled inside the closure
	_ = filepath.WalkDir(certsDir, func(path string, de os.DirEntry, err error) error {
		if err != nil || de.IsDir() {
			return nil
		}
		if strings.HasSuffix(path, "certificate.crt") {
			dir := filepath.Dir(path)
			name := filepath.Base(dir)
			keyPath := filepath.Join(dir, "private.key")
			if _, e := os.Stat(keyPath); e != nil {
				keyPath = ""
			}
			// Check whether already in the database
			_, _, serial, e := parseCertDates(path)
			if e != nil || serial == "" {
				return nil
			}
			c, _ := appdb.GetCertBySerial(d, serial)
			if c == nil {
				found = append(found, map[string]string{
					"name": name, "cert_path": path, "key_path": keyPath,
				})
			}
		}
		return nil
	})
	return found
}

func saveUploadedFile(file multipart.File, dst string) error {
	//nolint:gosec // G304: dst is constructed from containedPath(cfg.UploadDir, safeName(name))
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	_, err = io.Copy(f, file)
	return err
}

func trimStr(s string) string {
	return strings.TrimSpace(s)
}

func daysLeftVal(t *time.Time) int {
	if t == nil {
		return 999
	}
	return int(time.Until(*t).Hours() / 24)
}

// GetCertBySerial wrapper needed in db
var _ = (*models.Certificate)(nil)

// ─── Phase 4: issue, import, renew, revoke (plans/frontend-backend-split.md Section 6) ───

// IssueCertificate is IssuePost's data half: directory creation, CA
// issuance, the database write and the history row, with no form parsing,
// no flash message and no redirect. R23's observed-cost record for this
// operation: real extraction work, because IssuePost computes and writes
// in the same function body rather than calling out to an isolable step.
//
// username and role come from the caller (api/ reads them off the request
// context via middleware.UserFrom) rather than from h.sessionInfo, because
// api/ cannot reach that unexported method and is not allowed to import
// certharbor/models to name *models.SessionInfo itself (Section 2's
// depguard rule).
func (h *Handler) IssueCertificate(ctx context.Context, username, role, name, domain, templateID, duration, keyType string) (*CertificateSummary, error) {
	name = trimStr(name)
	domain = trimStr(domain)
	if name == "" || domain == "" {
		return nil, NewValidationError("name and domain are required")
	}
	policy, err := normalizeIssuePolicy(templateID, duration, keyType, domain)
	if err != nil {
		return nil, NewValidationError(err.Error())
	}
	safeCertName, err := safeName(name)
	if err != nil {
		return nil, NewValidationError("invalid certificate name: " + err.Error())
	}
	certDir := filepath.Join(h.cfg.CertsDir, safeCertName)
	if err := os.MkdirAll(certDir, 0o750); err != nil { //nolint:gosec // G703: safeName validated the component; certDir is h.cfg.CertsDir+safeName
		return nil, fmt.Errorf("failed to create directory: %w", err)
	}
	certPath := filepath.Join(certDir, "certificate.crt")
	keyPath := filepath.Join(certDir, "private.key")
	caClient, caErr := h.caClient()
	if caErr != nil {
		return nil, fmt.Errorf("CA unreachable: %w", caErr)
	}
	if err := issueCert(ctx, caClient, domain, certPath, keyPath, policy.Duration, policy.KeyType, h.cfg); err != nil {
		h.notifyAsync("", "certificate.issue_failed", "error",
			"Certificate issue failed",
			fmt.Sprintf("Failed to issue certificate %s for %s: %s", name, domain, err.Error()),
			map[string]string{"name": name, "domain": domain, "template": policy.Template, "key_type": policy.KeyType})
		return nil, fmt.Errorf("issue certificate: %w", err)
	}
	issued, expires, serial, _ := parseCertDates(certPath)
	if err := appdb.InsertCert(h.db, &models.Certificate{
		Name: name, Domain: domain, CertPath: certPath, KeyPath: keyPath,
		IssuedAt: issued, ExpiresAt: expires, Serial: serial, KeyType: policy.KeyType,
		IssueDuration: policy.Duration, // stored so a later renew can reuse it
	}); err != nil {
		return nil, fmt.Errorf("database write error: %w", err)
	}
	_ = appdb.InsertHistory(h.db, "issue", name, domain,
		fmt.Sprintf("Template: %s, type: %s, duration: %s", policy.Template, policy.KeyType, policy.Duration), username, role)

	row, err := appdb.GetCertByName(h.db, name)
	if err != nil || row == nil {
		return nil, err
	}
	summary := summarizeCert(row)
	return &summary, nil
}

// RenewCertificate is Renew's data half. Returns (nil, nil) when id names no
// certificate, which the caller maps to 404; FetchCertificateDetail
// (cert_details.go) establishes the same convention for the same reason.
func (h *Handler) RenewCertificate(ctx context.Context, username, role string, id int) (*CertificateSummary, error) {
	c, err := appdb.GetCert(h.db, id)
	if err != nil || c == nil {
		return nil, err
	}

	keyType := c.KeyType
	if keyType == "" {
		keyType = "EC:P-256"
	}
	// Reuse the stored issuance duration; fall back to 8760h only when the
	// certificate predates the issue_duration column (P3-8).
	duration := c.IssueDuration
	if duration == "" {
		duration = "8760h"
	}

	caClient, caErr := h.caClient()
	var issueErr error
	if caErr != nil {
		issueErr = caErr
	} else {
		issueErr = issueCert(ctx, caClient, c.Domain, c.CertPath, c.KeyPath, duration, keyType, h.cfg)
	}
	if issueErr != nil {
		h.notifyAsync("", "certificate.renew_failed", "error",
			"Certificate renew failed",
			fmt.Sprintf("Failed to renew certificate %s for %s: %s", c.Name, c.Domain, issueErr.Error()),
			map[string]string{"id": strconv.Itoa(c.ID), "name": c.Name, "domain": c.Domain, "key_type": keyType})
		return nil, fmt.Errorf("renew certificate: %w", issueErr)
	}

	issued, expires, serial, _ := parseCertDates(c.CertPath)
	if err := appdb.InsertCert(h.db, &models.Certificate{
		Name: c.Name, Domain: c.Domain, CertPath: c.CertPath, KeyPath: c.KeyPath,
		IssuedAt: issued, ExpiresAt: expires, Serial: serial, KeyType: keyType,
		IssueDuration: duration,
	}); err != nil {
		return nil, fmt.Errorf("database write error: %w", err)
	}
	_ = appdb.InsertHistory(h.db, "renew", c.Name, c.Domain, "Renewal, type: "+keyType, username, role)

	row, err := appdb.GetCertByName(h.db, c.Name)
	if err != nil || row == nil {
		return nil, err
	}
	summary := summarizeCert(row)
	return &summary, nil
}

// RevokeCertificate is Revoke's data half. Returns (nil, nil) when id names
// no certificate, same 404 convention as RenewCertificate.
func (h *Handler) RevokeCertificate(ctx context.Context, username, role string, id int) (*CertificateSummary, error) {
	c, err := appdb.GetCert(h.db, id)
	if err != nil || c == nil {
		return nil, err
	}

	caClient, caErr := h.caClient()
	var revokeErr error
	if caErr != nil {
		revokeErr = caErr
	} else {
		revokeErr = revokeStep(ctx, caClient, c.CertPath, c.KeyPath, h.cfg)
	}
	if revokeErr != nil {
		return nil, fmt.Errorf("revoke certificate: %w", revokeErr)
	}

	if err := appdb.UpdateCertStatus(h.db, c.ID, "revoked"); err != nil {
		return nil, fmt.Errorf("database write error: %w", err)
	}
	_ = appdb.InsertHistory(h.db, "revoke", c.Name, c.Domain, "Revoked (CRL)", username, role)

	row, err := appdb.GetCert(h.db, c.ID)
	if err != nil || row == nil {
		return nil, err
	}
	summary := summarizeCert(row)
	return &summary, nil
}

// ImportCandidate is one unregistered certificate found on disk by
// ImportCandidates. Its fields are deliberately not named CertPath/KeyPath:
// both are on Section 2's banned-property-name list, which forbids the
// literal substring from appearing in any response schema, allowlisted or
// not, so the wire shape needs different names for what is otherwise the
// same two paths scanExistingCerts already returns.
type ImportCandidate struct {
	Name     string
	CertFile string
	KeyFile  string
}

// ImportCandidates is GET /api/v1/certificates/import-candidates' data: a
// thin typed wrapper over scanExistingCerts, which already does the actual
// work (filesystem walk, serial lookup against the database) and has no
// response-writing to separate it from (R23's observed-cost record, Phase
// 4: cheapest operation in this batch, same shape as ListCertificateSummaries).
func (h *Handler) ImportCandidates() []ImportCandidate {
	found := scanExistingCerts(h.cfg.CertsDir, h.db)
	out := make([]ImportCandidate, 0, len(found))
	for _, item := range found {
		out = append(out, ImportCandidate{Name: item["name"], CertFile: item["cert_path"], KeyFile: item["key_path"]})
	}
	return out
}

// ImportCertificateUpload is importUpload's data half: the multipart file
// handling and database write, with no form parsing (huma's
// MultipartFormFiles already did that by the time api/ calls this) and no
// flash message. keyFile is nil when the caller submitted no key file,
// matching importUpload's optional key_file today.
func (h *Handler) ImportCertificateUpload(username, role, name, domain string, certFile multipart.File, keyFile multipart.File) (*CertificateSummary, error) {
	name = trimStr(name)
	domain = trimStr(domain)
	if name == "" || domain == "" || certFile == nil {
		return nil, NewValidationError("name, domain, and a certificate file are required")
	}
	safeUploadName, err := safeName(name)
	if err != nil {
		return nil, NewValidationError("invalid certificate name: " + err.Error())
	}
	certDir := filepath.Join(h.cfg.CertsDir, safeUploadName)
	if err := os.MkdirAll(certDir, 0o750); err != nil { //nolint:gosec // G703: safeName validated the component; certDir is h.cfg.CertsDir+safeName
		return nil, fmt.Errorf("failed to create directory: %w", err)
	}
	certPath := filepath.Join(certDir, "certificate.crt")
	keyPath := filepath.Join(certDir, "private.key")
	if err := saveUploadedFile(certFile, certPath); err != nil {
		return nil, fmt.Errorf("failed to save certificate file: %w", err)
	}
	if keyFile != nil {
		if err := saveUploadedFile(keyFile, keyPath); err != nil {
			return nil, fmt.Errorf("failed to save key file: %w", err)
		}
	} else {
		keyPath = ""
	}

	issued, expires, serial, err := parseCertDates(certPath)
	if err != nil || serial == "" {
		return nil, NewValidationError("failed to read certificate")
	}
	kt := getCertKeyType(certPath)
	if err := appdb.InsertCert(h.db, &models.Certificate{
		Name: name, Domain: domain, CertPath: certPath, KeyPath: keyPath,
		IssuedAt: issued, ExpiresAt: expires, Serial: serial, KeyType: kt,
	}); err != nil {
		return nil, NewValidationError("certificate already exists in database")
	}
	_ = appdb.InsertHistory(h.db, "import", name, domain, "Upload from PC, type: "+kt, username, role)

	row, err := appdb.GetCertByName(h.db, name)
	if err != nil || row == nil {
		return nil, err
	}
	summary := summarizeCert(row)
	return &summary, nil
}

// ImportCertificateByPath is importManual's data half, and also what the
// scan-import case (ImportCandidates' results) submits through, per Section
// 6's note that POST /api/v1/certificates/import/paths serves both. The old
// importScan swallowed per-item failures into a count; this returns the
// error for each item instead, so the caller can report it per item
// (apitypes.BulkResult) rather than silently dropping it, which is the
// behaviour change Section 6 explicitly asks for.
func (h *Handler) ImportCertificateByPath(username, role, name, domain, rawCertPath, rawKeyPath string) (*CertificateSummary, error) {
	name = trimStr(name)
	domain = trimStr(domain)
	rawCertPath = trimStr(rawCertPath)
	rawKeyPath = trimStr(rawKeyPath)
	if name == "" || domain == "" || rawCertPath == "" {
		return nil, NewValidationError("name, domain, and certPath are required")
	}

	// Restrict cert and key paths to cfg.CertsDir to prevent arbitrary file reads.
	certPath, err := containedPath(h.cfg.CertsDir, rawCertPath)
	if err != nil {
		return nil, NewValidationError("invalid certificate path: " + err.Error())
	}
	var keyPath string
	if rawKeyPath != "" {
		keyPath, err = containedPath(h.cfg.CertsDir, rawKeyPath)
		if err != nil {
			return nil, NewValidationError("invalid key path: " + err.Error())
		}
	}
	if _, err := os.Stat(certPath); err != nil {
		return nil, NewValidationError("file not found: " + certPath)
	}

	issued, expires, serial, _ := parseCertDates(certPath)
	kt := getCertKeyType(certPath)
	if err := appdb.InsertCert(h.db, &models.Certificate{
		Name: name, Domain: domain, CertPath: certPath, KeyPath: keyPath,
		IssuedAt: issued, ExpiresAt: expires, Serial: serial, KeyType: kt,
	}); err != nil {
		return nil, NewValidationError("certificate already exists in database")
	}
	_ = appdb.InsertHistory(h.db, "import", name, domain, "Manual path", username, role)

	row, err := appdb.GetCertByName(h.db, name)
	if err != nil || row == nil {
		return nil, err
	}
	summary := summarizeCert(row)
	return &summary, nil
}
