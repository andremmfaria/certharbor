package api

import (
	"context"
	"net/http"

	"certharbor/apitypes"
	"certharbor/handlers"
)

// acmeEventBody is one le_logs row on the wire.
type acmeEventBody struct {
	ID        int    `json:"id"`
	Domain    string `json:"domain,omitempty"`
	Action    string `json:"action,omitempty"`
	Message   string `json:"message,omitempty"`
	CreatedAt string `json:"createdAt"`
}

func newAcmeEventBody(e *handlers.ACMEEvent) acmeEventBody {
	return acmeEventBody{
		ID: e.ID, Domain: e.Domain, Action: e.Action, Message: e.Message, CreatedAt: e.CreatedAt,
	}
}

// ─── GET /api/v1/acme/events ────────────────────────────────────────────────

// listAcmeEventsInput's pagination bounds match 5.6 exactly (DefaultPageSize
// 25, MaxPageSize 200, MaxOffset's page*pageSize guard below), the same
// convention already proven for history, security/events and
// audit-events (apitypes.Page, db/pagination_integration_test.go's
// assertTiedTimestampPagination).
type listAcmeEventsInput struct {
	Domain   string `query:"domain" maxLength:"255" doc:"Exact domain filter."`
	Page     int    `query:"page" minimum:"1" default:"1"`
	PageSize int    `query:"pageSize" minimum:"1" maximum:"200" default:"25"`
}

type listAcmeEventsOutput struct {
	Body apitypes.Page[acmeEventBody]
}

// listAcmeEvents implements GET /api/v1/acme/events, replacing GET
// /le/logs (Section 6, Phase 7). db.ListACMELogsPaged carries the
// (created_at DESC, id DESC) total order fix history and security/events
// already have, so a page boundary cannot repeat or omit a row when many
// entries share one created_at.
func listAcmeEvents(h *handlers.Handler) func(context.Context, *listAcmeEventsInput) (*listAcmeEventsOutput, error) {
	return func(_ context.Context, in *listAcmeEventsInput) (*listAcmeEventsOutput, error) {
		page, pageSize := in.Page, in.PageSize
		if page < 1 {
			page = 1
		}
		if pageSize < 1 {
			pageSize = apitypes.DefaultPageSize
		}
		if page*pageSize > apitypes.MaxOffset {
			return nil, newProblem(http.StatusUnprocessableEntity, "Unprocessable Entity", problemTypeBase+"validation",
				"page*pageSize exceeds the maximum offset", "")
		}

		events, total, err := h.ListACMEEvents(in.Domain, page, pageSize)
		if err != nil {
			return nil, err
		}
		items := make([]acmeEventBody, 0, len(events))
		for i := range events {
			items = append(items, newAcmeEventBody(&events[i]))
		}
		return &listAcmeEventsOutput{Body: apitypes.NewPage(items, page, pageSize, total)}, nil
	}
}
