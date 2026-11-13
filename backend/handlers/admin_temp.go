package handlers

import (
	"crypto/rand"
	"math/big"
)

// generateTempUsername → "guest-ab12cd"
func generateTempUsername() string {
	const alphabet = "abcdefghijkmnopqrstuvwxyz23456789" // excluding 0,1,l,o
	b := make([]byte, 6)
	for i := range b {
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
		b[i] = alphabet[n.Int64()]
	}
	return "guest-" + string(b)
}

// generateTempPassword, cryptographically secure password of length n, excluding ambiguous characters
func generateTempPassword(n int) string {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghjkmnpqrstuvwxyz23456789!@#$%&*+-=?"
	b := make([]byte, n)
	for i := range b {
		idx, _ := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
		b[i] = alphabet[idx.Int64()]
	}
	return string(b)
}
