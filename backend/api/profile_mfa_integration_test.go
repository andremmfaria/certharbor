//go:build integration

package api_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
)

// mfaTestPassword is openCertTestDB's own CERTHARBOR_ADMIN_PASSWORD value:
// InitSchema seeds exactly one admin row on an empty users table, landing on
// id=1 (RESTART IDENTITY), which is the same id testUser.ID (mount_test.go)
// and the session/UserLoader seam here authenticate as. So the account
// profile/mfa's viewer operations act on, for this test, is that seeded
// admin — not a second user this test would otherwise need to insert.
const mfaTestPassword = "IntegrationTest123!"

// enableMfaFor starts and confirms an enrollment for the seeded user via the
// handlers package directly isn't available from api_test (no DB access to
// h), so this drives it the same way the frontend would: through the HTTP
// API, returning the TOTP secret so the test can compute fresh codes later.
func enableMfaFor(t *testing.T, client *http.Client, srv string, token string) string {
	t.Helper()
	startResp := doRequest(t, client, http.MethodPost, srv+"/api/v1/profile/mfa/enrollment", nil, func(r *http.Request) { //nolint:bodyclose // closed via t.Cleanup inside doRequest
		r.Header.Set("X-CSRF-Token", token)
	})
	if startResp.StatusCode != http.StatusCreated {
		t.Fatalf("start enrollment: status = %d, want 201", startResp.StatusCode)
	}
	startBody := decodeJSONBody(t, startResp)
	secret, _ := startBody["manualEntryKey"].(string)
	if secret == "" {
		t.Fatal("start enrollment: manualEntryKey is empty")
	}

	code, err := totp.GenerateCode(secret, time.Now())
	if err != nil {
		t.Fatalf("totp.GenerateCode: %v", err)
	}
	confirmReq := `{"code":"` + code + `"}`
	confirmResp := doRequest(t, client, http.MethodPost, srv+"/api/v1/profile/mfa/enrollment/confirm", strings.NewReader(confirmReq), func(r *http.Request) { //nolint:bodyclose // closed via t.Cleanup inside doRequest
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", token)
	})
	if confirmResp.StatusCode != http.StatusOK {
		t.Fatalf("confirm enrollment: status = %d, want 200", confirmResp.StatusCode)
	}
	confirmBody := decodeJSONBody(t, confirmResp)
	codes, _ := confirmBody["recoveryCodes"].([]any)
	if len(codes) == 0 {
		t.Fatal("confirm enrollment: recoveryCodes is empty, want 8 one-time codes")
	}
	return secret
}

// TestDisableMfa_RequiresBothCurrentPasswordAndFreshCode is the load-bearing
// test for the dual-reauth requirement called out in totp_api.go's
// DisableMfa doc comment: a stolen session cookie alone must not be able to
// turn off the second factor. It exercises all three cells of the 2x2 that
// matters (password-only, code-only, both) against one real enrolled user.
func TestDisableMfa_RequiresBothCurrentPasswordAndFreshCode(t *testing.T) {
	const password = mfaTestPassword
	db := openCertTestDB(t)
	srv := newCertTestServer(t, db)
	client := newClient(t)
	token := login(t, client, srv)
	secret := enableMfaFor(t, client, srv.URL, token)

	freshCode := func() string {
		code, err := totp.GenerateCode(secret, time.Now())
		if err != nil {
			t.Fatalf("totp.GenerateCode: %v", err)
		}
		return code
	}

	// Wrong password, correct code: must fail with 401 invalid-credentials,
	// not 422 — it is the credential check that failed, not an input
	// validation problem (api/profile_mfa.go's disableMfa).
	wrongPasswordBody := `{"currentPassword":"not-the-password","code":"` + freshCode() + `"}`
	wrongPasswordResp := doRequest(t, client, http.MethodPost, srv.URL+"/api/v1/profile/mfa/disable", strings.NewReader(wrongPasswordBody), func(r *http.Request) { //nolint:bodyclose // closed via t.Cleanup inside doRequest
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", token)
	})
	if wrongPasswordResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong password: status = %d, want 401", wrongPasswordResp.StatusCode)
	}

	// Correct password, wrong code: must fail with 422, a different reauth
	// factor failing rather than the same credential.
	wrongCodeBody := `{"currentPassword":"` + password + `","code":"000000"}`
	wrongCodeResp := doRequest(t, client, http.MethodPost, srv.URL+"/api/v1/profile/mfa/disable", strings.NewReader(wrongCodeBody), func(r *http.Request) { //nolint:bodyclose // closed via t.Cleanup inside doRequest
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", token)
	})
	if wrongCodeResp.StatusCode != http.StatusUnprocessableEntity {
		dbg := decodeJSONBody(t, wrongCodeResp)
		t.Fatalf("wrong code: status = %d, want 422, body = %v", wrongCodeResp.StatusCode, dbg)
	}

	// Both correct: 204, MFA actually disabled.
	bothCorrectBody := `{"currentPassword":"` + password + `","code":"` + freshCode() + `"}`
	bothCorrectResp := doRequest(t, client, http.MethodPost, srv.URL+"/api/v1/profile/mfa/disable", strings.NewReader(bothCorrectBody), func(r *http.Request) { //nolint:bodyclose // closed via t.Cleanup inside doRequest
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", token)
	})
	if bothCorrectResp.StatusCode != http.StatusNoContent {
		t.Fatalf("both correct: status = %d, want 204", bothCorrectResp.StatusCode)
	}

	statusResp := doGet(t, client, srv.URL+"/api/v1/profile/mfa")
	if statusResp.body["enabled"] != false {
		t.Fatalf("after disable: enabled = %v, want false", statusResp.body["enabled"])
	}
}
