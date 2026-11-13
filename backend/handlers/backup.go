package handlers

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	appdb "certharbor/db"
)

// PGDumpTimeout is the longest operation any handler runs. It is exported so
// a test can assert nginx's proxy_read_timeout exceeds it: a ceiling raised
// here without nginx following would surface as an unattributed 504.
const PGDumpTimeout = 2 * time.Minute

type backupManifest struct {
	Format     string            `json:"format"`
	CreatedAt  string            `json:"created_at"`
	Version    string            `json:"version"`
	BuildDate  string            `json:"build_date"`
	GitCommit  string            `json:"git_commit"`
	Hostname   string            `json:"hostname"`
	Database   map[string]string `json:"database"`
	Components []backupComponent `json:"components"`
	Warnings   []string          `json:"warnings,omitempty"`
	RestoreDoc string            `json:"restore_doc"`
}

type backupComponent struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

// backupIDPattern is the same charset the API layer's path parameter
// schema constrains POST /api/v1/admin/backups' returned id to (Section
// 6, 5.7's downloadUrl pattern): safe to use as a bare filename component
// with no path-traversal risk, so BackupFilePath never needs filepath.Clean
// to defend against a caller-controlled id.
var backupIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// BackupRecord is one persisted backup bundle: the handler-layer row
// GET/POST /api/v1/admin/backups and GET .../{id}/download build their
// response from, kept separate from any wire DTO (Section 2).
type BackupRecord struct {
	ID        string
	SizeBytes int64
	CreatedAt time.Time
}

func (h *Handler) backupFilename(id string) string { return id + ".tgz" }

// newBackupID mints a filesystem-safe, collision-resistant id: a sortable
// UTC timestamp (matching the legacy bundle filename convention above)
// plus a random suffix, since two backups requested within the same
// second must not collide.
func newBackupID() (string, error) {
	suffix := make([]byte, 6)
	if _, err := rand.Read(suffix); err != nil {
		return "", fmt.Errorf("generating backup id: %w", err)
	}
	return fmt.Sprintf("%s-%s", time.Now().UTC().Format("20060102T150405Z"), hex.EncodeToString(suffix)), nil
}

// CreateBackup implements POST /api/v1/admin/backups' data half (Section
// 6, replacing POST /admin/backup/download): it builds the same bundle
// buildBackupBundle already produces, then persists it under cfg.BackupDir
// under a fresh id so a later, separate request (GET .../{id}/download)
// can still serve it — the create-then-navigate pattern 5.7 asks for needs
// the bundle to outlive the request that created it, unlike the legacy
// route's stream-then-delete-tmp-dir.
func (h *Handler) CreateBackup(ctx context.Context) (*BackupRecord, error) {
	bundlePath, _, err := h.buildBackupBundle(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(filepath.Dir(bundlePath)) }()

	id, err := newBackupID()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(h.cfg.BackupDir, 0o750); err != nil { //nolint:gosec // G301: app data dir, restrictive perms appropriate for non-root service user
		return nil, fmt.Errorf("creating backup directory: %w", err)
	}
	dest := filepath.Join(h.cfg.BackupDir, h.backupFilename(id))
	if err := copyFile(bundlePath, dest); err != nil {
		return nil, fmt.Errorf("persisting backup bundle: %w", err)
	}
	info, err := os.Stat(dest)
	if err != nil {
		return nil, err
	}
	return &BackupRecord{ID: id, SizeBytes: info.Size(), CreatedAt: info.ModTime().UTC()}, nil
}

// ListBackups implements GET /api/v1/admin/backups (Section 6, replacing
// GET /admin/backup), newest first, by stat'ing cfg.BackupDir directly:
// backups are infrequent, admin-only, and not expected to reach a count
// where a directory listing becomes the bottleneck.
func (h *Handler) ListBackups() ([]BackupRecord, error) {
	entries, err := os.ReadDir(h.cfg.BackupDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []BackupRecord{}, nil
		}
		return nil, err
	}
	records := make([]BackupRecord, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".tgz") {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".tgz")
		if !backupIDPattern.MatchString(id) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		records = append(records, BackupRecord{ID: id, SizeBytes: info.Size(), CreatedAt: info.ModTime().UTC()})
	}
	sort.Slice(records, func(i, j int) bool { return records[i].CreatedAt.After(records[j].CreatedAt) })
	return records, nil
}

// errBackupNotFound is BackupFilePath's not-found sentinel, so the api
// layer can tell "no such backup" apart from "the directory is
// unreadable" without inspecting os.IsNotExist on an already-wrapped error.
var errBackupNotFound = NewValidationError("no backup with that id")

// BackupFilePath implements GET /api/v1/admin/backups/{id}/download's
// lookup half (Section 6, **new**): id is re-validated against
// backupIDPattern independently of the api layer's path-parameter schema,
// because a handler that trusts its caller's validation is a handler that
// breaks silently the day that validation moves.
func (h *Handler) BackupFilePath(id string) (path string, err error) {
	if !backupIDPattern.MatchString(id) {
		return "", errBackupNotFound
	}
	path = filepath.Join(h.cfg.BackupDir, h.backupFilename(id))
	if _, statErr := os.Stat(path); statErr != nil {
		return "", errBackupNotFound
	}
	return path, nil
}

