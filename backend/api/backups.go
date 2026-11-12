package api

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"certharbor/handlers"
)

// backupBody is the wire shape of one persisted backup, shared by the list
// and create operations. downloadUrl is a relative path constrained by the
// same pattern 5.7 specifies: a path outside a proxied prefix falls through
// nginx's try_files and the browser would save the SPA shell, 200
// text/html, named as a backup — a corrupt backup that looks successful.
type backupBody struct {
	ID          string    `json:"id"`
	SizeBytes   int64     `json:"sizeBytes"`
	CreatedAt   time.Time `json:"createdAt"`
	DownloadURL string    `json:"downloadUrl" pattern:"^/api/v1/admin/backups/[A-Za-z0-9_-]+/download$"`
}

func newBackupBody(r handlers.BackupRecord) backupBody {
	return backupBody{
		ID: r.ID, SizeBytes: r.SizeBytes, CreatedAt: r.CreatedAt,
		DownloadURL: fmt.Sprintf("%s/admin/backups/%s/download", BasePath, r.ID),
	}
}

// ─── GET /api/v1/admin/backups ──────────────────────────────────────────────

type listBackupsOutput struct {
	Body []backupBody
}

// listBackups implements GET /api/v1/admin/backups, replacing GET
// /admin/backup (Section 6).
func listBackups(h *handlers.Handler) func(context.Context, *struct{}) (*listBackupsOutput, error) {
	return func(_ context.Context, _ *struct{}) (*listBackupsOutput, error) {
		records, err := h.ListBackups()
		if err != nil {
			return nil, err
		}
		body := make([]backupBody, 0, len(records))
		for _, r := range records {
			body = append(body, newBackupBody(r))
		}
		return &listBackupsOutput{Body: body}, nil
	}
}

// ─── POST /api/v1/admin/backups ─────────────────────────────────────────────

type createBackupOutput struct {
	Body backupBody
}

// createBackup implements POST /api/v1/admin/backups, replacing POST
// /admin/backup/download (Section 6): it builds and persists the bundle,
// then returns {id, sizeBytes, downloadUrl} for the SPA's create-then-
// navigate flow (5.7) rather than streaming the archive in this response.
func createBackup(h *handlers.Handler) func(context.Context, *struct{}) (*createBackupOutput, error) {
	return func(ctx context.Context, _ *struct{}) (*createBackupOutput, error) {
		record, err := h.CreateBackup(ctx)
		if err != nil {
			return nil, err
		}
		r, _ := httpFrom(ctx)
		username, _ := userIdentity(ctx)
		h.AuditBackupCreate(r, username, record.ID)
		return &createBackupOutput{Body: newBackupBody(*record)}, nil
	}
}

// ─── GET /api/v1/admin/backups/{id}/download ────────────────────────────────

// downloadBackupInput's pattern matches backupBody.DownloadURL's path
// segment exactly (5.7): a generated client's encodeURIComponent cannot
// disturb this charset, so no canonicalisation step like deleteIPBlock's
// is needed here.
type downloadBackupInput struct {
	ID string `path:"id" pattern:"^[A-Za-z0-9_-]+$" doc:"Backup id, as returned by POST /api/v1/admin/backups."`
}

var errBackupDownloadNotFound = newProblem(http.StatusNotFound, "Not Found", "", "no backup with that id", "")

// downloadBackup implements GET /api/v1/admin/backups/{id}/download
// (Section 6, **new**): the target of the create-then-navigate pattern's
// direct browser navigation (5.7), so the response is a real octet-stream
// download, never a generated-client Blob call (5.7's note on buffering a
// whole pg_dump into JS heap being unacceptable).
func downloadBackup(h *handlers.Handler) func(context.Context, *downloadBackupInput) (*binaryFileOutput, error) {
	return func(_ context.Context, in *downloadBackupInput) (*binaryFileOutput, error) {
		path, err := h.BackupFilePath(in.ID)
		if err != nil {
			if handlers.IsBackupNotFound(err) {
				return nil, errBackupDownloadNotFound
			}
			return nil, err
		}
		//nolint:gosec // G304: path is built from cfg.BackupDir joined with an id BackupFilePath already validated against its pattern
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		return &binaryFileOutput{
			ContentType:        "application/gzip",
			ContentDisposition: fmt.Sprintf(`attachment; filename="%s"`, in.ID+".tgz"),
			Body:               data,
		}, nil
	}
}
