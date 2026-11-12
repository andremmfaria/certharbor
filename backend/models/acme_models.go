package models

import "time"

// ACMECertificate represents an ACME (Let's Encrypt) certificate tracked by the system.
type ACMECertificate struct {
	ID        int
	Domain    string
	Email     string
	Provider  string // http01, cloudflare, route53, manual
	CertPath  string
	KeyPath   string
	IssuedAt  *time.Time
	ExpiresAt *time.Time
	AutoRenew bool
	Status    string // active, expired, pending, error
	LastError string
	CreatedAt *time.Time
	UpdatedAt *time.Time
}

// ACMESettings holds the ACME provider configuration (Cloudflare, Route53, etc.).
type ACMESettings struct {
	ID           int
	Email        string
	Provider     string
	CFToken      string // Cloudflare API token
	CFZoneID     string // Cloudflare Zone ID
	R53KeyID     string // AWS Route53 Access Key ID
	R53SecretKey string // AWS Route53 Secret Key
	R53Region    string // AWS Region
	UpdatedAt    *time.Time
}

// ACMELog is a single ACME lifecycle event log entry.
type ACMELog struct {
	ID        int
	Domain    string
	Action    string // issue, renew, revoke, error
	Message   string
	CreatedAt time.Time
}
