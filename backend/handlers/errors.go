package handlers

import "errors"

// ValidationError marks a certificate-mutation failure caused by the
// caller's input (missing field, unsafe name, unreadable path, a template
// or duration the server does not recognise) rather than an internal
// failure. backend/api maps it to 422; everything else surfaces as 500.
//
// The old template handlers made exactly this distinction by rendering a
// flash message instead of failing the request outright, but that
// distinction lived only in which http.Error path the code happened to
// take. Phase 4's extraction (R23) needs it as a real type so api/, which
// never sees the original request, can still tell the two apart.
type ValidationError struct{ msg string }

func (e *ValidationError) Error() string { return e.msg }

// NewValidationError builds a ValidationError.
func NewValidationError(msg string) error { return &ValidationError{msg: msg} }

// ErrInvalidCredentials marks a reauth failure caused by a wrong current
// password, distinct from ValidationError so api/ can map it to 401
// invalid-credentials rather than 422 (mirroring the convention used by
// changePassword on the sibling profile-core branch: a wrong password is an
// authentication problem, not a validation one). A wrong second factor
// (e.g. an invalid TOTP code) stays a ValidationError/422: it is a
// different reauth factor failing, not the same credential.
var ErrInvalidCredentials = errors.New("current password is incorrect")
