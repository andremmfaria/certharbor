package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"certharbor/models"

	appdb "certharbor/db"
)

type notificationPayload struct {
	Type      string            `json:"type"`
	Severity  string            `json:"severity"`
	Title     string            `json:"title"`
	Message   string            `json:"message"`
	Timestamp time.Time         `json:"timestamp"`
	Source    string            `json:"source"`
	Version   string            `json:"version"`
	Meta      map[string]string `json:"meta,omitempty"`
}

// NotificationSettingsRow is the handler-layer view of
// models.NotificationSettings: GET/PATCH /api/v1/admin/notifications build
// their response from this, never from the models type directly, which is
// what keeps api/ from naming certharbor/models (Section 2's depguard
// rule, the same boundary certificateSummaryBody's source enforces).
// SMTPPassword is deliberately absent: it is writeOnly on the way in and
// never belongs on the way out.
type NotificationSettingsRow struct {
	WebhookEnabled  bool
	WebhookURL      string
	NotifyExpiry    bool
	ExpiryDays      int
	NotifyFailures  bool
	NotifyAuthBurst bool
	SMTPEnabled     bool
	SMTPHost        string
	SMTPPort        int
	SMTPSecurity    string
	SMTPUsername    string
	SMTPFrom        string
}

// NotificationSettingsPatch is PATCH /api/v1/admin/notifications' merge-style
// body (5.8): a nil field means absent, which UpdateNotificationSettings
// leaves unchanged. SMTPPassword is the one field this phase's exit
// criterion is written against: a PATCH omitting it must leave the stored
// password unchanged, never zeroed.
type NotificationSettingsPatch struct {
	WebhookEnabled, NotifyExpiry, NotifyFailures, NotifyAuthBurst, SMTPEnabled *bool
	WebhookURL, SMTPHost, SMTPSecurity, SMTPUsername, SMTPPassword, SMTPFrom   *string
	ExpiryDays, SMTPPort                                                       *int
}

func toNotificationSettingsRow(s *models.NotificationSettings) *NotificationSettingsRow {
	return &NotificationSettingsRow{
		WebhookEnabled: s.WebhookEnabled, WebhookURL: s.WebhookURL,
		NotifyExpiry: s.NotifyExpiry, ExpiryDays: s.ExpiryDays,
		NotifyFailures: s.NotifyFailures, NotifyAuthBurst: s.NotifyAuthBurst,
		SMTPEnabled: s.SMTPEnabled, SMTPHost: s.SMTPHost, SMTPPort: s.SMTPPort,
		SMTPSecurity: s.SMTPSecurity, SMTPUsername: s.SMTPUsername, SMTPFrom: s.SMTPFrom,
	}
}

// applyNotificationPatch merges patch onto current and returns a new
// value (5.8: absent means unchanged). Pure and database-free on purpose:
// it is the one piece of PATCH semantics worth unit-testing directly,
// without a database, against the specific claim this phase's exit
// criterion makes — omitting SMTPPassword leaves it unchanged.
func applyNotificationPatch(current *models.NotificationSettings, patch *NotificationSettingsPatch) *models.NotificationSettings {
	merged := *current
	if patch.WebhookEnabled != nil {
		merged.WebhookEnabled = *patch.WebhookEnabled
	}
	if patch.WebhookURL != nil {
		merged.WebhookURL = *patch.WebhookURL
	}
	if patch.NotifyExpiry != nil {
		merged.NotifyExpiry = *patch.NotifyExpiry
	}
	if patch.ExpiryDays != nil {
		merged.ExpiryDays = *patch.ExpiryDays
	}
	if patch.NotifyFailures != nil {
		merged.NotifyFailures = *patch.NotifyFailures
	}
	if patch.NotifyAuthBurst != nil {
		merged.NotifyAuthBurst = *patch.NotifyAuthBurst
	}
	if patch.SMTPEnabled != nil {
		merged.SMTPEnabled = *patch.SMTPEnabled
	}
	if patch.SMTPHost != nil {
		merged.SMTPHost = *patch.SMTPHost
	}
	if patch.SMTPPort != nil {
		merged.SMTPPort = *patch.SMTPPort
	}
	if patch.SMTPSecurity != nil {
		merged.SMTPSecurity = *patch.SMTPSecurity
	}
	if patch.SMTPUsername != nil {
		merged.SMTPUsername = *patch.SMTPUsername
	}
	// Omitted (nil): merged.SMTPPassword keeps current's value, carried
	// over by the `merged := *current` copy above. This is the whole of
	// the exit criterion: no branch here means no code path can zero it.
	if patch.SMTPPassword != nil {
		merged.SMTPPassword = *patch.SMTPPassword
	}
	if patch.SMTPFrom != nil {
		merged.SMTPFrom = *patch.SMTPFrom
	}
	return &merged
}