// IsBackupNotFound reports whether err is BackupFilePath's not-found
// sentinel, letting the api layer map it to 404 without a direct
// comparison against an unexported value.
func IsBackupNotFound(err error) bool { return err == errBackupNotFound } //nolint:errorlint // sentinel comparison by design, never wrapped

// AuditBackupCreate is CreateBackup's h.auditSecurity call site, adapted
// for the api layer exactly as AuditKeyDownload is (ca_api.go's comment):
// the huma session middleware never populates the gorilla-session
// "username" key h.auditSecurity reads, so the authenticated username is
// passed in explicitly from context instead.
func (h *Handler) AuditBackupCreate(r *http.Request, username, id string) {
	if r == nil || username == "" {
		return
	}
	_ = appdb.LogAuth(h.db, username, clientIP(r), true, auditPrefix+"backup.create id="+id)
}

// copyFile copies src to dest, creating dest with 0600 permissions: a
// backup bundle contains a full pg_dump and is exactly as sensitive as the
// database it was taken from.
func copyFile(src, dest string) error {
	//nolint:gosec // G304: src is the tmp bundle path buildBackupBundle itself created, not user input
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	//nolint:gosec // G304: dest is built from cfg.BackupDir joined with a generated, pattern-validated id
	out, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()
	_, err = io.Copy(out, in)
	return err
}

func (h *Handler) buildBackupBundle(ctx context.Context) (string, string, error) {
	ts := time.Now().UTC().Format("20060102T150405Z")
	tmp, err := os.MkdirTemp("", "certharbor-backup-*")
	if err != nil {
		return "", "", err
	}

	manifest := backupManifest{
		Format:     "certharbor-backup-v1",
		CreatedAt:  time.Now().UTC().Format(time.RFC3339),
		Version:    Version,
		BuildDate:  BuildDate,
		GitCommit:  GitCommit,
		Database:   h.backupDBInfo(),
		RestoreDoc: "README.md (Backup & restore section)",
	}
	if host, err := os.Hostname(); err == nil {
		manifest.Hostname = host
	}

	addFile := func(name, path, detail string) {
		c := backupComponent{Name: name, Path: filepath.Base(path), Status: "ok", Detail: detail}
		if size, sum, err := fileStatSHA256(path); err == nil {
			c.Size = size
			c.SHA256 = sum
		} else {
			c.Status = "error"
			c.Detail = err.Error()
			manifest.Warnings = append(manifest.Warnings, fmt.Sprintf("%s: %v", name, err))
		}
		manifest.Components = append(manifest.Components, c)
	}

	sqlPath := filepath.Join(tmp, "postgres-certharbor.sql")
	if err := h.writePGDump(ctx, sqlPath); err != nil {
		manifest.Warnings = append(manifest.Warnings, "postgres dump failed: "+err.Error())
	} else {
		addFile("postgres", sqlPath, "pg_dump custom-free plain SQL")
	}

	for _, item := range []struct {
		name   string
		source string
		target string
	}{
		{"step-ca-data", "/home/step", "step-ca-data.tgz"},
		{"certharbor-data", "/opt/certharbor/data", "certharbor-data.tgz"},
		{"certharbor-certs", h.cfg.CertsDir, "certharbor-certs.tgz"},
		{"certharbor-uploads", h.cfg.UploadDir, "certharbor-uploads.tgz"},
	} {
		target := filepath.Join(tmp, item.target)
		if err := writeDirTGZ(item.source, target); err != nil {
			manifest.Warnings = append(manifest.Warnings, fmt.Sprintf("%s failed: %v", item.name, err))
			continue
		}
		addFile(item.name, target, item.source)
	}

	manifestPath := filepath.Join(tmp, "manifest.json")
	if err := writeManifest(manifestPath, &manifest); err != nil {
		return "", "", err
	}

	bundleName := fmt.Sprintf("certharbor-backup-%s.tgz", ts)
	bundlePath := filepath.Join(tmp, bundleName)
	if err := writeBundleTGZ(bundlePath, []string{
		manifestPath,
		filepath.Join(tmp, "postgres-certharbor.sql"),
		filepath.Join(tmp, "step-ca-data.tgz"),
		filepath.Join(tmp, "certharbor-data.tgz"),
		filepath.Join(tmp, "certharbor-certs.tgz"),
		filepath.Join(tmp, "certharbor-uploads.tgz"),
	}); err != nil {
		return "", "", err
	}
	return bundlePath, bundleName, nil
}

