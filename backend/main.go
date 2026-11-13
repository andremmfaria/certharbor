// Package main is the entry point for the Certharbor web application.
package main

import (
	"context"
	"crypto/tls"
	"encoding/gob"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	humaapi "certharbor/api"
	"certharbor/config"
	"certharbor/handlers"
	"certharbor/models"
	"certharbor/security"
	"certharbor/stepca"

	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/gorilla/sessions"

	appdb "certharbor/db"

	mw "certharbor/middleware"
)

// trustedProxyRefresh bounds how long a recreated nginx container is
// attributed its own address rather than the client's.
const trustedProxyRefresh = 5 * time.Second

// serverWriteTimeout must comfortably exceed handlers.PGDumpTimeout (2
// minutes): a backup download holds the response open for the entire
// pg_dump plus archive-write, and Go's own WriteTimeout cuts the
// connection off regardless of what nginx's proxy_read_timeout allows
// (plans/frontend-backend-split.md, Phase 6's "Owed from Phase 3b" note).
// 180s matches nginx's proxy_read_timeout (frontend/nginx.conf.template),
// which TestNginxReadTimeoutExceedsBackendOperations already asserts
// exceeds PGDumpTimeout; TestGoWriteTimeoutExceedsBackendOperations
// (nginx_timeout_test.go) asserts this constant does too, so the two
// ceilings cannot silently drift back out of agreement.
const serverWriteTimeout = 180 * time.Second

