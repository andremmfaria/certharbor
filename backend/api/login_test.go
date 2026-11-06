package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"step-webui/config"
	"step-webui/models"
	"step-webui/security"

	"github.com/pquerna/otp/totp"
)

// loopback is the address every httptest request arrives from, and so the
// rate-limiter key for the whole file.
const loopback = "127.0.0.1"

const (
	alicePassword = "correct horse battery"
	recoveryCode  = "ABCDEF-123456-FEDCBA"
)

// bcrypt at the production cost is the slow part of building a fixture, and
// the hashes never change, so each is computed once for the whole file.
var (
	passwordHash = sync.OnceValue(func() string { return security.HashPassword(alicePassword) })
	recoveryHash = sync.OnceValue(func() string { return security.HashPassword(recoveryCode) })
)

// fakeAuthStore stands in for the database behind the login flow.
type fakeAuthStore struct {
	mu       sync.Mutex
	users    map[string]*models.User
	recovery map[int]string
	lastStep map[int]int64
	audit    []string
}

func (f *fakeAuthStore) UserByUsername(name string) (*models.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.users[name], nil
}

func (f *fakeAuthStore) UserByID(id int) (*models.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, u := range f.users {
		if u.ID == id {
			return u, nil
		}
	}
	return nil, nil
}

func (f *fakeAuthStore) LogAuth(username, _ string, success bool, reason string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	outcome := "fail"
	if success {
		outcome = "ok"
	}
	f.audit = append(f.audit, username+" "+outcome+" "+reason)
	return nil
}

func (f *fakeAuthStore) SetPasswordHash(id int, hash string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, u := range f.users {
		if u.ID == id {
			u.PasswordHash = hash
		}
	}
	return nil
}

func (f *fakeAuthStore) UnusedRecoveryCodes(userID int) (map[int]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if hash, ok := f.recovery[userID]; ok {
		return map[int]string{userID: hash}, nil
	}
	return map[int]string{}, nil
}

func (f *fakeAuthStore) UseRecoveryCode(codeID int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.recovery, codeID)
	return nil
}

func (f *fakeAuthStore) TOTPLastStep(_ context.Context, userID int) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastStep[userID], nil
}

func (f *fakeAuthStore) SetTOTPLastStep(_ context.Context, userID int, step int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastStep[userID] = step
	return nil
}

type loginFixture struct {
	srv        *httptest.Server
	client     *http.Client
	store      *fakeAuthStore
	totpSecret string
}

func newLoginFixture(t *testing.T, cfg *config.Config) *loginFixture {
	t.Helper()
	secret, err := totp.Generate(totp.GenerateOpts{Issuer: "test", AccountName: "mfa"})
	if err != nil {
		t.Fatalf("totp.Generate: %v", err)
	}
	store := &fakeAuthStore{
		users: map[string]*models.User{
			"alice": {ID: 10, Username: "alice", Role: "manager", IsActive: true, PasswordHash: passwordHash()},
			"mfa": {
				ID: 11, Username: "mfa", Role: "admin", IsActive: true, PasswordHash: passwordHash(),
				TOTPEnabled: true, TOTPSecret: secret.Secret(),
			},
			"off": {ID: 12, Username: "off", Role: "viewer", IsActive: false, PasswordHash: passwordHash()},
		},
		recovery: map[int]string{11: recoveryHash()},
		lastStep: map[int]int64{},
	}
	// The limiter is process-global and keyed on the loopback address, so
	// every test starts and ends with it clear.
	security.RL.Clear(loopback)
	t.Cleanup(func() { security.RL.Clear(loopback) })

	return &loginFixture{
		srv:        newTestServerWith(t, cfg, store),
		client:     newClient(t),
		store:      store,
		totpSecret: secret.Secret(),
	}
}

func localLogin() *config.Config { return &config.Config{LocalLoginEnabled: true} }

// csrf primes the anonymous CSRF pair the way the SPA's boot call does and
// returns the token to echo.
func (f *loginFixture) csrf(t *testing.T) string {
	t.Helper()
	res := doGet(t, f.client, f.srv.URL+"/api/v1/session")
	token := cookieValue(res.cookies, "step-webui-csrf")
	if token == "" {
		t.Fatal("GET /api/v1/session did not mint a CSRF cookie")
	}
	return token
}

