package api_test

import (
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
