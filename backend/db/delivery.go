// Package db additions for certharbor-delivery (plans/distribution.md,
// Section 3). These three tables are owned by certharbor-delivery: its own
// binary runs InitDeliverySchema at startup, the same way backend/main.go
// runs InitSchema/InitACMESchema/etc. for certharbor-control's own tables.
// certharbor-control never calls InitDeliverySchema and never reads or
// writes these tables.
package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"certharbor/models"
)

// InitDeliverySchema creates the delivery_targets, delivery_bindings and
// delivery_runs tables used by certharbor-delivery, following the same
// CREATE TABLE IF NOT EXISTS / ALTER TABLE ADD COLUMN IF NOT EXISTS startup
// migration pattern as InitSchema.
func InitDeliverySchema(d *sql.DB) error {
	schema := `
	CREATE TABLE IF NOT EXISTS delivery_targets (
		id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		name             TEXT UNIQUE NOT NULL,
		type             TEXT NOT NULL,
		params           JSONB NOT NULL DEFAULT '{}',
		credentials_ref  TEXT NOT NULL DEFAULT '',
		created_at       TIMESTAMPTZ DEFAULT NOW(),
		updated_at       TIMESTAMPTZ DEFAULT NOW()
	);

	CREATE TABLE IF NOT EXISTS delivery_bindings (
		id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		certificate_name TEXT NOT NULL,
		target_id        UUID NOT NULL REFERENCES delivery_targets(id) ON DELETE CASCADE,
		target_params    JSONB NOT NULL DEFAULT '{}',
		enabled          BOOLEAN NOT NULL DEFAULT TRUE,
		consecutive_failures INTEGER NOT NULL DEFAULT 0,
		created_at       TIMESTAMPTZ DEFAULT NOW(),
		updated_at       TIMESTAMPTZ DEFAULT NOW(),
		UNIQUE (certificate_name, target_id)
	);
	CREATE INDEX IF NOT EXISTS idx_delivery_bindings_cert ON delivery_bindings(certificate_name);
	CREATE INDEX IF NOT EXISTS idx_delivery_bindings_target ON delivery_bindings(target_id);

	CREATE TABLE IF NOT EXISTS delivery_runs (
		id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		binding_id           UUID NOT NULL REFERENCES delivery_bindings(id) ON DELETE CASCADE,
		fingerprint          TEXT NOT NULL DEFAULT '',
		started_at           TIMESTAMPTZ NOT NULL,
		finished_at          TIMESTAMPTZ,
		result               TEXT NOT NULL,
		consecutive_failures INTEGER NOT NULL DEFAULT 0
	);
	CREATE INDEX IF NOT EXISTS idx_delivery_runs_binding ON delivery_runs(binding_id, started_at DESC);
	`
	// gen_random_uuid() needs pgcrypto on Postgres < 13; certharbor's compose
	// pins postgres:16-alpine, which ships it built in via the uuid-ossp /
	// pgcrypto extension state already enabled for superuser-owned DBs on
	// modern images, but enabling it explicitly keeps this migration correct
	// even against a Postgres image that doesn't preload it.
	if _, err := d.Exec(`CREATE EXTENSION IF NOT EXISTS pgcrypto`); err != nil { //nolint:noctx // matches InitSchema's pre-existing signature
		return fmt.Errorf("delivery schema: enabling pgcrypto: %w", err)
	}
	if _, err := d.Exec(schema); err != nil { //nolint:noctx // matches InitSchema's pre-existing signature
		return fmt.Errorf("delivery schema: %w", err)
	}
	return nil
}

// ─── delivery_targets ──────────────────────────────────────────────────────

// UpsertTarget inserts or updates a target row by its unique name.
func UpsertTarget(ctx context.Context, d *sql.DB, t *models.DeliveryTarget) error {
	params := t.Params
	if params == nil {
		params = json.RawMessage(`{}`)
	}
	_, err := d.ExecContext(ctx, `
		INSERT INTO delivery_targets (name, type, params, credentials_ref)
		VALUES ($1,$2,$3,$4)
		ON CONFLICT (name) DO UPDATE SET
			type=$2, params=$3, credentials_ref=$4, updated_at=NOW()`,
		t.Name, t.Type, params, t.CredentialsRef)
	return err
}

