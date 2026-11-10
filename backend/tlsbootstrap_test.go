package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"certharbor/config"
	"certharbor/leafcert"
	"certharbor/stepca"
)

// fakeCA is a minimal stepca.CA test double for package main, a plain copy
// of handlers.FakeCA's shape since the two packages' test files cannot share
// an unexported test-only type (see plan Phase 1.4).
type fakeCA struct {
	issueCertPEM []byte
	issueKeyPEM  []byte
	issueErr     error
	issueCalls   int
}

var _ stepca.CA = (*fakeCA)(nil)

func (f *fakeCA) Health(context.Context) error { return nil }

func (f *fakeCA) Provisioners(context.Context) ([]stepca.ProvisionerInfo, error) {
	return nil, nil
}

func (f *fakeCA) IssueCertificate(context.Context, stepca.IssueRequest) ([]byte, []byte, error) {
	f.issueCalls++
	if f.issueErr != nil {
		return nil, nil, f.issueErr
	}
	return f.issueCertPEM, f.issueKeyPEM, nil
}

func (f *fakeCA) Revoke(context.Context, string, string) error { return nil }

// newFakeIssuedCert returns cert/key PEM valid for the given duration, for a
// fakeCA to hand back from IssueCertificate.
func newFakeIssuedCert(t *testing.T, validity time.Duration) (certPEM, keyPEM []byte) {
	t.Helper()
	dir := t.TempDir()
	certPath := filepath.Join(dir, "c.crt")
	keyPath := filepath.Join(dir, "c.key")
	if err := generateSelfSignedCertWithValidity(certPath, keyPath, "fake.example.com", "127.0.0.1", validity); err != nil {
		t.Fatalf("generateSelfSignedCertWithValidity: %v", err)
	}
	var err error
	certPEM, err = os.ReadFile(certPath) //nolint:gosec // G304: t.TempDir()-derived path
	if err != nil {
		t.Fatalf("read cert: %v", err)
	}
	keyPEM, err = os.ReadFile(keyPath) //nolint:gosec // G304: t.TempDir()-derived path
	if err != nil {
		t.Fatalf("read key: %v", err)
	}
	return certPEM, keyPEM
}

// ─── generateSelfSignedCert ────────────────────────────────────────────────

func TestGenerateSelfSignedCert(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "server.crt")
	keyPath := filepath.Join(dir, "server.key")

	if err := generateSelfSignedCert(certPath, keyPath, "ui.internal.example.com", "10.0.0.5"); err != nil {
		t.Fatalf("generateSelfSignedCert: %v", err)
	}

	pair, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		t.Fatalf("tls.LoadX509KeyPair: %v", err)
	}
	cert, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		t.Fatalf("x509.ParseCertificate: %v", err)
	}

	wantDNS := map[string]bool{"localhost": true, "ui.internal.example.com": true}
	if len(cert.DNSNames) != len(wantDNS) {
		t.Errorf("DNSNames = %v; want exactly %v", cert.DNSNames, wantDNS)
	}
	for _, name := range cert.DNSNames {
		if !wantDNS[name] {
			t.Errorf("unexpected DNS SAN %q", name)
		}
	}
	foundIP := false
	for _, ip := range cert.IPAddresses {
		if ip.Equal(net.ParseIP("10.0.0.5")) {
			foundIP = true
		}
	}
	if !foundIP {
		t.Errorf("IPAddresses = %v; want 10.0.0.5 present", cert.IPAddresses)
	}

	validity := cert.NotAfter.Sub(cert.NotBefore)
	const tenYears = 10 * 365 * 24 * time.Hour
	if validity < tenYears-24*time.Hour || validity > tenYears+24*time.Hour {
		t.Errorf("validity = %v; want ~10 years", validity)
	}
}

// ─── ensureUICert ───────────────────────────────────────────────────────────

func TestEnsureUICert_Provided(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{
		UITLSMode: "provided",
		SSLCert:   filepath.Join(dir, "server.crt"),
		SSLKey:    filepath.Join(dir, "server.key"),
	}
	ca := &fakeCA{}
	if err := ensureUICert(context.Background(), cfg, ca); err != nil {
		t.Fatalf("ensureUICert: %v", err)
	}
	if ca.issueCalls != 0 {
		t.Errorf("expected 0 IssueCertificate calls for provided mode, got %d", ca.issueCalls)
	}
	if _, err := os.Stat(cfg.SSLCert); err == nil {
		t.Error("provided mode must not write a cert file")
	}
}

