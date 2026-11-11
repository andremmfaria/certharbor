package api

import (
	"context"

	"certharbor/handlers"
)

// provisionerBody is one step-ca provisioner on the wire.
type provisionerBody struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type listProvisionersOutput struct {
	Body []provisionerBody
}

// listProvisioners implements GET /api/v1/provisioners, replacing GET
// /provisioners (Section 6). No pagination: it is today's full list
// (handlers.Provisioners renders every provisioner step-ca reports, with no
// paging in the template either), and the CA client call it depends on
// already degrades to an empty list rather than an error (handlers.
// ListProvisioners), matching the template route's existing behaviour.
func listProvisioners(h *handlers.Handler) func(context.Context, *struct{}) (*listProvisionersOutput, error) {
	return func(ctx context.Context, _ *struct{}) (*listProvisionersOutput, error) {
		provs, err := h.ListProvisioners(ctx)
		if err != nil {
			return nil, err
		}
		body := make([]provisionerBody, 0, len(provs))
		for _, p := range provs {
			body = append(body, provisionerBody{Name: p.Name, Type: p.Type})
		}
		return &listProvisionersOutput{Body: body}, nil
	}
}
