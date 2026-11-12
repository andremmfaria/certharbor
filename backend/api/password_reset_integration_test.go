//go:build integration

package api_test

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	appdb "certharbor/db"
	"certharbor/security"
)

// Phase 8 (plans/frontend-backend-split.md Section 8 "Phase 8. Profile and
// MFA", Section 6): the three public password-reset operations. Needs a real
// database the same way certificates_integration_test.go's list/detail suite
// does, so this reuses openCertTestDB/newCertTestServer from that file
// rather than duplicating the DSN/InitSchema/truncate plumbing.

func pwResetTokenHash(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func pwResetCreateUser(t *testing.T, conn *sql.DB, username, email, password string) int {
	t.Helper()
	if err := appdb.CreateUser(conn, username, security.HashPassword(password), "viewer"); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	u, err := appdb.GetUserByUsername(conn, username)
	if err != nil || u == nil {
		t.Fatalf("GetUserByUsername(%q): %v", username, err)
	}
	if email != "" {
		if err := appdb.UpdateUserInfo(conn, u.ID, "Test User", email); err != nil {
			t.Fatalf("UpdateUserInfo: %v", err)
		}
	}
	return u.ID
}

// pwResetCSRF primes the anonymous CSRF pair the way the SPA's boot call
// does, mirroring loginFixture.csrf (login_test.go) for a plain client/srv
// pair rather than a loginFixture.
func pwResetCSRF(t *testing.T, client *http.Client, srvURL string) string {
	t.Helper()
	res := doRequest(t, client, http.MethodGet, srvURL+"/api/v1/session", nil, nil) //nolint:bodyclose // closed via t.Cleanup inside doRequest
	for _, c := range res.Cookies() {
		if c.Name == "certharbor-csrf" {
			return c.Value
		}
	}
	t.Fatal("GET /api/v1/session did not mint a CSRF cookie")
	return ""
}

func pwResetPost(t *testing.T, client *http.Client, srvURL, path, csrf string, body map[string]any) (int, []byte) {
	t.Helper()
	buf, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	resp := doRequest(t, client, http.MethodPost, srvURL+path, bytes.NewReader(buf), func(r *http.Request) { //nolint:bodyclose // closed via t.Cleanup inside doRequest
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", csrf)
	})
	raw := make([]byte, 0)
	buf2 := new(bytes.Buffer)
	_, _ = buf2.ReadFrom(resp.Body)
	raw = buf2.Bytes()
	return resp.StatusCode, raw
}

// TestRequestPasswordReset_NoEnumeration is the invariant Section 8 Phase 8
// states explicitly: POST /api/v1/password-reset/requests must answer the
// same status and same-shaped body whether the submitted email exists or
// not, across both a found-and-active account and an unknown address, so a
// caller cannot learn account existence from the response.
func TestRequestPasswordReset_NoEnumeration(t *testing.T) {
	conn := openCertTestDB(t)
	srv := newCertTestServer(t, conn)
	defer srv.Close()

	username := fmt.Sprintf("pwreset_user_%d", time.Now().UnixNano())
	email := fmt.Sprintf("%s@example.com", username)
	pwResetCreateUser(t, conn, username, email, "Pw1!Pw1!Pw1!")

	client := newClient(t)
	csrf := pwResetCSRF(t, client, srv.URL)

	statusKnown, bodyKnown := pwResetPost(t, client, srv.URL, "/api/v1/password-reset/requests", csrf,
		map[string]any{"email": email})
	statusUnknown, bodyUnknown := pwResetPost(t, client, srv.URL, "/api/v1/password-reset/requests", csrf,
		map[string]any{"email": "definitely-not-a-real-account@example.invalid"})

	if statusKnown != http.StatusAccepted {
		t.Fatalf("known-account status = %d, want 202", statusKnown)
	}
	if statusUnknown != http.StatusAccepted {
		t.Fatalf("unknown-account status = %d, want 202 (enumeration via status code)", statusUnknown)
	}
	if string(bodyKnown) != string(bodyUnknown) {
		t.Fatalf("response bodies differ by account existence: known=%q unknown=%q (enumeration via body shape)", bodyKnown, bodyUnknown)
	}
}

// TestRequestPasswordReset_InactiveAndNoEmail extends the no-enumeration
// invariant to the two other silent-skip branches inside
// handlers.RequestPasswordReset (inactive account, account with no email):
// each must still answer 202 with the same empty body as the happy path.
func TestRequestPasswordReset_InactiveAndNoEmail(t *testing.T) {
	conn := openCertTestDB(t)
	srv := newCertTestServer(t, conn)
	defer srv.Close()

	inactiveUser := fmt.Sprintf("pwreset_inactive_%d", time.Now().UnixNano())
	inactiveEmail := fmt.Sprintf("%s@example.com", inactiveUser)
	inactiveID := pwResetCreateUser(t, conn, inactiveUser, inactiveEmail, "Pw1!Pw1!Pw1!")
	if err := appdb.UpdateUserActive(conn, inactiveID, false); err != nil {
		t.Fatalf("UpdateUserActive: %v", err)
	}

	noEmailUser := fmt.Sprintf("pwreset_noemail_%d", time.Now().UnixNano())
	pwResetCreateUser(t, conn, noEmailUser, "", "Pw1!Pw1!Pw1!")

	client := newClient(t)
	csrf := pwResetCSRF(t, client, srv.URL)

	for _, identifier := range []string{inactiveEmail, noEmailUser} {
		status, body := pwResetPost(t, client, srv.URL, "/api/v1/password-reset/requests", csrf,
			map[string]any{"email": identifier})
		if status != http.StatusAccepted {
			t.Fatalf("identifier %q: status = %d, want 202", identifier, status)
		}
		if len(body) != 0 {
			t.Fatalf("identifier %q: body = %q, want empty", identifier, body)
		}
	}
}

// TestValidatePasswordReset_InvalidToken confirms a garbage token is
// rejected, mapped onto the same problem type confirm uses.
func TestValidatePasswordReset_InvalidToken(t *testing.T) {
	conn := openCertTestDB(t)
	srv := newCertTestServer(t, conn)
	defer srv.Close()

	client := newClient(t)
	csrf := pwResetCSRF(t, client, srv.URL)

	status, body := pwResetPost(t, client, srv.URL, "/api/v1/password-reset/validate", csrf,
		map[string]any{"token": "no-such-token"})
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422: %s", status, body)
	}
}

