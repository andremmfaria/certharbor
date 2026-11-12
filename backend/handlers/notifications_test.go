package handlers

import (
	"testing"

	"certharbor/models"
)

// TestApplyNotificationPatch_OmittedSMTPPasswordUnchanged is the test
// patchNotificationsBody.SMTPPassword's doc comment (api/notifications.go)
// already names: PATCH /api/v1/admin/notifications' exit criterion is that
// omitting smtpPassword leaves the stored value unchanged, never zeroed.
// applyNotificationPatch is pure and database-free, so this proves the
// merge logic itself without a database.
func TestApplyNotificationPatch_OmittedSMTPPasswordUnchanged(t *testing.T) {
	current := &models.NotificationSettings{
		SMTPPassword: "s3cret", SMTPHost: "smtp.example.com", SMTPFrom: "a@example.com",
	}
	newHost := "new.example.com"
	patch := &NotificationSettingsPatch{SMTPHost: &newHost} // SMTPPassword left nil (omitted)

	merged := applyNotificationPatch(current, patch)

	if merged.SMTPPassword != "s3cret" {
		t.Fatalf("SMTPPassword = %q, want unchanged %q", merged.SMTPPassword, "s3cret")
	}
	if merged.SMTPHost != newHost {
		t.Fatalf("SMTPHost = %q, want %q (the field that was actually patched)", merged.SMTPHost, newHost)
	}
}

// TestApplyNotificationPatch_NilSMTPPasswordNeverZeroesNonNullableField is
// this phase's previously-unverified claim, stated precisely: SMTPPassword
// on models.NotificationSettings is a plain (non-nullable) string, and the
// wire's only way to represent "leave unchanged" — a nil
// *NotificationSettingsPatch.SMTPPassword, which is exactly what a JSON
// body omitting the field or sending it as explicit null both decode to
// (see api/notifications_test.go's
// TestPatchNotificationsBody_ExplicitJSONNullSMTPPasswordDecodesToNil) —
// must never reach a branch that writes the zero value "" into it. This
// asserts that directly against a current value that already has content,
// so a regression that replaced the current value with the zero string
// would fail loudly rather than silently matching an empty starting state.
func TestApplyNotificationPatch_NilSMTPPasswordNeverZeroesNonNullableField(t *testing.T) {
	current := &models.NotificationSettings{SMTPPassword: "do-not-erase-me"}

	merged := applyNotificationPatch(current, &NotificationSettingsPatch{})

	if merged.SMTPPassword == "" {
		t.Fatal("SMTPPassword was zeroed by a patch with a nil SMTPPassword field")
	}
	if merged.SMTPPassword != "do-not-erase-me" {
		t.Fatalf("SMTPPassword = %q, want unchanged %q", merged.SMTPPassword, "do-not-erase-me")
	}
}

// TestApplyNotificationPatch_ExplicitSMTPPasswordOverwrites is the other
// side of the same contract: a non-nil SMTPPassword must actually take
// effect, or "omit to leave unchanged" would be indistinguishable from
// "the field can never be changed at all".
func TestApplyNotificationPatch_ExplicitSMTPPasswordOverwrites(t *testing.T) {
	current := &models.NotificationSettings{SMTPPassword: "old"}
	newPassword := "new-password"

	merged := applyNotificationPatch(current, &NotificationSettingsPatch{SMTPPassword: &newPassword})

	if merged.SMTPPassword != newPassword {
		t.Fatalf("SMTPPassword = %q, want %q", merged.SMTPPassword, newPassword)
	}
}