func (h *Handler) writePGDump(ctx context.Context, path string) error {
	info, err := parsePostgresURL(h.cfg.DatabaseURL)
	if err != nil {
		return err
	}

	// Write a temporary .pgpass file so the password is never visible in the
	// process environment (readable via /proc on Linux).  pg_dump honours
	// PGPASSFILE pointing at a 0600 file.
	pgpassFile, err := os.CreateTemp("", "pgpass-*")
	if err != nil {
		return fmt.Errorf("creating pgpass tempfile: %w", err)
	}
	pgpassPath := pgpassFile.Name()
	defer func() { _ = os.Remove(pgpassPath) }()

	if err := pgpassFile.Chmod(0o600); err != nil {
		_ = pgpassFile.Close()
		return fmt.Errorf("chmod pgpass: %w", err)
	}
	// Format: hostname:port:database:username:password
	pgpassLine := fmt.Sprintf("%s:%s:%s:%s:%s\n", info.host, info.port, info.db, info.user, info.password)
	if _, err := pgpassFile.WriteString(pgpassLine); err != nil {
		_ = pgpassFile.Close()
		return fmt.Errorf("writing pgpass: %w", err)
	}
	if err := pgpassFile.Close(); err != nil {
		return fmt.Errorf("closing pgpass: %w", err)
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, PGDumpTimeout)
	defer cancel()

	args := []string{"-h", info.host, "-p", info.port, "-U", info.user, "-d", info.db, "--no-owner", "--no-privileges"}
	//nolint:gosec // G204: pg_dump is a known binary; args are DB connection params from config, not user input
	cmd := exec.CommandContext(timeoutCtx, "pg_dump", args...)
	// Pass PGPASSFILE rather than PGPASSWORD to keep the password off /proc.
	env := make([]string, 0, len(os.Environ())+1)
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "PGPASSWORD=") {
			env = append(env, e)
		}
	}
	env = append(env, "PGPASSFILE="+pgpassPath)
	cmd.Env = env
	//nolint:gosec // G304: path is constructed from a validated admin-specified backup directory
	out, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()
	cmd.Stdout = out
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("pg_dump: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func (h *Handler) backupDBInfo() map[string]string {
	info, err := parsePostgresURL(h.cfg.DatabaseURL)
	if err != nil {
		return map[string]string{"error": err.Error()}
	}
	return map[string]string{"host": info.host, "port": info.port, "database": info.db, "user": info.user}
}

type postgresInfo struct {
	host     string
	port     string
	user     string
	password string
	db       string
}

func parsePostgresURL(raw string) (postgresInfo, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return postgresInfo{}, err
	}
	if u.Scheme != "postgres" && u.Scheme != "postgresql" {
		return postgresInfo{}, fmt.Errorf("unsupported database URL scheme: %s", u.Scheme)
	}
	host, port, err := net.SplitHostPort(u.Host)
	if err != nil {
		host = u.Hostname()
		port = u.Port()
	}
	if port == "" {
		port = "5432"
	}
	user := u.User.Username()
	password, _ := u.User.Password()
	db := strings.TrimPrefix(u.Path, "/")
	if host == "" || user == "" || db == "" {
		return postgresInfo{}, fmt.Errorf("database URL must include host, user and database")
	}
	return postgresInfo{host: host, port: port, user: user, password: password, db: db}, nil
}

func writeManifest(path string, manifest *backupManifest) error {
	//nolint:gosec // G304: path is within the validated backup tmp directory
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(manifest)
}

func fileStatSHA256(path string) (int64, string, error) {
	//nolint:gosec // G304: path comes from config-dir file listing (admin-controlled)
	f, err := os.Open(path)
	if err != nil {
		return 0, "", err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	size, err := io.Copy(h, f)
	if err != nil {
		return 0, "", err
	}
	return size, hex.EncodeToString(h.Sum(nil)), nil
}

func writeDirTGZ(source, target string) error {
	info, err := os.Stat(source)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", source)
	}

	//nolint:gosec // G304: target is the output bundle path constructed from a validated temp directory
	out, err := os.Create(target)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()
	gw := gzip.NewWriter(out)
	defer func() { _ = gw.Close() }()
	tw := tar.NewWriter(gw)
	defer func() { _ = tw.Close() }()

	return filepath.WalkDir(source, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == source {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			rel += "/"
		}
		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		hdr.Name = rel
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		//nolint:gosec // G304: path comes from filepath.WalkDir of validated source directory
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(tw, f)
		closeErr := f.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
}

func writeBundleTGZ(target string, files []string) error {
	//nolint:gosec // G304: target is the output path constructed from a validated temp directory
	out, err := os.Create(target)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()
	gw := gzip.NewWriter(out)
	defer func() { _ = gw.Close() }()
	tw := tar.NewWriter(gw)
	defer func() { _ = tw.Close() }()

	for _, path := range files {
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			continue
		}
		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		hdr.Name = filepath.Base(path)
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		//nolint:gosec // G304: path comes from a caller-supplied file list validated before this call
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		if _, err := io.Copy(tw, f); err != nil {
			_ = f.Close()
			return err
		}
		_ = f.Close()
	}
	return nil
}
