package localstore

import (
	"path/filepath"
	"testing"

	"github.com/amg-rfid/amg-rfid-gateway/internal/cache"
)

// TestUpsertUsers_ProductionSchema runs UpsertUsers against the schema the
// gateway actually creates (cache.NewSQLite), not a test-only copy.
func TestUpsertUsers_ProductionSchema(t *testing.T) {
	c, err := cache.NewSQLite(filepath.Join(t.TempDir(), "cache.db"))
	if err != nil {
		t.Fatalf("failed to open cache: %v", err)
	}
	defer c.Close()

	store := New(c.GetDB())

	// Users without a tag arrive as rfid_tag "" from the backend.
	if err := store.UpsertUsers([]User{
		{ID: 1, CompanyID: "amg", Name: "Admin", Role: "admin", Active: true},
		{ID: 2, CompanyID: "amg", Name: "Operator", Role: "operator", Active: true},
	}); err != nil {
		t.Fatalf("initial upsert failed: %v", err)
	}

	// Re-sync updates the same users by id instead of duplicating them.
	if err := store.UpsertUsers([]User{
		{ID: 1, CompanyID: "amg", Name: "Admin Renamed", Role: "admin", RFIDTag: "TAG-001", Active: false},
	}); err != nil {
		t.Fatalf("update upsert failed: %v", err)
	}

	count, err := store.GetUsersCount()
	if err != nil {
		t.Fatalf("count failed: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 users, got %d", count)
	}

	user, err := store.GetUserByRFIDTag("TAG-001")
	if err != nil {
		t.Fatalf("lookup failed: %v", err)
	}
	if user == nil || user.ID != 1 || user.Name != "Admin Renamed" || user.Active {
		t.Errorf("expected updated user 1 inactive with new name, got %+v", user)
	}
}