func main() {
	handlers.StartedAt = time.Now()
	// An unregistered type makes sessions.Save fail and write no cookie at
	// all, so every value ever put in a session must appear here (V11).
	gob.Register(int(0))
	gob.Register(int64(0))
	gob.Register("")
	gob.Register(models.FlashMsg{})
	cfg := config.Load()

	// ─── Startup security checks ─────────────────────────────────────────────
	const weakKey = "change-me-in-production-32chars!"
	if cfg.SecretKey == weakKey || len(cfg.SecretKey) < 32 {
		log.Fatal("FATAL: SECRET_KEY is the default or shorter than 32 chars, set a strong SECRET_KEY before starting")
	}
	if !cfg.SessionSecure {
		slog.Warn("SESSION_SECURE=false: session cookies will not carry the Secure flag; do not use this in production")
	}
	// Believing a forwarding header from an untrusted peer hands the login rate
	// limiter and the auth log to the client (V4). An explicit list is used
	// verbatim; otherwise the proxy's own name is resolved, and re-resolved,
	// rather than pinned (D5 rule 3). Resolution cannot be a startup fatal:
	// nginx is only started once this process is healthy, so its name never
	// resolves this early.
	var trustedProxies mw.ProxySet
	if cfg.TrustProxy {
		switch {
		case strings.TrimSpace(cfg.TrustedProxyCIDRs) != "":
			parsed, err := mw.ParseTrustedProxies(cfg.TrustedProxyCIDRs)
			if err != nil {
				log.Fatalf("FATAL: TRUST_PROXY=true requires a usable TRUSTED_PROXY_CIDRS: %v", err)
			}
			trustedProxies = parsed
		case cfg.WebUpstreamHost == "":
			log.Fatal("FATAL: TRUST_PROXY=true requires a usable TRUSTED_PROXY_CIDRS or a WEB_UPSTREAM_HOST to resolve")
		default:
			resolved := mw.NewResolvedProxies(cfg.WebUpstreamHost, net.DefaultResolver.LookupIPAddr)
			if err := resolved.Refresh(context.Background()); err != nil {
				slog.Warn("trusted proxy name does not resolve yet: forwarding headers are ignored until it does",
					"host", cfg.WebUpstreamHost, "err", err)
			}
			go resolved.Run(context.Background(), trustedProxyRefresh)
			trustedProxies = resolved
		}
	}
	switch cfg.WebTLSMode {
	case "stepca":
		if cfg.WebHostname == "" {
			log.Fatal("FATAL: WEB_HOSTNAME is required when WEB_TLS_MODE=stepca: it is the only name on nginx's certificate and there is no fallback")
		}
	case "provided":
	default:
		log.Fatalf("FATAL: WEB_TLS_MODE=%q is not one of stepca, provided", cfg.WebTLSMode)
	}
	// mapGroupsToRole only ever returns a known role, but it falls through to
	// this operator-supplied value, which would otherwise reach users.role
	// unchecked (V9).
	if cfg.OIDCEnabled && cfg.OIDCDefaultRole != "" && !appdb.ValidRole(cfg.OIDCDefaultRole) {
		log.Fatalf("FATAL: OIDC_DEFAULT_ROLE=%q is not one of viewer, manager, admin, leave it empty to deny access instead", cfg.OIDCDefaultRole)
	}
	if len(cfg.AllowedDomainSuffixes) == 0 {
		slog.Warn("ALLOWED_DOMAIN_SUFFIXES is unset: certificate issuance is unrestricted and any manager can have the CA sign any name")
	}
	if cfg.UICertDuration < 10*time.Minute {
		slog.Warn("UI_CERT_DURATION is under 10 minutes: this is a testing cadence, not an operational one", "duration", cfg.UICertDuration)
	}
	if cfg.ACMEDirectoryURL != config.ACMEProductionDirectoryURL {
		slog.Warn("LE_ACME_DIRECTORY_URL is not the Let's Encrypt production directory: ACME issuance will dial this instead", "url", cfg.ACMEDirectoryURL)
	}

	// ─── Database ────────────────────────────────────────────────────────────
	conn, err := appdb.Connect(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("Cannot connect to database: %v", err)
	}
	// Run schema migrations before registering the close defer; log.Fatalf
	// calls os.Exit which skips defers, so we keep the conn available and
	// only register the defer after the startup sequence completes.
	if err := appdb.InitSchema(conn); err != nil {
		_ = conn.Close()
		log.Fatalf("Cannot init DB schema: %v", err)
	}
	if err := appdb.InitACMESchema(conn); err != nil {
		_ = conn.Close()
		log.Fatalf("Cannot init ACME schema: %v", err)
	}
	if err := appdb.InitNotificationSchema(conn); err != nil {
		_ = conn.Close()
		log.Fatalf("Cannot init notification schema: %v", err)
	}
	if err := appdb.InitPasswordResetSchema(conn); err != nil {
		_ = conn.Close()
		log.Fatalf("Cannot init password reset schema: %v", err)
	}
	// ─── Sessions ────────────────────────────────────────────────────────────
	hashKey, blockKey, err := security.DeriveSessionKeys(cfg.SecretKey)
	if err != nil {
		_ = conn.Close()
		log.Fatalf("Cannot derive session keys: %v", err)
	}
	defer func() { _ = conn.Close() }()

	store := sessions.NewCookieStore(hashKey, blockKey)
	store.Options = &sessions.Options{
		Path:     "/",
		MaxAge:   28800,
		HttpOnly: true,
		// Strict, not Lax (D6). The one flow this would have broken is the
		// OIDC callback, whose round-trip state now lives in its own Lax
		// cookie (handlers.oidcSession), so nothing reaches this cookie by a
		// cross-site navigation any more.
		SameSite: http.SameSiteStrictMode,
		Secure:   cfg.SessionSecure,
	}

	// ─── Handlers ────────────────────────────────────────────────────────────
	h := handlers.NewWithFS(conn, cfg, store, nil)

	// ─── Let's Encrypt auto-renewer ──────────────────────────────────────────
	h.StartRenewer()
	h.StartNotificationWorker()

	// ─── Router ──────────────────────────────────────────────────────────────
	r := chi.NewRouter()
	r.Use(chiMiddleware.Recoverer)
	// Leaving this off means the rate limiter and auth log always see the real
	// socket peer.
	if cfg.TrustProxy {
		r.Use(mw.RealIP(trustedProxies))
	}
	r.Use(mw.RequestLog())
	r.Use(mw.SecurityHeaders())

	// Public routes
	r.Get("/health", h.Liveness)
	r.Get("/ready", h.Readiness)
	// OIDC's browser-redirect login/callback flow is not yet migrated to the
	// SPA/JSON API (no huma replacement exists), so it stays mounted here
	// rather than being deleted with the other template routes (Phase 9).
	if cfg.OIDCEnabled {
		r.Get("/auth/oidc/login", h.OIDCLogin)
		r.Get("/auth/oidc/callback", h.OIDCCallback)
	}

	// ─── JSON API (Phase 0 of plans/frontend-backend-split.md) ──────────────
	// Mounted on the same router as every route above: huma operation paths
	// carry the /api/v1 prefix themselves (5.1).
	humaapi.Mount(r, h, h.UserLoader())

	// ─── Start server ─────────────────────────────────────────────────────────
	for _, dir := range []string{cfg.CertsDir, cfg.UploadDir, cfg.BackupDir, "/opt/certharbor/ssl", "/opt/certharbor/web-ssl", "/opt/certharbor/data"} {
		_ = os.MkdirAll(dir, 0o750) //nolint:gosec // G301: app data dirs; restrictive perms appropriate for non-root service user
	}

	// temp_users_expire_ticker, panic-safe background goroutine
	handlers.SafeGoExported("temp-users-expiry", func() {
		t := time.NewTicker(60 * time.Second)
		defer t.Stop()
		for range t.C {
			if n, err := appdb.ExpireOverdueTempUsers(conn); err == nil && n > 0 {
				slog.Info("temp users expired", "count", n)
			}
		}
	})

	// ─── In-process TLS bootstrap ────────────────────────────────────────────
	// Replaces entrypoint.sh's former root-cert-fetch/leaf-issuance shell
	// blocks. ensureRootCert, ensureUICert and ensureWebCert can each retry up
	// to caBootstrapRetries×caBootstrapInterval (30×1s default). The outer
	// ceiling must exceed all three back-to-back plus HTTP overhead, so it's
	// derived from those same constants rather than a second hardcoded number
	// that could drift out of sync. With 2× the web leaf would start on an
	// already-cancelled context and fail on its first attempt.
	bootstrapCtx, bootstrapCancel := context.WithTimeout(
		context.Background(),
		3*time.Duration(caBootstrapRetries)*caBootstrapInterval+30*time.Second,
	)
	if err := writeInlineRootCert(cfg); err != nil {
		slog.Error("writing inline root cert failed", "err", err)
	}
	if err := ensureRootCert(bootstrapCtx, cfg); err != nil {
		slog.Error("ensuring root cert failed", "err", err)
	}
	// Constructed only after writeInlineRootCert/ensureRootCert have had a
	// chance to establish cfg.RootCert: ca.WithRootFile reads that file
	// EAGERLY inside ca.NewClient (R2), so building the client any earlier
	// would eagerly fail against a root cert that legitimately doesn't exist
	// yet on a fresh CA_FINGERPRINT-only boot, permanently leaving caClient
	// nil for the rest of this function (it's a one-shot local variable, not
	// retried like Handler.caClient()'s per-request lazy cache) and silently
	// skipping both stepca leaf issuance below and the renewer at the bottom
	// of this function, even once the root cert becomes available moments
	// later.
	var caClient stepca.CA
	if c, caErr := stepca.New(cfg); caErr != nil {
		// Tolerate construction failure (R2): proceed to the self-signed
		// fallback inside ensureUICert rather than log.Fatalf, a step-ca
		// that hasn't produced its root cert yet at UI boot must not crash
		// the whole process.
		slog.Warn("CA client construction failed during TLS bootstrap", "err", caErr)
	} else {
		caClient = c
	}
	if err := ensureUICert(bootstrapCtx, cfg, caClient); err != nil {
		slog.Error("ensuring UI cert failed", "err", err)
	}
	if err := ensureWebCert(bootstrapCtx, cfg, caClient); err != nil {
		slog.Error("web leaf issuance failed: nginx waits for it and the renewer keeps retrying", "certPath", cfg.WebSSLCert, "err", err)
	}
	if err := syncWebRootCA(cfg); err != nil {
		slog.Error("copying the root CA for nginx failed", "err", err)
	}
	bootstrapCancel()

	addr := fmt.Sprintf("0.0.0.0:%d", cfg.Port)

	// useHTTPS: explicit config flag takes precedence; falls back to probing
	// whether the SSL cert file exists (preserves original auto-detect behaviour).
	useHTTPS := cfg.UseHTTPS || func() bool {
		_, err := os.Stat(cfg.SSLCert)
		return err == nil
	}()

	srv := &http.Server{
		Addr:              addr,
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      serverWriteTimeout,
		IdleTimeout:       120 * time.Second,
	}

	// Graceful-shutdown: block on SIGINT/SIGTERM then drain in-flight requests.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	srvErr := make(chan error, 1)
	if useHTTPS {
		// Hot-reloading TLS: GetCertificate re-stats both files on every handshake
		// and reloads when mtime changes.  This lets startUICertRenewer (below)
		// replace the cert on disk and have it picked up with zero restart.
		reloader := newCertReloader(cfg.SSLCert, cfg.SSLKey)
		srv.TLSConfig = &tls.Config{
			MinVersion:     tls.VersionTLS12,
			GetCertificate: reloader.GetCertificate,
		}
		slog.Info("starting Certharbor (HTTPS)", "port", cfg.Port)
		// Empty cert/key paths are valid when GetCertificate is set.
		go func() { srvErr <- srv.ListenAndServeTLS("", "") }()
	} else {
		slog.Warn("starting Certharbor (HTTP), not suitable for production", "port", cfg.Port)
		go func() { srvErr <- srv.ListenAndServe() }()
	}

	startTLSRenewer(cfg, caClient)

	select {
	case err := <-srvErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			// Use slog+os.Exit so defers (stop/conn.Close) can run before exit.
			slog.Error("server error", "err", err)
			stop()
			_ = conn.Close()
			os.Exit(1) //nolint:gocritic // exitAfterDefer: stop() called explicitly above
		}
	case <-ctx.Done():
		stop()
		slog.Info("shutdown signal received; draining in-flight requests")
		shutCtx, shutCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer shutCancel()
		if err := srv.Shutdown(shutCtx); err != nil {
			slog.Error("graceful shutdown error", "err", err)
		}
		_ = conn.Close()
		slog.Info("server stopped cleanly")
	}
}
