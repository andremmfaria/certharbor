package api_test

import (
	"bytes"
	"net/http"
	"testing"
)

// ─── Users: session and role gates ──────────────────────────────────────────
//
// These mirror certificates_test.go's pattern: the session and role
// middleware answer before any operation handler (and therefore the
// database) is ever reached, so none of these need a real database.

func TestListUsers_RequiresSession(t *testing.T) {
	srv := newTestServer(t)
	client := newClient(t)

	resp := doRequest(t, client, http.MethodGet, srv.URL+"/api/v1/users", nil, nil) //nolint:bodyclose // closed via t.Cleanup inside doRequest
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

// TestListUsers_ManagerDenied is the elevated-role case: every Users and
// Temporary Users operation is admin-only (Section 6), so a manager
// session (testUser.Role's default) must get 403.
func TestListUsers_ManagerDenied(t *testing.T) {
	srv := newTestServer(t)
	client := newClient(t)
	login(t, client, srv) // testUser.Role defaults to "manager"

	resp := doRequest(t, client, http.MethodGet, srv.URL+"/api/v1/users", nil, nil) //nolint:bodyclose // closed via t.Cleanup inside doRequest
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
}

func TestGetUser_RequiresSession(t *testing.T) {
	srv := newTestServer(t)
	client := newClient(t)

	resp := doRequest(t, client, http.MethodGet, srv.URL+"/api/v1/users/1", nil, nil) //nolint:bodyclose // closed via t.Cleanup inside doRequest
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestCreateUser_RequiresSession(t *testing.T) {
	srv := newTestServer(t)
	client := newClient(t)

	resp := doRequest(t, client, http.MethodPost, srv.URL+"/api/v1/users", nil, nil) //nolint:bodyclose // closed via t.Cleanup inside doRequest
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestCreateUser_ManagerDenied(t *testing.T) {
	srv := newTestServer(t)
	client := newClient(t)
	token := login(t, client, srv)

	resp := doRequest(t, client, http.MethodPost, srv.URL+"/api/v1/users", bytes.NewReader([]byte(`{"username":"x","password":"Str0ng!Passw0rd","role":"viewer"}`)), func(r *http.Request) { //nolint:bodyclose // closed via t.Cleanup inside doRequest
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", token)
	})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
}

func TestUpdateUser_RequiresSession(t *testing.T) {
	srv := newTestServer(t)
	client := newClient(t)

	resp := doRequest(t, client, http.MethodPatch, srv.URL+"/api/v1/users/2", nil, nil) //nolint:bodyclose // closed via t.Cleanup inside doRequest
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestUpdateUser_ManagerDenied(t *testing.T) {
	srv := newTestServer(t)
	client := newClient(t)
	token := login(t, client, srv)

	resp := doRequest(t, client, http.MethodPatch, srv.URL+"/api/v1/users/2", bytes.NewReader([]byte(`{"isActive":false}`)), func(r *http.Request) { //nolint:bodyclose // closed via t.Cleanup inside doRequest
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", token)
	})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
}

// TestUpdateUser_SelfTargetRejected is the end-to-end self-target
// invariant test: an admin session targeting its own id with the admin
// PATCH operation must get 403, never reaching the (here, nil) database.
func TestUpdateUser_SelfTargetRejected(t *testing.T) {
	srv := newTestServer(t)
	client := newClient(t)
	token := login(t, client, srv)

	original := testUser.Role
	testUser.Role = "admin"
	t.Cleanup(func() { testUser.Role = original })

	resp := doRequest(t, client, http.MethodPatch, srv.URL+"/api/v1/users/1", bytes.NewReader([]byte(`{"isActive":false}`)), func(r *http.Request) { //nolint:bodyclose // closed via t.Cleanup inside doRequest
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", token)
	})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
}

func TestDeleteUser_RequiresSession(t *testing.T) {
	srv := newTestServer(t)
	client := newClient(t)

	resp := doRequest(t, client, http.MethodDelete, srv.URL+"/api/v1/users/2", nil, nil) //nolint:bodyclose // closed via t.Cleanup inside doRequest
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

// TestDeleteUser_SelfTargetRejected mirrors TestUpdateUser_SelfTargetRejected
// for DELETE /api/v1/users/{id}.
func TestDeleteUser_SelfTargetRejected(t *testing.T) {
	srv := newTestServer(t)
	client := newClient(t)
	token := login(t, client, srv)

	original := testUser.Role
	testUser.Role = "admin"
	t.Cleanup(func() { testUser.Role = original })

	resp := doRequest(t, client, http.MethodDelete, srv.URL+"/api/v1/users/1", nil, func(r *http.Request) { //nolint:bodyclose // closed via t.Cleanup inside doRequest
		r.Header.Set("X-CSRF-Token", token)
	})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
}

func TestResetUserPassword_RequiresSession(t *testing.T) {
	srv := newTestServer(t)
	client := newClient(t)

	resp := doRequest(t, client, http.MethodPost, srv.URL+"/api/v1/users/2/password-reset", nil, nil) //nolint:bodyclose // closed via t.Cleanup inside doRequest
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

// ─── Temporary users: session and role gates ────────────────────────────────

func TestListTemporaryUsers_RequiresSession(t *testing.T) {
	srv := newTestServer(t)
	client := newClient(t)

	resp := doRequest(t, client, http.MethodGet, srv.URL+"/api/v1/temporary-users", nil, nil) //nolint:bodyclose // closed via t.Cleanup inside doRequest
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestListTemporaryUsers_ManagerDenied(t *testing.T) {
	srv := newTestServer(t)
	client := newClient(t)
	login(t, client, srv)

	resp := doRequest(t, client, http.MethodGet, srv.URL+"/api/v1/temporary-users", nil, nil) //nolint:bodyclose // closed via t.Cleanup inside doRequest
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
}

func TestCreateTemporaryUser_RequiresSession(t *testing.T) {
	srv := newTestServer(t)
	client := newClient(t)

	resp := doRequest(t, client, http.MethodPost, srv.URL+"/api/v1/temporary-users", nil, nil) //nolint:bodyclose // closed via t.Cleanup inside doRequest
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestDeleteTemporaryUser_RequiresSession(t *testing.T) {
	srv := newTestServer(t)
	client := newClient(t)

	resp := doRequest(t, client, http.MethodDelete, srv.URL+"/api/v1/temporary-users/1", nil, nil) //nolint:bodyclose // closed via t.Cleanup inside doRequest
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}
