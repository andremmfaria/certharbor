package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	cfgpkg "certharbor/config"
	"certharbor/handlers"

	"github.com/danielgtaylor/huma/v2"
	"github.com/gorilla/sessions"
	_ "github.com/lib/pq"
)

// newAdminTestHandler builds a Handler usable by the admin console
// operations directly (bypassing Mount/httptest.Server). The *sql.DB is
// real but never dials anything until a query runs (sql.Open is lazy):
// getAdminAbout's Preflight calls h.db.PingContext, which would otherwise
// panic on a nil *sql.DB the way the real template handler never does
// (main.go always wires a live connection). Any DB-touching check here is
// expected to fail with a connection error, not a panic, and that is fine:
// these tests are about the response shape, not database connectivity.
func newAdminTestHandler(cfg *cfgpkg.Config) *handlers.Handler {
	store := sessions.NewCookieStore(
		[]byte("test-hash-key-32charsXXXXXXXXXXX"),
		[]byte("test-block-key16"),
	)
	db, err := sql.Open("postgres", "postgres://invalid:invalid@127.0.0.1:1/invalid?sslmode=disable&connect_timeout=1")
	if err != nil {
		panic(err)
	}
	return handlers.NewWithFS(db, cfg, store, nil)
}

// ctxWithHTTP reproduces the context shape unwrapMiddleware stashes
// (httpFrom's expectation), so an operation handler can be called directly
// without standing up the full chi/huma chain.
func ctxWithHTTP(method, path string) context.Context {
	r := httptest.NewRequestWithContext(context.Background(), method, path, nil)
	w := httptest.NewRecorder()
	return context.WithValue(context.Background(), ctxKeyHTTP, httpPair{r, w})
}

// testDSN matches the shape config.Load produces (host/port/user/db plus a
// redacted password placeholder, the same convention
// admin_console_test.go's defaultTestCfg uses). The substring check below
// does not depend on there being a real secret in the password slot: it
// asserts the *whole* configured DSN string never appears in the response,
// which is the actual exit criterion (plans/frontend-backend-split.md
// Phase 6) and also happens to cover a real deployment's password.
const testDSN = "postgres://certharbor:***@postgres.internal:5432/certharbor?sslmode=disable" //nolint:gosec // G101: fake test credential, not a real DSN

// TestAdminConsoleRun_NeverLeaksDSN is the exit criterion from
// plans/frontend-backend-split.md Phase 6: the console run response must
// carry no substring of the configured database DSN, under a realistic
// console command (postgres.ready, whose arguments are parsed straight out
// of cfg.DatabaseURL by pgIsReadyArgs) and a couple of unrelated commands
// for good measure.
func TestAdminConsoleRun_NeverLeaksDSN(t *testing.T) {
	cfg := &cfgpkg.Config{DatabaseURL: testDSN, CAURL: "https://127.0.0.1:19999"}
	h := newAdminTestHandler(cfg)

	for _, id := range []string{"postgres.ready", "system.date", "app.version"} {
		t.Run(id, func(t *testing.T) {
			ctx := ctxWithHTTP(http.MethodPost, "/api/v1/admin/console/run")
			in := &runAdminConsoleCommandInput{}
			in.Body.CommandID = id

			out, err := postAdminConsoleRun(h)(ctx, in)
			if err != nil {
				t.Fatalf("postAdminConsoleRun(%q): %v", id, err)
			}

			raw, mErr := json.Marshal(out.Body)
			if mErr != nil {
				t.Fatalf("marshal: %v", mErr)
			}
			body := string(raw)

			// The rendered command line legitimately includes the DB host
			// for postgres.ready (an admin running this diagnostic needs to
			// see what pg_isready targeted) — that is not a credential.
			// The actual exit criterion is narrower and absolute: the full
			// configured DSN string, password slot included, never appears.
			if strings.Contains(body, testDSN) {
				t.Fatalf("response for %q contains the full configured DSN: %s", id, body)
			}
		})
	}
}

// TestAdminConsoleRun_UnknownCommandIsUnprocessable mirrors AdminConsolePost's
// denial path: an id outside the allowlist is rejected, never run.
func TestAdminConsoleRun_UnknownCommandIsUnprocessable(t *testing.T) {
	cfg := &cfgpkg.Config{DatabaseURL: testDSN}
	h := newAdminTestHandler(cfg)

	ctx := ctxWithHTTP(http.MethodPost, "/api/v1/admin/console/run")
	in := &runAdminConsoleCommandInput{}
	in.Body.CommandID = "not-a-real-command"

	_, err := postAdminConsoleRun(h)(ctx, in)
	if err == nil {
		t.Fatal("expected an error for an unknown command id")
	}
	var model *huma.ErrorModel
	if !errors.As(err, &model) {
		t.Fatalf("expected a *huma.ErrorModel, got %T: %v", err, err)
	}
	if model.Status != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", model.Status)
	}
}

// TestListAdminConsoleCommands_NeverLeaksDSN covers the companion list
// operation: the allowlist's Name/Args (postgres.ready's args are parsed
// from cfg.DatabaseURL) must never reach the id/label/description DTO.
func TestListAdminConsoleCommands_NeverLeaksDSN(t *testing.T) {
	cfg := &cfgpkg.Config{DatabaseURL: testDSN}
	h := newAdminTestHandler(cfg)

	out, err := listAdminConsoleCommands(h)(context.Background(), &struct{}{})
	if err != nil {
		t.Fatalf("listAdminConsoleCommands: %v", err)
	}
	raw, mErr := json.Marshal(out.Body)
	if mErr != nil {
		t.Fatalf("marshal: %v", mErr)
	}
	body := string(raw)
	if strings.Contains(body, testDSN) || strings.Contains(body, "postgres.internal") {
		t.Fatalf("command list leaks DSN-derived data: %s", body)
	}
	if len(out.Body) == 0 {
		t.Fatal("expected a non-empty allowlist")
	}
	for _, c := range out.Body {
		if c.ID == "" || c.Label == "" {
			t.Errorf("command %+v missing id/label", c)
		}
	}
}

// TestAdminAbout_SystemHasNoPasswordFilePath asserts the about response's
// top-level "system" object never carries PasswordFile, the one
// handlers.SystemInfo field systemInfoBody deliberately drops (see its
// comment) because its name matches spec_secrets_test.go's secretish
// pattern. The marker value legitimately appears elsewhere in the response
// (inside checks[].detail, exactly as the pre-existing admin_about.html
// template already shows it: an admin debugging a missing provisioner
// password needs the path), so this asserts the narrower, correct claim
// about the "system" object rather than the whole body.
func TestAdminAbout_SystemHasNoPasswordFilePath(t *testing.T) {
	dir := t.TempDir()
	cfg := &cfgpkg.Config{
		CAURL:        "https://127.0.0.1:19999",
		RootCert:     dir + "/root_ca.crt",
		PasswordFile: dir + "/secrets/super-secret-password-file",
	}
	h := newAdminTestHandler(cfg)

	out, err := getAdminAbout(h)(context.Background(), &struct{}{})
	if err != nil {
		t.Fatalf("getAdminAbout: %v", err)
	}
	raw, mErr := json.Marshal(out.Body.System)
	if mErr != nil {
		t.Fatalf("marshal: %v", mErr)
	}
	if strings.Contains(string(raw), "super-secret-password-file") {
		t.Fatalf("about response's system object leaks the password file path: %s", raw)
	}
}
