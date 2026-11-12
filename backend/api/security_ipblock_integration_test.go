//go:build integration

package api_test

import (
	"net/http"
	"testing"

	"certharbor/security"
)

// TestDeleteIPBlock_IPv6LoopbackEncodedPath is deleteIPBlock's regression
// test for the ::1 defect (api/security.go's canonical-form comment): a
// generated client's encodeURIComponent turns the literal path segment
// "::1" into "%3A%3A1" before it ever reaches chi's router, and chi
// decodes it back to "::1" before deleteIPBlock sees in.IP. That round
// trip must still produce the same canonical string netip.Addr.String()
// emits, or every real client unblocking an IPv6 loopback would 422
// forever. This exercises the real HTTP path (encoded, not the bare
// "::1") against the actual router, not just the handler function.
func TestDeleteIPBlock_IPv6LoopbackEncodedPath(t *testing.T) {
	// deleteIPBlock is admin-only (roleOp("admin", ...), api/register.go);
	// testUser defaults to "manager" (mount_test.go), so it is flipped here
	// exactly as certificates_integration_test.go's admin-only cases do.
	original := testUser.Role
	testUser.Role = "admin"
	t.Cleanup(func() { testUser.Role = original })

	db := openCertTestDB(t)
	srv := newCertTestServer(t, db)
	client := newClient(t)
	csrf := login(t, client, srv)

	const loopback = "::1"
	for i := 0; i < security.LimitCount; i++ {
		security.RL.Register(loopback)
	}
	if !security.RL.IsBlocked(loopback) {
		t.Fatalf("setup: expected %q to be blocked after %d registrations", loopback, security.LimitCount)
	}

	// encodeURIComponent("::1") == "%3A%3A1"; this is the literal path a
	// generated client sends, never the bare "::1".
	url := srv.URL + "/api/v1/security/ip-blocks/%3A%3A1"
	resp := doRequest(t, client, http.MethodDelete, url, nil, func(req *http.Request) {
		req.Header.Set("X-CSRF-Token", csrf)
	}) //nolint:bodyclose // closed via t.Cleanup inside doRequest
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE %s: status = %d, want 204", url, resp.StatusCode)
	}

	if security.RL.IsBlocked(loopback) {
		t.Fatalf("%q is still blocked after DELETE ip-blocks/%%3A%%3A1", loopback)
	}
}
