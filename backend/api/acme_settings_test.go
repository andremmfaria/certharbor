package api

import (
	"encoding/json"
	"testing"
)

// TestPatchAcmeSettingsBody_ExplicitJSONNullSecretsDecodeToNil mirrors
// TestPatchNotificationsBody_ExplicitJSONNullSMTPPasswordDecodesToNil: Go's
// encoding/json decodes an explicit `"cfToken": null` / `"r53SecretKey":
// null` identically to the field being absent entirely — both produce a
// nil *string — so toAcmeSettingsPatch/applyACMESettingsPatch's "nil means
// unchanged" contract holds for a caller that sends an explicit null
// exactly as it does for one that omits the field.
func TestPatchAcmeSettingsBody_ExplicitJSONNullSecretsDecodeToNil(t *testing.T) {
	var b patchAcmeSettingsBody
	if err := json.Unmarshal([]byte(`{"email":"a@example.com","cfToken":null,"r53SecretKey":null}`), &b); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if b.CFToken != nil {
		t.Fatalf("CFToken = %q, want nil (JSON null must decode like an absent field)", *b.CFToken)
	}
	if b.R53SecretKey != nil {
		t.Fatalf("R53SecretKey = %q, want nil (JSON null must decode like an absent field)", *b.R53SecretKey)
	}
	// A sibling field present in the same body must still decode normally.
	if b.Email == nil || *b.Email != "a@example.com" {
		t.Fatalf("Email = %v, want pointer to \"a@example.com\"", b.Email)
	}

	patch := toAcmeSettingsPatch(&b)
	if patch.CFToken != nil || patch.R53SecretKey != nil {
		t.Fatalf("toAcmeSettingsPatch(...) secrets = (%v, %v), want (nil, nil)", patch.CFToken, patch.R53SecretKey)
	}
}

// TestPatchAcmeSettingsBody_OmittedSecretsDecodeToNil is the complementary
// case: a body that never mentions the secret fields at all must decode
// to the same nil pointers explicit null does.
func TestPatchAcmeSettingsBody_OmittedSecretsDecodeToNil(t *testing.T) {
	var b patchAcmeSettingsBody
	if err := json.Unmarshal([]byte(`{"email":"a@example.com"}`), &b); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if b.CFToken != nil {
		t.Fatalf("CFToken = %q, want nil (omitted field)", *b.CFToken)
	}
	if b.R53SecretKey != nil {
		t.Fatalf("R53SecretKey = %q, want nil (omitted field)", *b.R53SecretKey)
	}
}
