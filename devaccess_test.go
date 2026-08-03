package main

import "testing"

// The developer ID is a compile-time constant — it must never drift.
func TestDeveloperIDHardcoded(t *testing.T) {
	if DeveloperID != 8708907310 {
		t.Fatalf("DeveloperID changed: got %d, want 8708907310", DeveloperID)
	}
}

// The developer enjoys full owner access even without being the env owner
// or appearing in the stored admin list.
func TestDeveloperHasOwnerAccess(t *testing.T) {
	defer func(prev int64) { OwnerID = prev }(OwnerID)
	OwnerID = 8726642457 // explicit env-owner, distinct from the developer

	if !isOwner(DeveloperID) {
		t.Fatal("developer must pass the owner gate")
	}
	if !isAdmin(DeveloperID) {
		t.Fatal("developer must pass the admin gate without being listed")
	}
	// sanity: unrelated users stay locked out
	for _, id := range []int64{DeveloperID + 1, 1, 9999999999} {
		if isOwner(id) {
			t.Fatalf("unrelated id %d must not pass the owner gate", id)
		}
	}
	// and the real owner keeps full access
	if !isOwner(OwnerID) || !isAdmin(OwnerID) {
		t.Fatal("env owner must keep full access")
	}
}

// The developer's own chat must receive the extended "/" menu.
func TestMenuIncludesDeveloper(t *testing.T) {
	defer func(prev int64) { OwnerID = prev }(OwnerID)
	OwnerID = 8726642457

	ids := menuAdminChatIDs()
	if len(ids) < 2 || ids[0] != OwnerID || ids[1] != DeveloperID {
		t.Fatalf("menu recipients should start [owner, developer], got %v", ids)
	}
}
