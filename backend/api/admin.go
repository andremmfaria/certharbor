package api

import (
	"context"
	"net/http"
	"time"

	"certharbor/handlers"
)

// ─── Shared health-check shapes (about + integrity) ─────────────────────────

// healthCheckBody is one named readiness/integrity probe result on the wire.
type healthCheckBody struct {
	Name     string `json:"name"`
	Status   string `json:"status" enum:"ok,warn,err"`
	Detail   string `json:"detail"`
	Critical bool   `json:"critical"`
}

func newHealthCheckBody(c handlers.HealthCheck) healthCheckBody {
	return healthCheckBody{Name: c.Name, Status: c.Status, Detail: c.Detail, Critical: c.Critical}
}

func newHealthCheckBodies(cs []handlers.HealthCheck) []healthCheckBody {
	items := make([]healthCheckBody, 0, len(cs))
	for i := range cs {
		items = append(items, newHealthCheckBody(cs[i]))
	}
	return items
}

// healthSummaryBody aggregates OK/warning/error counts across a checks list.
type healthSummaryBody struct {
	OK       int `json:"ok"`
	Warnings int `json:"warnings"`
	Errors   int `json:"errors"`
}

func newHealthSummaryBody(s handlers.HealthSummary) healthSummaryBody {
	return healthSummaryBody{OK: s.OK, Warnings: s.Warnings, Errors: s.Errors}
}

// ─── GET /api/v1/admin/about ────────────────────────────────────────────────

// systemInfoBody is handlers.SystemInfo narrowed for the wire: RootCert,
// PasswordFile, CertsDir, UploadDir, SSLCert and SSLKey are deliberately
// dropped. Every one of those paths that is actually useful to an operator
// already appears inside this same response's Checks[].Detail (health.go's
// checkFile/checkDir), so carrying them a second time here adds no
// information; PasswordFile additionally matches spec_secrets_test.go's
// secretish name pattern (it contains "password"), even though the value is
// a path rather than a credential, which is exactly the kind of accidental
// disclosure DTO narrowing exists to avoid rather than argue past with an
// allowlist entry.
type systemInfoBody struct {
	Version       string    `json:"version"`
	BuildDate     string    `json:"buildDate"`
	GitCommit     string    `json:"gitCommit"`
	StartedAt     time.Time `json:"startedAt"`
	Uptime        string    `json:"uptime"`
	CAURL         string    `json:"caUrl"`
	Provisioner   string    `json:"provisioner"`
	StepCAImage   string    `json:"stepCaImage"`
	SessionSecure bool      `json:"sessionSecure"`
}

func newSystemInfoBody(s *handlers.SystemInfo) systemInfoBody {
	return systemInfoBody{
		Version: s.Version, BuildDate: s.BuildDate, GitCommit: s.GitCommit,
		StartedAt: s.StartedAt, Uptime: s.Uptime, CAURL: s.CAURL,
		Provisioner: s.Provisioner, StepCAImage: s.StepCAImage, SessionSecure: s.SessionSecure,
	}
}

type adminAboutBody struct {
	System  systemInfoBody    `json:"system"`
	Checks  []healthCheckBody `json:"checks"`
	Summary healthSummaryBody `json:"summary"`
}

type getAdminAboutOutput struct {
	Body adminAboutBody
}

// getAdminAbout implements GET /api/v1/admin/about, replacing GET
// /admin/about (Section 6): the same preflight checks and system metadata
// AdminAboutGet renders, narrowed through systemInfoBody.
func getAdminAbout(h *handlers.Handler) func(context.Context, *struct{}) (*getAdminAboutOutput, error) {
	return func(ctx context.Context, _ *struct{}) (*getAdminAboutOutput, error) {
		checks, summary := h.Preflight(ctx)
		sysInfo := h.SystemInfo()
		return &getAdminAboutOutput{Body: adminAboutBody{
			System:  newSystemInfoBody(&sysInfo),
			Checks:  newHealthCheckBodies(checks),
			Summary: newHealthSummaryBody(summary),
		}}, nil
	}
}

// ─── GET /api/v1/admin/integrity ────────────────────────────────────────────

type adminIntegrityBody struct {
	Checks  []healthCheckBody `json:"checks"`
	Summary healthSummaryBody `json:"summary"`
}

type getAdminIntegrityOutput struct {
	Body adminIntegrityBody
}

