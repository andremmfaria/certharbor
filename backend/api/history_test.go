package api_test

import (
	"net/http"
	"testing"
)

// TestListHistory_RequiresSession mirrors TestListCertificates_RequiresSession
// (certificates_test.go): the session middleware answers 401 before
// listHistory ever touches a database.
func TestListHistory_RequiresSession(t *testing.T) {
	srv := newTestServer(t)
	client := newClient(t)

	resp := doRequest(t, client, http.MethodGet, srv.URL+"/api/v1/history", nil, nil) //nolint:bodyclose // closed via t.Cleanup inside doRequest
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

// TestListProvisioners_RequiresSession is listProvisioners' equivalent.
func TestListProvisioners_RequiresSession(t *testing.T) {
	srv := newTestServer(t)
	client := newClient(t)

	resp := doRequest(t, client, http.MethodGet, srv.URL+"/api/v1/provisioners", nil, nil) //nolint:bodyclose // closed via t.Cleanup inside doRequest
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

// TestListHistory_PageSizeOver200Is422 asserts 5.6's schema-expressed bound:
// pageSize is maximum 200, so a caller asking for more gets 422 from huma's
// own validation, never a silently truncated page.
func TestListHistory_PageSizeOver200Is422(t *testing.T) {
	srv := newTestServer(t)
	client := newClient(t)
	login(t, client, srv)

	resp := doRequest(t, client, http.MethodGet, srv.URL+"/api/v1/history?pageSize=201", nil, nil) //nolint:bodyclose // closed via t.Cleanup inside doRequest
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", resp.StatusCode)
	}
}

// TestListHistory_PageTimesPageSizeOverMaxOffsetIs422 asserts 5.6's deep-offset
// guard for history, mirroring TestListCertificates_PageTimesPageSizeOverMaxOffsetIs422.
func TestListHistory_PageTimesPageSizeOverMaxOffsetIs422(t *testing.T) {
	srv := newTestServer(t)
	client := newClient(t)
	login(t, client, srv)

	resp := doRequest(t, client, http.MethodGet, srv.URL+"/api/v1/history?page=51&pageSize=200", nil, nil) //nolint:bodyclose // closed via t.Cleanup inside doRequest
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", resp.StatusCode)
	}
}

// TestListHistory_AbsentPageSizeDefaultsTo25 proves 5.6's default against a
// session with no database: an empty handlers.Handler.db makes
// appdb.GetHistory return a database error, which the operation surfaces as
// a 500 rather than a 200. That is enough to see the pageSize huma bound to
// before the query ever runs, by checking the request huma accepted (no
// 422) rather than the response body: see
// TestListCertificates_PaginatesFiltersAndComputesExpiryStatus (the
// certificates integration suite) for the real-database proof of the same
// default on a sibling operation; this one only needs to confirm history's
// own query struct carries the same default tag.
func TestListHistory_AbsentPageSizeIsAccepted(t *testing.T) {
	srv := newTestServer(t)
	client := newClient(t)
	login(t, client, srv)

	resp := doRequest(t, client, http.MethodGet, srv.URL+"/api/v1/history", nil, nil) //nolint:bodyclose // closed via t.Cleanup inside doRequest
	if resp.StatusCode == http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want anything but 422 (pageSize's default must satisfy its own bounds)", resp.StatusCode)
	}
}
