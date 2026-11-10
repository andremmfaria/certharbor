//go:build integration

package api_test

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"database/sql"
	"encoding/json"
	"encoding/pem"
	"io"
	"math/big"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
	"certharbor/stepca"

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
	return newCertTestServerWithCA(t, db, &config.Config{}, nil)
}

// newCertTestServerWithCA is newCertTestServer with a caller-supplied cfg
// (issue/renew/import need a real CertsDir, unlike list/detail) and an
// optional CA client injected via handlers.SetCAClient, the seam that lets
// the issue/renew/revoke success paths run without a live step-ca.
func newCertTestServerWithCA(t *testing.T, db *sql.DB, cfg *config.Config, ca stepca.CA) *httptest.Server {
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

	h := handlers.NewWithFS(db, cfg, store, nil)
	if ca != nil {
		h.SetCAClient(ca)
	}
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

// ─── Phase 4, second batch: issue, import, renew, revoke success paths ────
//
// These exercise what TestCreateCertificate_ViewerDenied and its siblings in
// certificates_test.go deliberately stop short of: the actual data path,
// which needs a real database and (for issue/renew/revoke) a CA client.
// fakeIssueCA plays that role; it cannot be handlers.FakeCA
// (stepca_fake_test.go) because that type lives in a _test.go file and is
// invisible outside package handlers' own test binary.

// fakeIssueCA is a minimal stepca.CA double: IssueCertificate returns a
// fixed cert/key PEM pair, Revoke always succeeds unless told otherwise.
type fakeIssueCA struct {
	certPEM, keyPEM []byte
	issueErr        error
	revokeErr       error
}

var _ stepca.CA = (*fakeIssueCA)(nil)

func (f *fakeIssueCA) Health(context.Context) error { return nil }

func (f *fakeIssueCA) Provisioners(context.Context) ([]stepca.ProvisionerInfo, error) {
	return nil, nil
}

func (f *fakeIssueCA) IssueCertificate(context.Context, stepca.IssueRequest) ([]byte, []byte, error) {
	return f.certPEM, f.keyPEM, f.issueErr
}

func (f *fakeIssueCA) Revoke(context.Context, string, string) error { return f.revokeErr }

// genSelfSignedPEM builds a throwaway EC P-256 self-signed leaf, matching
// what parseCertDates/getCertKeyType (cert_ops.go) read: a single PEM
// CERTIFICATE block. notAfter drives the computed expiryStatus.
func genSelfSignedPEM(t *testing.T, cn string, notAfter time.Time) (certPEM, keyPEM []byte) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 62))
	if err != nil {
		t.Fatalf("rand.Int: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: cn},
		DNSNames:              []string{cn},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("CreateCertificate: %v", err)
	}
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		t.Fatalf("MarshalPKCS8PrivateKey: %v", err)
	}
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	return certPEM, keyPEM
}

// decodeJSONBody is doGet's decode half for responses this file's own
// doRequest calls produce (POST, not GET, so doGet itself does not apply).
func decodeJSONBody(t *testing.T, resp *http.Response) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response body: %v", err)
	}
	return body
}