// TestValidatePasswordReset_DoesNotConsume is the stated behaviour (Section
// 8 Phase 8): validating must not burn the token, so a link scanner or mail
// prefetcher hitting this operation first does not break the real
// recipient's later confirm.
func TestValidatePasswordReset_DoesNotConsume(t *testing.T) {
	conn := openCertTestDB(t)
	srv := newCertTestServer(t, conn)
	defer srv.Close()

	username := fmt.Sprintf("pwreset_validate_%d", time.Now().UnixNano())
	userID := pwResetCreateUser(t, conn, username, "", "Pw1!Pw1!Pw1!")

	rawToken := "integration-validate-token"
	if err := appdb.CreatePasswordResetToken(conn, userID, pwResetTokenHash(rawToken), "127.0.0.1", time.Now().Add(30*time.Minute)); err != nil {
		t.Fatalf("CreatePasswordResetToken: %v", err)
	}

	client := newClient(t)
	csrf := pwResetCSRF(t, client, srv.URL)

	for i, want := range []int{http.StatusNoContent, http.StatusNoContent} {
		status, body := pwResetPost(t, client, srv.URL, "/api/v1/password-reset/validate", csrf,
			map[string]any{"token": rawToken})
		if status != want {
			t.Fatalf("validate attempt %d: status = %d, want %d: %s", i+1, status, want, body)
		}
	}

	// The token must still work for confirm after being validated twice.
	status, body := pwResetPost(t, client, srv.URL, "/api/v1/password-reset/confirm", csrf,
		map[string]any{"token": rawToken, "newPassword": "NewPw1!NewPw1!"})
	if status != http.StatusNoContent {
		t.Fatalf("confirm after repeated validate: status = %d, want 204: %s", status, body)
	}
}

// TestConfirmPasswordReset_SingleUse confirms the token is consumed on a
// successful confirm: a second confirm with the same token must be refused.
func TestConfirmPasswordReset_SingleUse(t *testing.T) {
	conn := openCertTestDB(t)
	srv := newCertTestServer(t, conn)
	defer srv.Close()

	username := fmt.Sprintf("pwreset_confirm_%d", time.Now().UnixNano())
	userID := pwResetCreateUser(t, conn, username, "", "Pw1!Pw1!Pw1!")

	rawToken := "integration-confirm-token"
	if err := appdb.CreatePasswordResetToken(conn, userID, pwResetTokenHash(rawToken), "127.0.0.1", time.Now().Add(30*time.Minute)); err != nil {
		t.Fatalf("CreatePasswordResetToken: %v", err)
	}

	client := newClient(t)
	csrf := pwResetCSRF(t, client, srv.URL)

	status, body := pwResetPost(t, client, srv.URL, "/api/v1/password-reset/confirm", csrf,
		map[string]any{"token": rawToken, "newPassword": "NewPw1!NewPw1!"})
	if status != http.StatusNoContent {
		t.Fatalf("first confirm: status = %d, want 204: %s", status, body)
	}

	status, body = pwResetPost(t, client, srv.URL, "/api/v1/password-reset/confirm", csrf,
		map[string]any{"token": rawToken, "newPassword": "AnotherPw1!AnotherPw1!"})
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("second confirm (reused token): status = %d, want 422: %s", status, body)
	}
}

// TestConfirmPasswordReset_WeakPasswordRejected confirms the policy from
// security.ValidatePassword is still enforced on this new operation.
func TestConfirmPasswordReset_WeakPasswordRejected(t *testing.T) {
	conn := openCertTestDB(t)
	srv := newCertTestServer(t, conn)
	defer srv.Close()

	username := fmt.Sprintf("pwreset_weak_%d", time.Now().UnixNano())
	userID := pwResetCreateUser(t, conn, username, "", "Pw1!Pw1!Pw1!")

	rawToken := "integration-weak-token"
	if err := appdb.CreatePasswordResetToken(conn, userID, pwResetTokenHash(rawToken), "127.0.0.1", time.Now().Add(30*time.Minute)); err != nil {
		t.Fatalf("CreatePasswordResetToken: %v", err)
	}

	client := newClient(t)
	csrf := pwResetCSRF(t, client, srv.URL)

	status, body := pwResetPost(t, client, srv.URL, "/api/v1/password-reset/confirm", csrf,
		map[string]any{"token": rawToken, "newPassword": "weak"})
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422: %s", status, body)
	}
}
