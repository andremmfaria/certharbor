package middleware

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// mustTrust parses CIDRs a test controls and fails loudly on a typo.
func mustTrust(t *testing.T, raw string) StaticProxies {
	t.Helper()
	nets, err := ParseTrustedProxies(raw)
	if err != nil {
		t.Fatalf("ParseTrustedProxies(%q): %v", raw, err)
	}
	return nets
}

// remoteAddrAfterRealIP runs one request through RealIP and reports the
// RemoteAddr the wrapped handler saw.
func remoteAddrAfterRealIP(t *testing.T, trusted ProxySet, peer string, headers map[string]string) string {
	t.Helper()
	var seen string
	handler := RealIP(trusted)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen = r.RemoteAddr
	}))
	req := newReq("GET", "/login")
	req.RemoteAddr = peer
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	handler.ServeHTTP(httptest.NewRecorder(), req)
	return seen
}

// ─── ParseTrustedProxies ───────────────────────────────────────────────────────

func TestParseTrustedProxies_Valid(t *testing.T) {
	nets, err := ParseTrustedProxies(" 10.0.0.7/32 ,192.168.0.0/16, ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(nets) != 2 {
		t.Fatalf("blocks: got %d want 2", len(nets))
	}
}

// TestParseTrustedProxies_Rejects covers the fail-closed contract: a
// misconfigured allowlist must stop the process, not shrink silently.
func TestParseTrustedProxies_Rejects(t *testing.T) {
	cases := map[string]string{
		"empty":          "",
		"only_separator": " , ",
		"bare_ip":        "10.0.0.7",
		"malformed":      "10.0.0.7/32,not-a-cidr",
		"bad_mask":       "10.0.0.0/33",
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseTrustedProxies(raw); err == nil {
				t.Errorf("ParseTrustedProxies(%q): expected an error", raw)
			}
		})
	}
}

// ─── RealIP (V4) ───────────────────────────────────────────────────────────────

// TestRealIP_UntrustedPeer_HeaderIgnored is the finding itself: a client that
// connects directly and crafts a forwarding header must not choose the address
// the rate limiter and the auth log key off.
func TestRealIP_UntrustedPeer_HeaderIgnored(t *testing.T) {
	trusted := mustTrust(t, "10.0.0.7/32")
	headers := map[string]string{
		"X-Forwarded-For": "1.2.3.4",
		"X-Real-IP":       "5.6.7.8",
		"True-Client-IP":  "9.10.11.12",
	}
	got := remoteAddrAfterRealIP(t, trusted, "203.0.113.9:44321", headers)
	if got != "203.0.113.9:44321" {
		t.Errorf("untrusted peer: RemoteAddr got %q, want the socket peer unchanged", got)
	}
}

// TestRealIP_TrustedPeer_HeaderHonoured is the control for the test above.
func TestRealIP_TrustedPeer_HeaderHonoured(t *testing.T) {
	trusted := mustTrust(t, "10.0.0.7/32")
	got := remoteAddrAfterRealIP(t, trusted, "10.0.0.7:9999", map[string]string{
		"X-Forwarded-For": "1.2.3.4",
	})
	if got != "1.2.3.4" {
		t.Errorf("trusted peer: RemoteAddr got %q want %q", got, "1.2.3.4")
	}
}

// TestRealIP_ChainReadRightToLeft confirms the client is the rightmost address
// that is not itself a proxy, so a client prepending its own entries cannot
// choose the result.
func TestRealIP_ChainReadRightToLeft(t *testing.T) {
	trusted := mustTrust(t, "10.0.0.0/24")
	got := remoteAddrAfterRealIP(t, trusted, "10.0.0.7:9999", map[string]string{
		"X-Forwarded-For": "203.0.113.9, 1.2.3.4, 10.0.0.9, 10.0.0.7",
	})
	if got != "1.2.3.4" {
		t.Errorf("RemoteAddr got %q want %q", got, "1.2.3.4")
	}
}

// TestRealIP_FourForgeryHeaders is the four-header forgery table (Section 2):
// from a trusted peer, with no X-Forwarded-For at all, none of the other
// forwarding headers may move the recorded address. A table forging only
// X-Forwarded-For would pass while a single-value fallback was wide open.
func TestRealIP_FourForgeryHeaders(t *testing.T) {
	trusted := mustTrust(t, "10.0.0.7/32")
	forged := map[string]string{
		"X-Real-IP":      "5.6.7.8",
		"True-Client-IP": "9.10.11.12",
		"Forwarded":      "for=13.14.15.16",
	}
	for header, value := range forged {
		t.Run(header, func(t *testing.T) {
			got := remoteAddrAfterRealIP(t, trusted, "10.0.0.7:9999", map[string]string{header: value})
			if got != "10.0.0.7:9999" {
				t.Errorf("%s moved RemoteAddr to %q; it must be ignored", header, got)
			}
		})
	}
	// nginx replaces X-Forwarded-For with $remote_addr, so beside it every
	// other forged header must lose to the proxy-set value.
	t.Run("X-Forwarded-For_with_all_three", func(t *testing.T) {
		headers := map[string]string{"X-Forwarded-For": "198.51.100.4"}
		for k, v := range forged {
			headers[k] = v
		}
		got := remoteAddrAfterRealIP(t, trusted, "10.0.0.7:9999", headers)
		if got != "198.51.100.4" {
			t.Errorf("RemoteAddr got %q want the proxy-set X-Forwarded-For", got)
		}
	})
}

