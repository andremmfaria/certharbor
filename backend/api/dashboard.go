package api

import (
	"context"

	"certharbor/handlers"
)

// dashboardCertificatesBody composes nothing but counts derived from the
// same rows GET /api/v1/certificates (listCertificates) already returns:
// a total, a breakdown keyed by the existing expiryStatus value, and an
// expiring-soon count that is just the "warning" bucket of that same
// breakdown (D10 rule 1 — no logic here has no independent source).
type dashboardCertificatesBody struct {
	Total        int            `json:"total" source:"listCertificates"`
	ByStatus     map[string]int `json:"byStatus" source:"listCertificates" doc:"Certificate counts keyed by expiryStatus: ok, warning, expired."`
	ExpiringSoon int            `json:"expiringSoon" source:"listCertificates" doc:"Certificates whose expiryStatus is warning."`
}

// dashboardBody is GET /api/v1/dashboard's aggregate shape (D10). It is
// viewer-role (testdata/roles.golden), so every part it composes must be
// viewer-role-or-lower: certificates and ca are both viewer (TestRoleMatrix).
//
// There is deliberately no ACME field yet. D10 rule 1 forbids an aggregate
// value with no independent source, and Phase 7 has not landed
// GET /api/v1/acme/* yet (confirmed absent from backend/api as of this
// change). The field is added here, sourced from that operation, once
// Phase 7 ships it — see plans/frontend-backend-split.md's Phase 5 step 3
// note.
type dashboardBody struct {
	Certificates dashboardCertificatesBody `json:"certificates"`
	CA           caInfoBody                `json:"ca"`
}

type getDashboardOutput struct {
	Body dashboardBody
}

// getDashboard implements GET /api/v1/dashboard, replacing GET / and GET
// /dashboard (D4 decision 8: one route, one component). It calls the same
// handler functions listCertificates and getCA already call — h.
// ListCertificateSummaries and h.FetchCAInfo — in process, rather than
// issuing an HTTP request to itself, exactly as D10 rule 1 asks: the
// aggregate composes an existing resource-shaped operation's data, it does
// not invent aggregate-only logic.
func getDashboard(h *handlers.Handler) func(context.Context, *struct{}) (*getDashboardOutput, error) {
	return func(ctx context.Context, _ *struct{}) (*getDashboardOutput, error) {
		all, err := h.ListCertificateSummaries("", "")
		if err != nil {
			return nil, err
		}
		counts := dashboardCertificatesBody{
			Total:    len(all),
			ByStatus: map[string]int{"ok": 0, "warning": 0, "expired": 0},
		}
		for i := range all {
			status := handlers.CertExpiryStatus(all[i].ExpiresAt)
			counts.ByStatus[status]++
			if status == "warning" {
				counts.ExpiringSoon++
			}
		}

		info, err := h.FetchCAInfo(ctx)
		if err != nil {
			return nil, err
		}
		notBefore, notAfter := info.NotBefore, info.NotAfter
		ca := caInfoBody{
			Subject:           info.Subject,
			FingerprintSha256: info.FingerprintSHA256,
			NotBefore:         &notBefore,
			NotAfter:          &notAfter,
			Reachable:         info.Reachable,
		}

		return &getDashboardOutput{Body: dashboardBody{Certificates: counts, CA: ca}}, nil
	}
}
