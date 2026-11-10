package delivery

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"time"

	"certharbor/models"
)

// resultSuccess/resultFailurePrefix mirror delivery_runs.result's shape from
// plans/distribution.md Section 3: "success" or an error message.
const resultSuccess = "success"

// CertificateInventory is the read-only view onto certharbor-control's
// existing certificate inventory this plane reconciles against (Section 3:
// "certharbor-delivery reads that inventory; it does not duplicate or
// shadow it").
type CertificateInventory interface {
	GetByName(ctx context.Context, name string) (*models.Certificate, error)
}

// BindingStore is the subset of delivery_bindings operations the
// reconciler needs.
type BindingStore interface {
	ListEnabled(ctx context.Context) ([]*models.DeliveryBinding, error)
	UpdateFailures(ctx context.Context, bindingID string, consecutiveFailures int) error
	UpdateTargetParams(ctx context.Context, bindingID string, params json.RawMessage) error
}

// TargetStore is the subset of delivery_targets operations the reconciler needs.
type TargetStore interface {
	GetByID(ctx context.Context, id string) (*models.DeliveryTarget, error)
}

// RunStore is the subset of delivery_runs operations the reconciler needs.
type RunStore interface {
	Latest(ctx context.Context, bindingID string) (*models.DeliveryRun, error)
	Record(ctx context.Context, run *models.DeliveryRun) error
}

// MaterialLoader reads the certificate/chain/key material for a bound
// certharbor-control certificate record and computes its fingerprint. It is
// an interface so tests can substitute fixed material instead of touching
// the filesystem.
type MaterialLoader interface {
	Load(ctx context.Context, cert *models.Certificate) (CertMaterial, error)
}

// Reconciler runs the Section 6 reconciliation loop.
type Reconciler struct {
	Certs    CertificateInventory
	Bindings BindingStore
	Targets  TargetStore
	Runs     RunStore
	Material MaterialLoader
	Registry *Registry
	Audit    *Auditor

	// AlertAfterFailures mirrors cert-distributor's default of 3 (Section 10).
	AlertAfterFailures int
}

// Tick runs one reconciliation pass over every enabled binding. A failure
// reconciling one binding is isolated: it is logged, audited and recorded
// against that binding only, and never stops the remaining bindings in the
// same tick (Section 6: "A failure on one binding never blocks any other
// binding's push").
func (r *Reconciler) Tick(ctx context.Context) error {
	bindings, err := r.Bindings.ListEnabled(ctx)
	if err != nil {
		return fmt.Errorf("listing enabled bindings: %w", err)
	}
	for _, b := range bindings {
		r.reconcileOneSafe(ctx, b)
	}
	return nil
}

// reconcileOneSafe wraps reconcileOne with a recover so a programming bug
// in one target's Check/Push can't crash the whole worker process either,
// which is the same isolation property Section 6 asks for, extended to
// panics and not just returned errors.
func (r *Reconciler) reconcileOneSafe(ctx context.Context, b *models.DeliveryBinding) {
	defer func() {
		if p := recover(); p != nil {
			slog.Error("certharbor-delivery: binding reconciliation panicked", "binding_id", b.ID, "panic", p)
		}
	}()
	if err := r.reconcileOne(ctx, b); err != nil {
		slog.Warn("certharbor-delivery: binding reconciliation error", "binding_id", b.ID, "err", err)
	}
}

