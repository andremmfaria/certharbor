package handlers

import (
	"context"
	"database/sql"

	"step-webui/models"

	appdb "step-webui/db"
)

// AuthStore is the database surface the login and MFA flow touches. It exists
// so that flow can be exercised without a database: the handlers package has
// no test database, and the rate limiter, CSRF and session mechanics around
// login are exactly what is worth proving.
type AuthStore interface {
	UserByUsername(username string) (*models.User, error)
	UserByID(id int) (*models.User, error)
	LogAuth(username, ip string, success bool, reason string) error
	SetPasswordHash(id int, hash string) error
	UnusedRecoveryCodes(userID int) (map[int]string, error)
	UseRecoveryCode(codeID int) error
	TOTPLastStep(ctx context.Context, userID int) (int64, error)
	SetTOTPLastStep(ctx context.Context, userID int, step int64) error
}

type dbAuthStore struct{ db *sql.DB }

func (s dbAuthStore) UserByUsername(username string) (*models.User, error) {
	return appdb.GetUserByUsername(s.db, username)
}

func (s dbAuthStore) UserByID(id int) (*models.User, error) {
	return appdb.GetUserByID(s.db, id)
}

func (s dbAuthStore) LogAuth(username, ip string, success bool, reason string) error {
	return appdb.LogAuth(s.db, username, ip, success, reason)
}

func (s dbAuthStore) SetPasswordHash(id int, hash string) error {
	return appdb.UpdateUserPassword(s.db, id, hash)
}

func (s dbAuthStore) UnusedRecoveryCodes(userID int) (map[int]string, error) {
	return appdb.GetUnusedRecoveryCodes(s.db, userID)
}

func (s dbAuthStore) UseRecoveryCode(codeID int) error {
	return appdb.UseRecoveryCode(s.db, codeID)
}

func (s dbAuthStore) TOTPLastStep(ctx context.Context, userID int) (int64, error) {
	return appdb.GetTOTPLastStep(ctx, s.db, userID)
}

func (s dbAuthStore) SetTOTPLastStep(ctx context.Context, userID int, step int64) error {
	return appdb.SetTOTPLastStep(ctx, s.db, userID, step)
}

// SetAuthStore replaces the database-backed store. Tests are the only caller.
func (h *Handler) SetAuthStore(s AuthStore) { h.auth = s }

func (h *Handler) authStore() AuthStore {
	if h.auth != nil {
		return h.auth
	}
	return dbAuthStore{db: h.db}
}
