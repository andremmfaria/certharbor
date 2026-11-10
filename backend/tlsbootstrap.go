package main

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"certharbor/config"
	"certharbor/handlers"
	"certharbor/leafcert"
	"certharbor/stepca"
)

// caBootstrapRetries/caBootstrapInterval bound the root-fetch and
// leaf-issuance retry loops below (entrypoint.sh's former 30×1s loops).
// Overridable so tests don't take 30 seconds.
var (
	caBootstrapRetries  = 30
	caBootstrapInterval = time.Second
)

// uiCertRenewFailureBackoff is how long the renewer waits before retrying
// after a failed renewal of a leaf that is still being served, distinct from
// the normal ~2/3-of-validity sleep on success.
const uiCertRenewFailureBackoff = leafcert.RenewFailureBackoff

// caRecoveryInterval is the retry cadence while a leaf is missing or the
// backend leaf is a self-signed fallback: nothing downstream works in either
// state, so recovering from a late CA should not wait a full backoff.
var caRecoveryInterval = 30 * time.Second

// uiIssueKeyType is the key type requested for the UI's own leaf cert,
// matching cert_ops.go's "server" template default (issueTemplates["server"])
// and step ca certificate's default key type, since entrypoint.sh's old
// `step ca certificate` call passed neither --not-after nor --kty/--curve and
// relied on the CA's defaults, which this mirrors. The duration is
// cfg.UICertDuration (UI_CERT_DURATION).
const uiIssueKeyType = "EC:P-256"

// minRenewalSleep keeps a very short UI_CERT_DURATION from driving the
// renewal loop into a busy spin.
const minRenewalSleep = time.Minute

// writeInlineRootCert writes cfg.CARootCertPEM to cfg.RootCert when set,
// replacing entrypoint.sh's CA_ROOT_CERT_PEM branch (ECS/Kubernetes
// deployments where a volume mount is impractical). No-op when unset.
func writeInlineRootCert(cfg *config.Config) error {
	if cfg.CARootCertPEM == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(cfg.RootCert), 0o750); err != nil { //nolint:gosec // G301: cfg.RootCert's parent dir, not user input
		return fmt.Errorf("creating root cert directory: %w", err)
	}
	if err := leafcert.WriteAtomic(cfg.RootCert, []byte(cfg.CARootCertPEM), 0o644); err != nil {
		return fmt.Errorf("writing inline root cert: %w", err)
	}
	slog.Info("wrote root CA certificate from CA_ROOT_CERT_PEM", "path", cfg.RootCert)
	return nil
}

// ensureRootCert is a no-op if cfg.RootCert already exists and is non-empty
// (e.g. written by writeInlineRootCert above, or present via a volume
// mount). Otherwise, if cfg.CAFingerprint is set, it retries
// stepca.FetchRootByFingerprint up to caBootstrapRetries times and writes
// the result. On exhaustion it logs a warning and returns nil (non-fatal,
// matches entrypoint.sh's old behavior; CA connectivity is still surfaced
// later via /ready).
func ensureRootCert(ctx context.Context, cfg *config.Config) error {
	if info, err := os.Stat(cfg.RootCert); err == nil && info.Size() > 0 {
		return nil
	}
	if cfg.CAFingerprint == "" {
		return nil
	}

	slog.Info("fetching root CA certificate via CA_FINGERPRINT", "caURL", cfg.CAURL)
	var lastErr error
	for attempt := 1; attempt <= caBootstrapRetries; attempt++ {
		rootPEM, err := stepca.FetchRootByFingerprint(ctx, cfg.CAURL, cfg.CAFingerprint)
		if err == nil {
			if mkErr := os.MkdirAll(filepath.Dir(cfg.RootCert), 0o750); mkErr != nil { //nolint:gosec // G301: cfg.RootCert's parent dir, not user input
				return fmt.Errorf("creating root cert directory: %w", mkErr)
			}
			if writeErr := leafcert.WriteAtomic(cfg.RootCert, rootPEM, 0o644); writeErr != nil {
				return fmt.Errorf("writing fetched root cert: %w", writeErr)
			}
			slog.Info("root CA certificate fetched and verified", "path", cfg.RootCert, "attempt", attempt)
			return nil
		}
		lastErr = err
		slog.Debug("waiting for step-ca root", "attempt", attempt, "of", caBootstrapRetries, "err", err)
		select {
		case <-ctx.Done():
			slog.Warn("root cert fetch aborted by context", "err", ctx.Err())
			return nil
		case <-time.After(caBootstrapInterval):
		}
	}
	slog.Warn("could not fetch root CA certificate after retries, continuing without it",
		"attempts", caBootstrapRetries, "lastErr", lastErr)
	return nil
}

