package api_test

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"testing"
)

// TestListCertificates_RequiresSession and TestGetCertificateOptions_Requires
// Session mirror TestStatus_RequiresSession (mount_test.go): the session
// middleware answers 401 before any operation handler runs, so these need
// no database despite listCertificates and getCertificate otherwise
// touching one.
func TestListCertificates_RequiresSession(t *testing.T) {
	srv := newTestServer(t)
	client := newClient(t)

	resp := doRequest(t, client, http.MethodGet, srv.URL+"/api/v1/certificates", nil, nil) //nolint:bodyclose // closed via t.Cleanup inside doRequest
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestGetCertificate_RequiresSession(t *testing.T) {
	srv := newTestServer(t)
	client := newClient(t)

	resp := doRequest(t, client, http.MethodGet, srv.URL+"/api/v1/certificates/1", nil, nil) //nolint:bodyclose // closed via t.Cleanup inside doRequest
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestGetCertificateOptions_RequiresSession(t *testing.T) {
	srv := newTestServer(t)
	client := newClient(t)

	resp := doRequest(t, client, http.MethodGet, srv.URL+"/api/v1/certificates/options", nil, nil) //nolint:bodyclose // closed via t.Cleanup inside doRequest
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

// TestGetCertificateOptions_ViewerDenied is the role-denial case the task
// specifically asks for: GET /api/v1/certificates/options is manager-only
// (Section 6), so a viewer session must get 403, not the options body.
func TestGetCertificateOptions_ViewerDenied(t *testing.T) {
	srv := newTestServer(t)
	client := newClient(t)
	login(t, client, srv)

	original := testUser.Role
	testUser.Role = "viewer"
	t.Cleanup(func() { testUser.Role = original })

	resp := doRequest(t, client, http.MethodGet, srv.URL+"/api/v1/certificates/options", nil, nil) //nolint:bodyclose // closed via t.Cleanup inside doRequest
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
}

// multipartImportBody builds a POST /api/v1/certificates/import/upload body
// with the given name/domain and an optional cert/key payload, mirroring
// mount_test.go's multipartUploadBody for the spike upload.
func multipartImportBody(t *testing.T, name, domain string, includeCert bool) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if name != "" {
		if err := mw.WriteField("name", name); err != nil {
			t.Fatalf("WriteField(name): %v", err)
		}
	}
	if domain != "" {
		if err := mw.WriteField("domain", domain); err != nil {
			t.Fatalf("WriteField(domain): %v", err)
		}
	}
	if includeCert {
		fw, err := mw.CreateFormFile("certFile", "certificate.crt")
		if err != nil {
			t.Fatalf("CreateFormFile(certFile): %v", err)
		}
		if _, err := fw.Write([]byte("not a real cert")); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("mw.Close: %v", err)
	}
	return &buf, mw.FormDataContentType()
}

// TestCreateCertificate_RequiresSession and the sibling Requires/Denied
// tests below mirror TestStatus_RequiresSession and
// TestGetCertificateOptions_ViewerDenied: the session and role middleware
// answer before any operation handler runs, so none of these touch a
// database despite the operations themselves needing one on the success path.
func TestCreateCertificate_RequiresSession(t *testing.T) {
	srv := newTestServer(t)
	client := newClient(t)

	resp := doRequest(t, client, http.MethodPost, srv.URL+"/api/v1/certificates", nil, nil) //nolint:bodyclose // closed via t.Cleanup inside doRequest
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestCreateCertificate_ViewerDenied(t *testing.T) {
	srv := newTestServer(t)
	client := newClient(t)
	token := login(t, client, srv)

	original := testUser.Role
	testUser.Role = "viewer"
	t.Cleanup(func() { testUser.Role = original })

	resp := doRequest(t, client, http.MethodPost, srv.URL+"/api/v1/certificates", bytes.NewReader([]byte(`{"name":"x","domain":"x.example.com"}`)), func(r *http.Request) { //nolint:bodyclose // closed via t.Cleanup inside doRequest
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", token)
	})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
}

func TestListCertificateImportCandidates_RequiresSession(t *testing.T) {
	srv := newTestServer(t)
	client := newClient(t)

	resp := doRequest(t, client, http.MethodGet, srv.URL+"/api/v1/certificates/import-candidates", nil, nil) //nolint:bodyclose // closed via t.Cleanup inside doRequest
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestImportCertificateUpload_RequiresSession(t *testing.T) {
	srv := newTestServer(t)
	client := newClient(t)

	buf, contentType := multipartImportBody(t, "name", "domain.example.com", true)
	resp := doRequest(t, client, http.MethodPost, srv.URL+"/api/v1/certificates/import/upload", buf, func(r *http.Request) { //nolint:bodyclose // closed via t.Cleanup inside doRequest
		r.Header.Set("Content-Type", contentType)
	})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

// TestImportCertificateUpload_MalformedUploadIsUnprocessableEntity proves
// huma's schema validation (422, not 400) on this operation specifically:
// certFile is required, and omitting it fails before handlers.
// ImportCertificateUpload ever runs.
func TestImportCertificateUpload_MalformedUploadIsUnprocessableEntity(t *testing.T) {
	srv := newTestServer(t)
	client := newClient(t)
	token := login(t, client, srv)

	buf, contentType := multipartImportBody(t, "name", "domain.example.com", false)
	resp := doRequest(t, client, http.MethodPost, srv.URL+"/api/v1/certificates/import/upload", buf, func(r *http.Request) { //nolint:bodyclose // closed via t.Cleanup inside doRequest
		r.Header.Set("Content-Type", contentType)
		r.Header.Set("X-CSRF-Token", token)
	})
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/problem+json" {
		t.Fatalf("Content-Type = %q, want application/problem+json", ct)
	}
}

func TestImportCertificatesByPaths_RequiresSession(t *testing.T) {
	srv := newTestServer(t)
	client := newClient(t)

	resp := doRequest(t, client, http.MethodPost, srv.URL+"/api/v1/certificates/import/paths", bytes.NewReader([]byte(`[]`)), func(r *http.Request) { //nolint:bodyclose // closed via t.Cleanup inside doRequest
		r.Header.Set("Content-Type", "application/json")
	})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestRenewCertificate_RequiresSession(t *testing.T) {
	srv := newTestServer(t)
	client := newClient(t)

	resp := doRequest(t, client, http.MethodPost, srv.URL+"/api/v1/certificates/1/renew", nil, nil) //nolint:bodyclose // closed via t.Cleanup inside doRequest
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestRevokeCertificate_RequiresSession(t *testing.T) {
	srv := newTestServer(t)
	client := newClient(t)

	resp := doRequest(t, client, http.MethodPost, srv.URL+"/api/v1/certificates/1/revoke", nil, nil) //nolint:bodyclose // closed via t.Cleanup inside doRequest
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

// TestRevokeCertificate_ManagerDenied is the elevated-role case Section 6
// calls out: revoke is admin, every other certificate mutation in this file
// is manager, so a manager session (which satisfies every other operation
// here) must still get 403 on revoke.
func TestRevokeCertificate_ManagerDenied(t *testing.T) {
	srv := newTestServer(t)
	client := newClient(t)
	token := login(t, client, srv) // testUser.Role defaults to "manager"

	resp := doRequest(t, client, http.MethodPost, srv.URL+"/api/v1/certificates/1/revoke", nil, func(r *http.Request) { //nolint:bodyclose // closed via t.Cleanup inside doRequest
		r.Header.Set("X-CSRF-Token", token)
	})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
}

// TestGetCertificateOptions_ManagerSucceeds proves the options data itself
// (templates, key types, durations, the domain-suffix policy) with no
// database and no CA: CertificateIssueOptions degrades provisioners to an
// empty list on a CA error, matching Provisioners' (the template route's
// handler) existing behaviour.
func TestGetCertificateOptions_ManagerSucceeds(t *testing.T) {
	srv := newTestServer(t)
	client := newClient(t)
	login(t, client, srv) // testUser.Role defaults to "manager"

	result := doGet(t, client, srv.URL+"/api/v1/certificates/options")
	if result.status != http.StatusOK {
		t.Fatalf("status = %d, want 200", result.status)
	}

	templates, _ := result.body["templates"].([]any)
	if len(templates) != 4 {
		t.Fatalf("templates: got %d entries, want 4", len(templates))
	}
	first, _ := templates[0].(map[string]any)
	if first["id"] != "server" || first["defaultKeyType"] != "EC:P-256" || first["defaultDuration"] != "8760h" {
		t.Fatalf("templates[0] = %v, want the server template's defaults", first)
	}

	keyTypes, _ := result.body["keyTypes"].([]any)
	if len(keyTypes) != 4 {
		t.Fatalf("keyTypes: got %d entries, want 4", len(keyTypes))
	}

	durations, _ := result.body["durations"].([]any)
	if len(durations) != 4 {
		t.Fatalf("durations: got %d entries, want 4", len(durations))
	}

	if provisioners, ok := result.body["provisioners"]; ok && provisioners != nil {
		if list, _ := provisioners.([]any); len(list) != 0 {
			t.Fatalf("provisioners = %v, want empty (no CA configured in this test)", provisioners)
		}
	}

	if _, ok := result.body["allowedDomainSuffixes"]; !ok {
		t.Fatal("response missing allowedDomainSuffixes")
	}
	if result.body["defaultTemplate"] != "server" {
		t.Fatalf("defaultTemplate = %v, want server", result.body["defaultTemplate"])
	}
}

// ─── Phase 4, third batch: downloads and GET /api/v1/ca ────────────────────
//
// The session- and role-denial cases below mirror the pattern every prior
// Phase 4 operation test uses: the session and role middleware answer
// before the operation handler (and therefore the database or CA client)
// is ever reached, so none of these need a real database. The success
// paths (actual byte content, correct headers) are integration-only,
// certificates_integration_test.go, because they need a real certificate
// file or a real root CA certificate on disk.

func TestGetCertificateCertificate_RequiresSession(t *testing.T) {
	srv := newTestServer(t)
	client := newClient(t)

	resp := doRequest(t, client, http.MethodGet, srv.URL+"/api/v1/certificates/1/certificate", nil, nil) //nolint:bodyclose // closed via t.Cleanup inside doRequest
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

// TestGetCertificateCertificate_ViewerDenied is this batch's elevated-role
// case: both downloads are manager, same as every other certificate
// mutation except revoke, so a viewer session (which satisfies list,
// detail and GET /api/v1/ca) must still get 403 here.
func TestGetCertificateCertificate_ViewerDenied(t *testing.T) {
	srv := newTestServer(t)
	client := newClient(t)
	login(t, client, srv)

	original := testUser.Role
	testUser.Role = "viewer"
	t.Cleanup(func() { testUser.Role = original })

	resp := doRequest(t, client, http.MethodGet, srv.URL+"/api/v1/certificates/1/certificate", nil, nil) //nolint:bodyclose // closed via t.Cleanup inside doRequest
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
}

func TestGetCertificateKey_RequiresSession(t *testing.T) {
	srv := newTestServer(t)
	client := newClient(t)

	resp := doRequest(t, client, http.MethodGet, srv.URL+"/api/v1/certificates/1/key", nil, nil) //nolint:bodyclose // closed via t.Cleanup inside doRequest
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestGetCertificateKey_ViewerDenied(t *testing.T) {
	srv := newTestServer(t)
	client := newClient(t)
	login(t, client, srv)

	original := testUser.Role
	testUser.Role = "viewer"
	t.Cleanup(func() { testUser.Role = original })

	resp := doRequest(t, client, http.MethodGet, srv.URL+"/api/v1/certificates/1/key", nil, nil) //nolint:bodyclose // closed via t.Cleanup inside doRequest
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
}

func TestGetCA_RequiresSession(t *testing.T) {
	srv := newTestServer(t)
	client := newClient(t)

	resp := doRequest(t, client, http.MethodGet, srv.URL+"/api/v1/ca", nil, nil) //nolint:bodyclose // closed via t.Cleanup inside doRequest
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

// TestGetCARoot_RequiresSession and its two siblings below assert all three
// CA chain downloads require a session, same shape as every other
// RequiresSession test; the role-denial case for all three is
// TestGetCARoot_ManagerDenied, since Section 6 makes every one admin.
func TestGetCARoot_RequiresSession(t *testing.T) {
	srv := newTestServer(t)
	client := newClient(t)

	resp := doRequest(t, client, http.MethodGet, srv.URL+"/api/v1/ca/root", nil, nil) //nolint:bodyclose // closed via t.Cleanup inside doRequest
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestGetCAIntermediate_RequiresSession(t *testing.T) {
	srv := newTestServer(t)
	client := newClient(t)

	resp := doRequest(t, client, http.MethodGet, srv.URL+"/api/v1/ca/intermediate", nil, nil) //nolint:bodyclose // closed via t.Cleanup inside doRequest
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestGetCAChain_RequiresSession(t *testing.T) {
	srv := newTestServer(t)
	client := newClient(t)

	resp := doRequest(t, client, http.MethodGet, srv.URL+"/api/v1/ca/chain", nil, nil) //nolint:bodyclose // closed via t.Cleanup inside doRequest
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

// TestGetCARoot_ManagerDenied is the role-denial case Section 6 calls out
// for the CA chain downloads: they are admin, unlike every certificate
// download above (manager), so a manager session (testUser.Role's default)
// must get 403 on all three.
func TestGetCARoot_ManagerDenied(t *testing.T) {
	srv := newTestServer(t)
	client := newClient(t)
	login(t, client, srv) // testUser.Role defaults to "manager"

	for _, path := range []string{"/api/v1/ca/root", "/api/v1/ca/intermediate", "/api/v1/ca/chain"} {
		resp := doRequest(t, client, http.MethodGet, srv.URL+path, nil, nil) //nolint:bodyclose // closed via t.Cleanup inside doRequest
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("%s: status = %d, want 403", path, resp.StatusCode)
		}
	}
}
