package handlers

import (
	"context"
)

// ListProvisioners is the data half of GET /api/v1/provisioners, replacing
// GET /provisioners (Section 6). It reuses CertProvisionerOption
// (cert_ops.go) rather than certharbor/stepca.ProvisionerInfo directly: both
// are in-repo types with the same two fields, but routing through the type
// backend/api already converts from elsewhere keeps this the one shape api/
// ever builds a provisioner response from. Degrades to an empty list on a
// CA error (no caClient, or Provisioners failing), matching this file's
// existing template-route behaviour rather than failing the whole response
// over a CA hiccup.
func (h *Handler) ListProvisioners(ctx context.Context) ([]CertProvisionerOption, error) {
	caClient, err := h.caClient()
	if err != nil {
		return []CertProvisionerOption{}, nil //nolint:nilerr // degrade to empty, matching Provisioners' template behaviour
	}
	list, err := caClient.Provisioners(ctx)
	if err != nil {
		return []CertProvisionerOption{}, nil //nolint:nilerr // degrade to empty, matching Provisioners' template behaviour
	}
	out := make([]CertProvisionerOption, 0, len(list))
	for _, p := range list {
		out = append(out, CertProvisionerOption{Name: p.Name, Type: p.Type})
	}
	return out, nil
}
