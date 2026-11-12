package handlers

import (
	"errors"
	"testing"

	"certharbor/config"
)

// TestUpdateUserAdmin_RejectsSelfTarget is the self-target invariant unit
// test the task calls for: a user must not be able to demote or
// deactivate their own account through the admin PATCH operation. h.db is
// nil, so the self-target check must run and return before any query, or
// this test panics instead of asserting.
func TestUpdateUserAdmin_RejectsSelfTarget(t *testing.T) {
	h := newTestHandler(&config.Config{}, testStore())

	role := "viewer"
	_, err := h.UpdateUserAdmin(nil, "alice", 7, 7, &role, nil)
	if !errors.Is(err, ErrSelfTarget) {
		t.Fatalf("UpdateUserAdmin (role change): got %v, want ErrSelfTarget", err)
	}

	active := false
	_, err = h.UpdateUserAdmin(nil, "alice", 7, 7, nil, &active)
	if !errors.Is(err, ErrSelfTarget) {
		t.Fatalf("UpdateUserAdmin (deactivate): got %v, want ErrSelfTarget", err)
	}
}

// TestDeleteUserAdmin_RejectsSelfTarget mirrors the PATCH test above for
// DELETE /api/v1/users/{id}: the last admin must not be able to delete
// themselves and leave the installation with no admin.
func TestDeleteUserAdmin_RejectsSelfTarget(t *testing.T) {
	h := newTestHandler(&config.Config{}, testStore())

	if err := h.DeleteUserAdmin(nil, "alice", 7, 7); !errors.Is(err, ErrSelfTarget) {
		t.Fatalf("DeleteUserAdmin: got %v, want ErrSelfTarget", err)
	}
}

// TestCreateUserAdmin_InvalidRoleRejected mirrors
// TestUsersPost_InvalidRoleRejected (users_role_test.go) at the new data
// layer: an unrecognised role must be refused before any database call, so
// h.db being nil must not panic.
func TestCreateUserAdmin_InvalidRoleRejected(t *testing.T) {
	h := newTestHandler(&config.Config{}, testStore())

	_, err := h.CreateUserAdmin(nil, "alice", "bob", "Str0ng!Passw0rd", "superuser")
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("CreateUserAdmin with an invalid role: got %v, want a *ValidationError", err)
	}
}

// TestUpdateUserAdmin_RequiresAtLeastOneField proves the PATCH operation
// (covering both role and active, Section 6) rejects an empty body rather
// than silently no-op-ing, which would otherwise look like a successful
// update that changed nothing.
func TestUpdateUserAdmin_RequiresAtLeastOneField(t *testing.T) {
	h := newTestHandler(&config.Config{}, testStore())

	_, err := h.UpdateUserAdmin(nil, "alice", 7, 8, nil, nil)
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("UpdateUserAdmin with no fields set: got %v, want a *ValidationError", err)
	}
}
