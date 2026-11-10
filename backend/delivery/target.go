// Package delivery implements certharbor-delivery's reconciler, target
// registry and YAML import/export, per plans/distribution.md. It is
// imported only by backend/cmd/certharbor-delivery; certharbor-control never
// imports it, keeping the credential boundary from Section 2 of that
// document at the Go package level too.
package delivery

import (
	"context"
	"encoding/json"
)

// CertMaterial is the certificate payload a Target pushes: leaf, chain and
// (if the target requires it) the private key.
type CertMaterial struct {
	CertPEM  []byte
	ChainPEM []byte
	KeyPEM   []byte

	// Fingerprint is the SHA-256 of CertPEM, used by the reconciler to skip
	// redundant pushes and by Check implementations that can't otherwise
	// derive a comparable value from the target.
	Fingerprint string
}

// Target is the interface every compiled-in delivery target implements
// (plans/distribution.md Section 7). Targets are compiled into the binary,
// never loaded dynamically.
type Target interface {
	// Name is the target type identifier used in delivery_targets.type
	// (e.g. "aws-acm").
	Name() string

	// Check reports whether the target currently holds a certificate
	// matching fingerprint. Used to skip a redundant push; a target that
	// cannot check the current state always returns pushRequired=true,
	// i.e. current=false, err=nil.
	Check(ctx context.Context, fingerprint string, params json.RawMessage) (current bool, err error)

	// Push delivers cert, chain and (if the target requires it) key. Must
	// be idempotent: repeated pushes of identical material are a no-op at
	// the target. params is the binding's target_params; implementations
	// that need to persist state back (e.g. a stable ARN) return it via
	// PushResult.UpdatedParams.
	Push(ctx context.Context, material *CertMaterial, params json.RawMessage) (PushResult, error)
}

// PushResult carries any target_params the target wants persisted back onto
// the binding after a successful push (e.g. the ACM ARN on first import).
// A nil UpdatedParams means "no change".
type PushResult struct {
	UpdatedParams json.RawMessage
}