type postResult struct {
	status int
	header http.Header
	body   map[string]any
	raw    string
}

func (f *loginFixture) post(t *testing.T, path, token string, body any) postResult {
	t.Helper()
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(body); err != nil {
		t.Fatalf("encode: %v", err)
	}
	resp := doRequest(t, f.client, http.MethodPost, f.srv.URL+path, &buf, func(r *http.Request) { //nolint:bodyclose // closed via t.Cleanup inside doRequest
		r.Header.Set("Content-Type", "application/json")
		if token != "" {
			r.Header.Set("X-CSRF-Token", token)
		}
	})
	var raw bytes.Buffer
	_, _ = raw.ReadFrom(resp.Body)
	out := postResult{status: resp.StatusCode, header: resp.Header, raw: raw.String()}
	_ = json.Unmarshal(raw.Bytes(), &out.body)
	return out
}

func (f *loginFixture) login(t *testing.T, token, user string) postResult {
	t.Helper()
	return f.post(t, "/api/v1/session", token, map[string]string{"username": user, "password": alicePassword})
}

func (f *loginFixture) sessionState(t *testing.T) string {
	t.Helper()
	res := doGet(t, f.client, f.srv.URL+"/api/v1/session")
	state, _ := res.body["state"].(string)
	return state
}

func problemType(r postResult) string {
	s, _ := r.body["type"].(string)
	return s
}

func TestCreateSession_LogsIn(t *testing.T) {
	f := newLoginFixture(t, localLogin())
	token := f.csrf(t)

	res := f.login(t, token, "alice")
	if res.status != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", res.status, res.raw)
	}
	if res.body["state"] != "authenticated" {
		t.Fatalf("state = %v, want authenticated", res.body["state"])
	}
	user, _ := res.body["user"].(map[string]any)
	if user["username"] != "alice" || user["role"] != "manager" {
		t.Fatalf("user = %v", user)
	}
	if strings.Contains(res.raw, "passwordHash") || strings.Contains(res.raw, alicePassword) {
		t.Fatalf("response leaked credential material: %s", res.raw)
	}
	if got := f.sessionState(t); got != "authenticated" {
		t.Fatalf("GET /session after login = %q, want authenticated", got)
	}
}

// TestCreateSession_ReMintsTheCSRFPair covers 5.4: completeLogin replaces the
// token, which is what closes session fixation, and the readable cookie has
// to move with it or the next mutation is refused.
func TestCreateSession_ReMintsTheCSRFPair(t *testing.T) {
	f := newLoginFixture(t, localLogin())
	before := f.csrf(t)

	body, _ := json.Marshal(map[string]string{"username": "alice", "password": alicePassword})
	resp := doRequest(t, f.client, http.MethodPost, f.srv.URL+"/api/v1/session", bytes.NewReader(body), func(r *http.Request) { //nolint:bodyclose // closed via t.Cleanup inside doRequest
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", before)
	})
	after := cookieValue(resp.Cookies(), "step-webui-csrf")
	if after == "" || after == before {
		t.Fatalf("csrf cookie after login = %q, want a new value different from %q", after, before)
	}
}

func TestCreateSession_RequiresCSRF(t *testing.T) {
	f := newLoginFixture(t, localLogin())
	f.csrf(t)

	res := f.login(t, "", "alice")
	if res.status != http.StatusForbidden || !strings.HasSuffix(problemType(res), "/csrf") {
		t.Fatalf("status = %d type = %q, want 403 .../csrf", res.status, problemType(res))
	}
}

func TestCreateSession_WrongPasswordIsAValueNotAReset(t *testing.T) {
	f := newLoginFixture(t, localLogin())
	token := f.csrf(t)

	wrong := f.post(t, "/api/v1/session", token, map[string]string{"username": "alice", "password": "nope"})
	unknown := f.post(t, "/api/v1/session", token, map[string]string{"username": "nobody", "password": "nope"})

	for name, res := range map[string]postResult{"wrong password": wrong, "unknown user": unknown} {
		if res.status != http.StatusUnauthorized {
			t.Errorf("%s: status = %d, want 401", name, res.status)
		}
		if ct := res.header.Get("Content-Type"); ct != "application/problem+json" {
			t.Errorf("%s: Content-Type = %q", name, ct)
		}
	}
	// Whether the account exists must not be observable from the refusal. The
	// attempts counter legitimately differs between the two calls.
	digits := regexp.MustCompile(`\d+`)
	wd, _ := wrong.body["detail"].(string)
	ud, _ := unknown.body["detail"].(string)
	if wd == "" || digits.ReplaceAllString(wd, "N") != digits.ReplaceAllString(ud, "N") {
		t.Errorf("details differ: %q vs %q", wd, ud)
	}
	if got := f.sessionState(t); got != "anonymous" {
		t.Fatalf("state after a refused login = %q, want anonymous", got)
	}
}

