package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	appdb "certharbor/db"
	"certharbor/models"
	"certharbor/security"
)

// ErrSelfTarget reports that an admin user-management operation was aimed at
// the acting user's own account. DELETE /api/v1/users/{id} and PATCH
// /api/v1/users/{id} both reject this unconditionally (Section 5.5, Section
// 8 Phase 6, handlers/users.go:68,87,109 today): without it the last admin
// could demote, deactivate or delete themselves and leave the installation
// with no admin.
var ErrSelfTarget = errors.New("handlers: cannot target your own account")

// ErrUserNotFound reports that the targeted user row does not exist.
var ErrUserNotFound = errors.New("handlers: user not found")

// UserSummary is one row of GET /api/v1/users, replacing GET /admin/users
// (Section 6). It carries no secret-shaped field: PasswordHash, TOTPSecret
// and TOTPPendingSecret never leave models.User (R15).
type UserSummary struct {
	ID        int
	Username  string
	Role      string
	IsActive  bool
	CreatedAt *time.Time
	LastLogin *time.Time
	LastIP    *string
	FailCount int
}

// UserDetail is the body of GET /api/v1/users/{id}, replacing GET
// /admin/users/{id}. IPBlocked and LastIP are called out explicitly by
// Section 6 (the endpoint map's Users row).
type UserDetail struct {
	UserSummary
	DisplayName string
	Email       string
	Theme       string
	TOTPEnabled bool
	IPBlocked   bool
}

// ListUsers is the data half of GET /api/v1/users, replacing GET
// /admin/users. q filters by a case-insensitive username substring; an
// empty q returns every user. Pagination is applied in-process: the user
// table is operator-sized, not a candidate for Section 5.6's deep-offset
// guard the certificate and history lists need.
func (h *Handler) ListUsers(q string, page, pageSize int) ([]UserSummary, int, error) {
	users, err := appdb.GetAllUsers(h.db)
	if err != nil {
		return nil, 0, err
	}
	since := time.Now().Add(-24 * time.Hour)

	filtered := make([]*models.User, 0, len(users))
	for _, u := range users {
		if q != "" && !strings.Contains(strings.ToLower(u.Username), strings.ToLower(q)) {
			continue
		}
		filtered = append(filtered, u)
	}
	total := len(filtered)

	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 25
	}
	start := (page - 1) * pageSize
	if start > total {
		start = total
	}
	end := start + pageSize
	if end > total {
		end = total
	}

	out := make([]UserSummary, 0, end-start)
	for _, u := range filtered[start:end] {
		out = append(out, UserSummary{
			ID: u.ID, Username: u.Username, Role: u.Role, IsActive: u.IsActive,
			CreatedAt: u.CreatedAt, LastLogin: u.LastLogin, LastIP: u.LastIP,
			FailCount: appdb.GetFailCount(h.db, u.Username, since),
		})
	}
	return out, total, nil
}

// GetUser is the data half of GET /api/v1/users/{id}, replacing GET
// /admin/users/{id}. It returns ErrUserNotFound rather than a nil, nil pair
// so the api layer can map it to a 404 with one comparison.
func (h *Handler) GetUser(id int) (*UserDetail, error) {
	u, err := appdb.GetUserByID(h.db, id)
	if err != nil {
		return nil, err
	}
	if u == nil {
		return nil, ErrUserNotFound
	}
	since := time.Now().Add(-24 * time.Hour)
	ipBlocked := false
	if u.LastIP != nil && *u.LastIP != "" {
		ipBlocked = security.RL.IsBlocked(*u.LastIP)
	}
	return &UserDetail{
		UserSummary: UserSummary{
			ID: u.ID, Username: u.Username, Role: u.Role, IsActive: u.IsActive,
			CreatedAt: u.CreatedAt, LastLogin: u.LastLogin, LastIP: u.LastIP,
			FailCount: appdb.GetFailCount(h.db, u.Username, since),
		},
		DisplayName: u.DisplayName, Email: u.Email, Theme: u.Theme,
		TOTPEnabled: u.TOTPEnabled, IPBlocked: ipBlocked,
	}, nil
}

// CreateUserAdmin is the data half of POST /api/v1/users, replacing the
// "create" action of POST /admin/users (handlers/users.go:43).
// Validation (role allowlist, password policy) happens before the write so
// a rejected request never reaches the database.
func (h *Handler) CreateUserAdmin(r *http.Request, actorUsername, username, password, role string) (*UserSummary, error) {
	if username == "" || password == "" {
		return nil, NewValidationError("username and password are required")
	}
	if !appdb.ValidRole(role) {
		return nil, NewValidationError(invalidRoleMsg)
	}
	if ok, msg := security.ValidatePassword(password); !ok {
		return nil, NewValidationError(msg)
	}
	if err := appdb.CreateUser(h.db, username, security.HashPassword(password), role); err != nil {
		return nil, NewValidationError("user already exists")
	}
	h.auditAdmin(r, actorUsername, "user.create target="+username+" role="+role)
	u, err := appdb.GetUserByUsername(h.db, username)
	if err != nil || u == nil {
		return nil, err
	}
	return &UserSummary{ID: u.ID, Username: u.Username, Role: u.Role, IsActive: u.IsActive, CreatedAt: u.CreatedAt}, nil
}

