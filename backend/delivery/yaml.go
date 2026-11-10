package delivery

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	appdb "certharbor/db"
	"certharbor/models"

	"gopkg.in/yaml.v3"
)

// RegistryDocument is the YAML shape from plans/distribution.md Section 5.
// It is a point-in-time snapshot an operator can diff and review, never the
// live source of truth: the database stays authoritative (Section 5).
type RegistryDocument struct {
	Targets  []TargetEntry  `yaml:"targets"`
	Bindings []BindingEntry `yaml:"bindings"`
}

// TargetEntry is one exported/imported target. credentials_ref is
// deliberately absent: credential material (and the pointer to it) never
// leaves this plane's own credential store (Section 5: "minus credential
// material, which never leaves the credential store").
type TargetEntry struct {
	Name   string          `yaml:"name"`
	Type   string          `yaml:"type"`
	Params json.RawMessage `yaml:"params,omitempty"`
}

// BindingEntry is one exported/imported binding.
type BindingEntry struct {
	Certificate string          `yaml:"certificate"`
	Target      string          `yaml:"target"`
	Enabled     bool            `yaml:"enabled"`
	Params      json.RawMessage `yaml:"params,omitempty"`
}

// Export reads the current delivery_targets/delivery_bindings rows and
// returns them in the YAML shape, with credentials_ref stripped.
func Export(ctx context.Context, db *sql.DB) (*RegistryDocument, error) {
	targets, err := appdb.ListTargets(ctx, db)
	if err != nil {
		return nil, fmt.Errorf("listing targets: %w", err)
	}
	byID := make(map[string]*models.DeliveryTarget, len(targets))
	doc := &RegistryDocument{}
	for _, t := range targets {
		byID[t.ID] = t
		doc.Targets = append(doc.Targets, TargetEntry{Name: t.Name, Type: t.Type, Params: t.Params})
	}

	bindings, err := appdb.ListAllBindings(ctx, db)
	if err != nil {
		return nil, fmt.Errorf("listing bindings: %w", err)
	}
	for _, b := range bindings {
		t, ok := byID[b.TargetID]
		if !ok {
			continue // dangling target reference; nothing sane to export it against
		}
		doc.Bindings = append(doc.Bindings, BindingEntry{
			Certificate: b.CertificateName,
			Target:      t.Name,
			Enabled:     b.Enabled,
			Params:      b.TargetParams,
		})
	}
	return doc, nil
}

// MarshalYAML serializes a RegistryDocument with the header comment shown
// in plans/distribution.md Section 5.
func MarshalYAML(doc *RegistryDocument) ([]byte, error) {
	body, err := yaml.Marshal(doc)
	if err != nil {
		return nil, err
	}
	header := []byte("# certharbor-delivery registry export/import — NOT the live source of truth\n")
	return append(header, body...), nil
}

// UnmarshalYAML parses a registry YAML file's bytes.
func UnmarshalYAML(data []byte) (*RegistryDocument, error) {
	doc := &RegistryDocument{}
	if err := yaml.Unmarshal(data, doc); err != nil {
		return nil, err
	}
	return doc, nil
}

// ImportDiff describes the operations Import would apply, computed before
// anything is written, so the CLI can log precisely what changed.
type ImportDiff struct {
	TargetsCreated  []string
	TargetsUpdated  []string
	BindingsCreated []string
	BindingsUpdated []string
}

func (d ImportDiff) String() string {
	return fmt.Sprintf("targets: +%d created, %d updated; bindings: +%d created, %d updated",
		len(d.TargetsCreated), len(d.TargetsUpdated), len(d.BindingsCreated), len(d.BindingsUpdated))
}

// Import diffs doc against the database and applies the difference as an
// explicit operation (Section 5: "it does not make the file authoritative
// or watch it for changes"). Targets in the file that already exist by
// name are updated in place (type/params); targets absent from the file
// are left untouched, never deleted, since credentials_ref (not exported)
// must survive re-import.
func Import(ctx context.Context, db *sql.DB, doc *RegistryDocument, audit *Auditor) (ImportDiff, error) {
	var diff ImportDiff

	existingTargets, err := appdb.ListTargets(ctx, db)
	if err != nil {
		return diff, fmt.Errorf("listing existing targets: %w", err)
	}
	existingByName := make(map[string]*models.DeliveryTarget, len(existingTargets))
	for _, t := range existingTargets {
		existingByName[t.Name] = t
	}

	for _, te := range doc.Targets {
		existing, ok := existingByName[te.Name]
		target := &models.DeliveryTarget{Name: te.Name, Type: te.Type, Params: te.Params}
		if ok {
			target.CredentialsRef = existing.CredentialsRef // never clobber a credential pointer the file doesn't carry
			diff.TargetsUpdated = append(diff.TargetsUpdated, te.Name)
		} else {
			diff.TargetsCreated = append(diff.TargetsCreated, te.Name)
		}
		if err := appdb.UpsertTarget(ctx, db, target); err != nil {
			return diff, fmt.Errorf("upserting target %q: %w", te.Name, err)
		}
	}

	// Re-read so newly created targets have IDs to bind against.
	refreshedTargets, err := appdb.ListTargets(ctx, db)
	if err != nil {
		return diff, fmt.Errorf("re-listing targets: %w", err)
	}
	idByName := make(map[string]string, len(refreshedTargets))
	for _, t := range refreshedTargets {
		idByName[t.Name] = t.ID
	}

	existingBindings, err := appdb.ListAllBindings(ctx, db)
	if err != nil {
		return diff, fmt.Errorf("listing existing bindings: %w", err)
	}
	type bindingKey struct{ cert, target string }
	existingBindingKeys := make(map[bindingKey]bool, len(existingBindings))
	for _, b := range existingBindings {
		existingBindingKeys[bindingKey{cert: b.CertificateName, target: b.TargetID}] = true
	}

	for _, be := range doc.Bindings {
		targetID, ok := idByName[be.Target]
		if !ok {
			return diff, fmt.Errorf("binding for certificate %q references unknown target %q", be.Certificate, be.Target)
		}
		binding := &models.DeliveryBinding{
			CertificateName: be.Certificate,
			TargetID:        targetID,
			TargetParams:    be.Params,
			Enabled:         be.Enabled,
		}
		key := bindingKey{cert: be.Certificate, target: targetID}
		label := fmt.Sprintf("%s -> %s", be.Certificate, be.Target)
		if existingBindingKeys[key] {
			diff.BindingsUpdated = append(diff.BindingsUpdated, label)
		} else {
			diff.BindingsCreated = append(diff.BindingsCreated, label)
		}
		if err := appdb.UpsertBinding(ctx, db, binding); err != nil {
			return diff, fmt.Errorf("upserting binding %s: %w", label, err)
		}
	}

	if audit != nil {
		audit.Record(true, fmt.Sprintf("delivery registry import applied: %s", diff.String()))
	}
	return diff, nil
}