// getAdminIntegrity implements GET /api/v1/admin/integrity, replacing GET
// /admin/integrity (Section 6).
//
// Deliberately uncached. Phase 6 step 2's exit criterion required measuring
// handlers.caIntegrity's cost before deciding, rather than guessing:
// BenchmarkCAIntegrity_LocalHealth and BenchmarkCAIntegrity_RealisticNetworkHealth
// (backend/handlers/health_bench_test.go) measure ~0.8ms/op with a
// zero-latency CA health probe and ~4.5ms/op with a 3ms synthetic one (a
// conservative stand-in for a same-docker-network HTTPS round trip). The
// single CA health call dominates both numbers; the remainder (two PEM
// parses, one JSON config read, two small secret-file reads, a handful of
// string checks) costs well under a millisecond and does not scale with the
// number of issued certificates, because caIntegrity never lists or reads
// the certificates directory at all — every check is a fixed, small set of
// known file paths. There is nothing here resembling the backup bundle's
// per-file cost (handlers/backup.go) to amortise with a 60s cache. If a
// future change makes this walk the certs directory, re-run the benchmark
// before assuming the uncached answer still holds.
func getAdminIntegrity(h *handlers.Handler) func(context.Context, *struct{}) (*getAdminIntegrityOutput, error) {
	return func(ctx context.Context, _ *struct{}) (*getAdminIntegrityOutput, error) {
		checks, summary := h.CAIntegrity(ctx)
		return &getAdminIntegrityOutput{Body: adminIntegrityBody{
			Checks:  newHealthCheckBodies(checks),
			Summary: newHealthSummaryBody(summary),
		}}, nil
	}
}

// ─── GET /api/v1/admin/console/commands ─────────────────────────────────────

// adminConsoleCommandBody is handlers.AdminConsoleCommandInfo on the wire:
// id, label and description only. This is the one place the allowlist's
// Name/Args/NativeFn could leak and does not — see
// handlers.ListAdminConsoleCommands and the test asserting the response
// never carries a substring of cfg.DatabaseURL.
type adminConsoleCommandBody struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description"`
}

type listAdminConsoleCommandsOutput struct {
	Body []adminConsoleCommandBody
}

// listAdminConsoleCommands implements GET /api/v1/admin/console/commands,
// replacing GET /admin/console's rendered command picker.
func listAdminConsoleCommands(h *handlers.Handler) func(context.Context, *struct{}) (*listAdminConsoleCommandsOutput, error) {
	return func(_ context.Context, _ *struct{}) (*listAdminConsoleCommandsOutput, error) {
		cmds := h.ListAdminConsoleCommands()
		items := make([]adminConsoleCommandBody, 0, len(cmds))
		for _, c := range cmds {
			items = append(items, adminConsoleCommandBody{ID: c.ID, Label: c.Label, Description: c.Description})
		}
		return &listAdminConsoleCommandsOutput{Body: items}, nil
	}
}

// ─── POST /api/v1/admin/console/run ─────────────────────────────────────────

type runAdminConsoleCommandInput struct {
	Body struct {
		CommandID string `json:"commandId" minLength:"1" maxLength:"64" doc:"One of GET /api/v1/admin/console/commands' ids."`
	}
}

// adminConsoleRunBody is handlers.AdminConsoleRunResult on the wire. It
// carries no Name/Args: CommandLine and Output are the already-sanitised
// text runAdminConsoleCommand (handlers/admin_console.go) produces, built
// from server-controlled arguments that exclude the database password by
// construction (pgIsReadyArgs).
type adminConsoleRunBody struct {
	CommandLine string `json:"commandLine"`
	Output      string `json:"output"`
	ExitCode    int    `json:"exitCode"`
	Duration    string `json:"duration"`
	TimedOut    bool   `json:"timedOut"`
	Truncated   bool   `json:"truncated"`
	Success     bool   `json:"success"`
}

type runAdminConsoleCommandOutput struct {
	Body adminConsoleRunBody
}

// postAdminConsoleRun implements POST /api/v1/admin/console/run, replacing
// POST /admin/console. An unknown commandId is 422, matching every other
// caller-input validation failure on this API (5.2) rather than the 404 a
// REST purist might reach for: the id names a value from a fixed, small
// enum the server itself publishes, not a resource.
func postAdminConsoleRun(h *handlers.Handler) func(context.Context, *runAdminConsoleCommandInput) (*runAdminConsoleCommandOutput, error) {
	return func(ctx context.Context, in *runAdminConsoleCommandInput) (*runAdminConsoleCommandOutput, error) {
		r, _ := httpFrom(ctx)
		result, err := h.RunAdminConsoleCommand(ctx, r, in.Body.CommandID)
		if err != nil {
			return nil, newProblem(http.StatusUnprocessableEntity, "Unprocessable Entity", problemTypeBase+"validation",
				"unknown command_id: only allowlisted commands may be run", "")
		}
		return &runAdminConsoleCommandOutput{Body: adminConsoleRunBody{
			CommandLine: result.CommandLine,
			Output:      result.Output,
			ExitCode:    result.ExitCode,
			Duration:    result.Duration,
			TimedOut:    result.TimedOut,
			Truncated:   result.Truncated,
			Success:     result.Success,
		}}, nil
	}
}
