//go:build integration

package api_test

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"
	"time"

	humaapi "certharbor/api"
	"certharbor/config"
	appdb "certharbor/db"
	"certharbor/handlers"
	appmw "certharbor/middleware"
	"certharbor/models"
	"certharbor/security"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/sessions"
)

// openCertTestDB mirrors db/integration_test.go's openTestDB: same DSN
// resolution, same InitSchema call, same truncate-and-close cleanup. It is
// not shared code because db/integration_test.go's helper is unexported and
// scoped to package db; certificate list/detail are the first backend/api
// operations needing a real database (Phase 4, R23's observed-cost record:
// list and detail are the two operations in this phase that could not be
// proven with the nil-db handler mount_test.go's suite otherwise uses).
func openCertTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("INTEGRATION_DB_DSN")
	if dsn == "" {
		dsn = "postgres://certharbor_test:certharbor_test@localhost:5432/certharbor_test?sslmode=disable"
	}
	t.Setenv("CERTHARBOR_ADMIN_PASSWORD", "IntegrationTest123!")
	conn, err := appdb.Connect(dsn)
	if err != nil {
		t.Skipf("integration DB not available (%v), skipping", err)
	}
	if err := appdb.InitSchema(conn); err != nil {
		_ = conn.Close()
		t.Fatalf("InitSchema: %v", err)
	}
	t.Cleanup(func() {
		// Mirrors db/integration_test.go's openTestDB truncate list: users
		// included, or a later test's InitSchema admin-seed collides with
		// this one's leftover row (the failure mode this comment exists
		// because of).
		_, _ = conn.Exec(`TRUNCATE users, auth_log, certificates, cert_history, user_recovery_codes RESTART IDENTITY CASCADE`) //nolint:noctx // test cleanup
		_ = conn.Close()
	})
	return conn
}

// newCertTestServer is newTestServerWith (mount_test.go) with a real *sql.DB
// instead of a hardcoded nil, which list and detail need to query. It is a
// separate helper rather than a parameter added to newTestServerWith so the
// large majority of this suite, which needs no database at all, is
// unaffected by this phase's one new dependency.
func newCertTestServer(t *testing.T, db *sql.DB) *httptest.Server {
	t.Helper()
	hashKey, blockKey, err := security.DeriveSessionKeys("phase4-cert-test-key-not-for-prod!")
	if err != nil {
		t.Fatalf("DeriveSessionKeys: %v", err)
	}
	store := sessions.NewCookieStore(hashKey, blockKey)
	store.Options = &sessions.Options{
		Path: "/", MaxAge: 28800, HttpOnly: true,
		SameSite: http.SameSiteLaxMode, Secure: false, // httptest.NewServer is plain HTTP
	}

	h := handlers.NewWithFS(db, &config.Config{}, store, nil)
	r := chi.NewRouter()
	r.Post("/test-login", func(w http.ResponseWriter, req *http.Request) {
		sess, _ := store.Get(req, h.SessionCookieName())
		now := time.Now().Unix()
		sess.Values["user_id"] = testUser.ID
		sess.Values["session_epoch"] = testUser.SessionEpoch
		sess.Values["session_created_at"] = now
		sess.Values["last_activity"] = now
		sess.Values["csrf_token"] = testCSRFToken
		if err := sess.Save(req, w); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "certharbor-csrf", Value: testCSRFToken, Path: "/"}) //nolint:gosec // G124: deliberately not HttpOnly (5.4), plain-HTTP test server
	})
	humaapi.Mount(r, h, appmw.UserLoader(stubUserLoader))

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv
}

func insertTestCert(t *testing.T, db *sql.DB, name, domain, status string, expiresAt time.Time) {
	t.Helper()
	issued := expiresAt.Add(-365 * 24 * time.Hour)
	c := &models.Certificate{
		Name: name, Domain: domain, CertPath: "/nonexistent/" + name + ".crt", KeyPath: "/nonexistent/" + name + ".key",
		IssuedAt: &issued, ExpiresAt: &expiresAt, Serial: name + "-serial", KeyType: "EC:P-256", IssueDuration: "8760h",
	}
	if err := appdb.InsertCert(db, c); err != nil {
		t.Fatalf("InsertCert(%s): %v", name, err)
	}
	if status != "active" {
		row, err := appdb.GetCertByName(db, name)
		if err != nil || row == nil {
			t.Fatalf("GetCertByName(%s): %v", name, err)
		}
		if err := appdb.UpdateCertStatus(db, row.ID, status); err != nil {
			t.Fatalf("UpdateCertStatus(%s): %v", name, err)
		}
	}
}

