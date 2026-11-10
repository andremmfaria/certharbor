package delivery

import (
	"database/sql"
	"log/slog"

	appdb "certharbor/db"
)

// auditActor is the username recorded against every audit_log row this
// plane writes. certharbor-delivery has no authenticated session (it is a
// worker, not an HTTP handler), so it identifies itself the same way any
// other non-interactive actor would.
const auditActor = "certharbor-delivery"

// Auditor records push attempts, binding changes and credential access
// events into the SAME audit trail certharbor-control already writes to
// (backend/handlers/audit.go's auditSecurity calls appdb.LogAuth against
// the auth_log table; this plane calls appdb.LogAuth directly, since it has
// no *http.Request/session to build a handlers.Handler around). Per
// plans/distribution.md Section 4/2, this is deliberate: "who/what pushed a
// key to AWS" lands in the one audit trail an operator already watches.
type Auditor struct {
	db *sql.DB
}

// NewAuditor wraps a DB handle for audit writes.
func NewAuditor(db *sql.DB) *Auditor {
	return &Auditor{db: db}
}

// Record writes one audit event. success and reason follow auth_log's
// existing shape (handlers/audit.go's auditPrefix convention); a write
// failure here is logged, never fatal, so a down database never blocks a
// reconciler tick that would otherwise have succeeded.
func (a *Auditor) Record(success bool, reason string) {
	if a == nil || a.db == nil {
		return
	}
	if err := appdb.LogAuth(a.db, auditActor, "", success, reason); err != nil {
		slog.Warn("certharbor-delivery: audit write failed", "err", err, "reason", reason)
	}
}
