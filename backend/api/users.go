package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"certharbor/apitypes"
	"certharbor/handlers"
	appmw "certharbor/middleware"
)

// actorUserID recovers the acting user's id, for the self-target checks
// updateUser and deleteUser both need (handlers.ErrSelfTarget). It mirrors
// userIdentity (certificates.go) rather than extending it, because that
// function's two callers (certificate history, UnblockIP's audit entry)
// need no id and depgaurd's allowlist is easiest to keep honest when each
// accessor reads only what its callers use.
func actorUserID(ctx context.Context) int {
	user, ok := appmw.UserFrom(ctx)
	if !ok || user == nil {
		return 0
	}
	return user.ID
}

// userBody is one row of GET /api/v1/users and the result of every user
// mutation. It carries no secret-shaped field: PasswordHash, TOTPSecret and
// TOTPPendingSecret never leave handlers.UserSummary, which never leaves
// models.User (R15).
type userBody struct {
	ID        int        `json:"id"`
	Username  string     `json:"username"`
	Role      string     `json:"role" enum:"viewer,manager,admin"`
	IsActive  bool       `json:"isActive"`
	CreatedAt *time.Time `json:"createdAt,omitempty"`
	LastLogin *time.Time `json:"lastLogin,omitempty"`
	LastIP    string     `json:"lastIp,omitempty"`
	FailCount int        `json:"failCount24h"`
}

func newUserBody(u *handlers.UserSummary) userBody {
	b := userBody{
		ID: u.ID, Username: u.Username, Role: u.Role, IsActive: u.IsActive,
		CreatedAt: u.CreatedAt, LastLogin: u.LastLogin, FailCount: u.FailCount,
	}
	if u.LastIP != nil {
		b.LastIP = *u.LastIP
	}
	return b
}

// userDetailBody is the body of GET /api/v1/users/{id}, adding the fields
// Section 6's endpoint map calls out explicitly (lastIp, ipBlocked) plus
// the profile fields the template's admin_user_profile page also shows.
type userDetailBody struct {
	userBody
	DisplayName string `json:"displayName,omitempty"`
	Email       string `json:"email,omitempty"`
	Theme       string `json:"theme,omitempty"`
	TOTPEnabled bool   `json:"totpEnabled"`
	IPBlocked   bool   `json:"ipBlocked"`
}

func newUserDetailBody(u *handlers.UserDetail) userDetailBody {
	return userDetailBody{
		userBody: newUserBody(&u.UserSummary), DisplayName: u.DisplayName,
		Email: u.Email, Theme: u.Theme, TOTPEnabled: u.TOTPEnabled, IPBlocked: u.IPBlocked,
	}
}

// ─── GET /api/v1/users ──────────────────────────────────────────────────────

type listUsersInput struct {
	Q        string `query:"q" maxLength:"255" doc:"Filter by a case-insensitive username substring."`
	Page     int    `query:"page" minimum:"1" default:"1"`
	PageSize int    `query:"pageSize" minimum:"1" maximum:"200" default:"25"`
}

type listUsersOutput struct {
	Body apitypes.Page[userBody]
}

// listUsers implements GET /api/v1/users, replacing GET /admin/users
// (Section 6).
func listUsers(h *handlers.Handler) func(context.Context, *listUsersInput) (*listUsersOutput, error) {
	return func(_ context.Context, in *listUsersInput) (*listUsersOutput, error) {
		page, pageSize := in.Page, in.PageSize
		if page < 1 {
			page = 1
		}
		if pageSize < 1 {
			pageSize = apitypes.DefaultPageSize
		}
		users, total, err := h.ListUsers(in.Q, page, pageSize)
		if err != nil {
			return nil, err
		}
		items := make([]userBody, 0, len(users))
		for i := range users {
			items = append(items, newUserBody(&users[i]))
		}
		return &listUsersOutput{Body: apitypes.NewPage(items, page, pageSize, total)}, nil
	}
}

// ─── GET /api/v1/users/{id} ─────────────────────────────────────────────────

type getUserInput struct {
	ID int `path:"id"`
}

type getUserOutput struct {
	Body userDetailBody
}

// getUser implements GET /api/v1/users/{id}, replacing GET
// /admin/users/{id} (Section 6).
func getUser(h *handlers.Handler) func(context.Context, *getUserInput) (*getUserOutput, error) {
	return func(_ context.Context, in *getUserInput) (*getUserOutput, error) {
		detail, err := h.GetUser(in.ID)
		if err != nil {
			if errors.Is(err, handlers.ErrUserNotFound) {
				return nil, newProblem(http.StatusNotFound, "Not Found", problemTypeBase+"not-found", "user not found", "")
			}
			return nil, err
		}
		return &getUserOutput{Body: newUserDetailBody(detail)}, nil
	}
}

// ─── POST /api/v1/users ─────────────────────────────────────────────────────

// createUserInput's Password carries the writeOnly allowlist entry
// openapi/secret-allowlist.txt already reserves for the login and
// create-user request bodies (R15).
type createUserInput struct {
	Body struct {
		Username string `json:"username" minLength:"1" maxLength:"255"`
		Password string `json:"password" minLength:"1" writeOnly:"true"`
		Role     string `json:"role" enum:"viewer,manager,admin"`
	}
}

type createUserOutput struct {
	Body userBody
}

