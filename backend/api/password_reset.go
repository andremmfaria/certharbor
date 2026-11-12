package api

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"certharbor/handlers"
)

// Phase 8 (plans/frontend-backend-split.md Section 8 "Phase 8. Profile and
// MFA", Section 6): the three public password-reset operations replacing the
// template's GET/POST /forgot-password and GET/POST /reset-password. All
// three need no role, carry the "auth" rate-limit scope (the same bucket
// createSession and submitMfa already share), and are exempt from the SPA's
// scoped 401 interceptor (frontend/src/api/authEndpoints.ts).

// ─── POST /api/v1/password-reset/requests ──────────────────────────────────

type requestPasswordResetBody struct {
	Email string `json:"email" minLength:"1" maxLength:"255" doc:"Account email or username."`
}

type requestPasswordResetInput struct {
	Body requestPasswordResetBody
}

type requestPasswordResetOutput struct {
	Status int
}

// requestPasswordReset implements POST /api/v1/password-reset/requests,
// replacing POST /forgot-password. It always answers 202 with no body,
// regardless of whether the submitted email corresponds to an account: the
// handler call below runs unconditionally and its result is never inspected,
// so there is no branch here that could answer differently depending on
// account existence (no enumeration via status code or response shape).
func requestPasswordReset(h *handlers.Handler) func(context.Context, *requestPasswordResetInput) (*requestPasswordResetOutput, error) {
	return func(ctx context.Context, in *requestPasswordResetInput) (*requestPasswordResetOutput, error) {
		r, _ := httpFrom(ctx)
		h.RequestPasswordReset(r, strings.TrimSpace(in.Body.Email))
		return &requestPasswordResetOutput{Status: http.StatusAccepted}, nil
	}
}

// ─── POST /api/v1/password-reset/validate ──────────────────────────────────

// validatePasswordResetBody's Token travels in the request body rather than
// as a path parameter or query string: a token in a path segment lands in
// the "instance" field of every problem document this operation could ever
// return, in route-templated metrics, and in trace labels, each of which
// would leak a live, unconsumed reset token into logs and dashboards no
// caller intended it to reach.
type validatePasswordResetBody struct {
	Token string `json:"token" minLength:"1" writeOnly:"true" doc:"The raw password reset token from the emailed link."`
}

type validatePasswordResetInput struct {
	Body validatePasswordResetBody
}

type validatePasswordResetOutput struct {
	Status int
}

// validatePasswordReset implements POST /api/v1/password-reset/validate,
// replacing GET /reset-password?token=.... It validates without consuming
// the token (handlers.Handler.ValidatePasswordResetToken, mirroring
// passwordResetTokenOK's existing behaviour), so a link scanner or mail
// prefetcher hitting this operation does not burn the token before the real
// recipient opens it.
func validatePasswordReset(h *handlers.Handler) func(context.Context, *validatePasswordResetInput) (*validatePasswordResetOutput, error) {
	return func(_ context.Context, in *validatePasswordResetInput) (*validatePasswordResetOutput, error) {
		if err := h.ValidatePasswordResetToken(strings.TrimSpace(in.Body.Token)); err != nil {
			return nil, newProblem(http.StatusUnprocessableEntity, "Unprocessable Entity",
				problemTypeBase+"password-reset-token-invalid",
				"This reset link is invalid or has expired. Please request a new one.", "")
		}
		return &validatePasswordResetOutput{Status: http.StatusNoContent}, nil
	}
}

// ─── POST /api/v1/password-reset/confirm ───────────────────────────────────

// confirmPasswordResetBody's NewPassword is a new writeOnly entry
// (openapi/secret-allowlist.txt), the exact property name "newPassword"
// already reserved alongside resetUserPassword's identical field. Token
// shares requestPasswordReset's body-not-path rationale above.
type confirmPasswordResetBody struct {
	Token       string `json:"token" minLength:"1" writeOnly:"true" doc:"The raw password reset token from the emailed link."`
	NewPassword string `json:"newPassword" minLength:"1" writeOnly:"true" doc:"The new account password."`
}

type confirmPasswordResetInput struct {
	Body confirmPasswordResetBody
}

type confirmPasswordResetOutput struct {
	Status int
}

// confirmPasswordReset implements POST /api/v1/password-reset/confirm,
// replacing POST /reset-password. Consumes the token (single-use) and bumps
// the target user's session epoch, same as the template handler.
func confirmPasswordReset(h *handlers.Handler) func(context.Context, *confirmPasswordResetInput) (*confirmPasswordResetOutput, error) {
	return func(ctx context.Context, in *confirmPasswordResetInput) (*confirmPasswordResetOutput, error) {
		r, _ := httpFrom(ctx)
		err := h.ConfirmPasswordReset(r, strings.TrimSpace(in.Body.Token), in.Body.NewPassword)
		if err != nil {
			if errors.Is(err, handlers.ErrPasswordResetTokenInvalid) {
				return nil, newProblem(http.StatusUnprocessableEntity, "Unprocessable Entity",
					problemTypeBase+"password-reset-token-invalid",
					"This reset link is invalid or has expired. Please request a new one.", "")
			}
			return nil, validationOrInternal(err)
		}
		return &confirmPasswordResetOutput{Status: http.StatusNoContent}, nil
	}
}