// TestCreateCertificate_Success is the issue operation's success path
// (handlers.IssueCertificate via createCertificate), proving the CA seam,
// the 201 status register.go sets for this operation specifically, and
// that the row lands in the database.
func TestCreateCertificate_Success(t *testing.T) {
	db := openCertTestDB(t)
	certsDir := t.TempDir()
	certPEM, keyPEM := genSelfSignedPEM(t, "issue.example.com", time.Now().Add(365*24*time.Hour))
	ca := &fakeIssueCA{certPEM: certPEM, keyPEM: keyPEM}
	srv := newCertTestServerWithCA(t, db, &config.Config{CertsDir: certsDir}, ca)
	client := newClient(t)
	token := login(t, client, srv)

	reqBody := `{"name":"issue-cert","domain":"issue.example.com","template":"server","duration":"8760h","keyType":"EC:P-256"}`
	resp := doRequest(t, client, http.MethodPost, srv.URL+"/api/v1/certificates", bytes.NewReader([]byte(reqBody)), func(r *http.Request) { //nolint:bodyclose // closed via t.Cleanup inside doRequest
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", token)
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
	body := decodeJSONBody(t, resp)
	if body["name"] != "issue-cert" || body["domain"] != "issue.example.com" {
		t.Fatalf("body = %v, want name=issue-cert domain=issue.example.com", body)
	}

	row, err := appdb.GetCertByName(db, "issue-cert")
	if err != nil || row == nil {
		t.Fatalf("GetCertByName(issue-cert): %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(certsDir, "issue-cert", "certificate.crt")); statErr != nil {
		t.Fatalf("certificate file not written: %v", statErr)
	}
}

// TestRenewCertificate_Success proves RenewCertificate's success path:
// the stored issue_duration/key_type are reused, the CA is called again,
// and the files on disk (already inside certsDir, as a real managed
// certificate's would be) are overwritten.
func TestRenewCertificate_Success(t *testing.T) {
	db := openCertTestDB(t)
	certsDir := t.TempDir()
	certDir := filepath.Join(certsDir, "renew-cert")
	if err := os.MkdirAll(certDir, 0o750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	certPath := filepath.Join(certDir, "certificate.crt")
	keyPath := filepath.Join(certDir, "private.key")
	origCert, origKey := genSelfSignedPEM(t, "renew.example.com", time.Now().Add(30*24*time.Hour))
	if err := os.WriteFile(certPath, origCert, 0o600); err != nil {
		t.Fatalf("WriteFile(cert): %v", err)
	}
	if err := os.WriteFile(keyPath, origKey, 0o600); err != nil {
		t.Fatalf("WriteFile(key): %v", err)
	}
	if err := appdb.InsertCert(db, &models.Certificate{
		Name: "renew-cert", Domain: "renew.example.com", CertPath: certPath, KeyPath: keyPath,
		Serial: "renew-cert-orig-serial", KeyType: "EC:P-256", IssueDuration: "8760h",
	}); err != nil {
		t.Fatalf("InsertCert: %v", err)
	}
	row, err := appdb.GetCertByName(db, "renew-cert")
	if err != nil || row == nil {
		t.Fatalf("GetCertByName: %v", err)
	}

	newCert, newKey := genSelfSignedPEM(t, "renew.example.com", time.Now().Add(365*24*time.Hour))
	ca := &fakeIssueCA{certPEM: newCert, keyPEM: newKey}
	srv := newCertTestServerWithCA(t, db, &config.Config{CertsDir: certsDir}, ca)
	client := newClient(t)
	token := login(t, client, srv)

	resp := doRequest(t, client, http.MethodPost, srv.URL+"/api/v1/certificates/"+strconv.Itoa(row.ID)+"/renew", nil, func(r *http.Request) { //nolint:bodyclose // closed via t.Cleanup inside doRequest
		r.Header.Set("X-CSRF-Token", token)
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body := decodeJSONBody(t, resp)
	if body["name"] != "renew-cert" {
		t.Fatalf("name = %v, want renew-cert", body["name"])
	}

	renewed, err := appdb.GetCertByName(db, "renew-cert")
	if err != nil || renewed == nil {
		t.Fatalf("GetCertByName after renew: %v", err)
	}
	if renewed.Serial == "renew-cert-orig-serial" {
		t.Fatal("serial unchanged after renew, want the new fake CA's cert serial")
	}
}

// TestRevokeCertificate_Success proves the admin-only mutation end to end:
// an admin session (not manager, Section 6's role split) hits revoke, the
// fake CA's Revoke is called, and the row's status becomes "revoked".
func TestRevokeCertificate_Success(t *testing.T) {
	db := openCertTestDB(t)
	certsDir := t.TempDir()
	if err := appdb.InsertCert(db, &models.Certificate{
		Name: "revoke-cert", Domain: "revoke.example.com", CertPath: "/nonexistent/revoke-cert.crt", KeyPath: "/nonexistent/revoke-cert.key",
		Serial: "revoke-cert-serial", KeyType: "EC:P-256", IssueDuration: "8760h",
	}); err != nil {
		t.Fatalf("InsertCert: %v", err)
	}
	row, err := appdb.GetCertByName(db, "revoke-cert")
	if err != nil || row == nil {
		t.Fatalf("GetCertByName: %v", err)
	}

	ca := &fakeIssueCA{}
	srv := newCertTestServerWithCA(t, db, &config.Config{CertsDir: certsDir}, ca)
	client := newClient(t)
	token := login(t, client, srv)

	original := testUser.Role
	testUser.Role = "admin"
	t.Cleanup(func() { testUser.Role = original })

	resp := doRequest(t, client, http.MethodPost, srv.URL+"/api/v1/certificates/"+strconv.Itoa(row.ID)+"/revoke", nil, func(r *http.Request) { //nolint:bodyclose // closed via t.Cleanup inside doRequest
		r.Header.Set("X-CSRF-Token", token)
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	revoked, err := appdb.GetCert(db, row.ID)
	if err != nil || revoked == nil {
		t.Fatalf("GetCert after revoke: %v", err)
	}
	if revoked.Status != "revoked" {
		t.Fatalf("status = %q, want revoked", revoked.Status)
	}
}

// multipartRealCertBody builds a POST /api/v1/certificates/import/upload
// body carrying real PEM bytes, the success-path sibling of
// certificates_test.go's multipartImportBody (which deliberately uploads
// "not a real cert" because it only exercises the 422/401 cases).
func multipartRealCertBody(t *testing.T, name, domain string, certPEM, keyPEM []byte) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if err := mw.WriteField("name", name); err != nil {
		t.Fatalf("WriteField(name): %v", err)
	}
	if err := mw.WriteField("domain", domain); err != nil {
		t.Fatalf("WriteField(domain): %v", err)
	}
	fw, err := mw.CreateFormFile("certFile", "certificate.crt")
	if err != nil {
		t.Fatalf("CreateFormFile(certFile): %v", err)
	}
	if _, err := fw.Write(certPEM); err != nil {
		t.Fatalf("Write(certFile): %v", err)
	}
	if keyPEM != nil {
		kw, err := mw.CreateFormFile("keyFile", "private.key")
		if err != nil {
			t.Fatalf("CreateFormFile(keyFile): %v", err)
		}
		if _, err := kw.Write(keyPEM); err != nil {
			t.Fatalf("Write(keyFile): %v", err)
		}
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("mw.Close: %v", err)
	}
	return &buf, mw.FormDataContentType()
}

// TestImportCertificateUpload_Success proves handlers.ImportCertificateUpload
// end to end: a real cert+key pair, saved under CertsDir, with the row
// readable afterward via GET /api/v1/certificates/{id}.
func TestImportCertificateUpload_Success(t *testing.T) {
	db := openCertTestDB(t)
	certsDir := t.TempDir()
	srv := newCertTestServerWithCA(t, db, &config.Config{CertsDir: certsDir}, nil)
	client := newClient(t)
	token := login(t, client, srv)

	certPEM, keyPEM := genSelfSignedPEM(t, "upload.example.com", time.Now().Add(365*24*time.Hour))
	buf, contentType := multipartRealCertBody(t, "upload-cert", "upload.example.com", certPEM, keyPEM)
	resp := doRequest(t, client, http.MethodPost, srv.URL+"/api/v1/certificates/import/upload", buf, func(r *http.Request) { //nolint:bodyclose // closed via t.Cleanup inside doRequest
		r.Header.Set("Content-Type", contentType)
		r.Header.Set("X-CSRF-Token", token)
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body := decodeJSONBody(t, resp)
	if body["name"] != "upload-cert" {
		t.Fatalf("name = %v, want upload-cert", body["name"])
	}
	if _, statErr := os.Stat(filepath.Join(certsDir, "upload-cert", "certificate.crt")); statErr != nil {
		t.Fatalf("certificate file not written: %v", statErr)
	}
}

// TestImportCertificatesByPaths_MixedBatchReportsPerItem is Section 6's
// explicit behaviour-change requirement: unlike the old import/scan, which
// swallowed per-item failures into a count, every item in a mixed batch
// (one importable file, one missing file) gets its own ok/error result.
func TestImportCertificatesByPaths_MixedBatchReportsPerItem(t *testing.T) {
	db := openCertTestDB(t)
	certsDir := t.TempDir()
	goodDir := filepath.Join(certsDir, "good-item")
	if err := os.MkdirAll(goodDir, 0o750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	certPEM, _ := genSelfSignedPEM(t, "good.example.com", time.Now().Add(365*24*time.Hour))
	goodCertPath := filepath.Join(goodDir, "certificate.crt")
	if err := os.WriteFile(goodCertPath, certPEM, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	srv := newCertTestServerWithCA(t, db, &config.Config{CertsDir: certsDir}, nil)
	client := newClient(t)
	token := login(t, client, srv)

	reqBody := `[
		{"name":"good-item","domain":"good.example.com","certPath":"good-item/certificate.crt"},
		{"name":"bad-item","domain":"bad.example.com","certPath":"missing-item/certificate.crt"}
	]`
	resp := doRequest(t, client, http.MethodPost, srv.URL+"/api/v1/certificates/import/paths", bytes.NewReader([]byte(reqBody)), func(r *http.Request) { //nolint:bodyclose // closed via t.Cleanup inside doRequest
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", token)
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 (bulk operations never fail the whole request for a partial failure)", resp.StatusCode)
	}
	body := decodeJSONBody(t, resp)
	results, _ := body["results"].([]any)
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}
	byRef := map[string]map[string]any{}
	for _, raw := range results {
		r, _ := raw.(map[string]any)
		byRef[r["ref"].(string)] = r
	}
	if byRef["good-item"]["status"] != "ok" {
		t.Fatalf("good-item status = %v, want ok", byRef["good-item"]["status"])
	}
	if byRef["bad-item"]["status"] != "error" {
		t.Fatalf("bad-item status = %v, want error", byRef["bad-item"]["status"])
	}
	if errMsg, _ := byRef["bad-item"]["error"].(string); errMsg == "" {
		t.Fatal("bad-item carries no error message")
	}

	if row, err := appdb.GetCertByName(db, "good-item"); err != nil || row == nil {
		t.Fatalf("good-item was not persisted: %v", err)
	}
	if row, _ := appdb.GetCertByName(db, "bad-item"); row != nil {
		t.Fatal("bad-item must not be persisted")
	}
}

// TestListCertificateImportCandidates_ReflectsDisk proves
// handlers.ImportCandidates against a real filesystem fixture: a cert file
// on disk with no matching database row must be listed; one already
// registered (insertTestCert below writes the DB row directly, matching
// serial) must not be.
func TestListCertificateImportCandidates_ReflectsDisk(t *testing.T) {
	db := openCertTestDB(t)
	certsDir := t.TempDir()

	strayDir := filepath.Join(certsDir, "stray-cert")
	if err := os.MkdirAll(strayDir, 0o750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	strayCertPEM, strayKeyPEM := genSelfSignedPEM(t, "stray.example.com", time.Now().Add(365*24*time.Hour))
	if err := os.WriteFile(filepath.Join(strayDir, "certificate.crt"), strayCertPEM, 0o600); err != nil {
		t.Fatalf("WriteFile(cert): %v", err)
	}
	if err := os.WriteFile(filepath.Join(strayDir, "private.key"), strayKeyPEM, 0o600); err != nil {
		t.Fatalf("WriteFile(key): %v", err)
	}

	// A registered certificate, on disk under the same root, must not show
	// up as a candidate: scanExistingCerts checks the serial against the
	// database (cert_ops.go).
	registeredDir := filepath.Join(certsDir, "registered-cert")
	if err := os.MkdirAll(registeredDir, 0o750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	registeredCertPEM, _ := genSelfSignedPEM(t, "registered.example.com", time.Now().Add(365*24*time.Hour))
	registeredCertPath := filepath.Join(registeredDir, "certificate.crt")
	if err := os.WriteFile(registeredCertPath, registeredCertPEM, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	block, _ := pem.Decode(registeredCertPEM)
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("ParseCertificate: %v", err)
	}
	if err := appdb.InsertCert(db, &models.Certificate{
		Name: "registered-cert", Domain: "registered.example.com", CertPath: registeredCertPath, KeyPath: "",
		Serial: cert.SerialNumber.String(), KeyType: "EC:P-256", IssueDuration: "8760h",
	}); err != nil {
		t.Fatalf("InsertCert: %v", err)
	}

	srv := newCertTestServerWithCA(t, db, &config.Config{CertsDir: certsDir}, nil)
	client := newClient(t)
	login(t, client, srv)

	// listCertificateImportCandidates answers a bare JSON array, not an
	// object, so this cannot use doGet (mount_test.go), which only decodes
	// into map[string]any.
	resp := doRequest(t, client, http.MethodGet, srv.URL+"/api/v1/certificates/import-candidates", nil, nil) //nolint:bodyclose // closed via t.Cleanup inside doRequest
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var candidates []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&candidates); err != nil {
		t.Fatalf("decode candidates: %v", err)
	}

	var names []string
	for _, item := range candidates {
		names = append(names, item["name"].(string))
	}
	if !containsStr(names, "stray-cert") {
		t.Fatalf("candidates = %v, want stray-cert present", names)
	}
	if containsStr(names, "registered-cert") {
		t.Fatalf("candidates = %v, want registered-cert absent (already in the database)", names)
	}
}

// containsStr is a small local helper; this file has no other use for a
// generic slice-contains and importing a library for one check is not
// worth it.
func containsStr(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

// ─── Phase 4, third batch: downloads and GET /api/v1/ca ────────────────────
//
// R23's observed-cost record, final entry: every success path below needed
// only a real file on disk (the certificate/key cases) or a real root/
// intermediate CA pair (the CA cases), both of which this file's existing
// helpers already produce for the issue/renew tests above. No new fixture
// machinery, which is most of why this batch priced out cheap rather than
// expensive despite being five operations: the download mechanic itself
// (binaryFileOutput, newBinaryFileOutput) is the one genuinely new piece,
// and it is shared by all five, so its cost is paid once, not five times.

// TestGetCertificateCertificate_Success proves the certificate download's
// headers and body against a real file, the same filename convention
// DownloadCert already used (handlers/certs.go).
func TestGetCertificateCertificate_Success(t *testing.T) {
	db := openCertTestDB(t)
	certsDir := t.TempDir()
	certDir := filepath.Join(certsDir, "dl-cert")
	if err := os.MkdirAll(certDir, 0o750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	certPath := filepath.Join(certDir, "certificate.crt")
	certPEM, _ := genSelfSignedPEM(t, "dl.example.com", time.Now().Add(365*24*time.Hour))
	if err := os.WriteFile(certPath, certPEM, 0o600); err != nil {
		t.Fatalf("WriteFile(cert): %v", err)
	}
	if err := appdb.InsertCert(db, &models.Certificate{
		Name: "dl-cert", Domain: "dl.example.com", CertPath: certPath, KeyPath: filepath.Join(certDir, "private.key"),
		Serial: "dl-cert-serial", KeyType: "EC:P-256", IssueDuration: "8760h",
	}); err != nil {
		t.Fatalf("InsertCert: %v", err)
	}
	row, err := appdb.GetCertByName(db, "dl-cert")
	if err != nil || row == nil {
		t.Fatalf("GetCertByName: %v", err)
	}

	srv := newCertTestServerWithCA(t, db, &config.Config{CertsDir: certsDir}, nil)
	client := newClient(t)
	login(t, client, srv) // testUser.Role defaults to "manager"

	resp := doRequest(t, client, http.MethodGet, srv.URL+"/api/v1/certificates/"+strconv.Itoa(row.ID)+"/certificate", nil, nil) //nolint:bodyclose // closed via t.Cleanup inside doRequest
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/x-pem-file" {
		t.Fatalf("Content-Type = %q, want application/x-pem-file", ct)
	}
	if cd := resp.Header.Get("Content-Disposition"); cd != "attachment; filename=dl-cert.crt" {
		t.Fatalf("Content-Disposition = %q, want attachment; filename=dl-cert.crt", cd)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if !bytes.Equal(body, certPEM) {
		t.Fatalf("body does not match the certificate file on disk")
	}
}

func TestGetCertificateCertificate_UnknownIDIs404(t *testing.T) {
	db := openCertTestDB(t)
	srv := newCertTestServerWithCA(t, db, &config.Config{CertsDir: t.TempDir()}, nil)
	client := newClient(t)
	login(t, client, srv)

	resp := doRequest(t, client, http.MethodGet, srv.URL+"/api/v1/certificates/999999/certificate", nil, nil) //nolint:bodyclose // closed via t.Cleanup inside doRequest
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/problem+json" {
		t.Fatalf("Content-Type = %q, want application/problem+json", ct)
	}
}

// TestGetCertificateKey_Success proves the key download's headers and
// body, plus DownloadKey's audit side effect (every key download is
// logged, regardless of which route served it): a security-log row must
// exist afterward naming this certificate.
func TestGetCertificateKey_Success(t *testing.T) {
	db := openCertTestDB(t)
	certsDir := t.TempDir()
	certDir := filepath.Join(certsDir, "dl-key-cert")
	if err := os.MkdirAll(certDir, 0o750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	keyPath := filepath.Join(certDir, "private.key")
	_, keyPEM := genSelfSignedPEM(t, "dlkey.example.com", time.Now().Add(365*24*time.Hour))
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		t.Fatalf("WriteFile(key): %v", err)
	}
	if err := appdb.InsertCert(db, &models.Certificate{
		Name: "dl-key-cert", Domain: "dlkey.example.com", CertPath: filepath.Join(certDir, "certificate.crt"), KeyPath: keyPath,
		Serial: "dl-key-cert-serial", KeyType: "EC:P-256", IssueDuration: "8760h",
	}); err != nil {
		t.Fatalf("InsertCert: %v", err)
	}
	row, err := appdb.GetCertByName(db, "dl-key-cert")
	if err != nil || row == nil {
		t.Fatalf("GetCertByName: %v", err)
	}

	srv := newCertTestServerWithCA(t, db, &config.Config{CertsDir: certsDir}, nil)
	client := newClient(t)
	login(t, client, srv)

	resp := doRequest(t, client, http.MethodGet, srv.URL+"/api/v1/certificates/"+strconv.Itoa(row.ID)+"/key", nil, nil) //nolint:bodyclose // closed via t.Cleanup inside doRequest
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if cd := resp.Header.Get("Content-Disposition"); cd != "attachment; filename=dl-key-cert.key" {
		t.Fatalf("Content-Disposition = %q, want attachment; filename=dl-key-cert.key", cd)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if !bytes.Equal(body, keyPEM) {
		t.Fatalf("body does not match the key file on disk")
	}

	var count int
	if err := db.QueryRow( //nolint:noctx // test assertion
		`SELECT count(*) FROM auth_log WHERE reason LIKE 'Audit: certificate.key_download%dl-key-cert%'`,
	).Scan(&count); err != nil {
		t.Fatalf("query auth_log: %v", err)
	}
	if count == 0 {
		t.Fatal("no security-log row for the key download, want DownloadKey's audit side effect preserved")
	}
}

// TestGetCA_Success proves GET /api/v1/ca's data against the real root CA
// certificate stepca-bootstrap-style fixtures provide via
// handlers.NewWithFS's RootCert config, matching readPEMCert's own
// expectations (health.go). Since no CA client is configured, Reachable
// must be false rather than the operation failing outright.
func TestGetCA_Success(t *testing.T) {
	db := openCertTestDB(t)
	rootDir := t.TempDir()
	rootPath := filepath.Join(rootDir, "root_ca.crt")
	rootPEM, _ := genSelfSignedPEM(t, "Test Root CA", time.Now().Add(10*365*24*time.Hour))
	if err := os.WriteFile(rootPath, rootPEM, 0o600); err != nil {
		t.Fatalf("WriteFile(root): %v", err)
	}

	srv := newCertTestServerWithCA(t, db, &config.Config{RootCert: rootPath}, nil)
	client := newClient(t)
	login(t, client, srv) // testUser.Role defaults to "manager", GET /api/v1/ca is viewer

	result := doGet(t, client, srv.URL+"/api/v1/ca")
	if result.status != http.StatusOK {
		t.Fatalf("status = %d, want 200", result.status)
	}
	if result.body["subject"] != "CN=Test Root CA" {
		t.Fatalf("subject = %v, want CN=Test Root CA", result.body["subject"])
	}
	if fp, _ := result.body["fingerprintSha256"].(string); fp == "" {
		t.Fatal("fingerprintSha256 is empty")
	}
	if result.body["reachable"] != false {
		t.Fatalf("reachable = %v, want false (no CA client configured in this test)", result.body["reachable"])
	}
	for _, key := range []string{"notBefore", "notAfter"} {
		if _, ok := result.body[key]; !ok {
			t.Errorf("response missing %q", key)
		}
	}
}

func TestGetCA_MissingRootCertIsError(t *testing.T) {
	db := openCertTestDB(t)
	srv := newCertTestServerWithCA(t, db, &config.Config{RootCert: filepath.Join(t.TempDir(), "absent.crt")}, nil)
	client := newClient(t)
	login(t, client, srv)

	resp := doRequest(t, client, http.MethodGet, srv.URL+"/api/v1/ca", nil, nil) //nolint:bodyclose // closed via t.Cleanup inside doRequest
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (a missing root cert is an installation error, not merely unreachable)", resp.StatusCode)
	}
}

// TestGetCARoot_Success, TestGetCAIntermediate_Success and
// TestGetCAChain_Success are the admin-only CA chain downloads' success
// paths, each against a fabricated root/intermediate pair under one
// t.TempDir(), mirroring intermediateCertPath()'s convention
// (filepath.Dir(RootCert)/intermediate_ca.crt).
func TestGetCARoot_Success(t *testing.T) {
	db := openCertTestDB(t)
	rootDir := t.TempDir()
	rootPath := filepath.Join(rootDir, "root_ca.crt")
	rootPEM, _ := genSelfSignedPEM(t, "Test Root CA", time.Now().Add(10*365*24*time.Hour))
	if err := os.WriteFile(rootPath, rootPEM, 0o600); err != nil {
		t.Fatalf("WriteFile(root): %v", err)
	}

	srv := newCertTestServerWithCA(t, db, &config.Config{RootCert: rootPath}, nil)
	client := newClient(t)
	login(t, client, srv)
	original := testUser.Role
	testUser.Role = "admin"
	t.Cleanup(func() { testUser.Role = original })

	resp := doRequest(t, client, http.MethodGet, srv.URL+"/api/v1/ca/root", nil, nil) //nolint:bodyclose // closed via t.Cleanup inside doRequest
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if cd := resp.Header.Get("Content-Disposition"); cd != "attachment; filename=home-ca-root.crt" {
		t.Fatalf("Content-Disposition = %q, want attachment; filename=home-ca-root.crt", cd)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if !bytes.Equal(body, rootPEM) {
		t.Fatal("body does not match the root CA file on disk")
	}
}

func TestGetCAIntermediate_Success(t *testing.T) {
	db := openCertTestDB(t)
	rootDir := t.TempDir()
	rootPath := filepath.Join(rootDir, "root_ca.crt")
	rootPEM, _ := genSelfSignedPEM(t, "Test Root CA", time.Now().Add(10*365*24*time.Hour))
	if err := os.WriteFile(rootPath, rootPEM, 0o600); err != nil {
		t.Fatalf("WriteFile(root): %v", err)
	}
	intermediatePEM, _ := genSelfSignedPEM(t, "Test Intermediate CA", time.Now().Add(5*365*24*time.Hour))
	if err := os.WriteFile(filepath.Join(rootDir, "intermediate_ca.crt"), intermediatePEM, 0o600); err != nil {
		t.Fatalf("WriteFile(intermediate): %v", err)
	}

	srv := newCertTestServerWithCA(t, db, &config.Config{RootCert: rootPath}, nil)
	client := newClient(t)
	login(t, client, srv)
	original := testUser.Role
	testUser.Role = "admin"
	t.Cleanup(func() { testUser.Role = original })

	resp := doRequest(t, client, http.MethodGet, srv.URL+"/api/v1/ca/intermediate", nil, nil) //nolint:bodyclose // closed via t.Cleanup inside doRequest
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if cd := resp.Header.Get("Content-Disposition"); cd != "attachment; filename=home-ca-intermediate.crt" {
		t.Fatalf("Content-Disposition = %q, want attachment; filename=home-ca-intermediate.crt", cd)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if !bytes.Equal(body, intermediatePEM) {
		t.Fatal("body does not match the intermediate CA file on disk")
	}
}

// TestGetCAChain_Success asserts the assembled chain is the intermediate
// followed by the root with nothing else, the same ordering
// DownloadFullChain already produced (handlers/certs.go).
func TestGetCAChain_Success(t *testing.T) {
	db := openCertTestDB(t)
	rootDir := t.TempDir()
	rootPath := filepath.Join(rootDir, "root_ca.crt")
	rootPEM, _ := genSelfSignedPEM(t, "Test Root CA", time.Now().Add(10*365*24*time.Hour))
	if err := os.WriteFile(rootPath, rootPEM, 0o600); err != nil {
		t.Fatalf("WriteFile(root): %v", err)
	}
	intermediatePEM, _ := genSelfSignedPEM(t, "Test Intermediate CA", time.Now().Add(5*365*24*time.Hour))
	if err := os.WriteFile(filepath.Join(rootDir, "intermediate_ca.crt"), intermediatePEM, 0o600); err != nil {
		t.Fatalf("WriteFile(intermediate): %v", err)
	}

	srv := newCertTestServerWithCA(t, db, &config.Config{RootCert: rootPath}, nil)
	client := newClient(t)
	login(t, client, srv)
	original := testUser.Role
	testUser.Role = "admin"
	t.Cleanup(func() { testUser.Role = original })

	resp := doRequest(t, client, http.MethodGet, srv.URL+"/api/v1/ca/chain", nil, nil) //nolint:bodyclose // closed via t.Cleanup inside doRequest
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if cd := resp.Header.Get("Content-Disposition"); cd != "attachment; filename=home-ca-full-chain.crt" {
		t.Fatalf("Content-Disposition = %q, want attachment; filename=home-ca-full-chain.crt", cd)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	wantIdx := 0
	gotIdx := bytes.Index(body, intermediatePEM)
	if gotIdx != wantIdx {
		t.Fatalf("intermediate does not come first in the chain (index %d)", gotIdx)
	}
	rootIdx := bytes.Index(body, rootPEM)
	if rootIdx <= gotIdx {
		t.Fatalf("root does not follow the intermediate in the chain")
	}
}