// createUser implements POST /api/v1/users, replacing the "create" action
// of POST /admin/users (handlers/users.go:43, Section 6).
func createUser(h *handlers.Handler) func(context.Context, *createUserInput) (*createUserOutput, error) {
	return func(ctx context.Context, in *createUserInput) (*createUserOutput, error) {
		r, _ := httpFrom(ctx)
		actor, _ := userIdentity(ctx)
		u, err := h.CreateUserAdmin(r, actor, in.Body.Username, in.Body.Password, in.Body.Role)
		if err != nil {
			return nil, validationOrInternal(err)
		}
		return &createUserOutput{Body: newUserBody(u)}, nil
	}
}

// ─── PATCH /api/v1/users/{id} ───────────────────────────────────────────────

// updateUserInput covers both the "change_role" (handlers/users.go:84) and
// "toggle_active" (handlers/users.go:107) actions of POST /admin/users,
// folded into one operation per Section 6 ("PATCH /api/v1/users/{id}
// (covering role and active)"). Either field may be omitted to leave it
// unchanged.
type updateUserInput struct {
	ID   int `path:"id"`
	Body struct {
		Role     *string `json:"role,omitempty" enum:"viewer,manager,admin"`
		IsActive *bool   `json:"isActive,omitempty"`
	}
}

type updateUserOutput struct {
	Body userBody
}

// updateUser implements PATCH /api/v1/users/{id}. It rejects a target equal
// to the acting user unconditionally (handlers.ErrSelfTarget): otherwise
// the last admin could demote or deactivate themselves and leave the
// installation with no admin (Section 5.5, Section 8 Phase 6).
func updateUser(h *handlers.Handler) func(context.Context, *updateUserInput) (*updateUserOutput, error) {
	return func(ctx context.Context, in *updateUserInput) (*updateUserOutput, error) {
		r, _ := httpFrom(ctx)
		actor, _ := userIdentity(ctx)
		actorID := actorUserID(ctx)
		u, err := h.UpdateUserAdmin(r, actor, actorID, in.ID, in.Body.Role, in.Body.IsActive)
		if err != nil {
			if errors.Is(err, handlers.ErrSelfTarget) {
				return nil, newProblem(http.StatusForbidden, "Forbidden", problemTypeBase+"self-target", "cannot target your own account", "")
			}
			if errors.Is(err, handlers.ErrUserNotFound) {
				return nil, newProblem(http.StatusNotFound, "Not Found", problemTypeBase+"not-found", "user not found", "")
			}
			return nil, validationOrInternal(err)
		}
		return &updateUserOutput{Body: newUserBody(u)}, nil
	}
}

// ─── DELETE /api/v1/users/{id} ──────────────────────────────────────────────

type deleteUserInput struct {
	ID int `path:"id"`
}

type deleteUserOutput struct {
	Status int
}

// deleteUser implements DELETE /api/v1/users/{id}, replacing the "delete"
// action of POST /admin/users (handlers/users.go:66, Section 6). Same
// self-target rejection as updateUser, for the same reason.
func deleteUser(h *handlers.Handler) func(context.Context, *deleteUserInput) (*deleteUserOutput, error) {
	return func(ctx context.Context, in *deleteUserInput) (*deleteUserOutput, error) {
		r, _ := httpFrom(ctx)
		actor, _ := userIdentity(ctx)
		actorID := actorUserID(ctx)
		if err := h.DeleteUserAdmin(r, actor, actorID, in.ID); err != nil {
			if errors.Is(err, handlers.ErrSelfTarget) {
				return nil, newProblem(http.StatusForbidden, "Forbidden", problemTypeBase+"self-target", "cannot target your own account", "")
			}
			return nil, err
		}
		return &deleteUserOutput{Status: http.StatusNoContent}, nil
	}
}

// ─── POST /api/v1/users/{id}/password-reset ─────────────────────────────────

// resetUserPasswordInput's NewPassword is a new entry in
// openapi/secret-allowlist.txt: the exact property name "newPassword" is
// what the allowlist's exact-match lookup requires, "password" alone does
// not cover it.
type resetUserPasswordInput struct {
	ID   int `path:"id"`
	Body struct {
		NewPassword string `json:"newPassword" minLength:"1" writeOnly:"true"`
	}
}

type resetUserPasswordOutput struct {
	Body userBody
}

// resetUserPassword implements POST /api/v1/users/{id}/password-reset,
// replacing the "reset_password" action of POST /admin/users
// (handlers/users.go:134, Section 6). Bumps the target's session epoch, so
// every session already issued to them ends immediately.
func resetUserPassword(h *handlers.Handler) func(context.Context, *resetUserPasswordInput) (*resetUserPasswordOutput, error) {
	return func(ctx context.Context, in *resetUserPasswordInput) (*resetUserPasswordOutput, error) {
		r, _ := httpFrom(ctx)
		actor, _ := userIdentity(ctx)
		u, err := h.ResetUserPasswordAdmin(r, actor, in.ID, in.Body.NewPassword)
		if err != nil {
			if errors.Is(err, handlers.ErrUserNotFound) {
				return nil, newProblem(http.StatusNotFound, "Not Found", problemTypeBase+"not-found", "user not found", "")
			}
			return nil, validationOrInternal(err)
		}
		return &resetUserPasswordOutput{Body: newUserBody(u)}, nil
	}
}

// validationOrInternal maps a handlers.ValidationError to 422 (the caller's
// input was the problem) and lets everything else surface as a plain error,
// which huma's router turns into a 500 (handlers/errors.go's doc comment).
func validationOrInternal(err error) error {
	var ve *handlers.ValidationError
	if errors.As(err, &ve) {
		return newProblem(http.StatusUnprocessableEntity, "Unprocessable Entity", problemTypeBase+"validation", ve.Error(), "")
	}
	return err
}
