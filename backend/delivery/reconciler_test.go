package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"certharbor/models"
)

// ─── fakes ──────────────────────────────────────────────────────────────────

type fakeCerts struct {
	byName map[string]*models.Certificate
}

func (f *fakeCerts) GetByName(_ context.Context, name string) (*models.Certificate, error) {
	return f.byName[name], nil
}

type fakeBindings struct {
	bindings     []*models.DeliveryBinding
	failures     map[string]int
	targetParams map[string]json.RawMessage
}

func (f *fakeBindings) ListEnabled(context.Context) ([]*models.DeliveryBinding, error) {
	return f.bindings, nil
}

func (f *fakeBindings) UpdateFailures(_ context.Context, bindingID string, n int) error {
	if f.failures == nil {
		f.failures = map[string]int{}
	}
	f.failures[bindingID] = n
	return nil
}

func (f *fakeBindings) UpdateTargetParams(_ context.Context, bindingID string, params json.RawMessage) error {
	if f.targetParams == nil {
		f.targetParams = map[string]json.RawMessage{}
	}
	f.targetParams[bindingID] = params
	return nil
}

type fakeTargets struct {
	byID map[string]*models.DeliveryTarget
}

func (f *fakeTargets) GetByID(_ context.Context, id string) (*models.DeliveryTarget, error) {
	return f.byID[id], nil
}

type fakeRuns struct {
	latest   map[string]*models.DeliveryRun
	recorded []*models.DeliveryRun
}

func (f *fakeRuns) Latest(_ context.Context, bindingID string) (*models.DeliveryRun, error) {
	return f.latest[bindingID], nil
}

func (f *fakeRuns) Record(_ context.Context, run *models.DeliveryRun) error {
	f.recorded = append(f.recorded, run)
	if f.latest == nil {
		f.latest = map[string]*models.DeliveryRun{}
	}
	f.latest[run.BindingID] = run
	return nil
}

type fakeMaterial struct {
	fingerprint string
	err         error
}

func (f fakeMaterial) Load(_ context.Context, _ *models.Certificate) (CertMaterial, error) {
	if f.err != nil {
		return CertMaterial{}, f.err
	}
	return CertMaterial{CertPEM: []byte("cert"), KeyPEM: []byte("key"), Fingerprint: f.fingerprint}, nil
}

// fakeTarget lets each test control Push's outcome per call.
type fakeTarget struct {
	name    string
	pushErr error
	calls   int
}

func (f *fakeTarget) Name() string { return f.name }

func (f *fakeTarget) Check(context.Context, string, json.RawMessage) (bool, error) {
	return false, nil
}

func (f *fakeTarget) Push(context.Context, *CertMaterial, json.RawMessage) (PushResult, error) {
	f.calls++
	if f.pushErr != nil {
		return PushResult{}, f.pushErr
	}
	return PushResult{}, nil
}

// ─── pure logic ─────────────────────────────────────────────────────────────

func TestShouldSkipPush(t *testing.T) {
	cases := []struct {
		name        string
		lastRun     *models.DeliveryRun
		fingerprint string
		want        bool
	}{
		{"no prior run", nil, "abc", false},
		{"matching fingerprint and success", &models.DeliveryRun{Fingerprint: "abc", Result: resultSuccess}, "abc", true},
		{"matching fingerprint but failed", &models.DeliveryRun{Fingerprint: "abc", Result: "boom"}, "abc", false},
		{"changed fingerprint", &models.DeliveryRun{Fingerprint: "old", Result: resultSuccess}, "new", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := shouldSkipPush(c.lastRun, c.fingerprint)
			if got != c.want {
				t.Errorf("shouldSkipPush() = %v, want %v", got, c.want)
			}
		})
	}
}

func TestNextConsecutiveFailures(t *testing.T) {
	cases := []struct {
		name    string
		current int
		success bool
		want    int
	}{
		{"success resets to zero", 5, true, 0},
		{"failure increments", 0, false, 1},
		{"repeated failure keeps incrementing", 2, false, 3},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := nextConsecutiveFailures(c.current, c.success)
			if got != c.want {
				t.Errorf("nextConsecutiveFailures(%d, %v) = %d, want %d", c.current, c.success, got, c.want)
			}
		})
	}
}

