// Package config loads application configuration from environment variables.
// Sensitive values (SECRET_KEY, DATABASE_URL) are read via *_FILE variants
// when set so that they never appear in the process environment at runtime.
package config

import (
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds all runtime configuration loaded from environment variables.
type Config struct {
	DatabaseURL   string
	CAURL         string
	RootCert      string
	Provisioner   string
	PasswordFile  string
	StepCAImage   string
	SecretKey     string
	SessionSecure bool
	TrustProxy    bool

	// Raw value. When set, main.go parses it and refuses to start on a bad
	// entry (V4). When empty, WebUpstreamHost is resolved instead (D5 rule 3).
	TrustedProxyCIDRs string

	// The name nginx answers to on the compose network. Resolved, never
	// pinned, because a recreated container comes back on a new address.
	WebUpstreamHost string

	// Empty means issuance is unrestricted, the behaviour that predates the key (V6).
	AllowedDomainSuffixes []string

	// PublicBaseURL is the externally reachable origin ("https://ca.example.com").
	// Links sent by email are built from this and never from the inbound
	// request, because Host and X-Forwarded-Proto are attacker controlled.
	PublicBaseURL string
	Port          int
	CertsDir      string
	UploadDir     string
	// BackupDir holds the persisted backup bundles POST /api/v1/admin/backups
	// creates (5.7's create-then-navigate convention): the create and the
	// download are two separate HTTP requests, so the bundle must outlive
	// the request that built it, unlike the legacy /admin/backup/download
	// route's stream-from-tmp-then-delete.
	BackupDir string
	SSLCert   string
	SSLKey    string
	UseHTTPS  bool // explicit override; defaults to auto-detect via os.Stat

	// OIDC / JumpCloud SSO
	OIDCEnabled      bool
	OIDCIssuerURL    string
	OIDCClientID     string
	OIDCClientSecret string
	OIDCRedirectURL  string
	OIDCGroupClaim   string
	OIDCGroupAdmin   string
	OIDCGroupManager string
	OIDCGroupViewer  string
	OIDCDefaultRole  string
	OIDCSyncRole     bool

	// Local password login (break-glass when OIDC is primary)
	LocalLoginEnabled bool

	// In-process TLS bootstrap (Phase 5), replaces entrypoint.sh's former
	// root-cert-fetch/leaf-issuance/renew-daemon shell logic.
	UITLSMode     string
	CAFingerprint string
	CARootCertPEM string
	UIHostname    string
	HostIP        string

	// Validity requested for the UI's own leaf when UITLSMode == "stepca".
	// The renewal loop sleeps ~2/3 of it.
	UICertDuration time.Duration

	// nginx's own leaf (D5 rule 5): "stepca" issues it here and writes it to
	// the certharbor-console-ssl volume, "provided" leaves the operator to mount
	// it. WebHostname has no fallback: a container id as the only SAN would
	// survive on the volume and be served to browsers that reject it.
	WebTLSMode  string
	WebHostname string
	WebSSLCert  string
	WebSSLKey   string
	WebRootCA   string

	// Env-only: a manager-editable DB setting must not be able to repoint
	// where LE issuance actually dials.
	LEACMEDirectoryURL string
}

// LEProductionDirectoryURL is LE_ACME_DIRECTORY_URL's default, so behaviour is
// unchanged until an operator points issuance somewhere else.
const LEProductionDirectoryURL = "https://acme-v02.api.letsencrypt.org/directory"

const defaultUICertDuration = 8760 * time.Hour

// Load reads configuration from environment variables.
// Sensitive values support a *_FILE variant: if SECRET_KEY_FILE is set its
// contents are used as SECRET_KEY (entrypoint.sh performs the same expansion
// for DATABASE_URL).
func Load() *Config {
	port, _ := strconv.Atoi(getEnv("PORT", "8443"))
	return &Config{
		DatabaseURL:   getEnv("DATABASE_URL", "postgres://certharbor:certharbor@postgres:5432/certharbor?sslmode=disable"),
		CAURL:         getEnv("CA_URL", "https://step-ca:9443"),
		RootCert:      getEnv("ROOT_CERT", "/home/step/certs/root_ca.crt"),
		Provisioner:   getEnv("PROVISIONER", "admin"),
		PasswordFile:  getEnv("PASSWORD_FILE", "/opt/certharbor/data/provisioner_password"),
		StepCAImage:   getEnv("STEP_CA_IMAGE", "smallstep/step-ca:0.30.2"),
		SecretKey:     getEnvOrFile("SECRET_KEY", "SECRET_KEY_FILE", "change-me-in-production-32chars!"),
		SessionSecure: getEnvBool("SESSION_SECURE", true),
		TrustProxy:    getEnvBool("TRUST_PROXY", false),

		TrustedProxyCIDRs:     getEnv("TRUSTED_PROXY_CIDRS", ""),
		WebUpstreamHost:       strings.TrimSpace(getEnv("WEB_UPSTREAM_HOST", "certharbor-console")),
		AllowedDomainSuffixes: getEnvList("ALLOWED_DOMAIN_SUFFIXES"),

		PublicBaseURL: strings.TrimRight(strings.TrimSpace(getEnv("PUBLIC_BASE_URL", "")), "/"),
		Port:          port,
		CertsDir:      "/opt/certharbor/certs",
		UploadDir:     "/opt/certharbor/uploads",
		BackupDir:     getEnv("BACKUP_DIR", "/opt/certharbor/backups"),
		SSLCert:       "/opt/certharbor/ssl/server.crt",
		SSLKey:        "/opt/certharbor/ssl/server.key",

		OIDCEnabled:      getEnvBool("OIDC_ENABLED", false),
		OIDCIssuerURL:    getEnv("OIDC_ISSUER_URL", ""),
		OIDCClientID:     getEnv("OIDC_CLIENT_ID", ""),
		OIDCClientSecret: getEnv("OIDC_CLIENT_SECRET", ""),
		OIDCRedirectURL:  getEnv("OIDC_REDIRECT_URL", ""),
		OIDCGroupClaim:   getEnv("OIDC_GROUP_CLAIM", "groups"),
		OIDCGroupAdmin:   getEnv("OIDC_GROUP_ADMIN", ""),
		OIDCGroupManager: getEnv("OIDC_GROUP_MANAGER", ""),
		OIDCGroupViewer:  getEnv("OIDC_GROUP_VIEWER", ""),
		OIDCDefaultRole:  getEnv("OIDC_DEFAULT_ROLE", ""),
		OIDCSyncRole:     getEnvBool("OIDC_SYNC_ROLE", true),

		LocalLoginEnabled: getEnvBool("LOCAL_LOGIN_ENABLED", true),
		// UseHTTPS: when false (default), the server probes os.Stat(SSLCert) to decide.
		// Set USE_HTTPS=true to force TLS; USE_HTTPS=false to force plain HTTP.
		// The empty env case preserves the existing auto-detect behaviour.
		UseHTTPS: getEnvBool("USE_HTTPS", false),

		UITLSMode:     getEnv("UI_TLS_MODE", "self-signed"),
		CAFingerprint: getEnv("CA_FINGERPRINT", ""),
		CARootCertPEM: getEnv("CA_ROOT_CERT_PEM", ""),
		UIHostname:    getEnv("UI_HOSTNAME", ""),
		HostIP:        getEnv("HOST_IP", "127.0.0.1"),

		UICertDuration: getEnvDuration("UI_CERT_DURATION", defaultUICertDuration),

		WebTLSMode:  getEnv("WEB_TLS_MODE", "stepca"),
		WebHostname: strings.TrimSpace(getEnv("WEB_HOSTNAME", "")),
		WebSSLCert:  "/opt/certharbor/web-ssl/web.crt",
		WebSSLKey:   "/opt/certharbor/web-ssl/web.key",
		WebRootCA:   "/opt/certharbor/web-ssl/root_ca.crt",

		LEACMEDirectoryURL: getEnv("LE_ACME_DIRECTORY_URL", LEProductionDirectoryURL),
	}
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// getEnvOrFile reads key from the environment; if absent it falls back to
// reading the path stored in fileKey (the *_FILE pattern).  def is returned
// when neither is set.
func getEnvOrFile(key, fileKey, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	if path := os.Getenv(fileKey); path != "" {
		//nolint:gosec // G304: path comes from an operator-controlled env var (*_FILE), same trust level as PasswordFile
		if data, err := os.ReadFile(path); err == nil {
			return strings.TrimRight(string(data), "\r\n")
		}
	}
	return def
}

// getEnvList splits a comma-separated variable into trimmed, lower-cased,
// non-empty entries. A leading dot on an entry is dropped so that
// ".example.com" and "example.com" mean the same thing.
func getEnvList(key string) []string {
	raw := os.Getenv(key)
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var out []string
	for _, part := range strings.Split(raw, ",") {
		entry := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(part)), ".")
		if entry != "" {
			out = append(out, entry)
		}
	}
	return out
}

// getEnvDuration parses a Go duration string. An unparseable or non-positive
// value falls back to def with a warning, the posture getEnvBool already uses.
func getEnvDuration(key string, def time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		slog.Warn("invalid duration, using default", "key", key, "value", v, "default", def)
		return def
	}
	return d
}

func getEnvBool(key string, def bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}
	return b
}
