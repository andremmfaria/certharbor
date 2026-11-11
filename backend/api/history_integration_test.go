//go:build integration

package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
	"time"

	"certharbor/config"
	appdb "certharbor/db"
	"certharbor/stepca"
)

// TestListHistory_AbsentPageSizeDefaultsTo25 is 5.6's default criterion
// against history specifically: 30 rows, no pageSize on the request, must
// come back as a 25-item first page.
func TestListHistory_AbsentPageSizeDefaultsTo25(t *testing.T) {
	db := openCertTestDB(t)
	srv := newCertTestServer(t, db)
	client := newClient(t)
	login(t, client, srv)

	for i := 0; i < 30; i++ {
		if err := appdb.InsertHistory(db, "issue", "default-cert", "default.example.com", "", "tester", "admin"); err != nil {
			t.Fatalf("InsertHistory %d: %v", i, err)
		}
	}

	result := doGet(t, client, srv.URL+"/api/v1/history")
	if result.status != http.StatusOK {
		t.Fatalf("status = %d, want 200", result.status)
	}
	items, _ := result.body["items"].([]any)
	if len(items) != 25 {
		t.Fatalf("got %d items with no pageSize, want 25 (the default)", len(items))
	}
	if pageSize, _ := result.body["pageSize"].(float64); pageSize != 25 {
		t.Fatalf("pageSize = %v, want 25", result.body["pageSize"])
	}
}

// TestListHistory_TiedTimestampPaginationThroughHTTP is the HTTP-level
// proof of the same property db/pagination_integration_test.go asserts at
// the query layer: 60 rows sharing one created_at, paged at 10 through the
// real operation (filters, envelope and all), produce 60 distinct ids
// across 6 pages with no repeat and no omission.
func TestListHistory_TiedTimestampPaginationThroughHTTP(t *testing.T) {
	db := openCertTestDB(t)
	srv := newCertTestServer(t, db)
	client := newClient(t)
	login(t, client, srv)

	tied := time.Now().UTC().Truncate(time.Second)
	const total = 60
	for i := 0; i < total; i++ {
		if _, err := db.Exec( //nolint:noctx // test setup
			`INSERT INTO cert_history (action,cert_name,domain,details,username,role,created_at) VALUES ($1,$2,$3,$4,$5,$6,$7)`,
			"issue", "http-tied-cert", "http-tied.example.com", "", "tester", "admin", tied,
		); err != nil {
			t.Fatalf("insert row %d: %v", i, err)
		}
	}

	seen := map[float64]bool{}
	for page := 1; page <= total/10; page++ {
		result := doGet(t, client, srv.URL+"/api/v1/history?page="+strconv.Itoa(page)+"&pageSize=10")
		if result.status != http.StatusOK {
			t.Fatalf("page %d: status = %d, want 200", page, result.status)
		}
		items, _ := result.body["items"].([]any)
		if len(items) != 10 {
			t.Fatalf("page %d: got %d items, want 10", page, len(items))
		}
		for _, raw := range items {
			item, _ := raw.(map[string]any)
			id, _ := item["id"].(float64)
			if seen[id] {
				t.Fatalf("page %d: id %v repeats a row already seen on an earlier page", page, id)
			}
			seen[id] = true
		}
	}
	if len(seen) != total {
		t.Fatalf("saw %d distinct ids across %d pages, want %d", len(seen), total/10, total)
	}
}

// fakeProvisionerCA is a minimal stepca.CA double exercising only
// Provisioners; the other three methods are never called by
// listProvisioners (handlers.ListProvisioners) and exist only to satisfy
// the interface, mirroring fakeIssueCA's shape in
// certificates_integration_test.go.
type fakeProvisionerCA struct {
	list []stepca.ProvisionerInfo
	err  error
}

var _ stepca.CA = (*fakeProvisionerCA)(nil)

func (f *fakeProvisionerCA) Health(context.Context) error { return nil }

func (f *fakeProvisionerCA) Provisioners(context.Context) ([]stepca.ProvisionerInfo, error) {
	return f.list, f.err
}

func (f *fakeProvisionerCA) IssueCertificate(context.Context, stepca.IssueRequest) ([]byte, []byte, error) {
	return nil, nil, nil
}

func (f *fakeProvisionerCA) Revoke(context.Context, string, string) error { return nil }

// TestListProvisioners_Success proves listProvisioners against a fake CA
// client returning a fixed provisioner list.
func TestListProvisioners_Success(t *testing.T) {
	db := openCertTestDB(t)
	ca := &fakeProvisionerCA{list: []stepca.ProvisionerInfo{
		{Name: "admin", Type: "JWK"},
		{Name: "acme", Type: "ACME"},
	}}
	srv := newCertTestServerWithCA(t, db, &config.Config{}, ca)
	client := newClient(t)
	login(t, client, srv)

	resp := doRequest(t, client, http.MethodGet, srv.URL+"/api/v1/provisioners", nil, nil) //nolint:bodyclose // closed via t.Cleanup inside doRequest
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var provs []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&provs); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(provs) != 2 || provs[0]["name"] != "admin" || provs[1]["type"] != "ACME" {
		t.Fatalf("provisioners = %v, want [{name:admin...}, {name:acme type:ACME}]", provs)
	}
}

// TestListProvisioners_DegradesToEmptyOnCAError proves
// handlers.ListProvisioners' degrade-to-empty behaviour (matching the
// template route it replaces): a CA client that errors on Provisioners
// must not fail the whole response.
func TestListProvisioners_DegradesToEmptyOnCAError(t *testing.T) {
	db := openCertTestDB(t)
	ca := &fakeProvisionerCA{err: context.DeadlineExceeded}
	srv := newCertTestServerWithCA(t, db, &config.Config{}, ca)
	client := newClient(t)
	login(t, client, srv)

	resp := doRequest(t, client, http.MethodGet, srv.URL+"/api/v1/provisioners", nil, nil) //nolint:bodyclose // closed via t.Cleanup inside doRequest
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 (CA error degrades to an empty list)", resp.StatusCode)
	}
	var provs []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&provs); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(provs) != 0 {
		t.Fatalf("provisioners = %v, want empty", provs)
	}
}
