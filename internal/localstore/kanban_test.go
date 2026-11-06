package localstore

import (
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/amg-rfid/amg-rfid-gateway/internal/cache"
)

// setupKanbanStore opens the schema the gateway really creates (cache.NewSQLite),
// so the tests cover the production migration and not a test-only DDL copy.
func setupKanbanStore(t *testing.T) *LocalStore {
	t.Helper()
	c, err := cache.NewSQLite(filepath.Join(t.TempDir(), "cache.db"))
	if err != nil {
		t.Fatalf("failed to open cache: %v", err)
	}
	t.Cleanup(func() { c.Close() })
	return New(c.GetDB())
}

func fordKanban() Kanban {
	return Kanban{
		Code:         "EXP758SF M16",
		Description:  "Burlete frontal",
		StandardPack: 250,
		Unit:         "PZ",
		Zones:        []string{"C", "A"},
		Extrusora:    "M16",
		Position:     "A4",
		Area:         "RAMFG",
		Customer:     "Ford",
		CardType:     "produccion",
		SKU:          "EXP758SF",
	}
}

func syncItem(id int64, uii string, active bool, k *Kanban) SyncDataItem {
	return SyncDataItem{ID: id, ToolID: id, UII: uii, SKU: "SKU-" + uii, Name: "Tool " + uii, Status: "active", Active: active, Kanban: k}
}

func TestApplyKanbanLinks_LinksActiveTagAndExposesAllFields(t *testing.T) {
	store := setupKanbanStore(t)
	k := fordKanban()

	if err := store.ApplyKanbanLinks([]SyncDataItem{syncItem(1, "A0000001", true, &k)}); err != nil {
		t.Fatalf("ApplyKanbanLinks failed: %v", err)
	}

	got, err := store.GetKanbanByUII("A0000001")
	if err != nil {
		t.Fatalf("GetKanbanByUII failed: %v", err)
	}
	if got == nil {
		t.Fatal("expected kanban for linked tag, got nil")
	}
	if !reflect.DeepEqual(*got, k) {
		t.Errorf("kanban mismatch:\n got  %+v\n want %+v", *got, k)
	}
}

func TestApplyKanbanLinks_NoKanbanLeavesTagUnassigned(t *testing.T) {
	store := setupKanbanStore(t)

	// Old payloads carry no kanban fields at all.
	if err := store.ApplyKanbanLinks([]SyncDataItem{syncItem(1, "A0000001", true, nil)}); err != nil {
		t.Fatalf("ApplyKanbanLinks failed: %v", err)
	}

	got, err := store.GetKanbanByUII("A0000001")
	if err != nil {
		t.Fatalf("GetKanbanByUII failed: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil kanban for tag without link, got %+v", got)
	}
}

func TestApplyKanbanLinks_UnknownUIIReturnsNil(t *testing.T) {
	store := setupKanbanStore(t)

	got, err := store.GetKanbanByUII("NOPE")
	if err != nil || got != nil {
		t.Errorf("expected (nil, nil) for unknown UII, got (%+v, %v)", got, err)
	}
}

func TestApplyKanbanLinks_KanbanNullClearsStaleLink(t *testing.T) {
	store := setupKanbanStore(t)
	k := fordKanban()

	if err := store.ApplyKanbanLinks([]SyncDataItem{syncItem(1, "A0000001", true, &k)}); err != nil {
		t.Fatalf("first apply failed: %v", err)
	}
	// Next sync: same tag now comes back with kanban:null.
	if err := store.ApplyKanbanLinks([]SyncDataItem{syncItem(1, "A0000001", true, nil)}); err != nil {
		t.Fatalf("second apply failed: %v", err)
	}

	got, err := store.GetKanbanByUII("A0000001")
	if err != nil {
		t.Fatalf("GetKanbanByUII failed: %v", err)
	}
	if got != nil {
		t.Errorf("expected stale link cleared by kanban:null, got %+v", got)
	}
}

func TestApplyKanbanLinks_InactiveTagClearsLinkEvenWithKanban(t *testing.T) {
	store := setupKanbanStore(t)
	k := fordKanban()

	if err := store.ApplyKanbanLinks([]SyncDataItem{syncItem(1, "A0000001", true, &k)}); err != nil {
		t.Fatalf("first apply failed: %v", err)
	}
	if err := store.ApplyKanbanLinks([]SyncDataItem{syncItem(1, "A0000001", false, &k)}); err != nil {
		t.Fatalf("second apply failed: %v", err)
	}

	got, err := store.GetKanbanByUII("A0000001")
	if err != nil {
		t.Fatalf("GetKanbanByUII failed: %v", err)
	}
	if got != nil {
		t.Errorf("expected inactive tag to have no active link, got %+v", got)
	}
}

