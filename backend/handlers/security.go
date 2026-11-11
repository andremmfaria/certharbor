package handlers

import (
	"net/http"
	"time"

	appdb "certharbor/db"
	"certharbor/security"
)

// SecurityEvent is one auth_log row on the wire, shared by GET
// /api/v1/security/events (the filtered admin security view) and GET
// /api/v1/audit-events (the full unfiltered feed): there is no separate
// audit_log table (backend/delivery/audit.go's own comment spells this out
// — certharbor-control's own audit writes already land in auth_log via
// appdb.LogAuth), so both operations read appdb.GetAuthLogs, one with a
// search/outcome filter and one without.
type SecurityEvent struct {
	ID        int
	Username  string
	IP        string
	Success   bool
	Reason    string
	CreatedAt time.Time
}

// outcomeFilter maps the wire's outcome=success|failure vocabulary onto
// appdb.GetAuthLogs' existing filter=ok|fail strings (SecurityLog's
// template handler reads the same filter off r.URL.Query().Get("filter")),
// so the query shape is unchanged from today's template route.
func outcomeFilter(outcome string) string {
	switch outcome {
	case "success":
		return "ok"
	case "failure":
		return "fail"
	default:
		return ""
	}
}

// ListSecurityEvents is the data half of GET /api/v1/security/events,
// replacing GET /admin/security (Section 6). q and outcome follow
// SecurityLog's existing search/filter semantics exactly.
func (h *Handler) ListSecurityEvents(q, outcome string, page, pageSize int) ([]SecurityEvent, int, error) {
	rows, total, err := appdb.GetAuthLogs(h.db, q, outcomeFilter(outcome), page, pageSize)
	if err != nil {
		return nil, 0, err
	}
	out := make([]SecurityEvent, 0, len(rows))
	for _, r := range rows {
		out = append(out, SecurityEvent{
			ID: r.ID, Username: r.Username, IP: r.IP, Success: r.Success,
			Reason: r.Reason, CreatedAt: r.CreatedAt,
		})
	}
	return out, total, nil
}

// ListAuditEvents is the data half of GET /api/v1/audit-events, replacing
// GET /admin/activity (Section 6). AdminActivityGet never wired this up
// (it is a stub today), so this is the operation's first real
// implementation: the full, unfiltered auth_log feed, paginated.
func (h *Handler) ListAuditEvents(page, pageSize int) ([]SecurityEvent, int, error) {
	return h.ListSecurityEvents("", "", page, pageSize)
}

// UnblockIP implements DELETE /api/v1/security/ip-blocks/{ip}'s side
// effect, replacing POST /admin/users' unblock_ip action (Section 6,
// handlers/users.go's case "unblock_ip"). It cannot reuse h.auditSecurity
// directly, for the same reason AuditKeyDownload cannot (see its comment
// in ca_api.go): the huma session middleware never stashes a "username"
// into the gorilla session h.sessionInfo reads, only "user_id". ip is the
// caller-supplied target to unblock, already validated as canonical by the
// api layer; username is the authenticated actor doing the unblocking.
func (h *Handler) UnblockIP(r *http.Request, username, ip string) {
	security.RL.Clear(ip)
	if r == nil || username == "" {
		return
	}
	_ = appdb.LogAuth(h.db, username, clientIP(r), true, auditPrefix+"ip.unblock target="+ip)
}
