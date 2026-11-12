package handlers

import (
	"testing"

	"certharbor/models"
)

//nolint:gosec // G101: fixture values, not credentials
func storedACMESettingsAPI() *models.ACMESettings {
	return &models.ACMESettings{
		ID:           1,
		Email:        "acme@example.com",
		Provider:     "cloudflare",
		CFToken:      "stored-cf-token",
		CFZoneID:     "stored-zone",
		R53KeyID:     "AKIAIO...MPLE",
		R53SecretKey: "stored-r53-secret",
		R53Region:    "eu-west-1",
	}
}

// TestApplyACMESettingsPatch_OmittedSecretsUnchanged is this phase's exit
// criterion ("Settings PATCH preserves account material"): a patch that
// omits CFToken and R53SecretKey must leave both exactly as stored,
// mirroring applyNotificationPatch's SMTPPassword guarantee.
func TestApplyACMESettingsPatch_OmittedSecretsUnchanged(t *testing.T) {
	current := storedACMESettingsAPI()
	newEmail := "new@example.com"
	patch := &ACMESettingsPatch{Email: &newEmail}

	got := applyACMESettingsPatch(current, patch)

	if got.CFToken != current.CFToken {
		t.Errorf("CFToken = %q, want unchanged %q", got.CFToken, current.CFToken)
	}
	if got.R53SecretKey != current.R53SecretKey {
		t.Errorf("R53SecretKey = %q, want unchanged %q", got.R53SecretKey, current.R53SecretKey)
	}
	if got.CFZoneID != current.CFZoneID {
		t.Errorf("CFZoneID = %q, want unchanged %q", got.CFZoneID, current.CFZoneID)
	}
	if got.R53KeyID != current.R53KeyID {
		t.Errorf("R53KeyID = %q, want unchanged %q", got.R53KeyID, current.R53KeyID)
	}
	if got.Email != newEmail {
		t.Errorf("Email = %q, want %q (the field actually present in the patch)", got.Email, newEmail)
	}
}

// TestApplyACMESettingsPatch_SubmittedSecretsReplace is the complementary
// case: a patch that does set CFToken/R53SecretKey must actually apply
// them, so the previous test isn't vacuously true from a merge bug that
// never writes anything.
func TestApplyACMESettingsPatch_SubmittedSecretsReplace(t *testing.T) {
	current := storedACMESettingsAPI()
	//nolint:gosec // G101: fixture values, not credentials
	newToken := "new-cf-token"
	//nolint:gosec // G101: fixture values, not credentials
	newSecret := "new-r53-secret"
	patch := &ACMESettingsPatch{CFToken: &newToken, R53SecretKey: &newSecret}

	got := applyACMESettingsPatch(current, patch)

	if got.CFToken != newToken {
		t.Errorf("CFToken = %q, want %q", got.CFToken, newToken)
	}
	if got.R53SecretKey != newSecret {
		t.Errorf("R53SecretKey = %q, want %q", got.R53SecretKey, newSecret)
	}
}
