package api

import (
	"context"
	"net/http"
	"time"

	"certharbor/apitypes"
	"certharbor/handlers"
)

// historyEntryBody is one cert_history row on the wire. CertName and Domain
// are the subject of the action; Details is a freeform note, never a
// filesystem path (Section 2's banned-property-name list has no entry this
// could collide with, since cert_history never stores one).
type historyEntryBody struct {
	ID        int       `json:"id"`
	Action    string    `json:"action"`
	CertName  string    `json:"certName"`
	Domain    string    `json:"domain,omitempty"`
	Details   string    `json:"details,omitempty"`
	Username  string    `json:"username,omitempty"`
	Role      string    `json:"role,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

func newHistoryEntryBody(e handlers.HistoryEntry) historyEntryBody {
	return historyEntryBody{
		ID: e.ID, Action: e.Action, CertName: e.CertName, Domain: e.Domain,
		Details: e.Details, Username: e.Username, Role: e.Role, CreatedAt: e.CreatedAt,
	}
}

// ─── GET /api/v1/history ────────────────────────────────────────────────────

// listHistoryInput's Action field is repeated and multi-valued (style:
// form, explode: true, 5.6), matching handlers/history.go's existing
// r.URL.Query()["action"] read exactly: huma decodes a repeated query key
// into a []string with no comma-splitting, so the wire shape is unchanged
// from today's template route.
type listHistoryInput struct {
	Cert     string   `query:"cert" maxLength:"255" doc:"Exact certificate name filter."`
	Action   []string `query:"action" doc:"Action filter, repeatable (e.g. ?action=issue&action=renew)."`
	Page     int      `query:"page" minimum:"1" default:"1"`
	PageSize int      `query:"pageSize" minimum:"1" maximum:"200" default:"25"`
}

type listHistoryOutput struct {
	Body apitypes.Page[historyEntryBody]
}

// listHistory implements GET /api/v1/history, replacing GET /history
// (Section 6). Pagination follows 5.6's envelope and bounds exactly as
// listCertificates does (apitypes.MaxOffset's page*pageSize guard, 422 on
// violation), and the underlying query is now ordered by the total order
// (created_at DESC, id DESC) db/history.go was fixed to use in this same
// change, so a page boundary cannot repeat or omit a row when many entries
// share one created_at.
func listHistory(h *handlers.Handler) func(context.Context, *listHistoryInput) (*listHistoryOutput, error) {
	return func(_ context.Context, in *listHistoryInput) (*listHistoryOutput, error) {
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

		entries, total, err := h.ListHistory(in.Action, in.Cert, page, pageSize)
		if err != nil {
			return nil, err
		}
		items := make([]historyEntryBody, 0, len(entries))
		for _, e := range entries {
			items = append(items, newHistoryEntryBody(e))
		}
		return &listHistoryOutput{Body: apitypes.NewPage(items, page, pageSize, total)}, nil
	}
}
