package api

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// TestPatchProfileBody_ExplicitJSONNullDecodesToNil mirrors
// TestPatchNotificationsBody_ExplicitJSONNullSMTPPasswordDecodesToNil: an
// explicit JSON null and an absent field must be indistinguishable by the
// time the handler sees patchProfileBody, so a merge-style PATCH never
// confuses "the caller sent null" with "the caller wants to clear this".
func TestPatchProfileBody_ExplicitJSONNullDecodesToNil(t *testing.T) {
	var b patchProfileBody
	if err := json.Unmarshal([]byte(`{"displayName":"Alice","email":null}`), &b); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if b.Email != nil {
		t.Fatalf("Email = %q, want nil (JSON null must decode like an absent field)", *b.Email)
	}
	if b.DisplayName == nil || *b.DisplayName != "Alice" {
		t.Fatalf("DisplayName = %v, want pointer to \"Alice\"", b.DisplayName)
	}
}

func TestPatchProfileBody_OmittedFieldDecodesToNil(t *testing.T) {
	var b patchProfileBody
	if err := json.Unmarshal([]byte(`{"displayName":"Alice"}`), &b); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if b.Email != nil {
		t.Fatalf("Email = %q, want nil (omitted field)", *b.Email)
	}
}

func TestPatchProfilePreferencesBody_OmittedThemeDecodesToNil(t *testing.T) {
	var b patchProfilePreferencesBody
	if err := json.Unmarshal([]byte(`{}`), &b); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if b.Theme != nil {
		t.Fatalf("Theme = %q, want nil (omitted field)", *b.Theme)
	}
}

// TestProfileOperations_DeriveSubjectFromSessionNotFromInput is the
// business-invariant test the task calls for: every profile operation acts
// on the session's own account, never on a caller-supplied id (Section 8
// "Profile": "No id parameter (5.5)", Section 5.5: "Profile operations
// derive the subject from the session and accept no id. A test asserts
// both."). It asserts this at the type level — none of the four
// operations' input structs carry an "id" field or a `path:"id"` tag,
// unlike every admin operation in this package (getUserInput, deleteUserInput,
// updateUserInput) which does — so the only way to compile a client for
// one of these id-free paths is to accept the server resolving the subject
// itself.
func TestProfileOperations_DeriveSubjectFromSessionNotFromInput(t *testing.T) {
	inputs := map[string]reflect.Type{
		"getProfile (no input)":   reflect.TypeOf(struct{}{}),
		"patchProfile":            reflect.TypeOf(patchProfileInput{}),
		"patchProfilePreferences": reflect.TypeOf(patchProfilePreferencesInput{}),
		"changePassword":          reflect.TypeOf(changePasswordInput{}),
	}
	for name, typ := range inputs {
		assertNoIDField(t, name, typ)
	}
}

func assertNoIDField(t *testing.T, name string, typ reflect.Type) {
	t.Helper()
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if strings.EqualFold(f.Name, "id") {
			t.Errorf("%s: input struct has a top-level %q field; profile operations must derive the subject from the session (5.5)", name, f.Name)
		}
		if path, ok := f.Tag.Lookup("path"); ok {
			t.Errorf("%s: field %s carries path tag %q; profile operations must take no path parameter (5.5)", name, f.Name, path)
		}
		// Body fields (DisplayName, Email, Theme, CurrentPassword,
		// NewPassword) are themselves structs; walk one level into Body
		// to catch an id smuggled into the request payload instead of
		// the path.
		if f.Name == "Body" && f.Type.Kind() == reflect.Struct {
			for j := 0; j < f.Type.NumField(); j++ {
				bf := f.Type.Field(j)
				if strings.EqualFold(bf.Name, "id") {
					t.Errorf("%s: Body has a %q field; profile operations must derive the subject from the session (5.5)", name, bf.Name)
				}
			}
		}
	}
}
