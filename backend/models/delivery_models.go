package models

import (
	"encoding/json"
	"time"
)

// DeliveryTarget is one configured delivery target (credential set + params).
// credentials_ref is an opaque pointer into certharbor-delivery's own
// credential storage; this table never holds credential material itself.
type DeliveryTarget struct {
	ID             string
	Name           string
	Type           string
	Params         json.RawMessage
	CredentialsRef string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// DeliveryBinding is one (certificate, target) pairing: what gets pushed where.
type DeliveryBinding struct {
	ID               string
	CertificateName  string
	TargetID         string
	TargetParams     json.RawMessage
	Enabled          bool
	ConsecutiveFails int
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// DeliveryRun is one reconciliation attempt for a binding.
type DeliveryRun struct {
	ID                  string
	BindingID           string
	Fingerprint         string
	StartedAt           time.Time
	FinishedAt          time.Time
	Result              string
	ConsecutiveFailures int
}

// RunSucceeded reports whether a run's Result field recorded success.
func (r *DeliveryRun) RunSucceeded() bool {
	return r.Result == "success"
}
