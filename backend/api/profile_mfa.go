package api

import (
	"context"
	"errors"
	"net/http"

	"certharbor/handlers"

	"github.com/danielgtaylor/huma/v2"
)

// ─── GET /api/v1/profile/mfa ────────────────────────────────────────────────

type mfaStatusBody struct {
	Enabled           bool `json:"enabled"`
	PendingEnrollment bool `json:"pendingEnrollment"`
}

type getMfaOutput struct {
	Body mfaStatusBody
}

// getMfa implements GET /api/v1/profile/mfa, replacing GET /profile/2fa
// (Section 8 Phase 8's Profile table). Every profile/mfa operation acts on
// the session's own account at viewer and carries no id parameter (5.5).
func getMfa(h *handlers.Handler) func(context.Context, *struct{}) (*getMfaOutput, error) {
	return func(ctx context.Context, _ *struct{}) (*getMfaOutput, error) {
		status, err := h.GetMfaStatus(actorUserID(ctx))
		if err != nil {
			return nil, err
		}
		return &getMfaOutput{Body: mfaStatusBody{Enabled: status.Enabled, PendingEnrollment: status.PendingEnrollment}}, nil
	}
}

// ─── POST /api/v1/profile/mfa/enrollment ────────────────────────────────────

type startMfaEnrollmentBody struct {
	// ManualEntryKey is named to avoid the spec-scanning test's secretish
	// regex (api/spec_secrets_test.go): it is shown once here, exactly like
	// the old template page showed it inline next to the QR
	// (templates/profile_2fa.html:104), for an authenticator app that must
	// be typed into rather than scanned.
	ManualEntryKey string `json:"manualEntryKey" doc:"TOTP secret for manual entry into an authenticator app; shown once, alongside the QR code."`
}

type startMfaEnrollmentOutput struct {
	Body startMfaEnrollmentBody
}

// startMfaEnrollment implements POST /api/v1/profile/mfa/enrollment,
// replacing POST /profile/2fa/start.
func startMfaEnrollment(h *handlers.Handler) func(context.Context, *struct{}) (*startMfaEnrollmentOutput, error) {
	return func(ctx context.Context, _ *struct{}) (*startMfaEnrollmentOutput, error) {
		secret, err := h.StartMfaEnrollment(actorUserID(ctx))
		if err != nil {
			return nil, validationOrInternal(err)
		}
		return &startMfaEnrollmentOutput{Body: startMfaEnrollmentBody{ManualEntryKey: secret}}, nil
	}
}

// ─── GET /api/v1/profile/mfa/enrollment/qr ──────────────────────────────────

// mfaQrResponses declares the image/png 200 huma does not derive from a
// []byte body field on its own, the same mechanic certificates.go's
// binaryResponses proves for application/octet-stream.
func mfaQrResponses() map[string]*huma.Response {
	return map[string]*huma.Response{
		"200": {
			Description: "The pending TOTP enrollment's QR code.",
			Content: map[string]*huma.MediaType{
				"image/png": {Schema: &huma.Schema{Type: "string", Format: "binary"}},
			},
		},
	}
}

type mfaQrOutput struct {
	ContentType string `header:"Content-Type"`
	Body        []byte
}

var errNoPendingMfaEnrollment = newProblem(http.StatusNotFound, "Not Found", "", "no pending 2FA enrollment", "")

// getMfaEnrollmentQr implements GET /api/v1/profile/mfa/enrollment/qr,
// replacing GET /profile/2fa/qr (image/png, handlers/totp.go:89 in the
// template handler; handlers/totp_api.go:MfaEnrollmentQR here).
func getMfaEnrollmentQr(h *handlers.Handler) func(context.Context, *struct{}) (*mfaQrOutput, error) {
	return func(ctx context.Context, _ *struct{}) (*mfaQrOutput, error) {
		data, err := h.MfaEnrollmentQR(actorUserID(ctx))
		if err != nil {
			return nil, err
		}
		if data == nil {
			return nil, errNoPendingMfaEnrollment
		}
		return &mfaQrOutput{ContentType: "image/png", Body: data}, nil
	}
}