// ─── Tick ───────────────────────────────────────────────────────────────────

func newTestReconciler(t *testing.T, target Target, bindings *fakeBindings, certs *fakeCerts, targetStore *fakeTargets, runs *fakeRuns) *Reconciler {
	t.Helper()
	reg := NewRegistry()
	reg.Register(target)
	return &Reconciler{
		Certs:              certs,
		Bindings:           bindings,
		Targets:            targetStore,
		Runs:               runs,
		Material:           fakeMaterial{fingerprint: "fp-1"},
		Registry:           reg,
		Audit:              NewAuditor(nil), // nil db: Record no-ops, exercised for its nil-safety
		AlertAfterFailures: 3,
	}
}

func TestTick_SkipsUnchangedBinding(t *testing.T) {
	target := &fakeTarget{name: "aws-acm"}
	binding := &models.DeliveryBinding{ID: "b1", CertificateName: "cert-1", TargetID: "t1", ConsecutiveFails: 0}
	certs := &fakeCerts{byName: map[string]*models.Certificate{"cert-1": {Name: "cert-1"}}}
	targetStore := &fakeTargets{byID: map[string]*models.DeliveryTarget{"t1": {ID: "t1", Type: "aws-acm"}}}
	runs := &fakeRuns{latest: map[string]*models.DeliveryRun{"b1": {Fingerprint: "fp-1", Result: resultSuccess}}}
	bindings := &fakeBindings{bindings: []*models.DeliveryBinding{binding}}

	rec := newTestReconciler(t, target, bindings, certs, targetStore, runs)
	if err := rec.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if target.calls != 0 {
		t.Errorf("expected Push not called for an unchanged fingerprint, got %d calls", target.calls)
	}
}

func TestTick_PushesChangedBinding(t *testing.T) {
	target := &fakeTarget{name: "aws-acm"}
	binding := &models.DeliveryBinding{ID: "b1", CertificateName: "cert-1", TargetID: "t1"}
	certs := &fakeCerts{byName: map[string]*models.Certificate{"cert-1": {Name: "cert-1"}}}
	targetStore := &fakeTargets{byID: map[string]*models.DeliveryTarget{"t1": {ID: "t1", Type: "aws-acm"}}}
	runs := &fakeRuns{latest: map[string]*models.DeliveryRun{"b1": {Fingerprint: "stale", Result: resultSuccess}}}
	bindings := &fakeBindings{bindings: []*models.DeliveryBinding{binding}}

	rec := newTestReconciler(t, target, bindings, certs, targetStore, runs)
	if err := rec.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if target.calls != 1 {
		t.Errorf("expected Push called once for a changed fingerprint, got %d calls", target.calls)
	}
	if bindings.failures["b1"] != 0 {
		t.Errorf("expected consecutive_failures reset to 0 on success, got %d", bindings.failures["b1"])
	}
	if runs.latest["b1"].Result != resultSuccess {
		t.Errorf("expected recorded run result %q, got %q", resultSuccess, runs.latest["b1"].Result)
	}
}