// UpdateUserAdmin is the data half of PATCH /api/v1/users/{id}, replacing
// the "change_role" (handlers/users.go:84) and "toggle_active"
// (handlers/users.go:107) actions of POST /admin/users, folded into one
// operation per Section 6. Either field may be nil to leave it unchanged,
// but at least one must be set.
//
// It rejects targetID == actorID unconditionally (ErrSelfTarget), before
// touching the database: the last admin must not be able to demote or
// deactivate themselves and leave the installation with no admin.
func (h *Handler) UpdateUserAdmin(r *http.Request, actorUsername string, actorID, targetID int, role *string, isActive *bool) (*UserSummary, error) {
	if targetID == actorID {
		return nil, ErrSelfTarget
	}
	if role == nil && isActive == nil {
		return nil, NewValidationError("at least one of role or isActive must be set")
	}
	if role != nil {
		if !appdb.ValidRole(*role) {
			return nil, NewValidationError(invalidRoleMsg)
		}
		if err := appdb.UpdateUserRole(h.db, targetID, *role); err != nil {
			return nil, err
		}
	}
	if isActive != nil {
		if err := appdb.UpdateUserActive(h.db, targetID, *isActive); err != nil {
			return nil, err
		}
	}
	u, err := appdb.GetUserByID(h.db, targetID)
	if err != nil {
		return nil, err
	}
	if u == nil {
		return nil, ErrUserNotFound
	}
	switch {
	case role != nil && isActive != nil:
		h.auditAdmin(r, actorUsername, "user.update target="+u.Username+" role="+*role+" isActive="+strconv.FormatBool(*isActive))
	case role != nil:
		h.auditAdmin(r, actorUsername, "user.change_role target="+u.Username+" role="+*role)
	case isActive != nil:
		action := "user.deactivate"
		if *isActive {
			action = "user.activate"
		}
		h.auditAdmin(r, actorUsername, action+" target="+u.Username)
	}
	return &UserSummary{ID: u.ID, Username: u.Username, Role: u.Role, IsActive: u.IsActive, CreatedAt: u.CreatedAt, LastLogin: u.LastLogin, LastIP: u.LastIP}, nil
}

// DeleteUserAdmin is the data half of DELETE /api/v1/users/{id}, replacing
// the "delete" action of POST /admin/users (handlers/users.go:66). Rejects
// targetID == actorID unconditionally (ErrSelfTarget), same invariant as
// UpdateUserAdmin and for the same reason.
func (h *Handler) DeleteUserAdmin(r *http.Request, actorUsername string, actorID, targetID int) error {
	if targetID == actorID {
		return ErrSelfTarget
	}
	target, _ := appdb.GetUserByID(h.db, targetID)
	if err := appdb.DeleteUser(h.db, targetID); err != nil {
		return err
	}
	if target != nil {
		h.auditAdmin(r, actorUsername, "user.delete target="+target.Username)
	} else {
		h.auditAdmin(r, actorUsername, "user.delete target_id="+strconv.Itoa(targetID))
	}
	return nil
}

// ResetUserPasswordAdmin is the data half of POST
// /api/v1/users/{id}/password-reset, replacing the "reset_password" action
// of POST /admin/users (handlers/users.go:134). Bumping the session epoch
// is what makes the reset immediate: without it a user whose password was
// just reset keeps every session already issued to them.
func (h *Handler) ResetUserPasswordAdmin(r *http.Request, actorUsername string, targetID int, newPassword string) (*UserSummary, error) {
	if ok, msg := security.ValidatePassword(newPassword); !ok {
		return nil, NewValidationError(msg)
	}
	u, err := appdb.GetUserByID(h.db, targetID)
	if err != nil {
		return nil, err
	}
	if u == nil {
		return nil, ErrUserNotFound
	}
	if err := appdb.UpdateUserPassword(h.db, targetID, security.HashPassword(newPassword)); err != nil {
		return nil, err
	}
	if err := appdb.BumpSessionEpoch(h.db, targetID); err != nil {
		return nil, err
	}
	h.auditAdmin(r, actorUsername, "user.reset_password target="+u.Username)
	return &UserSummary{ID: u.ID, Username: u.Username, Role: u.Role, IsActive: u.IsActive, CreatedAt: u.CreatedAt, LastLogin: u.LastLogin, LastIP: u.LastIP}, nil
}

// auditAdmin records an administrative user-management action. It cannot
// reuse h.auditSecurity, for the same reason UnblockIP cannot (security.go):
// the huma session middleware never stashes a "username" into the gorilla
// session h.sessionInfo reads, only "user_id", so the acting username must
// be passed in by the caller (api/users.go, via middleware.UserFrom).
func (h *Handler) auditAdmin(r *http.Request, actorUsername, reason string) {
	if r == nil || actorUsername == "" {
		return
	}
	_ = appdb.LogAuth(h.db, actorUsername, clientIP(r), true, auditPrefix+reason)
}