func TestEnsureUICert_SelfSignedDefault(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{
		UITLSMode: "self-signed",
		SSLCert:   filepath.Join(dir, "server.crt"),
		SSLKey:    filepath.Join(dir, "server.key"),
		HostIP:    "127.0.0.1",
	}
	ca := &fakeCA{}
	if err := ensureUICert(context.Background(), cfg, ca); err != nil {
		t.Fatalf("ensureUICert: %v", err)
	}
	if ca.issueCalls != 0 {
		t.Errorf("expected 0 IssueCertificate calls for self-signed mode, got %d", ca.issueCalls)
	}
	if _, err := tls.LoadX509KeyPair(cfg.SSLCert, cfg.SSLKey); err != nil {
		t.Errorf("expected a loadable self-signed cert, got: %v", err)
	}
}

func TestEnsureUICert_SelfSignedSkipsExisting(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{
		UITLSMode: "self-signed",
		SSLCert:   filepath.Join(dir, "server.crt"),
		SSLKey:    filepath.Join(dir, "server.key"),
		HostIP:    "127.0.0.1",
	}
	if err := generateSelfSignedCert(cfg.SSLCert, cfg.SSLKey, "", cfg.HostIP); err != nil {
		t.Fatalf("seed cert: %v", err)
	}
	info1, _ := os.Stat(cfg.SSLCert)

	ca := &fakeCA{}
	if err := ensureUICert(context.Background(), cfg, ca); err != nil {
		t.Fatalf("ensureUICert: %v", err)
	}
	info2, _ := os.Stat(cfg.SSLCert)
	if !info1.ModTime().Equal(info2.ModTime()) {
		t.Error("existing SSL cert was regenerated when it should have been left alone")
	}
}

func TestEnsureUICert_Stepca_Success(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{
		UITLSMode:   "stepca",
		SSLCert:     filepath.Join(dir, "server.crt"),
		SSLKey:      filepath.Join(dir, "server.key"),
		UIHostname:  "ui.internal.example.com",
		Provisioner: "admin",
	}
	certPEM, keyPEM := newFakeIssuedCert(t, time.Hour)
	ca := &fakeCA{issueCertPEM: certPEM, issueKeyPEM: keyPEM}

	if err := ensureUICert(context.Background(), cfg, ca); err != nil {
		t.Fatalf("ensureUICert: %v", err)
	}
	if ca.issueCalls != 1 {
		t.Errorf("expected exactly 1 IssueCertificate call, got %d", ca.issueCalls)
	}
	got, err := os.ReadFile(cfg.SSLCert) //nolint:gosec // G304: t.TempDir()-derived path
	if err != nil || !bytes.Equal(got, certPEM) {
		t.Errorf("SSLCert contents mismatch: err=%v", err)
	}
}

func TestEnsureUICert_Stepca_NilCAClientFallsBackToSelfSigned(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{
		UITLSMode:  "stepca",
		SSLCert:    filepath.Join(dir, "server.crt"),
		SSLKey:     filepath.Join(dir, "server.key"),
		UIHostname: "ui.internal.example.com",
		HostIP:     "127.0.0.1",
	}
	// A nil stepca.CA simulates R2's "CA client construction failed" case,
	// must not panic on a nil-interface method call.
	if err := ensureUICert(context.Background(), cfg, nil); err != nil {
		t.Fatalf("ensureUICert: %v", err)
	}
	if _, err := tls.LoadX509KeyPair(cfg.SSLCert, cfg.SSLKey); err != nil {
		t.Errorf("expected a self-signed fallback cert, got: %v", err)
	}
	assertMarker(t, cfg.SSLCert, true)
}