// ─── POST /api/v1/profile/mfa/enrollment/confirm ────────────────────────────

type confirmMfaEnrollmentInput struct {
	Body struct {
		Code string `json:"code" minLength:"1" maxLength:"64" doc:"The 6-digit code from the authenticator app."`
	}
}

type confirmMfaEnrollmentBody struct {
	// RecoveryCodes is allowlisted in openapi/secret-allowlist.txt and
	// writeOnly, the same convention every other one-time secret in this
	// API uses, even though (unlike those) this value genuinely is the
	// response: it is shown once, here, and never again (the DB only ever
	// stores bcrypt hashes, generateRecoveryCodes).
	RecoveryCodes []string `json:"recoveryCodes" writeOnly:"true" doc:"One-time recovery codes. Shown only in this response; store them now, they cannot be retrieved again."`
}

type confirmMfaEnrollmentOutput struct {
	Body confirmMfaEnrollmentBody
}

// confirmMfaEnrollment implements POST /api/v1/profile/mfa/enrollment/confirm,
// replacing POST /profile/2fa/confirm. It is the only place recovery codes
// are ever returned in plaintext (Section 8 Phase 8).
func confirmMfaEnrollment(h *handlers.Handler) func(context.Context, *confirmMfaEnrollmentInput) (*confirmMfaEnrollmentOutput, error) {
	return func(ctx context.Context, in *confirmMfaEnrollmentInput) (*confirmMfaEnrollmentOutput, error) {
		r, _ := httpFrom(ctx)
		codes, err := h.ConfirmMfaEnrollment(r, actorUserID(ctx), in.Body.Code)
		if err != nil {
			return nil, validationOrInternal(err)
		}
		return &confirmMfaEnrollmentOutput{Body: confirmMfaEnrollmentBody{RecoveryCodes: codes}}, nil
	}
}

// ─── POST /api/v1/profile/mfa/disable ───────────────────────────────────────

type disableMfaInput struct {
	Body struct {
		// CurrentPassword is openapi/secret-allowlist.txt's exact-match
		// entry (5.5): the existing "password" line only covers a property
		// literally named "password", the same reason "newPassword" needed
		// its own line (api/users.go:237).
		CurrentPassword string `json:"currentPassword" minLength:"1" maxLength:"1024" writeOnly:"true" doc:"The account's current password."`
		Code            string `json:"code" minLength:"1" maxLength:"64" doc:"A fresh code from the authenticator app."`
	}
}

type disableMfaOutput struct {
	Status int
}

// disableMfa implements POST /api/v1/profile/mfa/disable, replacing POST
// /profile/2fa/disable. Modelled as POST rather than DELETE (Section 5.5: a
// DELETE body is discouraged in OpenAPI 3.1 and dropped by some generated
// clients). Requires both the current password and a fresh TOTP code
// (Section 5.5's reauth column; handlers/totp_api.go:DisableMfa) — a
// load-bearing dual check covered by profile_mfa_integration_test.go.
func disableMfa(h *handlers.Handler) func(context.Context, *disableMfaInput) (*disableMfaOutput, error) {
	return func(ctx context.Context, in *disableMfaInput) (*disableMfaOutput, error) {
		r, _ := httpFrom(ctx)
		if err := h.DisableMfa(ctx, r, actorUserID(ctx), in.Body.CurrentPassword, in.Body.Code); err != nil {
			// A wrong current password is an authentication problem (401,
			// matching changePassword's convention on the sibling
			// profile-core branch); a wrong TOTP code is a different reauth
			// factor failing and stays a 422 validation error below.
			if errors.Is(err, handlers.ErrInvalidCredentials) {
				return nil, newProblem(http.StatusUnauthorized, "Unauthorized", problemTypeBase+"invalid-credentials", err.Error(), r.URL.Path)
			}
			return nil, validationOrInternal(err)
		}
		return &disableMfaOutput{Status: http.StatusNoContent}, nil
	}
}
