package handlers

import (
	"errors"
	"log/slog"
	"net/mail"

	appdb "certharbor/db"
	"certharbor/models"
	"certharbor/security"
)

// ErrIncorrectPassword reports that a reauthentication check (POST
// /api/v1/profile/password, Section 5.5's reauth column) was presented
// with a current password that does not match the stored hash.
var ErrIncorrectPassword = errors.New("handlers: current password is incorrect")

// profileValidThemes mirrors the template handler's inline validity map
// (handlers/users.go ProfilePost's "theme" action), kept as the one place
// both the API layer and any future caller agree on the vocabulary.
var profileValidThemes = map[string]bool{"dark": true, "light": true, "blue": true, "auto": true}

// ProfileRow is the handler-layer view of models.User that GET
// /api/v1/profile and every profile PATCH response build from, never from
// models.User directly (api/ never imports certharbor/models, Section 2's
// depguard rule, mirrors UserDetail's comment). PasswordHash, TOTPSecret
// and TOTPPendingSecret never leave models.User (R15).
type ProfileRow struct {
	ID          int
	Username    string
	DisplayName string
	Email       string
	Role        string
	Theme       string
}

func toProfileRow(u *models.User) *ProfileRow {
	return &ProfileRow{
		ID: u.ID, Username: u.Username, DisplayName: u.DisplayName,
		Email: u.Email, Role: u.Role, Theme: u.Theme,
	}
}

// GetProfile implements GET /api/v1/profile, replacing GET /profile
// (Section 8 "Profile"). id is the acting session's own user id, derived
// by the caller from the request context (appmw.UserFrom) and never from a
// path or body parameter: every profile operation acts on the session's own
// account (5.5).
func (h *Handler) GetProfile(id int) (*ProfileRow, error) {
	u, err := appdb.GetUserByID(h.db, id)
	if err != nil {
		return nil, err
	}
	if u == nil {
		return nil, ErrUserNotFound
	}
	return toProfileRow(u), nil
}

// ProfilePatch is PATCH /api/v1/profile's merge-style body (5.8): a nil
// field means absent, left unchanged. Replaces the "update_info" action of
// POST /profile (handlers/users.go:226).
type ProfilePatch struct {
	DisplayName *string
	Email       *string
}

// applyProfilePatch merges patch onto current and returns the resulting
// display name and email (5.8: absent means unchanged). Pure and
// database-free, mirroring applyACMESettingsPatch/applyNotificationPatch,
// so the email-validation and merge behaviour is unit-testable without a
// database.
func applyProfilePatch(current *models.User, patch *ProfilePatch) (displayName, email string, err error) {
	displayName = current.DisplayName
	if patch.DisplayName != nil {
		displayName = trimStr(*patch.DisplayName)
	}
	email = current.Email
	if patch.Email != nil {
		email = trimStr(*patch.Email)
		if email != "" {
			parsed, parseErr := mail.ParseAddress(email)
			if parseErr != nil {
				return "", "", NewValidationError("invalid email address")
			}
			email = parsed.Address
		}
	}
	return displayName, email, nil
}

// UpdateProfile implements PATCH /api/v1/profile. Email is validated and
// normalised exactly as the template handler's "update_info" action did
// (mail.ParseAddress), surfaced as a ValidationError rather than the
// template's flash message (R23).
func (h *Handler) UpdateProfile(id int, patch *ProfilePatch) (*ProfileRow, error) {
	u, err := appdb.GetUserByID(h.db, id)
	if err != nil {
		return nil, err
	}
	if u == nil {
		return nil, ErrUserNotFound
	}

	displayName, email, err := applyProfilePatch(u, patch)
	if err != nil {
		return nil, err
	}

	if err := appdb.UpdateUserInfo(h.db, id, displayName, email); err != nil {
		return nil, err
	}
	fresh, err := appdb.GetUserByID(h.db, id)
	if err != nil {
		return nil, err
	}
	if fresh == nil {
		return nil, ErrUserNotFound
	}
	return toProfileRow(fresh), nil
}

// ProfilePreferencesPatch is PATCH /api/v1/profile/preferences' merge-style
// body (5.8), replacing the "theme" action of POST /profile
// (handlers/users.go:212).
type ProfilePreferencesPatch struct {
	Theme *string
}

// UpdateProfilePreferences implements PATCH /api/v1/profile/preferences.
// An unrecognised theme is rejected rather than silently coerced to
// "dark" the way the template handler's inline fallback did: the template
// route had nowhere to report the rejection back to the caller, a JSON
// operation does.
func (h *Handler) UpdateProfilePreferences(id int, patch *ProfilePreferencesPatch) (*ProfileRow, error) {
	if patch.Theme != nil && !profileValidThemes[*patch.Theme] {
		return nil, NewValidationError("theme must be one of: dark, light, blue, auto")
	}
	if patch.Theme != nil {
		if err := appdb.UpdateUserTheme(h.db, id, *patch.Theme); err != nil {
			return nil, err
		}
	}
	u, err := appdb.GetUserByID(h.db, id)
	if err != nil {
		return nil, err
	}
	if u == nil {
		return nil, ErrUserNotFound
	}
	return toProfileRow(u), nil
}

// validateChangePassword checks the reauthentication requirement (the
// current password must match) and the new password's strength, against
// an already-loaded user row. Pure and database-free, mirroring
// applyProfilePatch, so both the reauth check and the strength check are
// unit-testable without a database.
func validateChangePassword(current *models.User, currentPassword, newPassword string) error {
	if !security.VerifyPassword(currentPassword, current.PasswordHash) {
		return ErrIncorrectPassword
	}
	if ok, msg := security.ValidatePassword(newPassword); !ok {
		return NewValidationError(msg)
	}
	return nil
}

// ChangePassword implements POST /api/v1/profile/password, replacing the
// "change_password" action of POST /profile (handlers/users.go:247). It
// requires the current password (5.5's reauth column: this mutates a
// credential). On success it returns the fresh session_epoch so the caller
// can re-stamp the acting session's own cookie: BumpSessionEpoch revokes
// every *other* session for this user (V8), and the caller must not be
// logged out of the request it is standing on.
func (h *Handler) ChangePassword(id int, currentPassword, newPassword string) (newEpoch int, err error) {
	u, err := appdb.GetUserByID(h.db, id)
	if err != nil {
		return 0, err
	}
	if u == nil {
		return 0, ErrUserNotFound
	}
	if err := validateChangePassword(u, currentPassword, newPassword); err != nil {
		return 0, err
	}
	if err := appdb.UpdateUserPassword(h.db, id, security.HashPassword(newPassword)); err != nil {
		return 0, err
	}
	if err := appdb.BumpSessionEpoch(h.db, id); err != nil {
		slog.Error("bumping session epoch after profile password change failed", "user_id", id, "err", err)
		return 0, err
	}
	fresh, err := appdb.GetUserByID(h.db, id)
	if err != nil {
		return 0, err
	}
	if fresh == nil {
		return 0, ErrUserNotFound
	}
	return fresh.SessionEpoch, nil
}
