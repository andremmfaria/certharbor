package api

import (
	"context"
	"errors"
	"net/http"

	"certharbor/handlers"
	appmw "certharbor/middleware"
)

// notificationsBody is the wire shape of GET and the result of PATCH
// /api/v1/admin/notifications. SMTPPassword is deliberately absent: it is
// writeOnly on patchNotificationsBody and must never round-trip into any
// response (5.8).
type notificationsBody struct {
	WebhookEnabled  bool   `json:"webhookEnabled"`
	WebhookURL      string `json:"webhookUrl,omitempty"`
	NotifyExpiry    bool   `json:"notifyExpiry"`
	ExpiryDays      int    `json:"expiryDays"`
	NotifyFailures  bool   `json:"notifyFailures"`
	NotifyAuthBurst bool   `json:"notifyAuthBurst"`
	SMTPEnabled     bool   `json:"smtpEnabled"`
	SMTPHost        string `json:"smtpHost,omitempty"`
	SMTPPort        int    `json:"smtpPort"`
	SMTPSecurity    string `json:"smtpSecurity,omitempty" enum:"none,starttls,tls"`
	SMTPUsername    string `json:"smtpUsername,omitempty"`
	SMTPFrom        string `json:"smtpFrom,omitempty"`
}

func newNotificationsBody(r *handlers.NotificationSettingsRow) notificationsBody {
	return notificationsBody{
		WebhookEnabled: r.WebhookEnabled, WebhookURL: r.WebhookURL,
		NotifyExpiry: r.NotifyExpiry, ExpiryDays: r.ExpiryDays,
		NotifyFailures: r.NotifyFailures, NotifyAuthBurst: r.NotifyAuthBurst,
		SMTPEnabled: r.SMTPEnabled, SMTPHost: r.SMTPHost, SMTPPort: r.SMTPPort,
		SMTPSecurity: r.SMTPSecurity, SMTPUsername: r.SMTPUsername, SMTPFrom: r.SMTPFrom,
	}
}

// ─── GET /api/v1/admin/notifications ────────────────────────────────────────

type getNotificationsOutput struct {
	Body notificationsBody
}

// getNotifications implements GET /api/v1/admin/notifications, replacing
// GET /admin/notifications (Section 6): secrets writeOnly, absent from the
// response.
func getNotifications(h *handlers.Handler) func(context.Context, *struct{}) (*getNotificationsOutput, error) {
	return func(ctx context.Context, _ *struct{}) (*getNotificationsOutput, error) {
		row, err := h.NotificationSettings(ctx)
		if err != nil {
			return nil, err
		}
		return &getNotificationsOutput{Body: newNotificationsBody(row)}, nil
	}
}

// ─── PATCH /api/v1/admin/notifications ──────────────────────────────────────

// patchNotificationsBody's fields are all pointers (5.8: absent means
// unchanged). SMTPPassword is writeOnly and allowlisted in
// openapi/secret-allowlist.txt: omitting it on a PATCH leaves the stored
// value unchanged, which is this phase's exit criterion, proven by
// handlers.TestApplyNotificationPatch_OmittedSMTPPasswordUnchanged.
type patchNotificationsBody struct {
	WebhookEnabled  *bool   `json:"webhookEnabled,omitempty"`
	WebhookURL      *string `json:"webhookUrl,omitempty" maxLength:"2048"`
	NotifyExpiry    *bool   `json:"notifyExpiry,omitempty"`
	ExpiryDays      *int    `json:"expiryDays,omitempty" minimum:"1" maximum:"365"`
	NotifyFailures  *bool   `json:"notifyFailures,omitempty"`
	NotifyAuthBurst *bool   `json:"notifyAuthBurst,omitempty"`
	SMTPEnabled     *bool   `json:"smtpEnabled,omitempty"`
	SMTPHost        *string `json:"smtpHost,omitempty" maxLength:"255"`
	SMTPPort        *int    `json:"smtpPort,omitempty" minimum:"1" maximum:"65535"`
	SMTPSecurity    *string `json:"smtpSecurity,omitempty" enum:"none,starttls,tls"`
	SMTPUsername    *string `json:"smtpUsername,omitempty" maxLength:"255"`
	SMTPPassword    *string `json:"smtpPassword,omitempty" writeOnly:"true" doc:"SMTP password. Omit to leave the stored value unchanged; never present in any response."`
	SMTPFrom        *string `json:"smtpFrom,omitempty" maxLength:"255"`
}

type patchNotificationsInput struct {
	Body patchNotificationsBody
}

func toSettingsPatch(b *patchNotificationsBody) *handlers.NotificationSettingsPatch {
	return &handlers.NotificationSettingsPatch{
		WebhookEnabled: b.WebhookEnabled, WebhookURL: b.WebhookURL,
		NotifyExpiry: b.NotifyExpiry, ExpiryDays: b.ExpiryDays,
		NotifyFailures: b.NotifyFailures, NotifyAuthBurst: b.NotifyAuthBurst,
		SMTPEnabled: b.SMTPEnabled, SMTPHost: b.SMTPHost, SMTPPort: b.SMTPPort,
		SMTPSecurity: b.SMTPSecurity, SMTPUsername: b.SMTPUsername,
		SMTPPassword: b.SMTPPassword, SMTPFrom: b.SMTPFrom,
	}
}

// mapNotificationsError mirrors mapCertError (certificates.go): a
// caller-input validation failure is a 422, not huma's default 500.
func mapNotificationsError(err error) error {
	var verr *handlers.ValidationError
	if errors.As(err, &verr) {
		return newProblem(http.StatusUnprocessableEntity, "Unprocessable Entity", problemTypeBase+"validation", err.Error(), "")
	}
	return err
}

// patchNotifications implements PATCH /api/v1/admin/notifications,
// replacing POST /admin/notifications (Section 6, 5.8).
func patchNotifications(h *handlers.Handler) func(context.Context, *patchNotificationsInput) (*getNotificationsOutput, error) {
	return func(ctx context.Context, in *patchNotificationsInput) (*getNotificationsOutput, error) {
		row, err := h.UpdateNotificationSettings(ctx, toSettingsPatch(&in.Body))
		if err != nil {
			return nil, mapNotificationsError(err)
		}
		r, _ := httpFrom(ctx)
		username, _ := userIdentity(ctx)
		h.AuditNotificationsSave(r, username)
		return &getNotificationsOutput{Body: newNotificationsBody(row)}, nil
	}
}

// ─── POST /api/v1/admin/notifications/test ──────────────────────────────────

type testNotificationsOutput struct {
	Status int
}

var errWebhookNotConfigured = newProblem(http.StatusUnprocessableEntity, "Unprocessable Entity", problemTypeBase+"validation",
	"please enable the webhook and provide a URL first", "")

// testNotifications implements POST /api/v1/admin/notifications/test
// (Section 6), unchanged in shape from today's route.
func testNotifications(h *handlers.Handler) func(context.Context, *struct{}) (*testNotificationsOutput, error) {
	return func(ctx context.Context, _ *struct{}) (*testNotificationsOutput, error) {
		r, _ := httpFrom(ctx)
		if err := h.SendTestNotification(ctx, appmw.ClientIP(r)); err != nil {
			if errors.Is(err, handlers.ErrWebhookNotConfigured) {
				return nil, errWebhookNotConfigured
			}
			return nil, err
		}
		return &testNotificationsOutput{Status: http.StatusNoContent}, nil
	}
}