func TestCreateSession_DisabledAccount(t *testing.T) {
	f := newLoginFixture(t, localLogin())
	res := f.login(t, f.csrf(t), "off")
	if res.status != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", res.status)
	}
}

func TestCreateSession_LocalLoginDisabled(t *testing.T) {
	f := newLoginFixture(t, &config.Config{})
	res := f.login(t, f.csrf(t), "alice")
	if res.status != http.StatusForbidden || !strings.HasSuffix(problemType(res), "/local-login-disabled") {
		t.Fatalf("status = %d type = %q, want 403 .../local-login-disabled", res.status, problemType(res))
	}
}

// TestCreateSession_ValidationDoesNotEchoThePassword is 5.2's rule applied to
// the one operation that carries a credential.
func TestCreateSession_ValidationDoesNotEchoThePassword(t *testing.T) {
	f := newLoginFixture(t, localLogin())
	token := f.csrf(t)

	res := f.post(t, "/api/v1/session", token, map[string]any{"username": "alice", "password": 987654321})
	if res.status != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422: %s", res.status, res.raw)
	}
	if strings.Contains(res.raw, "987654321") {
		t.Fatalf("problem document echoed the submitted password: %s", res.raw)
	}
}

// TestLoginRateLimit_BlocksOnTheFifthFailure exercises both halves of the
// limiter: the handler reports the attempt that trips it, and the middleware
// refuses the next one without reaching the handler. This is the end-to-end
// coverage of rateLimitMiddleware that its scoping was waiting for.
func TestLoginRateLimit_BlocksOnTheFifthFailure(t *testing.T) {
	f := newLoginFixture(t, localLogin())
	token := f.csrf(t)
	bad := map[string]string{"username": "alice", "password": "nope"}

	for i := 1; i < security.LimitCount; i++ {
		if res := f.post(t, "/api/v1/session", token, bad); res.status != http.StatusUnauthorized {
			t.Fatalf("attempt %d: status = %d, want 401", i, res.status)
		}
	}
	last := f.post(t, "/api/v1/session", token, bad)
	if last.status != http.StatusTooManyRequests || last.header.Get("Retry-After") == "" {
		t.Fatalf("tripping attempt: status = %d Retry-After = %q, want 429 with a header", last.status, last.header.Get("Retry-After"))
	}
	if !strings.HasSuffix(problemType(last), "/rate-limit") {
		t.Fatalf("type = %q, want .../rate-limit", problemType(last))
	}

	// A correct password from the blocked address is refused by the middleware.
	blocked := f.login(t, token, "alice")
	if blocked.status != http.StatusTooManyRequests || blocked.header.Get("Retry-After") == "" {
		t.Fatalf("blocked address: status = %d, want 429 with Retry-After", blocked.status)
	}

	// The limiter is scoped: nothing else on the API is affected.
	if res := doGet(t, f.client, f.srv.URL+"/api/v1/config"); res.status != http.StatusOK {
		t.Fatalf("GET /config while blocked = %d, want 200", res.status)
	}
}

func (f *loginFixture) totpCode(t *testing.T) string {
	t.Helper()
	code, err := totp.GenerateCode(f.totpSecret, time.Now())
	if err != nil {
		t.Fatalf("GenerateCode: %v", err)
	}
	return code
}