func TestApplyKanbanLinks_UpdatesKanbanFieldsAndRelinks(t *testing.T) {
	store := setupKanbanStore(t)
	k := fordKanban()

	if err := store.ApplyKanbanLinks([]SyncDataItem{syncItem(1, "A0000001", true, &k)}); err != nil {
		t.Fatalf("first apply failed: %v", err)
	}

	// Cloud edited the kanban and another tag now points to a different one.
	k.Description = "Burlete frontal v2"
	k.StandardPack = 300
	other := Kanban{Code: "TOY-001", Description: "Sello", StandardPack: 10, Unit: "KG", Zones: []string{"H"}, SKU: "TOY001"}
	if err := store.ApplyKanbanLinks([]SyncDataItem{
		syncItem(1, "A0000001", true, &k),
		syncItem(2, "A0000002", true, &other),
	}); err != nil {
		t.Fatalf("second apply failed: %v", err)
	}

	got, _ := store.GetKanbanByUII("A0000001")
	if got == nil || got.Description != "Burlete frontal v2" || got.StandardPack != 300 {
		t.Errorf("expected updated kanban fields, got %+v", got)
	}
	got2, _ := store.GetKanbanByUII("A0000002")
	if got2 == nil || got2.Code != "TOY-001" || got2.Unit != "KG" {
		t.Errorf("expected second tag linked to TOY-001, got %+v", got2)
	}
}

func TestApplyKanbanLinks_ManyTagsShareOneKanban(t *testing.T) {
	store := setupKanbanStore(t)
	k := fordKanban()

	if err := store.ApplyKanbanLinks([]SyncDataItem{
		syncItem(1, "A0000001", true, &k),
		syncItem(2, "A0000002", true, &k),
	}); err != nil {
		t.Fatalf("apply failed: %v", err)
	}

	for _, uii := range []string{"A0000001", "A0000002"} {
		got, err := store.GetKanbanByUII(uii)
		if err != nil || got == nil || got.Code != k.Code {
			t.Errorf("expected %s linked to %s, got (%+v, %v)", uii, k.Code, got, err)
		}
	}
	list, err := store.ListKanbans()
	if err != nil || len(list) != 1 {
		t.Errorf("expected a single catalog row for the shared kanban, got (%d, %v)", len(list), err)
	}
}

func TestApplyKanbanLinks_EmptyCodeIsNotALink(t *testing.T) {
	store := setupKanbanStore(t)
	blank := Kanban{Code: "  ", Description: "broken"}

	if err := store.ApplyKanbanLinks([]SyncDataItem{syncItem(1, "A0000001", true, &blank)}); err != nil {
		t.Fatalf("apply failed: %v", err)
	}

	got, _ := store.GetKanbanByUII("A0000001")
	if got != nil {
		t.Errorf("expected kanban with blank code to be ignored, got %+v", got)
	}
}

func TestReplaceKanbanCatalog_ListsSortedAndReplacesUnlinkedRows(t *testing.T) {
	store := setupKanbanStore(t)

	if err := store.ReplaceKanbanCatalog([]Kanban{
		{Code: "B-2", Description: "second", Zones: []string{"A"}},
		{Code: "A-1", Description: "first"},
		{Code: "C-3", Description: "third"},
	}); err != nil {
		t.Fatalf("ReplaceKanbanCatalog failed: %v", err)
	}
	list, err := store.ListKanbans()
	if err != nil {
		t.Fatalf("ListKanbans failed: %v", err)
	}
	codes := []string{}
	for _, k := range list {
		codes = append(codes, k.Code)
	}
	if !reflect.DeepEqual(codes, []string{"A-1", "B-2", "C-3"}) {
		t.Errorf("expected codes sorted A-1,B-2,C-3 got %v", codes)
	}
	if list[1].Zones == nil || len(list[1].Zones) != 1 || list[1].Zones[0] != "A" {
		t.Errorf("expected zones preserved, got %+v", list[1].Zones)
	}
	if list[0].Zones == nil {
		t.Errorf("expected empty non-nil zones so JSON renders [], got nil")
	}

	// A later catalog without C-3 drops it.
	if err := store.ReplaceKanbanCatalog([]Kanban{{Code: "A-1", Description: "first v2"}}); err != nil {
		t.Fatalf("second replace failed: %v", err)
	}
	list, _ = store.ListKanbans()
	if len(list) != 1 || list[0].Code != "A-1" || list[0].Description != "first v2" {
		t.Errorf("expected only A-1 (updated) after replace, got %+v", list)
	}
}