func TestEnsureUICert_Stepca_FallsBackToSelfSigned(t *testing.T) {
	restoreRetries, restoreInterval := caBootstrapRetries, caBootstrapInterval
	caBootstrapRetries = 2
	caBootstrapInterval = time.Millisecond
	t.Cleanup(func() {
		caBootstrapRetries = restoreRetries
		caBootstrapInterval = restoreInterval
	})

	dir := t.TempDir()
	cfg := &config.Config{
		UITLSMode:  "stepca",
		SSLCert:    filepath.Join(dir, "server.crt"),
		SSLKey:     filepath.Join(dir, "server.key"),
		UIHostname: "ui.internal.example.com",
		HostIP:     "127.0.0.1",
	}
	ca := &fakeCA{issueErr: errors.New("CA unreachable")}

	if err := ensureUICert(context.Background(), cfg, ca); err != nil {
		t.Fatalf("ensureUICert: %v", err)
	}
	if ca.issueCalls != caBootstrapRetries {
		t.Errorf("expected %d IssueCertificate attempts, got %d", caBootstrapRetries, ca.issueCalls)
	}
	if _, err := tls.LoadX509KeyPair(cfg.SSLCert, cfg.SSLKey); err != nil {
		t.Errorf("expected a self-signed fallback cert, got: %v", err)
	}
	assertMarker(t, cfg.SSLCert, true)
}

// ─── renewUICertOnce ────────────────────────────────────────────────────────

func TestRenewUICertOnce(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{
		SSLCert:     filepath.Join(dir, "server.crt"),
		SSLKey:      filepath.Join(dir, "server.key"),
		UIHostname:  "ui.internal.example.com",
		Provisioner: "admin",
	}
	const validity = 90 * 24 * time.Hour
	certPEM, keyPEM := newFakeIssuedCert(t, validity)
	ca := &fakeCA{issueCertPEM: certPEM, issueKeyPEM: keyPEM}

	nextSleep, err := renewUICertOnce(context.Background(), cfg, ca, cfg.SSLCert, cfg.SSLKey, cfg.UIHostname)
	if err != nil {
		t.Fatalf("renewUICertOnce: %v", err)
	}
	got, err := os.ReadFile(cfg.SSLCert) //nolint:gosec // G304: t.TempDir()-derived path
	if err != nil || !bytes.Equal(got, certPEM) {
		t.Errorf("SSLCert not rewritten as expected: err=%v", err)
	}

	wantSleep := validity * 2 / 3
	// Allow a small tolerance since generateSelfSignedCertWithValidity backdates
	// NotBefore by an hour relative to "now".
	if diff := nextSleep - wantSleep; diff < -time.Hour || diff > time.Hour {
		t.Errorf("nextSleep = %v; want ~%v", nextSleep, wantSleep)
	}
}

func TestRenewUICertOnce_IssueError(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{
		SSLCert: filepath.Join(dir, "server.crt"),
		SSLKey:  filepath.Join(dir, "server.key"),
	}
	ca := &fakeCA{issueErr: errors.New("boom")}

	if _, err := renewUICertOnce(context.Background(), cfg, ca, cfg.SSLCert, cfg.SSLKey, "x"); err == nil {
		t.Fatal("expected an error from renewUICertOnce when issuance fails")
	}
}

// ─── writeInlineRootCert / ensureRootCert ──────────────────────────────────

func TestWriteInlineRootCert(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{
		RootCert:      filepath.Join(dir, "root.crt"),
		CARootCertPEM: "-----BEGIN CERTIFICATE-----\nabc\n-----END CERTIFICATE-----",
	}
	if err := writeInlineRootCert(cfg); err != nil {
		t.Fatalf("writeInlineRootCert: %v", err)
	}
	got, err := os.ReadFile(cfg.RootCert) //nolint:gosec // G304: t.TempDir()-derived path
	if err != nil || string(got) != cfg.CARootCertPEM {
		t.Errorf("RootCert contents = %q, err=%v; want %q", got, err, cfg.CARootCertPEM)
	}
}

func TestWriteInlineRootCert_NoOpWhenUnset(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{RootCert: filepath.Join(dir, "root.crt")}
	if err := writeInlineRootCert(cfg); err != nil {
		t.Fatalf("writeInlineRootCert: %v", err)
	}
	if _, err := os.Stat(cfg.RootCert); err == nil {
		t.Error("expected no root cert file to be written when CARootCertPEM is unset")
	}
}

