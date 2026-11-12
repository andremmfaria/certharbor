package handlers

import (
	"errors"
	"testing"

	"certharbor/models"
	"certharbor/security"
)

func TestApplyProfilePatch_OmittedFieldsUnchanged(t *testing.T) {
	current := &models.User{DisplayName: "Alice", Email: "alice@example.com"}
	displayName, email, err := applyProfilePatch(current, &ProfilePatch{})
	if err != nil {
		t.Fatalf("applyProfilePatch: %v", err)
	}
	if displayName != current.DisplayName || email != current.Email {
		t.Fatalf("got (%q, %q), want unchanged (%q, %q)", displayName, email, current.DisplayName, current.Email)
	}
}

func TestApplyProfilePatch_SetsExplicitFields(t *testing.T) {
	current := &models.User{DisplayName: "Alice", Email: "alice@example.com"}
	newName := "Alice B"
	newEmail := "alice.b@example.com"
	displayName, email, err := applyProfilePatch(current, &ProfilePatch{DisplayName: &newName, Email: &newEmail})
	if err != nil {
		t.Fatalf("applyProfilePatch: %v", err)
	}
	if displayName != newName {
		t.Errorf("DisplayName = %q, want %q", displayName, newName)
	}
	if email != newEmail {
		t.Errorf("Email = %q, want %q", email, newEmail)
	}
}

// TestApplyProfilePatch_RejectsInvalidEmail is this phase's replacement for
// the template handler's flash-message validation (handlers/users.go:226's
// mail.ParseAddress check): an unparseable address must be a
// *ValidationError, not a silently-stored garbage string.
func TestApplyProfilePatch_RejectsInvalidEmail(t *testing.T) {
	current := &models.User{DisplayName: "Alice", Email: "alice@example.com"}
	badEmail := "not-an-email"
	_, _, err := applyProfilePatch(current, &ProfilePatch{Email: &badEmail})
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("applyProfilePatch with an invalid email: got %v, want a *ValidationError", err)
	}
}

// TestApplyProfilePatch_BlankEmailClears proves an explicit empty string
// (distinct from an absent field, 5.8) clears the stored email rather than
// failing validation: the template handler's mail.ParseAddress was only
// ever reached when email != "".
func TestApplyProfilePatch_BlankEmailClears(t *testing.T) {
	current := &models.User{DisplayName: "Alice", Email: "alice@example.com"}
	blank := ""
	_, email, err := applyProfilePatch(current, &ProfilePatch{Email: &blank})
	if err != nil {
		t.Fatalf("applyProfilePatch: %v", err)
	}
	if email != "" {
		t.Errorf("Email = %q, want cleared", email)
	}
}

func TestValidateChangePassword_RejectsWrongCurrentPassword(t *testing.T) {
	current := &models.User{PasswordHash: security.HashPassword("correct-horse")}
	err := validateChangePassword(current, "wrong-password", "Str0ng!Passw0rd")
	if !errors.Is(err, ErrIncorrectPassword) {
		t.Fatalf("got %v, want ErrIncorrectPassword", err)
	}
}

func TestValidateChangePassword_RejectsWeakNewPassword(t *testing.T) {
	current := &models.User{PasswordHash: security.HashPassword("correct-horse")}
	err := validateChangePassword(current, "correct-horse", "weak")
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("got %v, want a *ValidationError", err)
	}
}

func TestValidateChangePassword_AcceptsCorrectCurrentAndStrongNew(t *testing.T) {
	current := &models.User{PasswordHash: security.HashPassword("correct-horse")}
	if err := validateChangePassword(current, "correct-horse", "Str0ng!Passw0rd"); err != nil {
		t.Fatalf("got %v, want nil", err)
	}
}

// TestUpdateProfilePreferences_RejectsInvalidThemeBeforeAnyQuery proves the
// theme is validated before any database call: h.db is nil, so a code path
// that queried first would panic instead of returning a *ValidationError,
// mirroring TestCreateUserAdmin_InvalidRoleRejected's structure.
func TestUpdateProfilePreferences_RejectsInvalidThemeBeforeAnyQuery(t *testing.T) {
	h := &Handler{}
	bogus := "psychedelic"

	_, err := h.UpdateProfilePreferences(1, &ProfilePreferencesPatch{Theme: &bogus})
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("UpdateProfilePreferences with an invalid theme: got %v, want a *ValidationError", err)
	}
}
