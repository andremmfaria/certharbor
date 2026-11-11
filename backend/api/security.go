package api

import (
	"context"
	"net/http"
	"net/netip"
	"time"

	"certharbor/apitypes"
	"certharbor/handlers"
)

// securityEventBody is one auth_log row on the wire, shared by
// listSecurityEvents and listAuditEvents (Section 6): both read the same
// table, one filtered by q/outcome and one not.
type securityEventBody struct {
	ID        int       `json:"id"`
	Username  string    `json:"username,omitempty"`
	IP        string    `json:"ip,omitempty"`
	Outcome   string    `json:"outcome" enum:"success,failure"`
	Reason    string    `json:"reason,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

func newSecurityEventBody(e *handlers.SecurityEvent) securityEventBody {
	outcome := "failure"
	if e.Success {
		outcome = "success"
	}
	return securityEventBody{
		ID: e.ID, Username: e.Username, IP: e.IP, Outcome: outcome,
		Reason: e.Reason, CreatedAt: e.CreatedAt,
	}
}

// ─── GET /api/v1/security/events ────────────────────────────────────────────

// listSecurityEventsInput's Q and Outcome match SecurityLog's existing q/
// filter query params (handlers/seclog.go), renamed to the wire vocabulary
// Section 6 specifies: outcome=success|failure instead of filter=ok|fail.
type listSecurityEventsInput struct {
	Q        string `query:"q" maxLength:"255" doc:"Search username or IP."`
	Outcome  string `query:"outcome" enum:"success,failure" doc:"Filter by outcome."`
	Page     int    `query:"page" minimum:"1" default:"1"`
	PageSize int    `query:"pageSize" minimum:"1" maximum:"200" default:"25"`
}

type listSecurityEventsOutput struct {
	Body apitypes.Page[securityEventBody]
}

// listSecurityEvents implements GET /api/v1/security/events, replacing GET
// /admin/security (Section 6). Pagination follows 5.6's envelope and bounds
// exactly as listHistory does, and the underlying query is already ordered
// by the total order (created_at DESC, id DESC) db/authlog.go carries from
// Phase 5 step 1, so a page boundary cannot repeat or omit a row when many
// entries share one created_at.
func listSecurityEvents(h *handlers.Handler) func(context.Context, *listSecurityEventsInput) (*listSecurityEventsOutput, error) {
	return func(_ context.Context, in *listSecurityEventsInput) (*listSecurityEventsOutput, error) {
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

		entries, total, err := h.ListSecurityEvents(in.Q, in.Outcome, page, pageSize)
		if err != nil {
			return nil, err
		}
		items := make([]securityEventBody, 0, len(entries))
		for i := range entries {
			items = append(items, newSecurityEventBody(&entries[i]))
		}
		return &listSecurityEventsOutput{Body: apitypes.NewPage(items, page, pageSize, total)}, nil
	}
}

// ─── GET /api/v1/audit-events ───────────────────────────────────────────────

type listAuditEventsInput struct {
	Page     int `query:"page" minimum:"1" default:"1"`
	PageSize int `query:"pageSize" minimum:"1" maximum:"200" default:"25"`
}

type listAuditEventsOutput struct {
	Body apitypes.Page[securityEventBody]
}

// listAuditEvents implements GET /api/v1/audit-events, replacing GET
// /admin/activity (Section 6). AdminActivityGet is a stub today with no
// data wiring, so this is the operation's first real implementation: the
// full, unfiltered auth_log feed (no q/outcome filter), paginated exactly
// as listSecurityEvents.
func listAuditEvents(h *handlers.Handler) func(context.Context, *listAuditEventsInput) (*listAuditEventsOutput, error) {
	return func(_ context.Context, in *listAuditEventsInput) (*listAuditEventsOutput, error) {
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

		entries, total, err := h.ListAuditEvents(page, pageSize)
		if err != nil {
			return nil, err
		}
		items := make([]securityEventBody, 0, len(entries))
		for i := range entries {
			items = append(items, newSecurityEventBody(&entries[i]))
		}
		return &listAuditEventsOutput{Body: apitypes.NewPage(items, page, pageSize, total)}, nil
	}
}

// ─── DELETE /api/v1/security/ip-blocks/{ip} ─────────────────────────────────

// deleteIPBlockInput's pattern is a loose charset gate; the real canonical-
// form check is netip.ParseAddr plus a round-trip comparison in
// deleteIPBlock below, because a regex alone cannot tell "::1" from
// "0:0:0:0:0:0:0:1" (both match a hex/colon charset, only one is
// netip.Addr.String()'s canonical compressed form). Generated clients
// encodeURIComponent path parameters, so "::1" arrives as "%3A%3A1" and
// decodes back to "::1" before reaching this handler — the constraint this
// guards against is a caller sending the non-canonical expansion, not the
// URL-encoding itself. security.RL.Clear(ip) keys its map by the exact
// string login attempts were registered under (clientIP's host-only,
// un-bracketed form), so the wrong form is a silent no-op delete, not an
// error.
type deleteIPBlockInput struct {
	IP string `path:"ip" pattern:"^[0-9a-f.:]+$" doc:"Canonical IP address form, as emitted by the server (lowercase, compressed IPv6, no brackets, no zone)."`
}

// deleteIPBlockOutput carries no body: the response is a 204.
type deleteIPBlockOutput struct {
	Status int
}

// deleteIPBlock implements DELETE /api/v1/security/ip-blocks/{ip},
// replacing the unblock_ip action of POST /admin/users (Section 6).
func deleteIPBlock(h *handlers.Handler) func(context.Context, *deleteIPBlockInput) (*deleteIPBlockOutput, error) {
	return func(ctx context.Context, in *deleteIPBlockInput) (*deleteIPBlockOutput, error) {
		addr, err := netip.ParseAddr(in.IP)
		if err != nil || addr.String() != in.IP {
			return nil, newProblem(http.StatusUnprocessableEntity, "Unprocessable Entity", problemTypeBase+"validation",
				"ip must be the canonical address form the server emits (lowercase, compressed, no brackets, no zone)", "")
		}
		r, _ := httpFrom(ctx)
		username, _ := userIdentity(ctx)
		h.UnblockIP(r, username, in.IP)
		return &deleteIPBlockOutput{Status: http.StatusNoContent}, nil
	}
}