func TestEnsureRootCert_NoOpWhenAlreadyPresent(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{RootCert: filepath.Join(dir, "root.crt")}
	if err := os.WriteFile(cfg.RootCert, []byte("existing"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := ensureRootCert(context.Background(), cfg); err != nil {
		t.Fatalf("ensureRootCert: %v", err)
	}
	got, _ := os.ReadFile(cfg.RootCert) //nolint:gosec // G304: t.TempDir()-derived path
	if string(got) != "existing" {
		t.Error("ensureRootCert must not touch an already-present non-empty root cert")
	}
}

func TestEnsureRootCert_NoOpWhenFingerprintUnset(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{RootCert: filepath.Join(dir, "root.crt")}
	if err := ensureRootCert(context.Background(), cfg); err != nil {
		t.Fatalf("ensureRootCert: %v", err)
	}
	if _, err := os.Stat(cfg.RootCert); err == nil {
		t.Error("expected no root cert file when CAFingerprint is unset (assume volume-mounted)")
	}
}

// ─── Fallback marker, web leaf, renewer ────────────────────────────────────

func assertMarker(t *testing.T, certPath string, want bool) {
	t.Helper()
	_, err := os.Stat(leafcert.FallbackMarker(certPath))
	if got := err == nil; got != want {
		t.Errorf("fallback marker present = %v, want %v", got, want)
	}
}

func fastBootstrap(t *testing.T) {
	t.Helper()
	restoreRetries, restoreInterval := caBootstrapRetries, caBootstrapInterval
	caBootstrapRetries = 2
	caBootstrapInterval = time.Millisecond
	t.Cleanup(func() {
		caBootstrapRetries = restoreRetries
		caBootstrapInterval = restoreInterval
	})
}

// stepcaConfig is both leaves in stepca mode under one temp dir.
func stepcaConfig(t *testing.T) *config.Config {
	t.Helper()
	dir := t.TempDir()
	root := filepath.Join(dir, "root_ca.crt")
	if err := os.WriteFile(root, []byte("ROOT PEM\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return &config.Config{
		UITLSMode:      "stepca",
		SSLCert:        filepath.Join(dir, "ssl", "server.crt"),
		SSLKey:         filepath.Join(dir, "ssl", "server.key"),
		UIHostname:     "certharbor",
		HostIP:         "127.0.0.1",
		WebTLSMode:     "stepca",
		WebHostname:    "ca.example.com",
		WebSSLCert:     filepath.Join(dir, "web-ssl", "web.crt"),
		WebSSLKey:      filepath.Join(dir, "web-ssl", "web.key"),
		WebRootCA:      filepath.Join(dir, "web-ssl", "root_ca.crt"),
		RootCert:       root,
		UICertDuration: time.Hour,
	}
}

// hostCA issues a real leaf for whatever hostname it is asked for, so the
// renewer's hostname check sees the name it requested.
type hostCA struct {
	fakeCA
	t        *testing.T
	validity time.Duration
	domains  []string
}

func (h *hostCA) IssueCertificate(_ context.Context, req stepca.IssueRequest) ([]byte, []byte, error) {
	h.issueCalls++
	h.domains = append(h.domains, req.Domain)
	if h.issueErr != nil {
		return nil, nil, h.issueErr
	}
	dir := h.t.TempDir()
	c, k := filepath.Join(dir, "c"), filepath.Join(dir, "k")
	if err := generateSelfSignedCertWithValidity(c, k, req.Domain, "", h.validity); err != nil {
		return nil, nil, err
	}
	cp, _ := os.ReadFile(c) //nolint:gosec // G304: t.TempDir()-derived path
	kp, _ := os.ReadFile(k) //nolint:gosec // G304: t.TempDir()-derived path
	return cp, kp, nil
}

func TestEnsureUICert_Stepca_SuccessClearsStaleMarker(t *testing.T) {
	cfg := stepcaConfig(t)
	if err := os.MkdirAll(filepath.Dir(cfg.SSLCert), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := writeFallbackMarker(cfg.SSLCert); err != nil {
		t.Fatal(err)
	}
	ca := &hostCA{t: t, validity: time.Hour}
	if err := ensureUICert(context.Background(), cfg, ca); err != nil {
		t.Fatalf("ensureUICert: %v", err)
	}
	assertMarker(t, cfg.SSLCert, false)
}

func TestEnsureWebCert_IssuesForWebHostname(t *testing.T) {
	cfg := stepcaConfig(t)
	ca := &hostCA{t: t, validity: time.Hour}
	if err := ensureWebCert(context.Background(), cfg, ca); err != nil {
		t.Fatalf("ensureWebCert: %v", err)
	}
	if len(ca.domains) != 1 || ca.domains[0] != "ca.example.com" {
		t.Fatalf("issued for %v; want exactly [ca.example.com]", ca.domains)
	}
	if _, err := tls.LoadX509KeyPair(cfg.WebSSLCert, cfg.WebSSLKey); err != nil {
		t.Errorf("web leaf not loadable: %v", err)
	}
}

// TestEnsureWebCert_NeverSelfSigns: no browser accepts a self-signed web leaf,
// so CA failure must leave no file rather than a useless one.
func TestEnsureWebCert_NeverSelfSigns(t *testing.T) {
	fastBootstrap(t)
	cfg := stepcaConfig(t)
	if err := ensureWebCert(context.Background(), cfg, &fakeCA{issueErr: errors.New("CA unreachable")}); err == nil {
		t.Fatal("expected an error")
	}
	if err := ensureWebCert(context.Background(), cfg, nil); err == nil {
		t.Fatal("expected an error with no CA client")
	}
	if _, err := os.Stat(cfg.WebSSLCert); !os.IsNotExist(err) {
		t.Errorf("web.crt written on failure: %v", err)
	}
	assertMarker(t, cfg.WebSSLCert, false)
}

func TestEnsureWebCert_ProvidedIsNoOp(t *testing.T) {
	cfg := stepcaConfig(t)
	cfg.WebTLSMode = "provided"
	ca := &hostCA{t: t, validity: time.Hour}
	if err := ensureWebCert(context.Background(), cfg, ca); err != nil {
		t.Fatal(err)
	}
	if err := syncWebRootCA(cfg); err != nil {
		t.Fatal(err)
	}
	if ca.issueCalls != 0 {
		t.Errorf("provided mode issued %d certificates", ca.issueCalls)
	}
	if _, err := os.Stat(cfg.WebRootCA); !os.IsNotExist(err) {
		t.Error("provided mode wrote root_ca.crt into the operator's directory")
	}
}

func TestSyncWebRootCA_CopiesAndIsIdempotent(t *testing.T) {
	cfg := stepcaConfig(t)
	if err := syncWebRootCA(cfg); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(cfg.WebRootCA)
	if err != nil || string(got) != "ROOT PEM\n" {
		t.Fatalf("root copy = %q, err %v", got, err)
	}
	before, _ := os.Stat(cfg.WebRootCA)
	time.Sleep(10 * time.Millisecond)
	if err := syncWebRootCA(cfg); err != nil {
		t.Fatal(err)
	}
	after, _ := os.Stat(cfg.WebRootCA)
	if !before.ModTime().Equal(after.ModTime()) {
		t.Error("unchanged root was rewritten")
	}
}

// TestRenewLeavesOnce_RecoversFromFallback is the late-CA path: the marker
// and the missing web leaf are both repaired by one iteration, with no
// restart.
func TestRenewLeavesOnce_RecoversFromFallback(t *testing.T) {
	cfg := stepcaConfig(t)
	if err := selfSignFallback(cfg); err != nil {
		t.Fatal(err)
	}
	ca := &hostCA{t: t, validity: time.Hour}
	sleep := renewLeavesOnce(context.Background(), cfg, ca, time.Now())
	if ca.issueCalls != 2 {
		t.Errorf("issued %d leaves, want both", ca.issueCalls)
	}
	assertMarker(t, cfg.SSLCert, false)
	if _, err := tls.LoadX509KeyPair(cfg.WebSSLCert, cfg.WebSSLKey); err != nil {
		t.Errorf("web leaf not issued: %v", err)
	}
	if _, err := os.Stat(cfg.WebRootCA); err != nil {
		t.Errorf("root CA not copied: %v", err)
	}
	// The fake CA backdates NotBefore by an hour, so a 1h leaf spans two
	// hours and is due 20 minutes from now.
	if sleep < 19*time.Minute || sleep > 21*time.Minute {
		t.Errorf("sleep %v; want the new leaf's due point, ~20m away", sleep)
	}
}

func TestRenewLeavesOnce_NotDueSleepsOnMinimum(t *testing.T) {
	cfg := stepcaConfig(t)
	ca := &hostCA{t: t, validity: time.Hour}
	_ = renewLeavesOnce(context.Background(), cfg, ca, time.Now())
	ca.issueCalls = 0
	sleep := renewLeavesOnce(context.Background(), cfg, ca, time.Now())
	if ca.issueCalls != 0 {
		t.Errorf("fresh leaves re-issued %d times", ca.issueCalls)
	}
	if sleep <= 0 || sleep > 45*time.Minute {
		t.Errorf("sleep %v", sleep)
	}
}

// TestRenewLeavesOnce_BothDueRenewTogether: one cycle, one change per file.
func TestRenewLeavesOnce_BothDueRenewTogether(t *testing.T) {
	cfg := stepcaConfig(t)
	ca := &hostCA{t: t, validity: time.Hour}
	_ = renewLeavesOnce(context.Background(), cfg, ca, time.Now())
	ca.issueCalls = 0
	_ = renewLeavesOnce(context.Background(), cfg, ca, time.Now().Add(40*time.Minute))
	if ca.issueCalls != 2 {
		t.Errorf("issued %d leaves at the due point, want both in one pass", ca.issueCalls)
	}
}

func TestRenewLeavesOnce_FailureBackoffs(t *testing.T) {
	cfg := stepcaConfig(t)
	ca := &hostCA{t: t, validity: time.Hour}
	_ = renewLeavesOnce(context.Background(), cfg, ca, time.Now())

	ca.issueErr = errors.New("CA down")
	if got := renewLeavesOnce(context.Background(), cfg, ca, time.Now().Add(40*time.Minute)); got != uiCertRenewFailureBackoff {
		t.Errorf("due-renewal failure slept %v, want the %v backoff", got, uiCertRenewFailureBackoff)
	}
	if err := os.Remove(cfg.WebSSLCert); err != nil {
		t.Fatal(err)
	}
	if got := renewLeavesOnce(context.Background(), cfg, ca, time.Now()); got != caRecoveryInterval {
		t.Errorf("missing-leaf failure slept %v, want the %v recovery interval", got, caRecoveryInterval)
	}
	if got := renewLeavesOnce(context.Background(), cfg, nil, time.Now()); got != caRecoveryInterval {
		t.Errorf("nil client slept %v, want %v", got, caRecoveryInterval)
	}
}

func TestRenewLeavesOnce_HostnameChangeReissues(t *testing.T) {
	cfg := stepcaConfig(t)
	ca := &hostCA{t: t, validity: time.Hour}
	_ = renewLeavesOnce(context.Background(), cfg, ca, time.Now())
	cfg.WebHostname = "new.example.com"
	ca.domains = nil
	_ = renewLeavesOnce(context.Background(), cfg, ca, time.Now())
	if len(ca.domains) != 1 || ca.domains[0] != "new.example.com" {
		t.Errorf("re-issued for %v; want only the web leaf for the new name", ca.domains)
	}
}

func TestManagedLeaves(t *testing.T) {
	cfg := stepcaConfig(t)
	if n := len(managedLeaves(cfg)); n != 2 {
		t.Errorf("both stepca: %d leaves", n)
	}
	cfg.UITLSMode = "self-signed"
	cfg.WebTLSMode = "provided"
	if n := len(managedLeaves(cfg)); n != 0 {
		t.Errorf("neither stepca: %d leaves, the renewer must not run", n)
	}
}

func TestBootstrapFilesNeverWrittenInPlace(t *testing.T) {
	src, err := os.ReadFile("tlsbootstrap.go")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(src, []byte("os.WriteFile")) {
		t.Error("tlsbootstrap.go calls os.WriteFile: certificate and key writes must go through leafcert.WriteAtomic")
	}
}