// ListTargets returns every configured delivery target.
func ListTargets(ctx context.Context, d *sql.DB) ([]*models.DeliveryTarget, error) {
	rows, err := d.QueryContext(ctx, `SELECT id,name,type,params,credentials_ref,created_at,updated_at FROM delivery_targets ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []*models.DeliveryTarget
	for rows.Next() {
		t := &models.DeliveryTarget{}
		if err := rows.Scan(&t.ID, &t.Name, &t.Type, &t.Params, &t.CredentialsRef, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// GetTargetByName returns the target with the given name, or (nil, nil) when
// not found.
func GetTargetByName(ctx context.Context, d *sql.DB, name string) (*models.DeliveryTarget, error) {
	t := &models.DeliveryTarget{}
	err := d.QueryRowContext(ctx, `SELECT id,name,type,params,credentials_ref,created_at,updated_at FROM delivery_targets WHERE name=$1`, name).
		Scan(&t.ID, &t.Name, &t.Type, &t.Params, &t.CredentialsRef, &t.CreatedAt, &t.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return t, err
}

// GetTargetByID returns the target with the given id, or (nil, nil) when not found.
func GetTargetByID(ctx context.Context, d *sql.DB, id string) (*models.DeliveryTarget, error) {
	t := &models.DeliveryTarget{}
	err := d.QueryRowContext(ctx, `SELECT id,name,type,params,credentials_ref,created_at,updated_at FROM delivery_targets WHERE id=$1`, id).
		Scan(&t.ID, &t.Name, &t.Type, &t.Params, &t.CredentialsRef, &t.CreatedAt, &t.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return t, err
}

// ─── delivery_bindings ─────────────────────────────────────────────────────

// UpsertBinding inserts or updates a binding for (certificate_name, target_id).
func UpsertBinding(ctx context.Context, d *sql.DB, b *models.DeliveryBinding) error {
	params := b.TargetParams
	if params == nil {
		params = json.RawMessage(`{}`)
	}
	_, err := d.ExecContext(ctx, `
		INSERT INTO delivery_bindings (certificate_name, target_id, target_params, enabled)
		VALUES ($1,$2,$3,$4)
		ON CONFLICT (certificate_name, target_id) DO UPDATE SET
			target_params=$3, enabled=$4, updated_at=NOW()`,
		b.CertificateName, b.TargetID, params, b.Enabled)
	return err
}

// ListEnabledBindings returns every binding with enabled=true, joined with
// its target's type for the reconciler's dispatch.
func ListEnabledBindings(ctx context.Context, d *sql.DB) ([]*models.DeliveryBinding, error) {
	rows, err := d.QueryContext(ctx, `
		SELECT id,certificate_name,target_id,target_params,enabled,consecutive_failures,created_at,updated_at
		FROM delivery_bindings WHERE enabled=TRUE ORDER BY certificate_name`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []*models.DeliveryBinding
	for rows.Next() {
		b := &models.DeliveryBinding{}
		if err := rows.Scan(&b.ID, &b.CertificateName, &b.TargetID, &b.TargetParams, &b.Enabled, &b.ConsecutiveFails, &b.CreatedAt, &b.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// ListAllBindings returns every binding, enabled or not (used by export).
func ListAllBindings(ctx context.Context, d *sql.DB) ([]*models.DeliveryBinding, error) {
	rows, err := d.QueryContext(ctx, `
		SELECT id,certificate_name,target_id,target_params,enabled,consecutive_failures,created_at,updated_at
		FROM delivery_bindings ORDER BY certificate_name`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []*models.DeliveryBinding
	for rows.Next() {
		b := &models.DeliveryBinding{}
		if err := rows.Scan(&b.ID, &b.CertificateName, &b.TargetID, &b.TargetParams, &b.Enabled, &b.ConsecutiveFails, &b.CreatedAt, &b.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// UpdateBindingTargetParams persists a per-binding target_params override
// (used by the aws-acm target to record the stable ARN after first push).
func UpdateBindingTargetParams(ctx context.Context, d *sql.DB, bindingID string, params json.RawMessage) error {
	_, err := d.ExecContext(ctx, `UPDATE delivery_bindings SET target_params=$1, updated_at=NOW() WHERE id=$2`, params, bindingID)
	return err
}

// UpdateBindingFailures persists the binding's consecutive_failures counter.
func UpdateBindingFailures(ctx context.Context, d *sql.DB, bindingID string, consecutiveFailures int) error {
	_, err := d.ExecContext(ctx, `UPDATE delivery_bindings SET consecutive_failures=$1, updated_at=NOW() WHERE id=$2`, consecutiveFailures, bindingID)
	return err
}

// ─── delivery_runs ─────────────────────────────────────────────────────────

// LatestRun returns the most recent run recorded for a binding, or
// (nil, nil) when no run has been recorded yet.
func LatestRun(ctx context.Context, d *sql.DB, bindingID string) (*models.DeliveryRun, error) {
	r := &models.DeliveryRun{}
	var finished sql.NullTime
	err := d.QueryRowContext(ctx, `
		SELECT id,binding_id,fingerprint,started_at,finished_at,result,consecutive_failures
		FROM delivery_runs WHERE binding_id=$1 ORDER BY started_at DESC LIMIT 1`, bindingID).
		Scan(&r.ID, &r.BindingID, &r.Fingerprint, &r.StartedAt, &finished, &r.Result, &r.ConsecutiveFailures)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if finished.Valid {
		r.FinishedAt = finished.Time
	}
	return r, nil
}

// RecordRun inserts a new delivery_runs row.
func RecordRun(ctx context.Context, d *sql.DB, r *models.DeliveryRun) error {
	_, err := d.ExecContext(ctx, `
		INSERT INTO delivery_runs (binding_id, fingerprint, started_at, finished_at, result, consecutive_failures)
		VALUES ($1,$2,$3,$4,$5,$6)`,
		r.BindingID, r.Fingerprint, r.StartedAt, nullableTime(r.FinishedAt), r.Result, r.ConsecutiveFailures)
	return err
}

func nullableTime(t time.Time) sql.NullTime {
	if t.IsZero() {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: t, Valid: true}
}