func TestTick_FailureIsolationAndConsecutiveFailures(t *testing.T) {
	failing := &fakeTarget{name: "aws-acm", pushErr: errors.New("push failed")}

	reg := NewRegistry()
	reg.Register(failing)
	secondTarget := &fakeTarget{name: "aws-acm-2"}
	reg.Register(secondTarget)

	bindingFail := &models.DeliveryBinding{ID: "b-fail", CertificateName: "cert-fail", TargetID: "t-fail"}
	bindingOK := &models.DeliveryBinding{ID: "b-ok", CertificateName: "cert-ok", TargetID: "t-ok"}

	certs := &fakeCerts{byName: map[string]*models.Certificate{
		"cert-fail": {Name: "cert-fail"},
		"cert-ok":   {Name: "cert-ok"},
	}}
	targetStore := &fakeTargets{byID: map[string]*models.DeliveryTarget{
		"t-fail": {ID: "t-fail", Type: "aws-acm"},
		"t-ok":   {ID: "t-ok", Type: "aws-acm-2"},
	}}
	runs := &fakeRuns{latest: map[string]*models.DeliveryRun{
		"b-fail": {Fingerprint: "stale", Result: resultSuccess, ConsecutiveFailures: 1},
		"b-ok":   {Fingerprint: "stale", Result: resultSuccess},
	}}
	bindings := &fakeBindings{bindings: []*models.DeliveryBinding{bindingFail, bindingOK}}
	bindingFail.ConsecutiveFails = 1

	rec := &Reconciler{
		Certs:              certs,
		Bindings:           bindings,
		Targets:            targetStore,
		Runs:               runs,
		Material:           fakeMaterial{fingerprint: "fp-new"},
		Registry:           reg,
		Audit:              NewAuditor(nil),
		AlertAfterFailures: 3,
	}

	if err := rec.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	// The failing binding's error must not have stopped the ok binding.
	if secondTarget.calls != 1 {
		t.Errorf("expected the healthy binding's Push to run despite the other binding failing, got %d calls", secondTarget.calls)
	}
	if bindings.failures["b-fail"] != 2 {
		t.Errorf("expected consecutive_failures to increment from 1 to 2, got %d", bindings.failures["b-fail"])
	}
	if bindings.failures["b-ok"] != 0 {
		t.Errorf("expected the healthy binding's consecutive_failures to be 0, got %d", bindings.failures["b-ok"])
	}
	if runs.latest["b-fail"].Result == resultSuccess {
		t.Error("expected the failing binding's run to record a non-success result")
	}
}

func TestTick_DanglingBindingSkipsWithoutError(t *testing.T) {
	target := &fakeTarget{name: "aws-acm"}
	binding := &models.DeliveryBinding{ID: "b1", CertificateName: "missing-cert", TargetID: "t1"}
	certs := &fakeCerts{byName: map[string]*models.Certificate{}} // no cert registered
	targetStore := &fakeTargets{byID: map[string]*models.DeliveryTarget{"t1": {ID: "t1", Type: "aws-acm"}}}
	runs := &fakeRuns{}
	bindings := &fakeBindings{bindings: []*models.DeliveryBinding{binding}}

	rec := newTestReconciler(t, target, bindings, certs, targetStore, runs)
	if err := rec.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if target.calls != 0 {
		t.Errorf("expected no push for a dangling binding, got %d calls", target.calls)
	}
}

func TestNextConsecutiveFailures_MonotonicUnderRepeatedFailure(t *testing.T) {
	n := 0
	for i := 0; i < 5; i++ {
		n = nextConsecutiveFailures(n, false)
	}
	if n != 5 {
		t.Errorf("expected 5 consecutive failures after 5 failed runs, got %d", n)
	}
	if nextConsecutiveFailures(n, true) != 0 {
		t.Error("expected a success to reset the streak to 0 regardless of its length")
	}
}

func TestFingerprintOf_Deterministic(t *testing.T) {
	a := fingerprintOf([]byte("same bytes"))
	b := fingerprintOf([]byte("same bytes"))
	c := fingerprintOf([]byte("different bytes"))
	if a != b {
		t.Error("fingerprintOf is not deterministic for identical input")
	}
	if a == c {
		t.Error("fingerprintOf collided for different input")
	}
}

func TestEffectiveExpiry(t *testing.T) {
	if !effectiveExpiry(&models.Certificate{}).IsZero() {
		t.Error("expected zero time for a certificate with no ExpiresAt")
	}
	when := time.Now().Add(48 * time.Hour)
	if got := effectiveExpiry(&models.Certificate{ExpiresAt: &when}); !got.Equal(when) {
		t.Errorf("effectiveExpiry() = %v, want %v", got, when)
	}
}
