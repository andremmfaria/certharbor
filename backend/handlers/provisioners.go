package handlers

import (
	"context"
	"net/http"

	"certharbor/stepca"
)

// Provisioners renders the CA provisioners list page.
func (h *Handler) Provisioners(w http.ResponseWriter, r *http.Request) {
	var provs []map[string]interface{}
	if caClient, err := h.caClient(); err == nil {
		if list, err := caClient.Provisioners(r.Context()); err == nil {
			provs = provisionerMaps(list)
		}
	}
	data := h.base(w, r, "prov")
	data["Provisioners"] = provs
	data["CAURL"] = h.cfg.CAURL
	data["RootCert"] = h.cfg.RootCert
	data["Provisioner"] = h.cfg.Provisioner
	h.render(w, "provisioners", data)
}

// provisionerMaps converts the typed stepca.ProvisionerInfo list into the
// []map[string]interface{}{"name":..., "type":...} shape provisioners.html
// (index . "name" / index . "type") already expects, kept as the template
// contract instead of switching the template to typed struct access, since
// the template has no other dependents that would benefit (see plan's
// Reasoning Transparency).
func provisionerMaps(list []stepca.ProvisionerInfo) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(list))
	for _, p := range list {
		out = append(out, map[string]interface{}{"name": p.Name, "type": p.Type})
	}
	return out
}

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
