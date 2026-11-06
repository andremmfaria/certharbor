package security

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/sessions"
)

const testSecret = "a-long-test-secret-key-32charsXX" //nolint:gosec // G101: fixture, not a credential

func TestDeriveSessionKeysDeterministic(t *testing.T) {
	h1, b1, err := DeriveSessionKeys(testSecret)
	if err != nil {
		t.Fatalf("DeriveSessionKeys: %v", err)
	}
	h2, b2, err := DeriveSessionKeys(testSecret)
	if err != nil {
		t.Fatalf("DeriveSessionKeys: %v", err)
	}
	if !bytes.Equal(h1, h2) || !bytes.Equal(b1, b2) {
		t.Fatal("same secret produced different keys")
	}
}

func TestDeriveSessionKeysAreDistinct(t *testing.T) {
	h, b, err := DeriveSessionKeys(testSecret)
	if err != nil {
		t.Fatalf("DeriveSessionKeys: %v", err)
	}
	if bytes.Equal(h, b) {
		t.Fatal("authentication and encryption keys are identical")
	}
}

func TestDeriveSessionKeysDifferPerSecret(t *testing.T) {
	h1, b1, _ := DeriveSessionKeys(testSecret)
	h2, b2, _ := DeriveSessionKeys(testSecret + "x")
	if bytes.Equal(h1, h2) || bytes.Equal(b1, b2) {
		t.Fatal("different secrets produced a shared key")
	}
}

func TestDeriveSessionKeysLengths(t *testing.T) {
	h, b, err := DeriveSessionKeys(testSecret)
	if err != nil {
		t.Fatalf("DeriveSessionKeys: %v", err)
	}
	if len(h) != 32 {
		t.Errorf("authentication key length = %d, want 32", len(h))
	}
	if n := len(b); n != 16 && n != 24 && n != 32 {
		t.Errorf("encryption key length = %d, not a valid AES key size", n)
	}
}

func TestDeriveSessionKeysRejectsEmptySecret(t *testing.T) {
	if _, _, err := DeriveSessionKeys(""); err == nil {
		t.Fatal("expected an error for an empty secret")
	}
}

// The derived keys must round-trip through the real cookie store, which is
// where an invalid AES key size would surface.
func TestDeriveSessionKeysWorkWithCookieStore(t *testing.T) {
	h, b, err := DeriveSessionKeys(testSecret)
	if err != nil {
		t.Fatalf("DeriveSessionKeys: %v", err)
	}
	store := sessions.NewCookieStore(h, b)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	s, err := store.Get(req, "s")
	if err != nil {
		t.Fatalf("store.Get: %v", err)
	}
	s.Values["k"] = "v"
	if err := s.Save(req, rec); err != nil {
		t.Fatalf("Save: %v", err)
	}

	req2 := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
	for _, c := range rec.Result().Cookies() {
		req2.AddCookie(c)
	}
	got, err := store.Get(req2, "s")
	if err != nil {
		t.Fatalf("store.Get after round trip: %v", err)
	}
	if got.Values["k"] != "v" {
		t.Fatalf("session value lost in round trip: %v", got.Values)
	}
}
