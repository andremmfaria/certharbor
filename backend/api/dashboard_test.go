package api

import (
	"reflect"
	"testing"

	appmw "certharbor/middleware"
)

// TestAggregateSourceTags enforces D10's third mechanical rule: every field
// an aggregate DTO carries names the operation it was independently
// obtainable from, that operation exists, and the aggregate's role is at
// least as high as the source's (so a viewer-role caller of the aggregate
// could have read the same field directly). GET /api/v1/dashboard is the
// first aggregate in this codebase; any aggregate added later is added to
// the aggregates map below rather than getting its own copy of this test.
func TestAggregateSourceTags(t *testing.T) {
	spec := NewForSpec().OpenAPI()

	roleByOpID := map[string]string{}
	knownOpID := map[string]bool{}
	for _, item := range spec.Paths {
		for _, op := range operationsOf(item) {
			knownOpID[op.OperationID] = true
			if role, declared := opRole(op); declared {
				roleByOpID[op.OperationID] = role
			}
		}
	}

	aggregates := map[string]reflect.Type{
		"getDashboard": reflect.TypeOf(dashboardBody{}),
	}

	for aggID, bodyType := range aggregates {
		if !knownOpID[aggID] {
			t.Fatalf("aggregate %s is not a registered operation", aggID)
		}
		aggRole, declared := roleByOpID[aggID]
		if !declared {
			t.Fatalf("aggregate %s has no declared role", aggID)
		}
		checkSourceTags(t, aggID, aggRole, bodyType, knownOpID, roleByOpID)
	}
}

// checkSourceTags walks bodyType's fields. A field typed as a struct with no
// source tag of its own is a nested view (e.g. dashboardBody.CA) and is
// recursed into; every other field must carry a source tag naming a
// registered operation whose role is no higher than the aggregate's own.
func checkSourceTags(t *testing.T, aggID, aggRole string, typ reflect.Type, knownOpID map[string]bool, roleByOpID map[string]string) {
	t.Helper()
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		src, hasSource := field.Tag.Lookup("source")

		if field.Type.Kind() == reflect.Struct && !hasSource {
			checkSourceTags(t, aggID, aggRole, field.Type, knownOpID, roleByOpID)
			continue
		}

		if !hasSource {
			t.Errorf("%s: field %s has no source tag", aggID, field.Name)
			continue
		}
		if !knownOpID[src] {
			t.Errorf("%s: field %s names source %q, which is not a registered operation", aggID, field.Name, src)
			continue
		}
		srcRole, hasRole := roleByOpID[src]
		if !hasRole {
			// Public or optional-auth sources carry no role at all, which is
			// never higher than any aggregate's, so there is nothing to check.
			continue
		}
		if appmw.RoleLevels[aggRole] < appmw.RoleLevels[srcRole] {
			t.Errorf("%s (role %s) composes field %s from %s (role %s): the source requires a higher role than the aggregate (D10 rule 2)",
				aggID, aggRole, field.Name, src, srcRole)
		}
	}
}
