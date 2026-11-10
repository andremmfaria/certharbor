package delivery

import (
	"context"
	"database/sql"
	"encoding/json"

	appdb "certharbor/db"
	"certharbor/models"
)

// dbCertInventory adapts appdb's certificate reads to CertificateInventory.
type dbCertInventory struct{ db *sql.DB }

// NewCertInventory adapts a *sql.DB to CertificateInventory.
func NewCertInventory(db *sql.DB) CertificateInventory { return dbCertInventory{db: db} }

func (a dbCertInventory) GetByName(_ context.Context, name string) (*models.Certificate, error) {
	return appdb.GetCertByName(a.db, name)
}

// dbBindingStore adapts appdb's delivery_bindings functions to BindingStore.
type dbBindingStore struct{ db *sql.DB }

// NewBindingStore adapts a *sql.DB to BindingStore.
func NewBindingStore(db *sql.DB) BindingStore { return dbBindingStore{db: db} }

func (a dbBindingStore) ListEnabled(ctx context.Context) ([]*models.DeliveryBinding, error) {
	return appdb.ListEnabledBindings(ctx, a.db)
}

func (a dbBindingStore) UpdateFailures(ctx context.Context, bindingID string, n int) error {
	return appdb.UpdateBindingFailures(ctx, a.db, bindingID, n)
}

func (a dbBindingStore) UpdateTargetParams(ctx context.Context, bindingID string, params json.RawMessage) error {
	return appdb.UpdateBindingTargetParams(ctx, a.db, bindingID, params)
}

// dbTargetStore adapts appdb's delivery_targets functions to TargetStore.
type dbTargetStore struct{ db *sql.DB }

// NewTargetStore adapts a *sql.DB to TargetStore.
func NewTargetStore(db *sql.DB) TargetStore { return dbTargetStore{db: db} }

func (a dbTargetStore) GetByID(ctx context.Context, id string) (*models.DeliveryTarget, error) {
	return appdb.GetTargetByID(ctx, a.db, id)
}

// dbRunStore adapts appdb's delivery_runs functions to RunStore.
type dbRunStore struct{ db *sql.DB }

// NewRunStore adapts a *sql.DB to RunStore.
func NewRunStore(db *sql.DB) RunStore { return dbRunStore{db: db} }

func (a dbRunStore) Latest(ctx context.Context, bindingID string) (*models.DeliveryRun, error) {
	return appdb.LatestRun(ctx, a.db, bindingID)
}

func (a dbRunStore) Record(ctx context.Context, run *models.DeliveryRun) error {
	return appdb.RecordRun(ctx, a.db, run)
}