func TestMFA_RefreshResumesOnTheCodeStep(t *testing.T) {
	f := newLoginFixture(t, localLogin())
	token := f.csrf(t)

	first := f.login(t, token, "mfa")
	if first.status != http.StatusOK || first.body["state"] != "pendingMfa" {
		t.Fatalf("login: status = %d state = %v, want 200 pendingMfa", first.status, first.body["state"])
	}
	if first.body["user"] != nil {
		t.Fatalf("a pending session must not reveal a user: %v", first.body["user"])
	}
	if got := f.sessionState(t); got != "pendingMfa" {
		t.Fatalf("state after a refresh = %q, want pendingMfa", got)
	}

	res := f.post(t, "/api/v1/session/mfa", token, map[string]string{"code": f.totpCode(t), "codeType": "totp"})
	if res.status != http.StatusOK || res.body["state"] != "authenticated" {
		t.Fatalf("mfa: status = %d body = %s", res.status, res.raw)
	}
	if got := f.sessionState(t); got != "authenticated" {
		t.Fatalf("state after mfa = %q, want authenticated", got)
	}
}

func TestMFA_WrongCodeIsRefusedAndStaysPending(t *testing.T) {
	f := newLoginFixture(t, localLogin())
	token := f.csrf(t)
	f.login(t, token, "mfa")

	res := f.post(t, "/api/v1/session/mfa", token, map[string]string{"code": "000000", "codeType": "totp"})
	if res.status != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", res.status)
	}
	if got := f.sessionState(t); got != "pendingMfa" {
		t.Fatalf("state after a wrong code = %q, want pendingMfa", got)
	}
}

func TestMFA_ReplayedCodeIsRefused(t *testing.T) {
	f := newLoginFixture(t, localLogin())
	token := f.csrf(t)
	code := f.totpCode(t)

	f.login(t, token, "mfa")
	if res := f.post(t, "/api/v1/session/mfa", token, map[string]string{"code": code, "codeType": "totp"}); res.status != http.StatusOK {
		t.Fatalf("first use: status = %d", res.status)
	}

	// New session, same physical code inside its 30 second window.
	f.client = newClient(t)
	token = f.csrf(t)
	f.login(t, token, "mfa")
	if res := f.post(t, "/api/v1/session/mfa", token, map[string]string{"code": code, "codeType": "totp"}); res.status != http.StatusUnauthorized {
		t.Fatalf("replay: status = %d, want 401", res.status)
	}
}

func TestMFA_RecoveryCodeIsSingleUse(t *testing.T) {
	f := newLoginFixture(t, localLogin())
	token := f.csrf(t)
	f.login(t, token, "mfa")

	res := f.post(t, "/api/v1/session/mfa", token, map[string]string{"code": recoveryCode, "codeType": "recovery"})
	if res.status != http.StatusOK || res.body["state"] != "authenticated" {
		t.Fatalf("recovery: status = %d body = %s", res.status, res.raw)
	}

	f.client = newClient(t)
	token = f.csrf(t)
	f.login(t, token, "mfa")
	if again := f.post(t, "/api/v1/session/mfa", token, map[string]string{"code": recoveryCode, "codeType": "recovery"}); again.status != http.StatusUnauthorized {
		t.Fatalf("second use: status = %d, want 401", again.status)
	}
}

func TestMFA_WithoutAPendingLogin(t *testing.T) {
	f := newLoginFixture(t, localLogin())
	token := f.csrf(t)

	res := f.post(t, "/api/v1/session/mfa", token, map[string]string{"code": "123456", "codeType": "totp"})
	if res.status != http.StatusUnauthorized || !strings.HasSuffix(problemType(res), "/mfa-expired") {
		t.Fatalf("status = %d type = %q, want 401 .../mfa-expired", res.status, problemType(res))
	}
}

// TestCreateSession_SupersedesAParkedMFAState is 5.3's reason for dropping
// pending_2fa_user_id before looking at the credentials.
func TestCreateSession_SupersedesAParkedMFAState(t *testing.T) {
	f := newLoginFixture(t, localLogin())
	token := f.csrf(t)
	f.login(t, token, "mfa")

	res := f.login(t, token, "alice")
	if res.status != http.StatusOK || res.body["state"] != "authenticated" {
		t.Fatalf("status = %d body = %s, want alice authenticated", res.status, res.raw)
	}
	if user, _ := res.body["user"].(map[string]any); user["username"] != "alice" {
		t.Fatalf("authenticated as %v, want alice", user)
	}
}

func TestMFA_ValidatesCodeType(t *testing.T) {
	f := newLoginFixture(t, localLogin())
	token := f.csrf(t)
	f.login(t, token, "mfa")

	res := f.post(t, "/api/v1/session/mfa", token, map[string]string{"code": "123456", "codeType": "sms"})
	if res.status != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", res.status)
	}
}
