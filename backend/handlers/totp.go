package handlers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"strings"
	"time"

	"certharbor/security"

	"github.com/pquerna/otp/totp"
)

const (
	totpIssuer     = "Certharbor"
	totpPendingTTL = 5 * time.Minute
)

func generateRecoveryCodes(n int) ([]string, []string) {
	codes := make([]string, 0, n)
	hashes := make([]string, 0, n)
	for i := 0; i < n; i++ {
		raw := make([]byte, 9)
		_, _ = rand.Read(raw)
		hexed := strings.ToUpper(hex.EncodeToString(raw))
		code := fmt.Sprintf("%s-%s-%s", hexed[:6], hexed[6:12], hexed[12:18])
		codes = append(codes, code)
		hashes = append(hashes, security.HashPassword(code))
	}
	return codes, hashes
}

func (h *Handler) verifyRecoveryCode(userID int, code string) bool {
	code = strings.ToUpper(strings.TrimSpace(code))
	if code == "" {
		return false
	}
	hashes, err := h.authStore().UnusedRecoveryCodes(userID)
	if err != nil {
		return false
	}
	for id, hash := range hashes {
		if security.VerifyPassword(code, hash) {
			_ = h.authStore().UseRecoveryCode(id)
			return true
		}
	}
	return false
}

// validateTOTPWithReplayCtx validates the TOTP code and rejects replay attacks
// by tracking the last accepted timestep per user.  The TOTP period is 30 s,
// so the current step is floor(unix / 30).  A code is only accepted when the
// current timestep is strictly greater than the last accepted one, preventing
// the same physical code from being used a second time within its validity
// window.
func (h *Handler) validateTOTPWithReplayCtx(ctx context.Context, userID int, secret, code string) bool {
	if !totp.Validate(code, secret) {
		return false
	}
	currentStep := time.Now().Unix() / 30
	lastStep, err := h.authStore().TOTPLastStep(ctx, userID)
	if err != nil {
		// Fail open on DB read error, we already validated the code, and
		// blocking valid users on a DB glitch is worse than the narrow replay
		// window left open.  The error is logged for operator visibility.
		log.Printf("[warn] TOTP: GetTOTPLastStep user=%d err=%T", userID, err) //nolint:gosec // G706: userID is int, err type is logged without value
		return true
	}
	if currentStep <= lastStep {
		log.Printf("[warn] TOTP replay rejected for user=%d", userID) //nolint:gosec // G706: only int userID logged
		return false
	}
	if err := h.authStore().SetTOTPLastStep(ctx, userID, currentStep); err != nil {
		// Write failure: accept the code but log, the step was valid and
		// the worst-case outcome is a single replay opportunity before the
		// window expires.
		log.Printf("[warn] TOTP: SetTOTPLastStep user=%d err=%T", userID, err) //nolint:gosec // G706: only int userID and error type logged
	}
	return true
}
