package handlers

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"certharbor/models"

	appdb "certharbor/db"
)

// ─── helper: formats a duration as a human-readable uptime string ────────────
func fmtUptime(d time.Duration) string {
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	mins := int(d.Minutes()) % 60
	if days > 0 {
		return fmt.Sprintf("%dd %dh %dm", days, hours, mins)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, mins)
	}
	return fmt.Sprintf("%dm", mins)
}

// CertificateSummary is one certificate row, stripped of filesystem paths
// (CertPath/KeyPath are on Section 2's banned-property-name list, so no DTO
// built from this may carry them). Phase 4's GET /api/v1/certificates and
// GET /api/v1/certificates/{id} both build their body from this instead of
// backend/api converting certharbor/models.Certificate directly, which
// api/ is not allowed to import (Section 2's depguard rule).
type CertificateSummary struct {
	ID        int
	Name      string
	Domain    string
	Status    string
	KeyType   string
	Serial    string
	IssuedAt  *time.Time
	ExpiresAt *time.Time
}

func summarizeCert(c *models.Certificate) CertificateSummary {
	return CertificateSummary{
		ID: c.ID, Name: c.Name, Domain: c.Domain, Status: c.Status,
		KeyType: c.KeyType, Serial: c.Serial, IssuedAt: c.IssuedAt, ExpiresAt: c.ExpiresAt,
	}
}

// ListCertificateSummaries is the data half of GET /api/v1/certificates: it
// reuses the same query the template route's Certificates already isolated
// from its own rendering, so this extraction cost nothing beyond the type
// conversion (R23's observed-cost record, Phase 4).
//
// statusFilter and q are applied the same way appdb.GetCerts and the
// template page do today: statusFilter at the SQL layer, q as an in-process
// case-insensitive substring match against name and domain. Moving q to SQL
// is Phase 5's list-envelope work, not this one.
func (h *Handler) ListCertificateSummaries(statusFilter, q string) ([]CertificateSummary, error) {
	certs, err := appdb.GetCerts(h.db, statusFilter)
	if err != nil {
		return nil, err
	}
	q = strings.ToLower(strings.TrimSpace(q))
	out := make([]CertificateSummary, 0, len(certs))
	for _, c := range certs {
		if q != "" && !strings.Contains(strings.ToLower(c.Name), q) && !strings.Contains(strings.ToLower(c.Domain), q) {
			continue
		}
		out = append(out, summarizeCert(c))
	}
	return out, nil
}

// CertExpiryStatus retires templateFuncs.badgeClass's threshold logic into
// one server-side computation (Section 6's note on the certificate DTOs):
// expired when already past NotAfter, warning inside the expiring-soon
// window, ok otherwise. badgeClass itself now calls this rather than
// carrying a second copy of the same three thresholds.
func CertExpiryStatus(t *time.Time) string {
	if t == nil {
		return "ok"
	}
	d := daysLeftVal(t)
	switch {
	case d <= 0:
		return "expired"
	case d <= ExpiringSoonDays:
		return "warning"
	default:
		return "ok"
	}
}

func (h *Handler) intermediateCertPath() string {
	return filepath.Join(filepath.Dir(h.cfg.RootCert), "intermediate_ca.crt")
}

// ExpiringSoonDays is the threshold APIStatus (and its huma port, GET
// /api/v1/status) uses to decide whether a certificate is "expiring soon".
const ExpiringSoonDays = 30

// StatusExpiring summarizes one certificate nearing expiry.
type StatusExpiring struct {
	Name   string `json:"name"`
	Domain string `json:"domain"`
	Days   int    `json:"days"`
}

// StatusSummary computes the active-certificate count and expiring-soon list
// that APIStatus renders as JSON. Exported so backend/api's GET /api/v1/status
// operation reuses this instead of duplicating the query and threshold logic.
func (h *Handler) StatusSummary() (total int, expiringSoon []StatusExpiring) {
	certs, _ := appdb.GetCerts(h.db, "active")
	for _, c := range certs {
		if d := daysLeftVal(c.ExpiresAt); d <= ExpiringSoonDays {
			expiringSoon = append(expiringSoon, StatusExpiring{c.Name, c.Domain, d})
		}
	}
	return len(certs), expiringSoon
}
