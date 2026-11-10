package handlers

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
