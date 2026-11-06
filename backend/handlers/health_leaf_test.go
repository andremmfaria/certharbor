package handlers

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"step-webui/config"
	"step-webui/leafcert"
)

func writeLeaf(t *testing.T, path string, notBefore, notAfter time.Time) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "x"}, NotBefore: notBefore, NotAfter: notAfter}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
}

func leafCfg(t *testing.T) *config.Config {
	dir := t.TempDir()
	return &config.Config{
		UITLSMode:  "stepca",
		WebTLSMode: "stepca",
		SSLCert:    filepath.Join(dir, "ssl", "server.crt"),
		WebSSLCert: filepath.Join(dir, "web-ssl", "web.crt"),
	}
}

func TestLeafReadiness_BothFreshReportNotAfter(t *testing.T) {
	cfg := leafCfg(t)
	now := time.Now()
	writeLeaf(t, cfg.SSLCert, now.Add(-time.Minute), now.Add(time.Hour))
	writeLeaf(t, cfg.WebSSLCert, now.Add(-time.Minute), now.Add(2*time.Hour))
	body := map[string]string{}
	if !newMinimalHandler(cfg).leafReadiness(body, now) {
		t.Fatalf("fresh leaves not ready: %v", body)
	}
	for _, k := range []string{"backendCert", "webCert"} {
		if body[k] != "ok" || body[k+"NotAfter"] == "" {
			t.Errorf("%s: %q notAfter %q", k, body[k], body[k+"NotAfter"])
		}
	}
}

func TestLeafReadiness_FailureModes(t *testing.T) {
	now := time.Now()
	cases := map[string]struct {
		setup   func(cfg *config.Config)
		key     string
		wantPre string
	}{
		"fallback_marker": {func(cfg *config.Config) {
			writeLeaf(t, cfg.SSLCert, now.Add(-time.Minute), now.Add(time.Hour))
			writeLeaf(t, cfg.WebSSLCert, now.Add(-time.Minute), now.Add(time.Hour))
			if err := os.WriteFile(leafcert.FallbackMarker(cfg.SSLCert), []byte("x"), 0o600); err != nil {
				t.Fatal(err)
			}
		}, "backendCert", "self-signed-fallback "},
		"web_missing": {func(cfg *config.Config) {
			writeLeaf(t, cfg.SSLCert, now.Add(-time.Minute), now.Add(time.Hour))
		}, "webCert", "missing "},
		"web_renewal_overdue": {func(cfg *config.Config) {
			writeLeaf(t, cfg.SSLCert, now.Add(-time.Minute), now.Add(time.Hour))
			// Due at 2/3 of 3h = 2h in; now is 2h10m in, past the 5m grace.
			writeLeaf(t, cfg.WebSSLCert, now.Add(-130*time.Minute), now.Add(50*time.Minute))
		}, "webCert", "renewal-overdue"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := leafCfg(t)
			tc.setup(cfg)
			body := map[string]string{}
			if newMinimalHandler(cfg).leafReadiness(body, now) {
				t.Fatalf("ready despite %s: %v", name, body)
			}
			if !strings.HasPrefix(body[tc.key], tc.wantPre) {
				t.Errorf("%s = %q, want prefix %q", tc.key, body[tc.key], tc.wantPre)
			}
		})
	}
}

// TestLeafReadiness_UnmanagedLeavesNeverFail: a provided web leaf is the
// operator's, and a self-signed backend leaf is the chosen mode, not a fault.
func TestLeafReadiness_UnmanagedLeavesNeverFail(t *testing.T) {
	cfg := leafCfg(t)
	cfg.UITLSMode = "self-signed"
	cfg.WebTLSMode = "provided"
	if err := os.MkdirAll(filepath.Dir(cfg.SSLCert), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(leafcert.FallbackMarker(cfg.SSLCert), []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	body := map[string]string{}
	if !newMinimalHandler(cfg).leafReadiness(body, time.Now()) {
		t.Errorf("unmanaged leaves failed readiness: %v", body)
	}
}

func TestReadiness_NamesFallbackMarker(t *testing.T) {
	cfg := leafCfg(t)
	cfg.CAURL = "https://127.0.0.1:19999"
	writeLeaf(t, cfg.SSLCert, time.Now().Add(-time.Minute), time.Now().Add(time.Hour))
	if err := os.WriteFile(leafcert.FallbackMarker(cfg.SSLCert), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	newMinimalHandler(cfg).Readiness(rr, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/ready", nil))
	if rr.Code == http.StatusOK {
		t.Fatal("ready with a fallback marker")
	}
	var body map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body["backendCert"], leafcert.FallbackMarkerName) {
		t.Errorf("body does not name the marker: %v", body)
	}
}
