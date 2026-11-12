package handlers

import (
	"bytes"
	"image/png"
	"testing"
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
