package handlers

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"certharbor/config"
	"certharbor/stepca"
)

// slowFakeCA is a stepca.CA stub whose Health call sleeps for a fixed
// duration before returning, standing in for a real step-ca round trip over
// the docker network (typically low single-digit milliseconds, but modelled
// generously here) when benchmarking caIntegrity's cost.
type slowFakeCA struct {
	delay time.Duration
	err   error
}

func (f *slowFakeCA) Health(ctx context.Context) error {
	select {
	case <-time.After(f.delay):
	case <-ctx.Done():
		return ctx.Err()
	}
	return f.err
}

func (f *slowFakeCA) Provisioners(context.Context) ([]stepca.ProvisionerInfo, error) { return nil, nil }

func (f *slowFakeCA) IssueCertificate(context.Context, stepca.IssueRequest) ([]byte, []byte, error) {
	return nil, nil, nil
}

func (f *slowFakeCA) Revoke(context.Context, string, string) error { return nil }

var _ stepca.CA = (*slowFakeCA)(nil)

// writeSignedCert writes a PEM-encoded certificate signed by signerKey (or
// self-signed when signerCert/signerKey are the subject's own), reproducing
// the shape health.go's checkCAChain expects: IsCA true, a validity window
// straddling now.
func writeSignedCert(t testing.TB, path, cn string, signerCert *x509.Certificate, signerKey *ecdsa.PrivateKey) (*x509.Certificate, *ecdsa.PrivateKey) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	tpl := &x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: cn},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	parent, signKey := tpl, key
	if signerCert != nil {
		parent, signKey = signerCert, signerKey
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, parent, &key.PublicKey, signKey)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	return cert, key
}

// integrityBenchCfg builds a representative on-disk layout for caIntegrity:
// a self-signed root, an intermediate signed by it, a ca.json naming the
// configured provisioner, and a password file. It deliberately does not
// create a certificates directory with many files, because caIntegrity's
// source (health.go: caHealthErr, checkCAChain, checkCAConfig,
// checkProvisionerPasswordSync, checkStepCAImagePin) never lists or reads
// one; this benchmark setup matches what the code actually touches.
func integrityBenchCfg(b *testing.B) *config.Config {
	b.Helper()
	dir := b.TempDir()
	rootPath := filepath.Join(dir, "certs", "root_ca.crt")
	root, rootKey := writeSignedCert(b, rootPath, "root", nil, nil)
	writeSignedCert(b, filepath.Join(dir, "certs", "intermediate_ca.crt"), "intermediate", root, rootKey)

	caJSON := filepath.Join(dir, "config", "ca.json")
	if err := os.MkdirAll(filepath.Dir(caJSON), 0o750); err != nil {
		b.Fatal(err)
	}
	const caJSONBody = `{"authority":{"provisioners":[{"name":"admin","type":"JWK","claims":{"defaultTLSCertDuration":"8760h","maxTLSCertDuration":"87600h"}}]}}`
	if err := os.WriteFile(caJSON, []byte(caJSONBody), 0o600); err != nil {
		b.Fatal(err)
	}

	pwPath := filepath.Join(dir, "secrets", "password")
	if err := os.MkdirAll(filepath.Dir(pwPath), 0o750); err != nil {
		b.Fatal(err)
	}
	if err := os.WriteFile(pwPath, []byte("bench-password\n"), 0o600); err != nil {
		b.Fatal(err)
	}

	return &config.Config{
		CAURL:        "https://step-ca:9443",
		RootCert:     rootPath,
		Provisioner:  "admin",
		PasswordFile: pwPath,
		StepCAImage:  "smallstep/step-ca:0.27.0",
	}
}

// BenchmarkCAIntegrity_LocalHealth measures caIntegrity's cost when the CA
// health probe itself is effectively free (a FakeCA with zero latency),
// isolating the cost of the file-system work: two PEM reads/parses/verifies,
// one JSON config read, two secret-file reads, and some string checks. This
// is the floor cost on an idle machine.
func BenchmarkCAIntegrity_LocalHealth(b *testing.B) {
	cfg := integrityBenchCfg(b)
	h := newMinimalHandler(cfg)
	h.SetCAClient(&slowFakeCA{delay: 0})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = h.caIntegrity(context.Background())
	}
}

// BenchmarkCAIntegrity_RealisticNetworkHealth adds a 3ms synthetic delay to
// the CA health probe, representative of a same-docker-network HTTPS round
// trip (measured step-ca /health responses on a loaded host are typically
// 1-5ms; 3ms is a conservative mid-point, not a floor). This isolates
// whether the *dominant* cost is the single network call rather than the
// file-system work, which it is: the file-system-only benchmark above
// should be roughly delay-independent and orders of magnitude cheaper.
func BenchmarkCAIntegrity_RealisticNetworkHealth(b *testing.B) {
	cfg := integrityBenchCfg(b)
	h := newMinimalHandler(cfg)
	h.SetCAClient(&slowFakeCA{delay: 3 * time.Millisecond})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = h.caIntegrity(context.Background())
	}
}