// generateSelfSignedCert creates a 10-year self-signed EC P-256 certificate
// at certPath/keyPath with SAN IP:hostIP, DNS:localhost, and DNS:hostname
// when set, replacing entrypoint.sh's shelled
// `openssl req -x509 -nodes -days 3650 -newkey rsa:2048 ...`. The key
// algorithm changes from RSA-2048 to EC-P256: a self-signed dev/fallback
// cert, not attacker-facing in any meaningful way beyond what it already
// was, an intentional, low-risk deviation, not an oversight.
func generateSelfSignedCert(certPath, keyPath, hostname, hostIP string) error {
	return generateSelfSignedCertWithValidity(certPath, keyPath, hostname, hostIP, 10*365*24*time.Hour)
}

// generateSelfSignedCertWithValidity is generateSelfSignedCert with an
// explicit validity window, factored out so tests can produce short-lived
// fake certs (e.g. to exercise renewUICertOnce's next-sleep calculation)
// without waiting a decade.
func generateSelfSignedCertWithValidity(certPath, keyPath, hostname, hostIP string, validity time.Duration) error {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("generating self-signed key: %w", err)
	}

	cn := hostname
	if cn == "" {
		cn = hostIP
	}
	if cn == "" {
		cn = "localhost"
	}

	dnsNames := []string{"localhost"}
	if hostname != "" {
		dnsNames = append(dnsNames, hostname)
	}
	var ips []net.IP
	if ip := net.ParseIP(hostIP); ip != nil {
		ips = append(ips, ip)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return fmt.Errorf("generating serial number: %w", err)
	}
	template := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: cn},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(validity),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		DNSNames:              dnsNames,
		IPAddresses:           ips,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return fmt.Errorf("creating self-signed certificate: %w", err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return fmt.Errorf("marshaling self-signed key: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(certPath), 0o750); err != nil { //nolint:gosec // G301: cfg.SSLCert's parent dir, not user input
		return fmt.Errorf("creating cert directory: %w", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	if err := writeKeyPair(certPath, keyPath, certPEM, keyPEM); err != nil {
		return fmt.Errorf("writing self-signed pair: %w", err)
	}
	slog.Info("generated self-signed TLS certificate", "certPath", certPath, "cn", cn)
	return nil
}

// resolveUIHostname returns cfg.UIHostname if set, else the OS-reported
// hostname, else "localhost", approximating entrypoint.sh's
// `${UI_HOSTNAME:-$(hostname -f 2>/dev/null || hostname)}`.
func resolveUIHostname(cfg *config.Config) string {
	if cfg.UIHostname != "" {
		return cfg.UIHostname
	}
	if h, err := os.Hostname(); err == nil && h != "" {
		return h
	}
	return "localhost"
}

// ensureUICert implements the three-way UITLSMode switch entrypoint.sh used
// to perform: "provided" is a no-op (operator supplies the cert out of
// band); "stepca" retries issuance via caClient up to caBootstrapRetries
// times and falls back to a self-signed leaf plus the fallback marker on
// exhaustion; anything else (including the default "self-signed") calls
// generateSelfSignedCert only if cfg.SSLCert is absent.
func ensureUICert(ctx context.Context, cfg *config.Config, caClient stepca.CA) error {
	if cfg.UITLSMode != "stepca" {
		// The marker only means something in stepca mode; a stale one from an
		// earlier mode would hold /ready down for ever.
		clearFallbackMarker(cfg.SSLCert)
	}
	switch cfg.UITLSMode {
	case "provided":
		return nil

	case "stepca":
		if caClient == nil {
			// CA client construction failed (R2), nothing in the retry loop
			// below could possibly succeed against a nil stepca.CA.
			slog.Warn("UI_TLS_MODE=stepca but no CA client is available, falling back to self-signed")
			return selfSignFallback(cfg)
		}
		hostname := resolveUIHostname(cfg)
		slog.Info("obtaining UI leaf certificate from step-ca", "hostname", hostname)
		if err := issueWithRetry(ctx, cfg, caClient, "UI", cfg.SSLCert, cfg.SSLKey, hostname); err != nil {
			// Distinct messages per cause: the e2e bootstrap specs and
			// operators grep for exactly these lines.
			if ctx.Err() != nil {
				slog.Warn("UI cert issuance aborted by context, falling back to self-signed", "err", err)
			} else {
				slog.Warn("step-ca certificate issuance failed after retries, falling back to self-signed", "err", err)
			}
			return selfSignFallback(cfg)
		}
		clearFallbackMarker(cfg.SSLCert)
		return nil

	default: // "self-signed" and any unrecognized value
		if _, err := os.Stat(cfg.SSLCert); err == nil {
			return nil
		}
		return generateSelfSignedCert(cfg.SSLCert, cfg.SSLKey, cfg.UIHostname, cfg.HostIP)
	}
}

// ensureWebCert issues nginx's leaf under WEB_TLS_MODE=stepca. Unlike the
// backend leaf it never falls back to self-signed: no browser would accept
// one, so a missing file that nginx waits on and then names is the more
// diagnosable failure. A still-valid leaf already on the volume is left in
// place when issuance fails, and the renewer keeps retrying.
func ensureWebCert(ctx context.Context, cfg *config.Config, caClient stepca.CA) error {
	if cfg.WebTLSMode != "stepca" {
		return nil
	}
	if caClient == nil {
		return errors.New("no CA client is available to issue the web leaf")
	}
	slog.Info("obtaining web leaf certificate from step-ca", "hostname", cfg.WebHostname)
	return issueWithRetry(ctx, cfg, caClient, "web", cfg.WebSSLCert, cfg.WebSSLKey, cfg.WebHostname)
}

// issueWithRetry is the bootstrap's bounded issuance loop, shared by both
// leaves. leaf names the leaf in the success line, "UI leaf certificate
// obtained" being the one the bootstrap specs match on.
func issueWithRetry(ctx context.Context, cfg *config.Config, caClient stepca.CA, leaf, certPath, keyPath, hostname string) error {
	var lastErr error
	for attempt := 1; attempt <= caBootstrapRetries; attempt++ {
		err := issueUICert(ctx, cfg, caClient, certPath, keyPath, hostname)
		if err == nil {
			slog.Info(leaf+" leaf certificate obtained", "certPath", certPath, "hostname", hostname, "attempt", attempt)
			return nil
		}
		lastErr = err
		slog.Debug("waiting for step-ca", "attempt", attempt, "of", caBootstrapRetries, "err", lastErr)
		select {
		case <-ctx.Done():
			return fmt.Errorf("issuance aborted by context: %w", ctx.Err())
		case <-time.After(caBootstrapInterval):
		}
	}
	return fmt.Errorf("%d attempts: %w", caBootstrapRetries, lastErr)
}

func selfSignFallback(cfg *config.Config) error {
	if err := generateSelfSignedCert(cfg.SSLCert, cfg.SSLKey, cfg.UIHostname, cfg.HostIP); err != nil {
		return err
	}
	return writeFallbackMarker(cfg.SSLCert)
}

func writeFallbackMarker(certPath string) error {
	msg := "UI_TLS_MODE=stepca fell back to a self-signed leaf; nginx cannot verify it\n"
	if err := leafcert.WriteAtomic(leafcert.FallbackMarker(certPath), []byte(msg), 0o644); err != nil { //nolint:gosec // G306: marker carries no secret, the healthcheck reads it
		return fmt.Errorf("writing fallback marker: %w", err)
	}
	slog.Warn("self-signed fallback marker written: /ready reports not ready until step-ca issues a real leaf",
		"marker", leafcert.FallbackMarker(certPath))
	return nil
}

func clearFallbackMarker(certPath string) {
	marker := leafcert.FallbackMarker(certPath)
	if err := os.Remove(marker); err == nil {
		slog.Info("self-signed fallback marker cleared", "marker", marker)
	} else if !errors.Is(err, os.ErrNotExist) {
		slog.Error("could not clear the fallback marker", "marker", marker, "err", err)
	}
}

// writeKeyPair renames the key into place before the certificate: nginx's
// watcher and the Go reloader both key off the certificate changing, so the
// matching key is already there when they see it.
func writeKeyPair(certPath, keyPath string, certPEM, keyPEM []byte) error {
	if err := os.MkdirAll(filepath.Dir(certPath), 0o750); err != nil { //nolint:gosec // G301: config-derived dir, not user input
		return fmt.Errorf("creating cert directory: %w", err)
	}
	if err := leafcert.WriteAtomic(keyPath, keyPEM, 0o600); err != nil {
		return fmt.Errorf("writing key: %w", err)
	}
	if err := leafcert.WriteAtomic(certPath, certPEM, 0o600); err != nil {
		return fmt.Errorf("writing certificate: %w", err)
	}
	return nil
}

// issueUICert issues one leaf for hostname via caClient and writes it to
// certPath/keyPath, the single-issuance step shared by both leaves'
// bootstrap retry loops and the renewer.
func issueUICert(ctx context.Context, cfg *config.Config, caClient stepca.CA, certPath, keyPath, hostname string) error {
	certPEM, keyPEM, err := caClient.IssueCertificate(ctx, stepca.IssueRequest{
		Domain:       hostname,
		Duration:     cfg.UICertDuration,
		KeyType:      uiIssueKeyType,
		Provisioner:  cfg.Provisioner,
		PasswordFile: cfg.PasswordFile,
	})
	if err != nil {
		return err
	}
	return writeKeyPair(certPath, keyPath, certPEM, keyPEM)
}

// renewUICertOnce re-issues one leaf, rewrites certPath/keyPath (picked up
// with zero downtime by tlsreload.go for the backend leaf and by nginx's
// reload watcher for the web leaf), and returns roughly 2/3 of the new
// certificate's validity as the next sleep.
func renewUICertOnce(ctx context.Context, cfg *config.Config, caClient stepca.CA, certPath, keyPath, hostname string) (time.Duration, error) {
	if err := issueUICert(ctx, cfg, caClient, certPath, keyPath, hostname); err != nil {
		return 0, fmt.Errorf("renewing %s: %w", certPath, err)
	}
	return nextRenewalSleep(certPath)
}

// nextRenewalSleep reads certPath and returns roughly 2/3 of its validity
// window, clamped so a very short UI_CERT_DURATION cannot spin the loop.
func nextRenewalSleep(certPath string) (time.Duration, error) {
	cert, err := leafcert.Read(certPath)
	if err != nil {
		return 0, fmt.Errorf("reading certificate: %w", err)
	}
	sleep := cert.NotAfter.Sub(cert.NotBefore) * 2 / 3
	if sleep <= 0 {
		sleep = uiCertRenewFailureBackoff
	}
	if sleep < minRenewalSleep {
		sleep = minRenewalSleep
	}
	return sleep, nil
}

// syncWebRootCA copies cfg.RootCert into the web volume as root_ca.crt, the
// trust anchor nginx verifies the backend leaf against. nginx therefore never
// mounts step-ca-data, and the copy is a no-op when the content is unchanged.
func syncWebRootCA(cfg *config.Config) error {
	if cfg.WebTLSMode != "stepca" {
		return nil
	}
	root, err := os.ReadFile(cfg.RootCert)
	if err != nil {
		return fmt.Errorf("reading root CA: %w", err)
	}
	if existing, err := os.ReadFile(cfg.WebRootCA); err == nil && bytes.Equal(existing, root) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(cfg.WebRootCA), 0o750); err != nil { //nolint:gosec // G301: config-derived dir, not user input
		return fmt.Errorf("creating web cert directory: %w", err)
	}
	if err := leafcert.WriteAtomic(cfg.WebRootCA, root, 0o644); err != nil { //nolint:gosec // G306: root cert is public, nginx's workers read it
		return fmt.Errorf("copying root CA: %w", err)
	}
	slog.Info("root CA copied for nginx", "path", cfg.WebRootCA)
	return nil
}

// tlsLeaf is one certificate this process issues from step-ca and renews.
type tlsLeaf struct {
	certPath string
	keyPath  string
	hostname string
	// fallback is true for the backend leaf, the only one that self-signs
	// and therefore the only one with a marker to clear.
	fallback bool
}

// managedLeaves lists the leaves in stepca mode, in the order they are
// renewed.
func managedLeaves(cfg *config.Config) []tlsLeaf {
	var leaves []tlsLeaf
	if cfg.UITLSMode == "stepca" {
		leaves = append(leaves, tlsLeaf{cfg.SSLCert, cfg.SSLKey, resolveUIHostname(cfg), true})
	}
	if cfg.WebTLSMode == "stepca" {
		leaves = append(leaves, tlsLeaf{cfg.WebSSLCert, cfg.WebSSLKey, cfg.WebHostname, false})
	}
	return leaves
}

// leafNeedsIssue reports whether a leaf must be issued now, and why. Leaves
// due within minRenewalSleep of now are renewed together so one cycle
// produces one change on each file rather than two passes a minute apart.
func leafNeedsIssue(l tlsLeaf, now time.Time) (bool, string) {
	if l.fallback {
		if _, err := os.Stat(leafcert.FallbackMarker(l.certPath)); err == nil {
			return true, "self-signed fallback"
		}
	}
	cert, err := leafcert.Read(l.certPath)
	if err != nil {
		return true, "missing or unreadable"
	}
	if l.hostname != "" && !leafHasName(cert, l.hostname) {
		return true, "hostname changed"
	}
	if !now.Add(minRenewalSleep).Before(leafcert.RenewalDue(cert)) {
		return true, "due"
	}
	return false, ""
}

func leafHasName(cert *x509.Certificate, name string) bool {
	for _, n := range cert.DNSNames {
		if strings.EqualFold(n, name) {
			return true
		}
	}
	if ip := net.ParseIP(name); ip != nil {
		for _, a := range cert.IPAddresses {
			if a.Equal(ip) {
				return true
			}
		}
	}
	return false
}

// renewLeavesOnce is one renewer iteration over every managed leaf. It
// returns the sleep until the next iteration: the minimum over the leaves,
// so there is one wake-up and one backoff for both.
func renewLeavesOnce(ctx context.Context, cfg *config.Config, caClient stepca.CA, now time.Time) time.Duration {
	if caClient == nil {
		slog.Warn("step-ca client unavailable, retrying", "retryIn", caRecoveryInterval)
		return caRecoveryInterval
	}
	if err := syncWebRootCA(cfg); err != nil {
		slog.Error("root CA copy for nginx failed, will retry", "err", err)
	}
	sleep := time.Duration(0)
	recovering := false
	shorten := func(d time.Duration) {
		if sleep == 0 || d < sleep {
			sleep = d
		}
	}
	for _, l := range managedLeaves(cfg) {
		need, why := leafNeedsIssue(l, now)
		if !need {
			cert, err := leafcert.Read(l.certPath)
			if err != nil {
				shorten(uiCertRenewFailureBackoff)
				continue
			}
			shorten(leafcert.RenewalDue(cert).Sub(now))
			continue
		}
		next, err := renewUICertOnce(ctx, cfg, caClient, l.certPath, l.keyPath, l.hostname)
		if err != nil {
			retry := uiCertRenewFailureBackoff
			if why != "due" {
				retry = caRecoveryInterval
				recovering = true
			}
			slog.Error("leaf renewal failed, will retry", "certPath", l.certPath, "reason", why, "err", err, "retryIn", retry)
			shorten(retry)
			continue
		}
		if l.fallback {
			clearFallbackMarker(l.certPath)
		}
		// The CA backdates NotBefore, so the due point is read off the new
		// leaf rather than taken as two thirds of its validity from now.
		if cert, err := leafcert.Read(l.certPath); err == nil {
			next = leafcert.RenewalDue(cert).Sub(now)
		}
		slog.Info("leaf renewed", "certPath", l.certPath, "reason", why, "nextRenewalIn", next)
		shorten(next)
	}
	floor := minRenewalSleep
	if recovering {
		floor = caRecoveryInterval
	}
	if sleep < floor {
		sleep = floor
	}
	return sleep
}

// startTLSRenewer runs renewLeavesOnce for ever in a panic-safe goroutine,
// the same fire-and-forget lifecycle as h.StartRenewer(). It runs whenever
// either leaf is in stepca mode, even with no CA client at boot: the client
// is built lazily per iteration, so a CA that comes up late is recovered
// from without a restart.
func startTLSRenewer(cfg *config.Config, caClient stepca.CA) {
	if len(managedLeaves(cfg)) == 0 {
		return
	}
	var paths []string
	for _, l := range managedLeaves(cfg) {
		paths = append(paths, l.certPath)
	}
	handlers.SafeGoExported("tls-renewer", func() {
		slog.Info("TLS leaf auto-renewer started", "leaves", paths)
		client := caClient
		for {
			if client == nil {
				if c, err := stepca.New(cfg); err == nil {
					client = c
				} else {
					slog.Debug("CA client construction still failing", "err", err)
				}
			}
			time.Sleep(renewLeavesOnce(context.Background(), cfg, client, time.Now()))
		}
	})
}
