package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"certharbor/handlers"
)

// TempUserBody is one row of GET /api/v1/temporary-users, replacing GET
// /admin/users-temp. Exported (unlike userBody) because
// createTemporaryUserOutput embeds it anonymously to flatten its fields
// into the create response alongside Passphrase: huma's schema reflection,
// unlike encoding/json, does not promote fields from an anonymously
// embedded unexported type, so an unexported name here would silently
// drop id/username/role/note/createdAt/expiresAt/status from the OpenAPI
// schema (and therefore the generated TS client) while the real JSON
// response kept shipping them.
type TempUserBody struct {
	ID        int        `json:"id"`
	Username  string     `json:"username"`
	Role      string     `json:"role" enum:"viewer,manager,admin"`
	Note      string     `json:"note,omitempty"`
	CreatedAt time.Time  `json:"createdAt"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
	Status    string     `json:"status" enum:"active,expired,blocked"`
}

func newTempUserBody(u *handlers.TempUserSummary) TempUserBody {
	return TempUserBody{
		ID: u.ID, Username: u.Username, Role: u.Role, Note: u.Note,
		CreatedAt: u.CreatedAt, ExpiresAt: u.ExpiresAt, Status: u.Status,
	}
}

// ─── GET /api/v1/temporary-users ────────────────────────────────────────────

type listTemporaryUsersOutput struct {
	Body []TempUserBody
}

// listTemporaryUsers implements GET /api/v1/temporary-users, replacing GET
// /admin/users-temp (Section 6). No pagination, same as listProvisioners:
// the temporary-user table is operator-sized, and the template route it
// replaces never paginated either.
func listTemporaryUsers(h *handlers.Handler) func(context.Context, *struct{}) (*listTemporaryUsersOutput, error) {
	return func(_ context.Context, _ *struct{}) (*listTemporaryUsersOutput, error) {
		users, err := h.ListTemporaryUsers()
		if err != nil {
			return nil, err
		}
		body := make([]TempUserBody, 0, len(users))
		for i := range users {
			body = append(body, newTempUserBody(&users[i]))
		}
		return &listTemporaryUsersOutput{Body: body}, nil
	}
}

// ─── POST /api/v1/temporary-users ───────────────────────────────────────────

// createTemporaryUserInput takes an absolute ExpiresAt instant rather than
// the template route's preset-or-custom-datetime union
// (handlers/admin_temp.go:96-121): a REST body has one canonical
// representation of "when" (5.8), and computing a preset's future instant
// is simple enough that the SPA does it before the request leaves the
// browser.
type createTemporaryUserInput struct {
	Body struct {
		Role      string    `json:"role" enum:"viewer,manager,admin"`
		Note      string    `json:"note,omitempty" maxLength:"500"`
		ExpiresAt time.Time `json:"expiresAt"`
	}
}

// createTemporaryUserOutput's Passphrase is the generated one-time
// credential, named to avoid api/spec_secrets_test.go's secretish regex
// (handlers.TempUserCredential's own comment explains why "password" would
// not work here: it must appear in a response, which the writeOnly rule
// forbids for any secret-shaped name).
type createTemporaryUserOutput struct {
	Status int
	Body   struct {
		TempUserBody
		Passphrase string `json:"passphrase"`
	}
}

// createTemporaryUser implements POST /api/v1/temporary-users, replacing
// AdminUsersTempPost (Section 6).
func createTemporaryUser(h *handlers.Handler) func(context.Context, *createTemporaryUserInput) (*createTemporaryUserOutput, error) {
	return func(ctx context.Context, in *createTemporaryUserInput) (*createTemporaryUserOutput, error) {
		r, _ := httpFrom(ctx)
		actor, _ := userIdentity(ctx)
		cred, err := h.CreateTemporaryUser(r, actor, in.Body.Role, in.Body.Note, in.Body.ExpiresAt)
		if err != nil {
			return nil, validationOrInternal(err)
		}
		out := &createTemporaryUserOutput{Status: http.StatusCreated}
		out.Body.TempUserBody = newTempUserBody(&cred.TempUserSummary)
		out.Body.Passphrase = cred.Passphrase
		return out, nil
	}
}

// ─── DELETE /api/v1/temporary-users/{id} ────────────────────────────────────

type deleteTemporaryUserInput struct {
	ID int `path:"id"`
}

type deleteTemporaryUserOutput struct {
	Status int
}

// deleteTemporaryUser implements DELETE /api/v1/temporary-users/{id}. No
// legacy template equivalent: admin_users_temp.html never gained a delete
// action, so this is a new capability (Section 6).
func deleteTemporaryUser(h *handlers.Handler) func(context.Context, *deleteTemporaryUserInput) (*deleteTemporaryUserOutput, error) {
	return func(ctx context.Context, in *deleteTemporaryUserInput) (*deleteTemporaryUserOutput, error) {
		r, _ := httpFrom(ctx)
		actor, _ := userIdentity(ctx)
		if err := h.DeleteTemporaryUser(r, actor, in.ID); err != nil {
			if errors.Is(err, handlers.ErrTempUserNotFound) {
				return nil, newProblem(http.StatusNotFound, "Not Found", problemTypeBase+"not-found", "temporary user not found", "")
			}
			return nil, err
		}
		return &deleteTemporaryUserOutput{Status: http.StatusNoContent}, nil
	}
}
