package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"certharbor/config"
	"certharbor/middleware"
	"certharbor/models"

	"github.com/gorilla/sessions"
)

// newTestHandler wires a Handler with no DB.
func newTestHandler(cfg *config.Config, store *sessions.CookieStore) *Handler {
	return &Handler{
		db:    nil,
		cfg:   cfg,
		store: store,
	}
}

// testReq builds a request with a background context.
func testReq(method, path, body string) *http.Request {
	var bodyReader *strings.Reader
	if body != "" {
		bodyReader = strings.NewReader(body)
	} else {
		bodyReader = strings.NewReader("")
	}
	req := httptest.NewRequestWithContext(context.Background(), method, path, bodyReader)
	if method == "POST" {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	return req
}

// ─── pending2FAUserID ─────────────────────────────────────────────────────────

// TestPending2FAUserID_Set, session has a valid pending_2fa_user_id.
func TestPending2FAUserID_Set(t *testing.T) {
	store := testStore()
	h := newTestHandler(testConfig(""), store)
	future := time.Now().Add(5 * time.Minute).Unix()
	cookies := injectSession(t, store, map[interface{}]interface{}{
		"pending_2fa_user_id": 42,
		"pending_2fa_expires": future,
	})
	req := testReq("GET", "/login", "")
	for _, c := range cookies {
		req.AddCookie(c)
	}
	if uid := h.pending2FAUserID(req); uid != 42 {
		t.Errorf("pending2FAUserID: got %d want 42", uid)
	}
}

// TestPending2FAUserID_Expired, pending_2fa_expires is in the past.
func TestPending2FAUserID_Expired(t *testing.T) {
	store := testStore()
	h := newTestHandler(testConfig(""), store)
	past := time.Now().Add(-1 * time.Minute).Unix()
	cookies := injectSession(t, store, map[interface{}]interface{}{
		"pending_2fa_user_id": 7,
		"pending_2fa_expires": past,
	})
	req := testReq("GET", "/login", "")
	for _, c := range cookies {
		req.AddCookie(c)
	}
	if uid := h.pending2FAUserID(req); uid != 0 {
		t.Errorf("expired 2FA pending: got %d want 0", uid)
	}
}

// TestPending2FAUserID_NotSet, no pending_2fa_user_id in session.
func TestPending2FAUserID_NotSet(t *testing.T) {
	store := testStore()
	h := newTestHandler(testConfig(""), store)
	cookies := injectSession(t, store, map[interface{}]interface{}{
		"user_id": 1,
	})
	req := testReq("GET", "/login", "")
	for _, c := range cookies {
		req.AddCookie(c)
	}
	if uid := h.pending2FAUserID(req); uid != 0 {
		t.Errorf("no 2FA pending: got %d want 0", uid)
	}
}

// ─── RBAC via RequireRole middleware ──────────────────────────────────────────

// TestRequireRole_AdminCanAccessAdminRoute confirms the middleware integration
// tested end-to-end: admin session → 200; viewer session → 403.  The role comes
// from the user RequireLogin loads, so the fake loader is the source of truth.
func TestRequireRole_AdminCanAccessAdminRoute(t *testing.T) {
	store := testStore()
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	for _, tc := range []struct {
		role string
		want int
	}{
		{"admin", http.StatusOK},
		{"manager", http.StatusForbidden},
		{"viewer", http.StatusForbidden},
		{"", http.StatusForbidden},
	} {
		t.Run("role_"+tc.role, func(t *testing.T) {
			loader := func(id int) (*models.User, error) {
				return &models.User{ID: id, Username: "u", Role: tc.role, IsActive: true}, nil
			}
			handler := middleware.RequireLogin(store, loader)(middleware.RequireRole("admin")(inner))
			cookies := injectSession(t, store, map[interface{}]interface{}{
				"user_id":       1,
				"role":          tc.role,
				"last_activity": time.Now().Unix(),
			})
			req := httptest.NewRequestWithContext(context.Background(), "GET", "/admin", nil)
			for _, c := range cookies {
				req.AddCookie(c)
			}
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)
			if rr.Code != tc.want {
				t.Errorf("role=%q: got %d want %d", tc.role, rr.Code, tc.want)
			}
		})
	}
}

// TestCSRFOK confirms csrfOK returns true only when form token matches session.
func TestCSRFOK(t *testing.T) {
	store := testStore()
	h := newTestHandler(testConfig(""), store)
	token := "abc123"
	cookies := injectSession(t, store, map[interface{}]interface{}{
		"csrf_token": token,
	})

	// Correct token.
	req := testReq("POST", "/", "csrf_token="+token)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	if !h.csrfOK(req) {
		t.Error("csrfOK should return true for matching token")
	}

	// Wrong token.
	req2 := testReq("POST", "/", "csrf_token=WRONG")
	for _, c := range cookies {
		req2.AddCookie(c)
	}
	if h.csrfOK(req2) {
		t.Error("csrfOK should return false for mismatched token")
	}
}

// TestCSRFOK_Empty confirms that an empty form token is always rejected.
func TestCSRFOK_Empty(t *testing.T) {
	store := testStore()
	h := newTestHandler(testConfig(""), store)
	cookies := injectSession(t, store, map[interface{}]interface{}{
		"csrf_token": "",
	})
	req := testReq("POST", "/", "csrf_token=")
	for _, c := range cookies {
		req.AddCookie(c)
	}
	if h.csrfOK(req) {
		t.Error("csrfOK should reject empty tokens")
	}
}
