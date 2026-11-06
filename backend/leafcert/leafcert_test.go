package leafcert

import (
	"crypto/x509"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWriteAtomic_RenamesTmpSibling(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "web.crt")

	var calls [][2]string
	orig := rename
	rename = func(from, to string) error {
		calls = append(calls, [2]string{from, to})
		if _, err := os.Stat(to); err == nil {
			t.Errorf("%s already exists before rename: the write went in place", to)
		}
		return orig(from, to)
	}
	t.Cleanup(func() { rename = orig })

	if err := WriteAtomic(path, []byte("pem"), 0o600); err != nil {
		t.Fatalf("WriteAtomic: %v", err)
	}
	if len(calls) != 1 || calls[0][0] != path+".tmp" || calls[0][1] != path {
		t.Fatalf("rename calls = %v; want exactly [%s.tmp -> %s]", calls, path, path)
	}
	got, err := os.ReadFile(path) //nolint:gosec // G304: t.TempDir()-derived path
	if err != nil || string(got) != "pem" {
		t.Fatalf("content = %q, err = %v", got, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v; want 0600", info.Mode().Perm())
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Errorf(".tmp sibling left behind: %v", err)
	}
}

func TestWriteAtomic_RenameFailureLeavesTargetUntouched(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "web.key")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	orig := rename
	rename = func(string, string) error { return errors.New("boom") }
	t.Cleanup(func() { rename = orig })

	if err := WriteAtomic(path, []byte("new"), 0o600); err == nil {
		t.Fatal("expected an error")
	}
	got, _ := os.ReadFile(path) //nolint:gosec // G304: t.TempDir()-derived path
	if string(got) != "old" {
		t.Errorf("target = %q; want the old content untouched", got)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Errorf(".tmp sibling left behind after a failed rename: %v", err)
	}
}

func TestRenewalDueAndOverdue(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c := &x509.Certificate{NotBefore: start, NotAfter: start.Add(3 * time.Hour)}
	if got := RenewalDue(c); !got.Equal(start.Add(2 * time.Hour)) {
		t.Fatalf("RenewalDue = %v; want two thirds in", got)
	}
	if Overdue(c, start.Add(2*time.Hour+RenewFailureBackoff)) {
		t.Error("exactly one backoff past the renewal point must not be overdue yet")
	}
	if !Overdue(c, start.Add(2*time.Hour+RenewFailureBackoff+time.Second)) {
		t.Error("more than one backoff past the renewal point must be overdue")
	}
}

func TestFallbackMarker(t *testing.T) {
	if got := FallbackMarker("/opt/step-webui/ssl/server.crt"); got != "/opt/step-webui/ssl/.fallback" {
		t.Errorf("FallbackMarker = %q", got)
	}
}
