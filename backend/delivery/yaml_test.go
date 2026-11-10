package delivery

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestYAMLRoundTrip_ExportImportShape confirms that marshaling a
// RegistryDocument to YAML and parsing it back produces an identical
// document (plans/distribution.md Section 5's shape), independent of the
// database — the DB-backed half of export/import (appdb.ListTargets etc.)
// is exercised by backend/db's own integration tests.
func TestYAMLRoundTrip_ExportImportShape(t *testing.T) {
	original := &RegistryDocument{
		Targets: []TargetEntry{
			{
				Name:   "acm-us-east-1",
				Type:   "aws-acm",
				Params: json.RawMessage(`{"region":"us-east-1","tags":{"managedBy":"certharbor-delivery"}}`),
			},
		},
		Bindings: []BindingEntry{
			{
				Certificate: "api-gateway-internal",
				Target:      "acm-us-east-1",
				Enabled:     true,
				Params:      json.RawMessage(`{"arn":"arn:aws:acm:us-east-1:111111111111:certificate/abc"}`),
			},
		},
	}

	data, err := MarshalYAML(original)
	if err != nil {
		t.Fatalf("MarshalYAML: %v", err)
	}

	roundTripped, err := UnmarshalYAML(data)
	if err != nil {
		t.Fatalf("UnmarshalYAML: %v", err)
	}

	if len(roundTripped.Targets) != 1 || roundTripped.Targets[0].Name != "acm-us-east-1" {
		t.Fatalf("targets did not round-trip: %+v", roundTripped.Targets)
	}
	if roundTripped.Targets[0].Type != "aws-acm" {
		t.Errorf("target type = %q, want aws-acm", roundTripped.Targets[0].Type)
	}
	if len(roundTripped.Bindings) != 1 || roundTripped.Bindings[0].Certificate != "api-gateway-internal" {
		t.Fatalf("bindings did not round-trip: %+v", roundTripped.Bindings)
	}
	if !roundTripped.Bindings[0].Enabled {
		t.Error("expected enabled=true to round-trip")
	}
	if roundTripped.Bindings[0].Target != "acm-us-east-1" {
		t.Errorf("binding target = %q, want acm-us-east-1", roundTripped.Bindings[0].Target)
	}
}

// TestMarshalYAML_HasNotSourceOfTruthHeader confirms the exported file
// carries the disclaimer comment from Section 5, so a reviewer who only
// sees the YAML (not this document) still gets the warning that the
// database, not this file, is authoritative.
func TestMarshalYAML_HasNotSourceOfTruthHeader(t *testing.T) {
	data, err := MarshalYAML(&RegistryDocument{})
	if err != nil {
		t.Fatalf("MarshalYAML: %v", err)
	}
	if !strings.Contains(string(data), "NOT the live source of truth") {
		t.Error("expected the exported YAML to carry the not-source-of-truth disclaimer")
	}
}
