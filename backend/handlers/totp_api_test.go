package handlers

import (
	"bytes"
	"encoding/base32"
	"image/png"
	"testing"

	"github.com/pquerna/otp/totp"
)

// TestBuildTOTPQRPNG_ValidPNG asserts the pure helper (R23's one genuinely
// new piece of logic in this batch) produces a decodable 220x220 PNG for a
// well-formed secret, with no database involved.
func TestBuildTOTPQRPNG_ValidPNG(t *testing.T) {
	data, err := buildTOTPQRPNG("alice", "JBSWY3DPEHPK3PXP")
	if err != nil {
		t.Fatalf("buildTOTPQRPNG: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("buildTOTPQRPNG returned no data")
	}
	if !bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")) {
		t.Fatal("buildTOTPQRPNG did not return a PNG-signed byte slice")
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("png.Decode: %v", err)
	}
	b := img.Bounds()
	if b.Dx() != 220 || b.Dy() != 220 {
		t.Fatalf("image size = %dx%d, want 220x220", b.Dx(), b.Dy())
	}
}

// TestBuildTOTPQRPNG_DifferentSecretsDifferentImages is a cheap sanity check
// that the secret actually participates in the encoded QR payload rather
// than the function returning a constant image regardless of input.
func TestBuildTOTPQRPNG_DifferentSecretsDifferentImages(t *testing.T) {
	a, err := buildTOTPQRPNG("alice", "JBSWY3DPEHPK3PXP")
	if err != nil {
		t.Fatalf("buildTOTPQRPNG(a): %v", err)
	}
	b, err := buildTOTPQRPNG("alice", "KRSXG5CTMVRXEZLU")
	if err != nil {
		t.Fatalf("buildTOTPQRPNG(b): %v", err)
	}
	if bytes.Equal(a, b) {
		t.Fatal("buildTOTPQRPNG produced identical PNGs for two different secrets")
	}
}

// TestBuildTOTPQRPNG_SecretRoundTrips guards against double-base32-encoding
// the secret: GenerateOpts.Secret takes raw bytes and base32-encodes them
// itself, so passing the already-base32 TOTPPendingSecret string's bytes
// directly (instead of decoding it first) silently re-encodes it into a
// different, longer secret. The QR then enrolls an authenticator app against
// a value the server never validates (ConfirmMfaEnrollment/APIVerifyMFA
// check the original TOTPPendingSecret), breaking every QR-code enrollment
// while the manual-entry key path stays correct — this is exactly the split
// that let the bug ship silently. This test exercises the same decode step
// buildTOTPQRPNG performs and asserts the otpauth key it would embed carries
// the identical secret, the property a PNG-only or "two different inputs
// differ" test cannot catch.
func TestBuildTOTPQRPNG_SecretRoundTrips(t *testing.T) {
	for _, secret := range []string{"JBSWY3DPEHPK3PXP", "KRSXG5CTMVRXEZLU", "2J4GOA3B7KXY3JGFE6K65U3CQLMZ4M6H"} {
		raw, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
		if err != nil {
			t.Fatalf("decode %q: %v", secret, err)
		}
		key, err := totp.Generate(totp.GenerateOpts{Issuer: totpIssuer, AccountName: "alice", Secret: raw})
		if err != nil {
			t.Fatalf("totp.Generate(%q): %v", secret, err)
		}
		if key.Secret() != secret {
			t.Fatalf("secret round-trip: got %q, want %q (double base32 encoding regression)", key.Secret(), secret)
		}
	}
}