// TestRealIP_NoUsableHeader leaves RemoteAddr alone rather than blanking it:
// the peer is the best address available when the chain says nothing.
func TestRealIP_NoUsableHeader(t *testing.T) {
	trusted := mustTrust(t, "10.0.0.0/24")
	cases := map[string]map[string]string{
		"absent":       {},
		"garbage":      {"X-Forwarded-For": "not-an-ip"},
		"all_trusted":  {"X-Forwarded-For": "10.0.0.9, 10.0.0.7"},
		"empty_string": {"X-Forwarded-For": ""},
	}
	for name, headers := range cases {
		t.Run(name, func(t *testing.T) {
			got := remoteAddrAfterRealIP(t, trusted, "10.0.0.7:9999", headers)
			if got != "10.0.0.7:9999" {
				t.Errorf("RemoteAddr got %q, want the socket peer unchanged", got)
			}
		})
	}
}

// TestRealIP_PortedEntriesAndIPv6 covers the shapes a proxy may actually write.
func TestRealIP_PortedEntriesAndIPv6(t *testing.T) {
	trusted := mustTrust(t, "10.0.0.7/32")
	cases := map[string]struct{ header, want string }{
		"host_port": {"1.2.3.4:5678", "1.2.3.4"},
		"ipv6":      {"2001:db8::1", "2001:db8::1"},
		"ipv6_port": {"[2001:db8::2]:443", "2001:db8::2"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := remoteAddrAfterRealIP(t, trusted, "10.0.0.7:9999", map[string]string{"X-Forwarded-For": tc.header})
			if got != tc.want {
				t.Errorf("RemoteAddr got %q want %q", got, tc.want)
			}
		})
	}
}

// ─── ResolvedProxies (D5 rule 3) ─────────────────────────────────────────────

type fakeLookup struct {
	mu    sync.Mutex
	addrs []string
	err   error
	calls int
}

func (f *fakeLookup) lookup(_ context.Context, _ string) ([]net.IPAddr, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	out := make([]net.IPAddr, 0, len(f.addrs))
	for _, a := range f.addrs {
		out = append(out, net.IPAddr{IP: net.ParseIP(a)})
	}
	return out, nil
}

func (f *fakeLookup) set(err error, addrs ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err, f.addrs = err, addrs
}

func TestResolvedProxies_TrustsNothingUntilResolved(t *testing.T) {
	f := &fakeLookup{err: errors.New("no such host")}
	p := NewResolvedProxies("certharbor-console", f.lookup)
	if err := p.Refresh(context.Background()); err == nil {
		t.Fatal("expected the failed lookup to surface")
	}
	if p.Resolved() {
		t.Fatal("Resolved() true after a failed first lookup")
	}
	got := remoteAddrAfterRealIP(t, p, "172.30.0.5:40000", map[string]string{"X-Forwarded-For": "1.2.3.4"})
	if got != "172.30.0.5:40000" {
		t.Errorf("unresolved set believed a header: RemoteAddr %q", got)
	}
}

func TestResolvedProxies_TrustsExactAddressOnly(t *testing.T) {
	f := &fakeLookup{addrs: []string{"172.30.0.5"}}
	p := NewResolvedProxies("certharbor-console", f.lookup)
	if err := p.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := remoteAddrAfterRealIP(t, p, "172.30.0.5:40000", map[string]string{"X-Forwarded-For": "1.2.3.4"}); got != "1.2.3.4" {
		t.Errorf("resolved proxy: RemoteAddr %q want 1.2.3.4", got)
	}
	// A neighbour on the same subnet (the bridge gateway, say) is not trusted.
	if got := remoteAddrAfterRealIP(t, p, "172.30.0.1:40000", map[string]string{"X-Forwarded-For": "1.2.3.4"}); got != "172.30.0.1:40000" {
		t.Errorf("subnet neighbour trusted: RemoteAddr %q", got)
	}
}

// TestResolvedProxies_FollowsRecreate is why the set is refreshed rather than
// resolved once: a recreated proxy container comes back on a new address.
func TestResolvedProxies_FollowsRecreate(t *testing.T) {
	f := &fakeLookup{addrs: []string{"172.30.0.5"}}
	p := NewResolvedProxies("certharbor-console", f.lookup)
	if err := p.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.set(errors.New("no such host"))
	_ = p.Refresh(context.Background())
	if !p.Contains(net.ParseIP("172.30.0.5")) {
		t.Error("a failed refresh dropped the last good address")
	}
	f.set(nil, "172.30.0.9")
	if err := p.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if p.Contains(net.ParseIP("172.30.0.5")) || !p.Contains(net.ParseIP("172.30.0.9")) {
		t.Error("refresh did not move trust to the recreated address")
	}
}

func TestResolvedProxies_RunRefreshesUntilCancelled(t *testing.T) {
	f := &fakeLookup{err: errors.New("no such host")}
	p := NewResolvedProxies("certharbor-console", f.lookup)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { p.Run(ctx, time.Millisecond); close(done) }()
	deadline := time.Now().Add(5 * time.Second)
	for !p.Resolved() {
		if time.Now().After(deadline) {
			t.Fatal("Run never picked up the name once it started resolving")
		}
		f.mu.Lock()
		n := f.calls
		f.mu.Unlock()
		if n >= 3 {
			f.set(nil, "172.30.0.5")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	<-done
}
