package handlers

import (
	"time"

	appdb "certharbor/db"
)

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
