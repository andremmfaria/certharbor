package security

import (
	"crypto/hkdf"
	"crypto/sha256"
	"errors"
	"fmt"
)

// Distinct info labels keep the signing key and the encryption key
// independent even though both come from one SECRET_KEY.
const (
	sessionHashKeyInfo  = "certharbor session cookie authentication key"
	sessionBlockKeyInfo = "certharbor session cookie encryption key"
)

// sessionKeyLen is 32 for both keys: an HMAC-SHA256 key at its block-friendly
// size and an AES-256 key, both accepted by gorilla/securecookie.
const sessionKeyLen = 32

// DeriveSessionKeys derives the cookie authentication key and the cookie
// encryption key from secret with HKDF-SHA256. SECRET_KEY is already a
// high-entropy random value, so a plain HKDF is the right KDF here and no
// password stretching is needed.
func DeriveSessionKeys(secret string) (hashKey, blockKey []byte, err error) {
	if secret == "" {
		return nil, nil, errors.New("session key secret is empty")
	}
	hashKey, err = hkdf.Key(sha256.New, []byte(secret), nil, sessionHashKeyInfo, sessionKeyLen)
	if err != nil {
		return nil, nil, fmt.Errorf("derive session authentication key: %w", err)
	}
	blockKey, err = hkdf.Key(sha256.New, []byte(secret), nil, sessionBlockKeyInfo, sessionKeyLen)
	if err != nil {
		return nil, nil, fmt.Errorf("derive session encryption key: %w", err)
	}
	return hashKey, blockKey, nil
}
