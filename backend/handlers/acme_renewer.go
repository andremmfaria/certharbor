package handlers

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"certharbor/acme"

	appdb "certharbor/db"
)

// StartRenewer starts the ACME auto-renewal loop as a panic-safe background
// goroutine.  It replaces the bare acme.StartRenewer call so renewal failures
// can flow through notifyAsync and InsertHistory, matching the synchronous
// manual-renew path.
func (h *Handler) StartRenewer() {
	safeGo("le-renewer", func() {
		slog.Info("ACME auto-renewer started", "interval", "24h")
		// First check after 5 min to avoid hammering ACME on startup.
		time.Sleep(5 * time.Minute)
		for {
			h.runRenewal()
			time.Sleep(24 * time.Hour)
		}
	})
}

func (h *Handler) runRenewal() {
	// Background context: this runs outside any HTTP request lifetime.
	ctx := context.Background()

	certs, err := appdb.GetACMECertsForRenewal(ctx, h.db)
	if err != nil {
		slog.Error("ACME renewal check failed", "err", err)
		return
	}
	if len(certs) == 0 {
		slog.Debug("ACME renewal: no certificates need renewal")
		return
	}
	slog.Info("ACME renewal: certificates to renew", "count", len(certs))

	settings, err := appdb.GetACMESettings(ctx, h.db)
	if err != nil {
		slog.Error("ACME renewal: cannot load settings", "err", err)
		return
	}

	for _, cert := range certs {
		// One out-of-policy certificate must not stop every other renewal, so
		// this skips rather than returning.
		if policyErr := checkDomainPolicy(cert.Domain, h.cfg.AllowedDomainSuffixes); policyErr != nil {
			slog.Error("ACME renewal skipped: certificate is outside the name policy",
				"domain", cert.Domain, "id", cert.ID, "err", policyErr)
			// Also to the ACME log, which is where an operator looks: a silently
			// skipped certificate expires in thirty days.
			appdb.AddACMELog(ctx, h.db, cert.Domain, "error",
				fmt.Sprintf("Renewal skipped: %v", policyErr))
			continue
		}
		slog.Info("ACME renewing certificate", "domain", cert.Domain)
		appdb.AddACMELog(ctx, h.db, cert.Domain, "renew", "Automatic renewal started")

		email := cert.Email
		if email == "" {
			email = settings.Email
		}
		provider := cert.Provider
		if provider == "" {
			provider = settings.Provider
		}

		result, err := acme.IssueCert(&acme.ACMEConfig{
			Email:        email,
			Domain:       cert.Domain,
			Provider:     provider,
			CFToken:      settings.CFToken,
			CFZoneID:     settings.CFZoneID,
			R53KeyID:     settings.R53KeyID,
			R53Secret:    settings.R53SecretKey,
			R53Region:    settings.R53Region,
			DirectoryURL: h.cfg.ACMEDirectoryURL,
		})
		if err != nil {
			if dbErr := appdb.UpdateACMECertStatus(ctx, h.db, cert.ID, "error", err.Error()); dbErr != nil {
				slog.Error("ACME renewal: failed to update cert status", "domain", cert.Domain, "err", dbErr)
			}
			appdb.AddACMELog(ctx, h.db, cert.Domain, "error", fmt.Sprintf("Renewal error: %v", err))
			slog.Error("ACME renewal failed", "domain", cert.Domain, "err", err)
			// Emit a notification so the failure appears in history like the manual path.
			h.notifyAsync(
				"", "certificate.renew_failed", "error",
				"LE certificate auto-renewal failed",
				fmt.Sprintf("Auto-renewal failed for %s: %v", cert.Domain, err),
				map[string]string{"domain": cert.Domain},
			)
			continue
		}
		if dbErr := appdb.UpdateACMECertPaths(ctx, h.db, cert.ID, result.CertPath, result.KeyPath, result.IssuedAt, result.ExpiresAt); dbErr != nil {
			slog.Error("ACME renewal: failed to update cert paths", "domain", cert.Domain, "err", dbErr)
		}
		appdb.AddACMELog(ctx, h.db, cert.Domain, "renew", "Certificate renewed successfully")
		slog.Info("ACME certificate renewed successfully", "domain", cert.Domain)
	}
}
