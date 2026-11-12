package api

import (
	"context"
	"errors"
	"net/http"

	"certharbor/handlers"
)

// profileBody is the wire shape of GET /api/v1/profile and the result of
// every profile PATCH operation. It carries no secret-shaped field
// (PasswordHash, TOTPSecret, TOTPPendingSecret never leave models.User,
// R15, mirroring userBody's comment), and no id in the request that
// produced it: every operation in this file derives its subject from the
// session (appmw.UserFrom via actorUserID), never from a path or body
// parameter (5.5, Section 8 "Profile").
type profileBody struct {
	ID          int    `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName,omitempty"`
	Email       string `json:"email,omitempty"`
	Role        string `json:"role"`
	Theme       string `json:"theme"`
}

func newProfileBody(r *handlers.ProfileRow) profileBody {
	return profileBody{
		ID: r.ID, Username: r.Username, DisplayName: r.DisplayName,
		Email: r.Email, Role: r.Role, Theme: r.Theme,
	}
}

// mapProfileError mirrors mapNotificationsError/mapAcmeSettingsError: a
// caller-input validation failure is a 422, not huma's default 500.
func mapProfileError(err error) error {
	var verr *handlers.ValidationError
	if errors.As(err, &verr) {
		return newProblem(http.StatusUnprocessableEntity, "Unprocessable Entity", problemTypeBase+"validation", err.Error(), "")
	}
	if errors.Is(err, handlers.ErrUserNotFound) {
		// Only reachable if the session's own user row was deleted out
		// from under it between the login that created the session and
		// this request; there is no caller-supplied id to have gotten
		// wrong (5.5).
		return newProblem(http.StatusNotFound, "Not Found", problemTypeBase+"not-found", "user not found", "")
	}
	return err
}

// ─── GET /api/v1/profile ────────────────────────────────────────────────────

type getProfileOutput struct {
	Body profileBody
}

// getProfile implements GET /api/v1/profile, replacing GET /profile
// (Section 8 "Profile"). It takes no input: the subject is the session's
// own user id, never a path or body parameter (5.5).
func getProfile(h *handlers.Handler) func(context.Context, *struct{}) (*getProfileOutput, error) {
	return func(ctx context.Context, _ *struct{}) (*getProfileOutput, error) {
		row, err := h.GetProfile(actorUserID(ctx))
		if err != nil {
			return nil, mapProfileError(err)
		}
		return &getProfileOutput{Body: newProfileBody(row)}, nil
	}
}

// ─── PATCH /api/v1/profile ──────────────────────────────────────────────────

// patchProfileBody's fields are pointers (5.8: absent means unchanged),
// replacing the "update_info" action of POST /profile
// (handlers/users.go:226).
type patchProfileBody struct {
	DisplayName *string `json:"displayName,omitempty" maxLength:"255"`
	Email       *string `json:"email,omitempty" maxLength:"255"`
}

type patchProfileInput struct {
	Body patchProfileBody
}

// patchProfile implements PATCH /api/v1/profile, replacing the
// "update_info" action of POST /profile (Section 6, 5.8). No id: the
// subject is always the acting session's own user (5.5).
func patchProfile(h *handlers.Handler) func(context.Context, *patchProfileInput) (*getProfileOutput, error) {
	return func(ctx context.Context, in *patchProfileInput) (*getProfileOutput, error) {
		row, err := h.UpdateProfile(actorUserID(ctx), &handlers.ProfilePatch{
			DisplayName: in.Body.DisplayName, Email: in.Body.Email,
		})
		if err != nil {
			return nil, mapProfileError(err)
		}
		return &getProfileOutput{Body: newProfileBody(row)}, nil
	}
}

// ─── PATCH /api/v1/profile/preferences ──────────────────────────────────────

// patchProfilePreferencesBody replaces the "theme" action of POST /profile
// (handlers/users.go:212, Section 6, 5.8).
type patchProfilePreferencesBody struct {
	Theme *string `json:"theme,omitempty" enum:"dark,light,blue,auto"`
}

type patchProfilePreferencesInput struct {
	Body patchProfilePreferencesBody
}

// patchProfilePreferences implements PATCH /api/v1/profile/preferences. No
// id: the subject is always the acting session's own user (5.5).
func patchProfilePreferences(h *handlers.Handler) func(context.Context, *patchProfilePreferencesInput) (*getProfileOutput, error) {
	return func(ctx context.Context, in *patchProfilePreferencesInput) (*getProfileOutput, error) {
		row, err := h.UpdateProfilePreferences(actorUserID(ctx), &handlers.ProfilePreferencesPatch{
			Theme: in.Body.Theme,
		})
		if err != nil {
			return nil, mapProfileError(err)
		}
		return &getProfileOutput{Body: newProfileBody(row)}, nil
	}
}

// ─── POST /api/v1/profile/password ──────────────────────────────────────────

// changePasswordBody requires the current password (5.5's reauth column:
// POST /api/v1/profile/password "requires the current password"). Modelled
// as POST rather than PATCH because this mutates a credential, matching
// the plan's reasoning for profile/mfa/disable.
type changePasswordBody struct {
	CurrentPassword string `json:"currentPassword" minLength:"1" maxLength:"1024" writeOnly:"true" doc:"The account's current password, required to authorise this change."`
	NewPassword     string `json:"newPassword" minLength:"1" maxLength:"1024" writeOnly:"true" doc:"The new password to set."`
}

type changePasswordInput struct {
	Body changePasswordBody
}

type changePasswordOutput struct {
	Status int
}

// changePassword implements POST /api/v1/profile/password, replacing the
// "change_password" action of POST /profile (handlers/users.go:247,
// Section 6). No id: the subject is always the acting session's own user
// (5.5). On success it re-stamps the acting session's own session_epoch
// cookie value, so BumpSessionEpoch's revocation of every *other* session
// for this user (V8) does not also log the caller out of the request it is
// standing on.
func changePassword(h *handlers.Handler) func(context.Context, *changePasswordInput) (*changePasswordOutput, error) {
	return func(ctx context.Context, in *changePasswordInput) (*changePasswordOutput, error) {
		id := actorUserID(ctx)
		newEpoch, err := h.ChangePassword(id, in.Body.CurrentPassword, in.Body.NewPassword)
		if err != nil {
			if errors.Is(err, handlers.ErrIncorrectPassword) {
				return nil, newProblem(http.StatusUnauthorized, "Unauthorized", problemTypeBase+"invalid-credentials", "current password is incorrect", "")
			}
			return nil, mapProfileError(err)
		}

		r, w := httpFrom(ctx)
		if s, sErr := h.Store().Get(r, h.SessionCookieName()); sErr == nil {
			s.Values["session_epoch"] = newEpoch
			if saveErr := s.Save(r, w); saveErr != nil {
				return nil, saveErr
			}
		}

		return &changePasswordOutput{Status: http.StatusNoContent}, nil
	}
}