func (r *Reconciler) reconcileOne(ctx context.Context, b *models.DeliveryBinding) error {
	cert, err := r.Certs.GetByName(ctx, b.CertificateName)
	if err != nil {
		return fmt.Errorf("looking up certificate %q: %w", b.CertificateName, err)
	}
	if cert == nil {
		// Unbound dangling binding: alert, do not push (Section 6).
		slog.Warn("certharbor-delivery: binding references unknown certificate, skipping", "binding_id", b.ID, "certificate_name", b.CertificateName)
		return nil
	}

	target, err := r.Targets.GetByID(ctx, b.TargetID)
	if err != nil {
		return fmt.Errorf("looking up target %s: %w", b.TargetID, err)
	}
	if target == nil {
		slog.Warn("certharbor-delivery: binding references unknown target, skipping", "binding_id", b.ID, "target_id", b.TargetID)
		return nil
	}

	material, err := r.Material.Load(ctx, cert)
	if err != nil {
		return fmt.Errorf("loading certificate material for %q: %w", cert.Name, err)
	}

	if daysRemaining := time.Until(effectiveExpiry(cert)).Hours() / 24; cert.ExpiresAt != nil {
		daysRemainingMetric(b.ID, b.CertificateName, daysRemaining)
	}

	lastRun, err := r.Runs.Latest(ctx, b.ID)
	if err != nil {
		return fmt.Errorf("loading last run for binding %s: %w", b.ID, err)
	}
	if shouldSkipPush(lastRun, material.Fingerprint) {
		return nil
	}

	impl, ok := r.Registry.Get(target.Type)
	if !ok {
		return fmt.Errorf("no target implementation registered for type %q", target.Type)
	}

	started := time.Now()
	pushParams := b.TargetParams
	result, pushErr := impl.Push(ctx, &material, pushParams)

	run := &models.DeliveryRun{
		BindingID:   b.ID,
		Fingerprint: material.Fingerprint,
		StartedAt:   started,
		FinishedAt:  time.Now(),
	}

	newFailures := nextConsecutiveFailures(b.ConsecutiveFails, pushErr == nil)
	if pushErr != nil {
		run.Result = pushErr.Error()
		r.Audit.Record(false, fmt.Sprintf("delivery push failed: binding=%s target=%s cert=%s err=%v", b.ID, target.Name, cert.Name, pushErr))
		pushResultMetric(b.ID, target.Type, "error")
	} else {
		run.Result = resultSuccess
		r.Audit.Record(true, fmt.Sprintf("delivery push succeeded: binding=%s target=%s cert=%s", b.ID, target.Name, cert.Name))
		pushResultMetric(b.ID, target.Type, resultSuccess)
		if result.UpdatedParams != nil {
			if err := r.Bindings.UpdateTargetParams(ctx, b.ID, result.UpdatedParams); err != nil {
				slog.Warn("certharbor-delivery: persisting updated target_params failed", "binding_id", b.ID, "err", err)
			}
		}
	}
	run.ConsecutiveFailures = newFailures
	consecutiveFailuresMetric(b.ID, newFailures, r.AlertAfterFailures)

	if err := r.Runs.Record(ctx, run); err != nil {
		slog.Warn("certharbor-delivery: recording delivery run failed", "binding_id", b.ID, "err", err)
	}
	if err := r.Bindings.UpdateFailures(ctx, b.ID, newFailures); err != nil {
		slog.Warn("certharbor-delivery: updating binding failure count failed", "binding_id", b.ID, "err", err)
	}
	return nil
}

// shouldSkipPush implements Section 6's no-op check: a binding whose last
// recorded run already pushed the certificate's current fingerprint
// successfully needs no further action this tick.
func shouldSkipPush(lastRun *models.DeliveryRun, fingerprint string) bool {
	if lastRun == nil {
		return false
	}
	return lastRun.Fingerprint == fingerprint && lastRun.Result == resultSuccess
}

// nextConsecutiveFailures carries a binding's failure streak forward across
// runs (Section 3: "carried forward across runs for the same binding, reset
// to 0 on success").
func nextConsecutiveFailures(current int, success bool) int {
	if success {
		return 0
	}
	return current + 1
}

// effectiveExpiry returns cert.ExpiresAt dereferenced, or the zero time
// when unset, so days-remaining metrics degrade rather than panic on a
// certificate record whose expiry hasn't been populated yet.
func effectiveExpiry(cert *models.Certificate) time.Time {
	if cert.ExpiresAt == nil {
		return time.Time{}
	}
	return *cert.ExpiresAt
}

// FileMaterialLoader reads cert/chain/key PEM from the paths certharbor-
// control already recorded on the certificate row (Section 2: "it does not
// introduce a second certificate store").
type FileMaterialLoader struct{}

// Load reads CertPath/KeyPath from disk and computes the fingerprint
// certharbor-delivery compares against delivery_runs.fingerprint.
func (FileMaterialLoader) Load(_ context.Context, cert *models.Certificate) (CertMaterial, error) {
	certPEM, err := os.ReadFile(cert.CertPath) //nolint:gosec // path is operator-controlled, from certharbor's own inventory
	if err != nil {
		return CertMaterial{}, fmt.Errorf("reading cert file %s: %w", cert.CertPath, err)
	}
	var keyPEM []byte
	if cert.KeyPath != "" {
		keyPEM, err = os.ReadFile(cert.KeyPath) //nolint:gosec // path is operator-controlled, from certharbor's own inventory
		if err != nil {
			return CertMaterial{}, fmt.Errorf("reading key file %s: %w", cert.KeyPath, err)
		}
	}
	return CertMaterial{
		CertPEM:     certPEM,
		KeyPEM:      keyPEM,
		Fingerprint: fingerprintOf(certPEM),
	}, nil
}

// fingerprintOf computes the SHA-256 of a certificate's PEM bytes, the
// fingerprint stored in delivery_runs.fingerprint (Section 3).
func fingerprintOf(certPEM []byte) string {
	sum := sha256.Sum256(certPEM)
	return hex.EncodeToString(sum[:])
}