// NotificationSettings implements GET /api/v1/admin/notifications
// (Section 6, replacing GET /admin/notifications).
func (h *Handler) NotificationSettings(ctx context.Context) (*NotificationSettingsRow, error) {
	s, err := appdb.GetNotificationSettings(ctx, h.db)
	if err != nil {
		return nil, err
	}
	return toNotificationSettingsRow(s), nil
}

// UpdateNotificationSettings implements PATCH /api/v1/admin/notifications
// (Section 6, replacing POST /admin/notifications): PUT would clear
// SMTPPassword on the first save of an unrelated field (5.8), which is
// exactly the defect a merge-style PATCH against the loaded current row
// avoids. Validation mirrors AdminNotificationsPost's: a webhook claiming
// to be enabled needs a parseable URL, SMTP claiming to be enabled needs a
// host and a From address.
func (h *Handler) UpdateNotificationSettings(ctx context.Context, patch *NotificationSettingsPatch) (*NotificationSettingsRow, error) {
	current, err := appdb.GetNotificationSettings(ctx, h.db)
	if err != nil {
		return nil, err
	}
	merged := applyNotificationPatch(current, patch)

	if merged.WebhookEnabled {
		if _, urlErr := url.ParseRequestURI(merged.WebhookURL); urlErr != nil {
			return nil, NewValidationError("webhook URL is invalid")
		}
	}
	if merged.SMTPEnabled && (merged.SMTPHost == "" || merged.SMTPFrom == "") {
		return nil, NewValidationError("SMTP requires a host and a From address")
	}

	if err := appdb.SaveNotificationSettings(ctx, h.db, merged); err != nil {
		return nil, err
	}
	return toNotificationSettingsRow(merged), nil
}

// ErrWebhookNotConfigured is SendTestNotification's sentinel for "there is
// nothing to test", mapped to 422 by the api layer rather than treated as
// a server error.
var ErrWebhookNotConfigured = errors.New("the webhook is not enabled or has no URL configured")

// SendTestNotification implements POST /api/v1/admin/notifications/test
// (Section 6), the same dispatch AdminNotificationsTest already performs,
// adapted to return an error the api layer maps to a problem document
// instead of a flash message.
func (h *Handler) SendTestNotification(ctx context.Context, remoteAddr string) error {
	settings, err := appdb.GetNotificationSettings(ctx, h.db)
	if err != nil {
		return err
	}
	if !settings.WebhookEnabled || strings.TrimSpace(settings.WebhookURL) == "" {
		return ErrWebhookNotConfigured
	}
	return h.sendNotification(ctx, "", "system.test", "info", "Certharbor test notification",
		"Test webhook dispatch from admin panel", map[string]string{"remote_addr": remoteAddr})
}

// AuditNotificationsSave is UpdateNotificationSettings' h.auditSecurity
// call site, adapted for the api layer exactly as AuditKeyDownload is
// (ca_api.go's comment): the huma session middleware never populates the
// gorilla-session "username" key h.auditSecurity reads.
func (h *Handler) AuditNotificationsSave(r *http.Request, username string) {
	if r == nil || username == "" {
		return
	}
	_ = appdb.LogAuth(h.db, username, clientIP(r), true, auditPrefix+"notifications.save (api)")
}