func TestReplaceKanbanCatalog_KeepsKanbanStillLinkedToATag(t *testing.T) {
	store := setupKanbanStore(t)
	k := fordKanban()
	if err := store.ApplyKanbanLinks([]SyncDataItem{syncItem(1, "A0000001", true, &k)}); err != nil {
		t.Fatalf("apply failed: %v", err)
	}

	// Catalog fetched later does not list the linked kanban (e.g. paginated/filtered).
	if err := store.ReplaceKanbanCatalog([]Kanban{{Code: "OTHER"}}); err != nil {
		t.Fatalf("replace failed: %v", err)
	}

	got, _ := store.GetKanbanByUII("A0000001")
	if got == nil || got.Code != k.Code {
		t.Errorf("expected linked kanban to survive catalog replace, got %+v", got)
	}
}

func TestAssignKanban_LinksTagStoresKanbanAndLeavesTheInbox(t *testing.T) {
	store := setupKanbanStore(t)
	k := fordKanban()
	store.UpsertUnassignedTag("A0000001", "ant-1", time.Now(), 3)
	store.UpsertUnassignedTag("A0000002", "ant-1", time.Now(), 1)

	if err := store.AssignKanban("A0000001", k); err != nil {
		t.Fatalf("AssignKanban failed: %v", err)
	}

	got, err := store.GetKanbanByUII("A0000001")
	if err != nil || got == nil || !reflect.DeepEqual(*got, k) {
		t.Errorf("expected tag linked to kanban, got (%+v, %v)", got, err)
	}
	pending, _ := store.ListUnassignedTags()
	if len(pending) != 1 || pending[0].EPC != "A0000002" {
		t.Errorf("expected only A0000002 left in the inbox, got %+v", pending)
	}
	var rows int
	store.db.QueryRow(`SELECT COUNT(*) FROM unassigned_tags WHERE epc = 'A0000001'`).Scan(&rows)
	if rows != 0 {
		t.Errorf("expected the assigned EPC deleted from unassigned_tags, still %d row(s)", rows)
	}
}

func TestAssignKanban_IsIdempotentAndCanRelink(t *testing.T) {
	store := setupKanbanStore(t)
	k := fordKanban()

	for i := 0; i < 2; i++ {
		if err := store.AssignKanban("A0000001", k); err != nil {
			t.Fatalf("assign %d failed: %v", i, err)
		}
	}
	list, _ := store.ListKanbans()
	if len(list) != 1 {
		t.Errorf("expected a single kanban row after repeated assign, got %d", len(list))
	}

	other := Kanban{Code: "TOY-001", Description: "Sello"}
	if err := store.AssignKanban("A0000001", other); err != nil {
		t.Fatalf("relink failed: %v", err)
	}
	got, _ := store.GetKanbanByUII("A0000001")
	if got == nil || got.Code != "TOY-001" {
		t.Errorf("expected relinked to TOY-001, got %+v", got)
	}
}

func TestAssignKanban_RejectsBlankInput(t *testing.T) {
	store := setupKanbanStore(t)
	if err := store.AssignKanban("", fordKanban()); err == nil {
		t.Error("expected error for empty UII")
	}
	if err := store.AssignKanban("A1", Kanban{Code: " "}); err == nil {
		t.Error("expected error for blank kanban code")
	}
}

// A UII that disappears from the sync (renormalized by the VPS) must not keep
// its link: otherwise it still resolves as assigned and pins a stale kanban in
// the picker catalog.
func TestApplyKanbanLinks_DropsLinksOfUIIsMissingFromTheSync(t *testing.T) {
	store := setupKanbanStore(t)
	stale := fordKanban()
	stale.Code = "EX3W0AR-A"
	current := fordKanban()
	current.Code = "EX3W0AR"

	if err := store.ApplyKanbanLinks([]SyncDataItem{syncItem(1, "003000e2806995000050104cf45e8d6952c3", true, &stale)}); err != nil {
		t.Fatalf("first apply failed: %v", err)
	}
	// The VPS renormalized the UII of tag 1 and relinked it to the new code.
	if err := store.ApplyKanbanLinks([]SyncDataItem{syncItem(1, "E2806995000050104CF45E8D", true, &current)}); err != nil {
		t.Fatalf("second apply failed: %v", err)
	}

	if got, err := store.GetKanbanByUII("003000e2806995000050104cf45e8d6952c3"); err != nil || got != nil {
		t.Errorf("old UII must not resolve to a kanban, got (%+v, %v)", got, err)
	}
	if err := store.ReplaceKanbanCatalog([]Kanban{current}); err != nil {
		t.Fatalf("replace catalog failed: %v", err)
	}
	catalog, err := store.ListKanbans()
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	var codes []string
	for _, k := range catalog {
		codes = append(codes, k.Code)
	}
	if !reflect.DeepEqual(codes, []string{"EX3W0AR"}) {
		t.Errorf("catalog = %v, want only EX3W0AR", codes)
	}
}
