package middleware

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ProxySet answers whether a socket peer is a proxy whose forwarding header
// may be believed.
type ProxySet interface {
	Contains(ip net.IP) bool
}

// StaticProxies is an operator-supplied TRUSTED_PROXY_CIDRS list, used
// verbatim.
type StaticProxies []*net.IPNet

// Contains implements ProxySet.
func (s StaticProxies) Contains(ip net.IP) bool { return ipInAny(ip, s) }

// ParseTrustedProxies turns a comma-separated list of CIDR blocks into the
// networks RealIP will trust. A malformed entry is an error rather than a
// skipped entry: a typo that silently shrank the allowlist would either strand
// the deployment on proxy addresses or, worse, leave a header-forging client
// looking like a trusted hop.
func ParseTrustedProxies(raw string) (StaticProxies, error) {
	var nets StaticProxies
	for _, part := range strings.Split(raw, ",") {
		entry := strings.TrimSpace(part)
		if entry == "" {
			continue
		}
		_, block, err := net.ParseCIDR(entry)
		if err != nil {
			return nil, fmt.Errorf("trusted proxy CIDR %q: %w", entry, err)
		}
		nets = append(nets, block)
	}
	if len(nets) == 0 {
		return nil, fmt.Errorf("no CIDR blocks given")
	}
	return nets, nil
}

// LookupFunc matches net.Resolver.LookupIPAddr.
type LookupFunc func(ctx context.Context, host string) ([]net.IPAddr, error)

// ResolvedProxies trusts exactly the addresses a hostname currently resolves
// to, each as a single host. It re-resolves on a timer rather than once,
// because the proxy is started after this process is healthy and gets a new
// address whenever its container is recreated. Until the first successful
// lookup it trusts nothing, so forwarding headers are ignored rather than
// believed from an unknown peer.
type ResolvedProxies struct {
	host   string
	lookup LookupFunc
	addrs  atomic.Pointer[[]net.IP]

	mu       sync.Mutex
	failures int
}

// NewResolvedProxies builds a set for host. Call Refresh or Run to populate it.
func NewResolvedProxies(host string, lookup LookupFunc) *ResolvedProxies {
	return &ResolvedProxies{host: host, lookup: lookup}
}

// Contains implements ProxySet.
func (p *ResolvedProxies) Contains(ip net.IP) bool {
	addrs := p.addrs.Load()
	if addrs == nil {
		return false
	}
	for _, a := range *addrs {
		if a.Equal(ip) {
			return true
		}
	}
	return false
}

// Resolved reports whether at least one lookup has succeeded.
func (p *ResolvedProxies) Resolved() bool { return p.addrs.Load() != nil }

// Refresh resolves the host once. A failed lookup keeps the last good set:
// a stopped proxy sends no traffic, so its stale address trusts nobody.
func (p *ResolvedProxies) Refresh(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	found, err := p.lookup(ctx, p.host)
	if err == nil && len(found) == 0 {
		err = fmt.Errorf("%s resolved to no addresses", p.host)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if err != nil {
		p.failures++
		return err
	}
	ips := make([]net.IP, 0, len(found))
	for _, a := range found {
		ips = append(ips, a.IP)
	}
	prev := p.addrs.Swap(&ips)
	if prev == nil || !sameIPs(*prev, ips) || p.failures > 0 {
		slog.Info("trusted proxy resolved", "host", p.host, "addrs", ips)
	}
	p.failures = 0
	return nil
}

// Run re-resolves every interval until ctx ends. It logs the first failure
// and then one in every twelve, so an unresolvable name stays visible without
// flooding the log.
func (p *ResolvedProxies) Run(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := p.Refresh(ctx); err != nil {
				p.mu.Lock()
				n := p.failures
				p.mu.Unlock()
				if n == 1 || n%12 == 0 {
					slog.Warn("trusted proxy name does not resolve: forwarding headers are ignored until it does",
						"host", p.host, "err", err, "consecutiveFailures", n)
				}
			}
		}
	}
}

func sameIPs(a, b []net.IP) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !a[i].Equal(b[i]) {
			return false
		}
	}
	return true
}

// RealIP rewrites r.RemoteAddr from X-Forwarded-For, but only when the
// immediate peer is itself a trusted proxy. chi's RealIP has no such check,
// which handed the login rate limiter and the auth log to any client willing
// to rotate X-Forwarded-For (V4).
func RealIP(trusted ProxySet) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if ip := clientFromHeaders(r, trusted); ip != "" {
				r.RemoteAddr = ip
			}
			next.ServeHTTP(w, r)
		})
	}
}

// clientFromHeaders returns the client address X-Forwarded-For claims, or ""
// when it must not be believed. No single-value forwarding header is ever
// read: a fallback to one is reachable whenever a proxy block loses its
// X-Forwarded-For header, and whoever reaches it picks their own address.
func clientFromHeaders(r *http.Request, trusted ProxySet) string {
	peer := parseAddr(r.RemoteAddr)
	if peer == nil || !trusted.Contains(peer) {
		return ""
	}
	// Right to left: everything the trusted chain appended is at the end, and
	// the first address that is not itself a proxy is the client. Entries to
	// the left of it were written by an upstream we do not control.
	forwarded := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
	for i := len(forwarded) - 1; i >= 0; i-- {
		ip := parseAddr(strings.TrimSpace(forwarded[i]))
		if ip == nil || trusted.Contains(ip) {
			continue
		}
		return ip.String()
	}
	return ""
}

// parseAddr accepts a bare IP or a host:port pair and returns nil for anything
// else, so an unparseable header entry is skipped rather than trusted.
func parseAddr(addr string) net.IP {
	if addr == "" {
		return nil
	}
	if ip := net.ParseIP(addr); ip != nil {
		return ip
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil
	}
	return net.ParseIP(host)
}

func ipInAny(ip net.IP, nets []*net.IPNet) bool {
	for _, block := range nets {
		if block.Contains(ip) {
			return true
		}
	}
	return false
}