func (h *Handler) notifyAsync(eventKey, eventType, severity, title, message string, meta map[string]string) {
	if h.db == nil {
		return
	}
	safeGo("notify:"+eventType, func() {
		ctx, cancel := context.WithTimeout(context.Background(), 7*time.Second)
		defer cancel()
		if err := h.sendNotification(ctx, eventKey, eventType, severity, title, message, meta); err != nil {
			log.Printf("notification %s failed: %v", eventType, err)
		}
	})
}

func (h *Handler) sendNotification(ctx context.Context, eventKey, eventType, severity, title, message string, meta map[string]string) error {
	settings, err := appdb.GetNotificationSettings(ctx, h.db)
	if err != nil {
		return err
	}
	if !settings.WebhookEnabled || strings.TrimSpace(settings.WebhookURL) == "" {
		return nil
	}
	if !notificationAllowed(settings, eventType) {
		return nil
	}
	if eventKey != "" && appdb.NotificationEventExists(ctx, h.db, eventKey) {
		return nil
	}

	payload := notificationPayload{
		Type:      eventType,
		Severity:  severity,
		Title:     title,
		Message:   message,
		Timestamp: time.Now(),
		Source:    "certharbor",
		Version:   Version,
		Meta:      meta,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, settings.WebhookURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "certharbor/"+Version)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	logEntry := &models.NotificationLog{
		EventKey:  eventKey,
		EventType: eventType,
		Severity:  severity,
		Title:     title,
		Message:   message,
	}
	if err != nil {
		logEntry.Success = false
		logEntry.Error = err.Error()
		_ = appdb.AddNotificationLog(ctx, h.db, logEntry)
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		err = fmt.Errorf("webhook returned HTTP %d", resp.StatusCode)
		logEntry.Success = false
		logEntry.Error = err.Error()
		_ = appdb.AddNotificationLog(ctx, h.db, logEntry)
		return err
	}
	logEntry.Success = true
	_ = appdb.AddNotificationLog(ctx, h.db, logEntry)
	return nil
}

func notificationAllowed(settings *models.NotificationSettings, eventType string) bool {
	switch {
	case strings.HasPrefix(eventType, "certificate.expir"):
		return settings.NotifyExpiry
	case strings.HasPrefix(eventType, "certificate.") && strings.HasSuffix(eventType, "_failed"):
		return settings.NotifyFailures
	case strings.HasPrefix(eventType, "auth."):
		return settings.NotifyAuthBurst
	default:
		return true
	}
}

// StartNotificationWorker starts a background goroutine that checks for expiring
// certificates once daily and emits webhook notifications.
func (h *Handler) StartNotificationWorker() {
	safeGo("notification-worker", func() {
		h.checkExpiringCertificates(context.Background())
		t := time.NewTicker(24 * time.Hour)
		defer t.Stop()
		for range t.C {
			h.checkExpiringCertificates(context.Background())
		}
	})
}

func (h *Handler) checkExpiringCertificates(ctx context.Context) {
	settings, err := appdb.GetNotificationSettings(ctx, h.db)
	if err != nil || !settings.NotifyExpiry || settings.ExpiryDays <= 0 {
		return
	}
	certs, err := appdb.GetCerts(h.db, "active")
	if err != nil {
		return
	}
	now := time.Now()
	limit := now.Add(time.Duration(settings.ExpiryDays) * 24 * time.Hour)
	for _, c := range certs {
		if c.ExpiresAt == nil || c.ExpiresAt.Before(now) || c.ExpiresAt.After(limit) {
			continue
		}
		eventKey := fmt.Sprintf("cert-expiry:%d:%s", c.ID, c.ExpiresAt.Format("2006-01-02"))
		days := int(time.Until(*c.ExpiresAt).Hours() / 24)
		_ = h.sendNotification(ctx, eventKey, "certificate.expiring", "warn",
			"Certificate expires soon",
			fmt.Sprintf("Certificate %s (%s) expires in %d day(s).", c.Name, c.Domain, days),
			map[string]string{
				"id":      strconv.Itoa(c.ID),
				"name":    c.Name,
				"domain":  c.Domain,
				"expires": c.ExpiresAt.Format(time.RFC3339),
			})
	}
}
