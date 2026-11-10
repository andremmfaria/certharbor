// Command certharbor-delivery is the worker process described in
// plans/distribution.md: it reconciles certharbor's certificate inventory
// against externally configured delivery targets (AWS ACM in the MVP). It
// has NO HTTP listener — no net/http server, nothing bound to a port
// (Section 2) — so it never appears in `docker ps` port lists and is
// debugged with `docker compose exec`, the same posture
// scripts/check-backend-port.sh already enforces for certharbor-control.
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	appdb "certharbor/db"
	"certharbor/delivery"
	"certharbor/delivery/targets"
)

// main is the single os.Exit call site: every other function returns an
// error instead of calling log.Fatal/os.Exit directly, so deferred
// cleanup (e.g. conn.Close()) along every path always runs before the
// process exits (gocritic's exitAfterDefer).
func main() {
	if err := run(os.Args); err != nil {
		slog.Error("certharbor-delivery: fatal", "err", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) < 2 {
		return runReconciler()
	}

	switch args[1] {
	case "export":
		return cmdExport(args[2:])
	case "import":
		return cmdImport(args[2:])
	case "run":
		return runReconciler()
	default:
		fmt.Fprintf(os.Stderr, "usage: %s [run|export|import <file>]\n", args[0])
		return fmt.Errorf("unknown command %q", args[1])
	}
}

// databaseURL reads the same POSTGRES_*/DATABASE_URL shape
// backend/entrypoint.sh constructs for certharbor-control, so this binary's
// compose service follows the exact secrets convention already in place
// rather than inventing a second one.
func databaseURL() (string, error) {
	if v := os.Getenv("DATABASE_URL"); v != "" {
		return v, nil
	}
	pass := os.Getenv("POSTGRES_PASSWORD")
	if pass == "" {
		if f := os.Getenv("POSTGRES_PASSWORD_FILE"); f != "" {
			if b, err := os.ReadFile(f); err == nil { //nolint:gosec // operator-controlled path, matches entrypoint.sh's own pattern
				pass = string(b)
			}
		}
	}
	host := envOr("POSTGRES_HOST", "postgres")
	port := envOr("POSTGRES_PORT", "5432")
	user := envOr("POSTGRES_USER", "certharbor")
	dbname := envOr("POSTGRES_DB", "certharbor")
	if pass == "" {
		return "", fmt.Errorf("neither DATABASE_URL nor POSTGRES_PASSWORD/POSTGRES_PASSWORD_FILE is set")
	}
	// url.UserPassword percent-encodes pass, since generated passwords can
	// contain characters (+, /, @) that are unsafe unescaped in a DSN.
	userinfo := url.UserPassword(user, pass)
	return fmt.Sprintf("postgres://%s@%s:%s/%s?sslmode=disable", userinfo.String(), host, port, dbname), nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func connectDB() (*sql.DB, error) {
	dbURL, err := databaseURL()
	if err != nil {
		return nil, fmt.Errorf("resolving database URL: %w", err)
	}
	conn, err := appdb.Connect(dbURL)
	if err != nil {
		return nil, fmt.Errorf("cannot connect to database: %w", err)
	}
	if err := appdb.InitDeliverySchema(conn); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("cannot init delivery schema: %w", err)
	}
	return conn, nil
}

func newRegistry() *delivery.Registry {
	reg := delivery.NewRegistry()
	reg.Register(targets.NewACMTarget())
	return reg
}

func runReconciler() error {
	conn, err := connectDB()
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	interval := tickInterval()
	alertAfter := alertAfterFailures()
	slog.Info("certharbor-delivery starting", "tick_interval", interval, "alert_after_failures", alertAfter)

	rec := &delivery.Reconciler{
		Certs:              delivery.NewCertInventory(conn),
		Bindings:           delivery.NewBindingStore(conn),
		Targets:            delivery.NewTargetStore(conn),
		Runs:               delivery.NewRunStore(conn),
		Material:           delivery.FileMaterialLoader{},
		Registry:           newRegistry(),
		Audit:              delivery.NewAuditor(conn),
		AlertAfterFailures: alertAfter,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Run once immediately, then on the configured tick.
	tick(ctx, rec)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			slog.Info("certharbor-delivery: shutdown signal received")
			return nil
		case <-ticker.C:
			tick(ctx, rec)
		}
	}
}

func tick(ctx context.Context, rec *delivery.Reconciler) {
	if err := rec.Tick(ctx); err != nil {
		slog.Error("certharbor-delivery: tick failed", "err", err)
	}
}

func tickInterval() time.Duration {
	if v := os.Getenv("DELIVERY_TICK_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
		slog.Warn("DELIVERY_TICK_INTERVAL is not a valid duration, using default", "value", v)
	}
	return 5 * time.Minute // Section 6 default
}

func alertAfterFailures() int {
	if v := os.Getenv("DELIVERY_ALERT_AFTER_FAILURES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return 3 // Section 10 default, matches cert-distributor's reference default
}

func cmdExport(args []string) error {
	fs := flag.NewFlagSet("export", flag.ContinueOnError)
	out := fs.String("out", "-", "output file path, or - for stdout")
	if err := fs.Parse(args); err != nil {
		return err
	}

	conn, err := connectDB()
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	doc, err := delivery.Export(context.Background(), conn)
	if err != nil {
		return fmt.Errorf("export failed: %w", err)
	}
	data, err := delivery.MarshalYAML(doc)
	if err != nil {
		return fmt.Errorf("export: marshaling YAML: %w", err)
	}
	if *out == "-" {
		os.Stdout.Write(data) //nolint:errcheck,gosec // best-effort stdout write, matches CLI tool convention
		return nil
	}
	if err := os.WriteFile(*out, data, 0o600); err != nil {
		return fmt.Errorf("export: writing %s: %w", *out, err)
	}
	fmt.Printf("exported registry to %s\n", *out)
	return nil
}

func cmdImport(args []string) error {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: certharbor-delivery import <file>")
		return fmt.Errorf("import: missing <file> argument")
	}
	path := args[0]

	// Logged via structured fields (not interpolated into a format
	// string) so the operator-supplied path never flows into a log sink
	// as taintable format input (gosec G706).
	data, err := os.ReadFile(path) //nolint:gosec // operator-supplied path, this is a CLI tool
	if err != nil {
		slog.Error("certharbor-delivery: import: reading file failed", "path", path, "err", err)
		return fmt.Errorf("import: reading file failed")
	}
	doc, err := delivery.UnmarshalYAML(data)
	if err != nil {
		slog.Error("certharbor-delivery: import: parsing file failed", "path", path, "err", err)
		return fmt.Errorf("import: parsing file failed")
	}

	conn, err := connectDB()
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	diff, err := delivery.Import(context.Background(), conn, doc, delivery.NewAuditor(conn))
	if err != nil {
		return fmt.Errorf("import failed: %w", err)
	}
	fmt.Printf("import applied: %s\n", diff.String())
	return nil
}
