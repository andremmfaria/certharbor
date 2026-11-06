// Package leafcert holds the certificate-file facts the TLS bootstrap writes
// and /ready reports, so the renewal window and the fallback marker have one
// definition shared by package main and handlers.
package leafcert

import (
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// RenewFailureBackoff is how long the renewer waits after a failed attempt.
// /ready also uses it as the grace past a leaf's renewal point before calling
// the leaf overdue, so one failed attempt is tolerated and a second is loud.
const RenewFailureBackoff = 5 * time.Minute

// FallbackMarkerName sits beside the backend leaf whenever UI_TLS_MODE=stepca
// had to self-sign. nginx verifies the backend against the CA root, so a
// self-signed backend leaf means every proxied request fails, and the marker
// is what lets /ready and the healthcheck say so.
const FallbackMarkerName = ".fallback"

// FallbackMarker returns the marker path for the leaf at certPath.
func FallbackMarker(certPath string) string {
	return filepath.Join(filepath.Dir(certPath), FallbackMarkerName)
}

// Read parses the first PEM certificate in path.
func Read(path string) (*x509.Certificate, error) {
	raw, err := os.ReadFile(path) //nolint:gosec // G304: config-derived path, not user input
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(raw)
	if block == nil {
		return nil, errors.New("certificate file has no PEM block")
	}
	return x509.ParseCertificate(block.Bytes)
}

// RenewalDue is the point at which the renewer re-issues: two thirds of the
// way through the validity window.
func RenewalDue(c *x509.Certificate) time.Time {
	return c.NotBefore.Add(c.NotAfter.Sub(c.NotBefore) * 2 / 3)
}

// Overdue reports whether c is more than one failure backoff past its renewal
// point, which only happens when at least one renewal attempt has failed.
func Overdue(c *x509.Certificate, now time.Time) bool {
	return now.After(RenewalDue(c).Add(RenewFailureBackoff))
}

// rename is swapped in tests to prove WriteAtomic never writes in place.
var rename = os.Rename

// WriteAtomic writes data to a ".tmp" sibling of path and renames it over
// path. Both readers of these files (the Go reloader and nginx's reload
// watcher) would otherwise be able to read a half-written PEM.
func WriteAtomic(path string, data []byte, perm os.FileMode) error {
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm) //nolint:gosec // G304: config-derived path, not user input
	if err != nil {
		return fmt.Errorf("creating %s: %w", tmp, err)
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("writing %s: %w", tmp, err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("syncing %s: %w", tmp, err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("closing %s: %w", tmp, err)
	}
	// OpenFile only applies perm to a new file; a stale .tmp keeps its old mode.
	if err := os.Chmod(tmp, perm); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("chmod %s: %w", tmp, err)
	}
	if err := rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("renaming %s: %w", tmp, err)
	}
	return nil
}
