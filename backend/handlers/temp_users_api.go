package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	appdb "certharbor/db"
	"certharbor/security"
)

// ErrTempUserNotFound reports that the targeted temporary-user row does not
// exist (or is not a temporary user).
var ErrTempUserNotFound = errors.New("handlers: temporary user not found")

// TempUserSummary is one row of GET /api/v1/temporary-users, replacing GET
// /admin/users-temp. It carries the same computed Status field the template
// view model does (active/expired/blocked), so the SPA does not reimplement
// the "expires_at in the past but is_active still true" window between a
// temp user's expiry and the next ExpireOverdueTempUsers sweep.
type TempUserSummary struct {
	ID        int
	Username  string
	Role      string
	Note      string
	CreatedAt time.Time
	ExpiresAt *time.Time
	Status    string // "active" | "expired" | "blocked"
}

// TempUserCredential is the one-time response body of POST
// /api/v1/temporary-users: the generated username and passphrase, shown to
// the caller exactly once. The field is named Passphrase rather than
// Password deliberately: it must appear in a response, which
// openapi/secret-allowlist.txt's writeOnly rule forbids for any
// secret-shaped property name, and "passphrase" does not match that
// regex (api/spec_secrets_test.go).
type TempUserCredential struct {
	TempUserSummary
	Passphrase string
}

func tempUserStatus(u *appdb.TempUserRow, now time.Time) string {
	switch {
	case u.IsActive:
		return "active"
	case u.ExpiresAt != nil && now.After(*u.ExpiresAt):
		return "expired"
	default:
		return "blocked"
	}
}

// ListTemporaryUsers is the data half of GET /api/v1/temporary-users,
// replacing GET /admin/users-temp (adminUsersTempData).
func (h *Handler) ListTemporaryUsers() ([]TempUserSummary, error) {
	rows, err := appdb.ListTempUsers(h.db)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	out := make([]TempUserSummary, 0, len(rows))
	for _, u := range rows {
		out = append(out, TempUserSummary{
			ID: u.ID, Username: u.Username, Role: u.Role, Note: u.Note,
			CreatedAt: u.CreatedAt, ExpiresAt: u.ExpiresAt,
			Status: tempUserStatus(&u, now),
		})
	}
	return out, nil
}

// CreateTemporaryUser is the data half of POST /api/v1/temporary-users,
// replacing AdminUsersTempPost. Unlike the template route, which hands the
// credential to the next GET through tempCredStore (a redirect needs
// somewhere to put a one-time secret that is not a cookie or the
// database), the SPA calls this once and reads the credential straight off
// the response: there is no redirect to carry it across, so the single-use
// token store (temp_creds.go) is not needed here. Expiry validation
// (future, at least one minute out) mirrors the template handler exactly.
func (h *Handler) CreateTemporaryUser(r *http.Request, actorUsername, role, note string, expiresAt time.Time) (*TempUserCredential, error) {
	if !appdb.ValidRole(role) {
		role = "viewer"
	}
	if !expiresAt.After(time.Now().Add(1 * time.Minute)) {
		return nil, NewValidationError("expiry must be in the future (at least one minute from now)")
	}

	username := generateTempUsername()
	password := generateTempPassword(16)
	hash := security.HashPassword(password)

	id, err := appdb.CreateTempUser(h.db, username, hash, role, expiresAt, note)
	if err != nil {
		return nil, NewValidationError("failed to create user: " + err.Error())
	}
	h.auditAdmin(r, actorUsername, "tempuser.create target="+username+" role="+role)
	return &TempUserCredential{
		TempUserSummary: TempUserSummary{
			ID: id, Username: username, Role: role, Note: note,
			CreatedAt: time.Now(), ExpiresAt: &expiresAt, Status: "active",
		},
		Passphrase: password,
	}, nil
}

// DeleteTemporaryUser is the data half of DELETE
// /api/v1/temporary-users/{id}. There is no legacy template equivalent: the
// admin_users_temp page never gained a delete action, so this is a new
// capability rather than a migration of an existing one.
func (h *Handler) DeleteTemporaryUser(r *http.Request, actorUsername string, id int) error {
	ok, err := appdb.DeleteTempUser(h.db, id)
	if err != nil {
		return err
	}
	if !ok {
		return ErrTempUserNotFound
	}
	h.auditAdmin(r, actorUsername, "tempuser.delete target_id="+strconv.Itoa(id))
	return nil
}