// TestListCertificates_PaginatesFiltersAndComputesExpiryStatus is the one
// test covering 5.6's envelope, the q/status filters and the three
// expiryStatus boundaries at once, against three certificates whose expiry
// each lands on a different side of ExpiringSoonDays (30).
func TestListCertificates_PaginatesFiltersAndComputesExpiryStatus(t *testing.T) {
	db := openCertTestDB(t)
	srv := newCertTestServer(t, db)
	client := newClient(t)
	login(t, client, srv)

	now := time.Now()
	insertTestCert(t, db, "expired-cert", "expired.example.com", "active", now.Add(-24*time.Hour))
	insertTestCert(t, db, "warning-cert", "warning.example.com", "active", now.Add(10*24*time.Hour))
	insertTestCert(t, db, "ok-cert", "ok.example.com", "active", now.Add(365*24*time.Hour))
	insertTestCert(t, db, "revoked-cert", "revoked.example.com", "revoked", now.Add(365*24*time.Hour))

	// Page 1 of 3, size 2: the full unfiltered set is 4 rows.
	first := doGet(t, client, srv.URL+"/api/v1/certificates?page=1&pageSize=2")
	if first.status != http.StatusOK {
		t.Fatalf("page 1: status = %d, want 200", first.status)
	}
	items, _ := first.body["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("page 1: got %d items, want 2", len(items))
	}
	if total, _ := first.body["total"].(float64); total != 4 {
		t.Fatalf("total = %v, want 4", first.body["total"])
	}

	// status filter: exactly the one revoked row.
	revoked := doGet(t, client, srv.URL+"/api/v1/certificates?status=revoked")
	revokedItems, _ := revoked.body["items"].([]any)
	if len(revokedItems) != 1 {
		t.Fatalf("status=revoked: got %d items, want 1", len(revokedItems))
	}

	// q filter: substring match against name or domain.
	q := doGet(t, client, srv.URL+"/api/v1/certificates?q=warning")
	qItems, _ := q.body["items"].([]any)
	if len(qItems) != 1 {
		t.Fatalf("q=warning: got %d items, want 1", len(qItems))
	}

	expiryByName := map[string]string{}
	all := doGet(t, client, srv.URL+"/api/v1/certificates?pageSize=200")
	allItems, _ := all.body["items"].([]any)
	for _, raw := range allItems {
		item, _ := raw.(map[string]any)
		expiryByName[item["name"].(string)] = item["expiryStatus"].(string)
	}
	want := map[string]string{
		"expired-cert": "expired",
		"warning-cert": "warning",
		"ok-cert":      "ok",
		"revoked-cert": "ok",
	}
	for name, status := range want {
		if got := expiryByName[name]; got != status {
			t.Errorf("expiryStatus[%s] = %q, want %q", name, got, status)
		}
	}
}

// TestListCertificates_PageTimesPageSizeOverMaxOffsetIs422 asserts 5.6's
// deep-offset guard: page*pageSize over apitypes.MaxOffset (10000) is a 422,
// not a slow query against however many rows exist.
func TestListCertificates_PageTimesPageSizeOverMaxOffsetIs422(t *testing.T) {
	db := openCertTestDB(t)
	srv := newCertTestServer(t, db)
	client := newClient(t)
	login(t, client, srv)

	resp := doRequest(t, client, http.MethodGet, srv.URL+"/api/v1/certificates?page=51&pageSize=200", nil, nil) //nolint:bodyclose // closed via t.Cleanup inside doRequest
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", resp.StatusCode)
	}
}

// TestGetCertificate_ReturnsDetailAndValidationsWithoutFilesystemPaths is the
// detail operation's success path, and the negative check that CertPath and
// KeyPath (both on Section 2's banned-property-name list) never appear.
// buildCertDetail cannot read the PEM at the fabricated path, which is
// exactly the shape of "a validation failed" this asserts: the certificate
// row itself still renders, with an "err" validation rather than a 500.
func TestGetCertificate_ReturnsDetailAndValidationsWithoutFilesystemPaths(t *testing.T) {
	db := openCertTestDB(t)
	srv := newCertTestServer(t, db)
	client := newClient(t)
	login(t, client, srv)

	insertTestCert(t, db, "detail-cert", "detail.example.com", "active", time.Now().Add(365*24*time.Hour))
	row, err := appdb.GetCertByName(db, "detail-cert")
	if err != nil || row == nil {
		t.Fatalf("GetCertByName: %v", err)
	}

	result := doGet(t, client, srv.URL+"/api/v1/certificates/"+strconv.Itoa(row.ID))
	if result.status != http.StatusOK {
		t.Fatalf("status = %d, want 200", result.status)
	}
	if result.body["name"] != "detail-cert" {
		t.Fatalf("name = %v, want detail-cert", result.body["name"])
	}
	if result.body["expiryStatus"] != "ok" {
		t.Fatalf("expiryStatus = %v, want ok", result.body["expiryStatus"])
	}
	validations, _ := result.body["validations"].([]any)
	if len(validations) == 0 {
		t.Fatal("validations is empty, want at least the failed certificate-file check")
	}
	first, _ := validations[0].(map[string]any)
	if first["status"] != "err" {
		t.Fatalf("validations[0].status = %v, want err (the fabricated cert path cannot be read)", first["status"])
	}
	for _, banned := range []string{"certPath", "keyPath"} {
		if _, present := result.body[banned]; present {
			t.Errorf("response carries %q, which is on Section 2's banned-property-name list", banned)
		}
	}
}

func TestGetCertificate_UnknownIDIs404(t *testing.T) {
	db := openCertTestDB(t)
	srv := newCertTestServer(t, db)
	client := newClient(t)
	login(t, client, srv)

	resp := doRequest(t, client, http.MethodGet, srv.URL+"/api/v1/certificates/999999", nil, nil) //nolint:bodyclose // closed via t.Cleanup inside doRequest
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/problem+json" {
		t.Fatalf("Content-Type = %q, want application/problem+json", ct)
	}
}
