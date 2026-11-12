package api

import (
	"encoding/json"
	"testing"
)

// TestPatchNotificationsBody_ExplicitJSONNullSMTPPasswordDecodesToNil is
// the previously-unverified half of 5.8's exit criterion
// (patchNotificationsBody.SMTPPassword's doc comment): Go's encoding/json
// decodes an explicit `"smtpPassword": null` identically to the field
// being absent from the body entirely — both produce a nil *string — so
// toSettingsPatch/applyNotificationPatch's "nil means unchanged" contract
// holds for a caller that sends an explicit null exactly as it does for
// one that omits the field. If that stopped being true (e.g. a custom
// UnmarshalJSON treating null specially, or a future switch to a
// null-aware wrapper type), this test would catch it before a client's
// explicit null zeroed a stored SMTP password.
func TestPatchNotificationsBody_ExplicitJSONNullSMTPPasswordDecodesToNil(t *testing.T) {
	var b patchNotificationsBody
	if err := json.Unmarshal([]byte(`{"webhookEnabled":true,"smtpPassword":null}`), &b); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if b.SMTPPassword != nil {
		t.Fatalf("SMTPPassword = %q, want nil (JSON null must decode like an absent field)", *b.SMTPPassword)
	}
	// A sibling field present in the same body must still decode
	// normally, so this isn't just an empty-body false positive.
	if b.WebhookEnabled == nil || !*b.WebhookEnabled {
		t.Fatalf("WebhookEnabled = %v, want pointer to true", b.WebhookEnabled)
	}

	patch := toSettingsPatch(&b)
	if patch.SMTPPassword != nil {
		t.Fatalf("toSettingsPatch(...).SMTPPassword = %q, want nil", *patch.SMTPPassword)
	}
}

// TestPatchNotificationsBody_OmittedSMTPPasswordDecodesToNil is the
// complementary case: a body that never mentions smtpPassword at all must
// decode to the same nil pointer explicit null does, confirming the two
// wire forms are genuinely indistinguishable by the time toSettingsPatch
// sees them.
func TestPatchNotificationsBody_OmittedSMTPPasswordDecodesToNil(t *testing.T) {
	var b patchNotificationsBody
	if err := json.Unmarshal([]byte(`{"webhookEnabled":true}`), &b); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if b.SMTPPassword != nil {
		t.Fatalf("SMTPPassword = %q, want nil (omitted field)", *b.SMTPPassword)
	}
}
