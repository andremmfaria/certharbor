package handlers

import (
	"net/http"
	"strconv"
	"time"

	appdb "certharbor/db"
)

const pageSize = 30

// History renders the certificate action history page with optional action filters.
func (h *Handler) History(w http.ResponseWriter, r *http.Request) {
	// Multi-select: collect all ?action=... values from the URL
	actions := r.URL.Query()["action"]
	// Filter out empty values
	filtered := actions[:0]
	for _, a := range actions {
		if a != "" {
			filtered = append(filtered, a)
		}
	}
	actions = filtered

	cert := r.URL.Query().Get("cert")
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	entries, total, _ := appdb.GetHistory(h.db, actions, cert, page, pageSize)
	totalPages := (total + pageSize - 1) / pageSize
	if totalPages < 1 {
		totalPages = 1
	}
	data := h.base(w, r, "history")
	data["Entries"] = entries
	data["FilterActions"] = actions
	data["FilterCert"] = cert
	data["CurrentPage"] = page
	data["TotalPages"] = totalPages
	data["Total"] = total
	h.render(w, "history", data)
}

// HistoryEntry is one cert_history row, converted from
// certharbor/models.CertHistory so that backend/api (which may not import
// certharbor/models, Section 2's depguard rule) can build GET
// /api/v1/history's response from a package it is allowed to see (D3 rule
// 3: a response DTO never embeds or aliases a models type, and the handlers
// conversion step this file already had for CertificateSummary is the
// established way to get there).
type HistoryEntry struct {
	ID        int
	Action    string
	CertName  string
	Domain    string
	Details   string
	Username  string
	Role      string
	CreatedAt time.Time
}

// ListHistory is the data half of GET /api/v1/history, replacing GET
// /history (Section 6). It reuses appdb.GetHistory unchanged, now ordered
// by the total order 5.6 asks for (created_at DESC, id DESC, fixed in
// db/history.go as part of this same change).
func (h *Handler) ListHistory(actions []string, cert string, page, pageSize int) ([]HistoryEntry, int, error) {
	rows, total, err := appdb.GetHistory(h.db, actions, cert, page, pageSize)
	if err != nil {
		return nil, 0, err
	}
	out := make([]HistoryEntry, 0, len(rows))
	for _, r := range rows {
		out = append(out, HistoryEntry{
			ID: r.ID, Action: r.Action, CertName: r.CertName, Domain: r.Domain,
			Details: r.Details, Username: r.Username, Role: r.Role, CreatedAt: r.CreatedAt,
		})
	}
	return out, total, nil
}
