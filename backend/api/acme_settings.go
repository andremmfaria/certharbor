package api

import (
	"context"
	"errors"
	"net/http"

	"certharbor/handlers"
)

// acmeSettingsBody is the wire shape of GET and the result of PATCH
// /api/v1/acme/settings. CFToken and R53SecretKey are deliberately
// absent: they are writeOnly on patchAcmeSettingsBody and must never
// round-trip into any response (5.8), exactly as notificationsBody omits
// SMTPPassword.
type acmeSettingsBody struct {
	Email     string `json:"email,omitempty"`
	Provider  string `json:"provider" enum:"http01,cloudflare,route53,manual"`
	CFZoneID  string `json:"cfZoneId,omitempty"`
	R53KeyID  string `json:"r53KeyId,omitempty"`
	R53Region string `json:"r53Region,omitempty"`
}

func newAcmeSettingsBody(r *handlers.ACMESettingsRow) acmeSettingsBody {
	return acmeSettingsBody{
		Email: r.Email, Provider: r.Provider,
		CFZoneID: r.CFZoneID, R53KeyID: r.R53KeyID, R53Region: r.R53Region,
	}
}

// ─── GET /api/v1/acme/settings ──────────────────────────────────────────────

type getAcmeSettingsOutput struct {
	Body acmeSettingsBody
}

// getAcmeSettings implements GET /api/v1/acme/settings, replacing GET
// /le/settings (Section 6, Phase 7): the provider secrets are writeOnly,
// absent from the response.
func getAcmeSettings(h *handlers.Handler) func(context.Context, *struct{}) (*getAcmeSettingsOutput, error) {
	return func(ctx context.Context, _ *struct{}) (*getAcmeSettingsOutput, error) {
		row, err := h.ACMESettingsAPI(ctx)
		if err != nil {
			return nil, err
		}
		return &getAcmeSettingsOutput{Body: newAcmeSettingsBody(row)}, nil
	}
}

// ─── PATCH /api/v1/acme/settings ────────────────────────────────────────────

// patchAcmeSettingsBody's fields are all pointers (5.8: absent means
// unchanged). CFToken and R53SecretKey are writeOnly: omitting either on a
// PATCH leaves the stored ACME provider account material unchanged, which
// is this phase's exit criterion, proven by
// handlers.TestApplyACMESettingsPatch_OmittedSecretsUnchanged.
type patchAcmeSettingsBody struct {
	Email        *string `json:"email,omitempty" maxLength:"255"`
	Provider     *string `json:"provider,omitempty" enum:"http01,cloudflare,route53,manual"`
	CFZoneID     *string `json:"cfZoneId,omitempty" maxLength:"255"`
	CFToken      *string `json:"cfToken,omitempty" writeOnly:"true" doc:"Cloudflare API token. Omit to leave the stored value unchanged; never present in any response."`
	R53KeyID     *string `json:"r53KeyId,omitempty" maxLength:"255"`
	R53SecretKey *string `json:"r53SecretKey,omitempty" writeOnly:"true" doc:"AWS Route53 secret access key. Omit to leave the stored value unchanged; never present in any response."`
	R53Region    *string `json:"r53Region,omitempty" maxLength:"50"`
}

type patchAcmeSettingsInput struct {
	Body patchAcmeSettingsBody
}

func toAcmeSettingsPatch(b *patchAcmeSettingsBody) *handlers.ACMESettingsPatch {
	return &handlers.ACMESettingsPatch{
		Email: b.Email, Provider: b.Provider, CFZoneID: b.CFZoneID,
		R53KeyID: b.R53KeyID, R53Region: b.R53Region,
		CFToken: b.CFToken, R53SecretKey: b.R53SecretKey,
	}
}

// mapAcmeSettingsError mirrors mapNotificationsError: a caller-input
// validation failure is a 422, not huma's default 500.
func mapAcmeSettingsError(err error) error {
	var verr *handlers.ValidationError
	if errors.As(err, &verr) {
		return newProblem(http.StatusUnprocessableEntity, "Unprocessable Entity", problemTypeBase+"validation", err.Error(), "")
	}
	return err
}

// patchAcmeSettings implements PATCH /api/v1/acme/settings, replacing
// POST /le/settings (Section 6, 5.8).
func patchAcmeSettings(h *handlers.Handler) func(context.Context, *patchAcmeSettingsInput) (*getAcmeSettingsOutput, error) {
	return func(ctx context.Context, in *patchAcmeSettingsInput) (*getAcmeSettingsOutput, error) {
		row, err := h.UpdateACMESettingsAPI(ctx, toAcmeSettingsPatch(&in.Body))
		if err != nil {
			return nil, mapAcmeSettingsError(err)
		}
		username, _ := userIdentity(ctx)
		h.AuditACMESettingsSave(username)
		return &getAcmeSettingsOutput{Body: newAcmeSettingsBody(row)}, nil
	}
}
