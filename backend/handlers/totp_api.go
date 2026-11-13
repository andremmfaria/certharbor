package handlers

import (
	"bytes"
	"context"
	"encoding/base32"
	"image/png"
	"net/http"
	"strings"

	appdb "certharbor/db"
	"certharbor/security"

	"github.com/pquerna/otp/totp"
)

// MfaStatus is the data half of GET /api/v1/profile/mfa, replacing GET
// /profile/2fa (handlers/totp.go:26, Section 8 Phase 8).
type MfaStatus struct {
	Enabled           bool
	PendingEnrollment bool
}

// GetMfaStatus implements the data half of GET /api/v1/profile/mfa.
func (h *Handler) GetMfaStatus(userID int) (*MfaStatus, error) {
	u, err := appdb.GetUserByID(h.db, userID)
	if err != nil {
		return nil, err
	}
	if u == nil {
		return nil, ErrUserNotFound
	}
	return &MfaStatus{
		Enabled:           u.TOTPEnabled,
		PendingEnrollment: !u.TOTPEnabled && u.TOTPPendingSecret != "",
	}, nil
}

// StartMfaEnrollment is the data half of POST /api/v1/profile/mfa/enrollment,
// replacing POST /profile/2fa/start (handlers/totp.go:34-65). It generates a
// fresh TOTP secret and parks it as pending, returning the manual-entry key
// once: the old template page showed the same value inline next to the QR
// (templates/profile_2fa.html:104) for an authenticator that cannot scan.
func (h *Handler) StartMfaEnrollment(userID int) (string, error) {
	u, err := appdb.GetUserByID(h.db, userID)
	if err != nil {
		return "", err
	}
	if u == nil {
		return "", ErrUserNotFound
	}
	if u.TOTPEnabled {
		return "", NewValidationError("2FA is already enabled")
	}
	key, err := totp.Generate(totp.GenerateOpts{Issuer: totpIssuer, AccountName: u.Username})
	if err != nil {
		return "", err
	}
	if err := appdb.UpdateUserTOTPPending(h.db, u.ID, key.Secret()); err != nil {
		return "", err
	}
	return key.Secret(), nil
}

// buildTOTPQRPNG renders a TOTP QR code as a PNG, pure of any database
// access so it can be unit tested without the integration DB (R23: the one
// genuinely new piece of logic in this batch, the rest is extraction).
//
// secret is the base32 string already stored in TOTPPendingSecret/TOTPSecret
// (itself the output of a prior totp.Generate's key.Secret()). GenerateOpts.Secret
// takes raw bytes and base32-encodes them itself (pquerna/otp/totp.go), so
// passing the base32 string's bytes directly double-encodes it: the QR then
// carries a different secret than the one ConfirmMfaEnrollment/APIVerifyMFA
// validate against, and no authenticator app scanning it will ever produce a
// matching code. Decoding back to raw bytes first keeps the QR's secret
// identical to the one already persisted and verified against.
func buildTOTPQRPNG(accountName, secret string) ([]byte, error) {
	raw, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	if err != nil {
		return nil, err
	}
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer: totpIssuer, AccountName: accountName, Secret: raw,
	})
	if err != nil {
		return nil, err
	}
	img, err := key.Image(220, 220)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// MfaEnrollmentQR is the data half of GET /api/v1/profile/mfa/enrollment/qr,
// replacing GET /profile/2fa/qr (handlers/totp.go:68-91). It returns nil,
// nil when there is no pending enrollment to render a QR for, letting the
// api layer turn that into a 404 the same way the template handler did.
func (h *Handler) MfaEnrollmentQR(userID int) ([]byte, error) {
	u, err := appdb.GetUserByID(h.db, userID)
	if err != nil {
		return nil, err
	}
	if u == nil || u.TOTPPendingSecret == "" || u.TOTPEnabled {
		return nil, nil
	}
	img, err := buildTOTPQRPNG(u.Username, u.TOTPPendingSecret)
	if err != nil {
		return nil, nil //nolint:nilerr // mirrors the template handler: a bad secret 404s, it is not a server error
	}
	return img, nil
}

// ConfirmMfaEnrollment is the data half of POST
// /api/v1/profile/mfa/enrollment/confirm, replacing POST /profile/2fa/confirm
// (handlers/totp.go:94-133). Recovery codes are generated and returned in
// plaintext exactly once, here: everywhere else only their bcrypt hashes
// exist (ReplaceRecoveryCodes), matching the old template's single render of
// data["RecoveryCodes"] on a successful confirm.
func (h *Handler) ConfirmMfaEnrollment(r *http.Request, userID int, code string) ([]string, error) {
	u, err := appdb.GetUserByID(h.db, userID)
	if err != nil {
		return nil, err
	}
	if u == nil || u.TOTPPendingSecret == "" || u.TOTPEnabled {
		return nil, NewValidationError("no active 2FA setup found")
	}
	if !totp.Validate(strings.TrimSpace(code), u.TOTPPendingSecret) {
		return nil, NewValidationError("invalid TOTP code")
	}
	recoveryCodes, hashes := generateRecoveryCodes(8)
	if err := appdb.EnableUserTOTP(h.db, u.ID, u.TOTPPendingSecret); err != nil {
		return nil, err
	}
	if err := appdb.ReplaceRecoveryCodes(h.db, u.ID, hashes); err != nil {
		return nil, err
	}
	_ = appdb.LogAuth(h.db, u.Username, clientIP(r), true, "2FA enabled")
	return recoveryCodes, nil
}

// DisableMfa is the data half of POST /api/v1/profile/mfa/disable, replacing
// POST /profile/2fa/disable (handlers/totp.go:136-166). It is modelled as
// POST rather than DELETE because a DELETE body is discouraged in OpenAPI
// 3.1 and dropped by some generated clients (Section 5.5), and requires
// both the current password and a fresh (non-replayed) TOTP code: disabling
// the second factor is a privileged action an already-stolen session
// cookie alone must not be able to perform.
func (h *Handler) DisableMfa(ctx context.Context, r *http.Request, userID int, password, code string) error {
	u, err := appdb.GetUserByID(h.db, userID)
	if err != nil {
		return err
	}
	if u == nil || !u.TOTPEnabled {
		return NewValidationError("2FA is not enabled")
	}
	if !security.VerifyPassword(password, u.PasswordHash) {
		return ErrInvalidCredentials
	}
	if !h.validateTOTPWithReplayCtx(ctx, u.ID, u.TOTPSecret, strings.TrimSpace(code)) {
		return NewValidationError("invalid TOTP code")
	}
	if err := appdb.DisableUserTOTP(h.db, u.ID); err != nil {
		return err
	}
	_ = appdb.LogAuth(h.db, u.Username, clientIP(r), true, "2FA disabled")
	return nil
}
