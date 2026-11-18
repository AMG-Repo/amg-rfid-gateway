package sync

import (
	"database/sql"
	"fmt"
	"github.com/amg-rfid/amg-rfid-gateway/internal/cache"
	"github.com/amg-rfid/amg-rfid-gateway/internal/localstore"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func tagIdentityStore(t *testing.T) (*localstore.LocalStore, *sql.DB) {
	t.Helper()
	c, e := cache.NewSQLite(filepath.Join(t.TempDir(), "full.db"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { c.Close() })
	db := c.GetDB()
	db.SetMaxOpenConns(1)
	s := localstore.New(db)
	if e = s.UpsertToolsRecords([]localstore.ToolRecord{{ID: 10, CompanyID: "test", SKU: "SKU", Name: "master"}}); e != nil {
		t.Fatal(e)
	}
	if e = s.UpsertToolTags([]localstore.ToolTagRecord{{ID: 1, ToolID: 10, UII: "A", Active: true}, {ID: 2, ToolID: 10, UII: "B", Active: true}}); e != nil {
		t.Fatal(e)
	}
	for _, u := range []string{"A", "B"} {
		if e = s.AssignKanban(u, localstore.Kanban{Code: "OLD-" + u}); e != nil {
			t.Fatal(e)
		}
	}
	return s, db
}
func tagIdentityRun(t *testing.T, s *localstore.LocalStore, items []localstore.SyncDataItem, catalog []localstore.Kanban) {
	t.Helper()
	calls := 0
	vps := &kanbanVPSClient{mockVPSClient: mockVPSClient{fetchSyncDataFunc: func(string) (*SyncDataResponse, error) { return &SyncDataResponse{Status: "OK", Tools: items}, nil }}, fetchKanbansFunc: func(string) ([]localstore.Kanban, error) { calls++; return catalog, nil }}
	ts := NewToolsSync(vps, s, "test", time.Hour, time.Hour)
	ts.performSync()
	if !ts.IsVPSOnline() || calls != 1 {
		t.Fatalf("sync failed; online=%v catalog fetches=%d", ts.IsVPSOnline(), calls)
	}
}
func tagIdentityCheck(t *testing.T, s *localstore.LocalStore, db *sql.DB, wantIDs map[int64]string, wantLinks map[string]string, wantCodes []string) {
	t.Helper()
	r, e := db.Query(`SELECT id,uii FROM tool_tags ORDER BY id`)
	if e != nil {
		t.Fatal(e)
	}
	gotIDs := map[int64]string{}
	for r.Next() {
		var id int64
		var u string
		if e = r.Scan(&id, &u); e != nil {
			t.Fatal(e)
		}
		gotIDs[id] = u
	}
	if e = r.Err(); e != nil {
		t.Fatal(e)
	}
	r.Close()
	if !reflect.DeepEqual(gotIDs, wantIDs) {
		t.Fatalf("ids=%v want=%v", gotIDs, wantIDs)
	}
	r, e = db.Query(`SELECT uii,kanban_code FROM tag_kanban_links ORDER BY uii`)
	if e != nil {
		t.Fatal(e)
	}
	gotLinks := map[string]string{}
	for r.Next() {
		var u, k string
		if e = r.Scan(&u, &k); e != nil {
			t.Fatal(e)
		}
		gotLinks[u] = k
	}
	if e = r.Err(); e != nil {
		t.Fatal(e)
	}
	r.Close()
	if !reflect.DeepEqual(gotLinks, wantLinks) {
		t.Fatalf("links=%v want=%v", gotLinks, wantLinks)
	}
	catalog, e := s.ListKanbans()
	if e != nil {
		t.Fatal(e)
	}
	var codes []string
	for _, k := range catalog {
		codes = append(codes, k.Code)
	}
	if !reflect.DeepEqual(codes, wantCodes) {
		t.Fatalf("codes=%v want=%v", codes, wantCodes)
	}
	t.Logf("ids=%v links=%v catalog=%v", gotIDs, gotLinks, codes)
}
func tagIdentityItem(id int64, u string, k *localstore.Kanban) localstore.SyncDataItem {
	return localstore.SyncDataItem{ID: id, ToolID: 10, UII: u, SKU: "SKU", Name: "current", Active: true, Kanban: k}
}

func TestTagIdentityFullRenormalizationRemovesStaleCatalog(t *testing.T) {
	s, db := tagIdentityStore(t)
	ka, kb := localstore.Kanban{Code: "CURRENT"}, localstore.Kanban{Code: "OLD-B"}
	tagIdentityRun(t, s, []localstore.SyncDataItem{tagIdentityItem(1, "NORMALIZED-A", &ka), tagIdentityItem(2, "B", &kb)}, []localstore.Kanban{ka, kb})
	tagIdentityCheck(t, s, db, map[int64]string{1: "NORMALIZED-A", 2: "B"}, map[string]string{"NORMALIZED-A": "CURRENT", "B": "OLD-B"}, []string{"CURRENT", "OLD-B"})
}
func TestTagIdentityFullUIIMoveFollowsSnapshotLinks(t *testing.T) {
	for _, existing := range []bool{false, true} {
		for _, state := range []string{"linked", "null", "inactive"} {
			t.Run(fmt.Sprintf("existing=%v/%s", existing, state), func(t *testing.T) {
				s, db := tagIdentityStore(t)
				current := localstore.Kanban{Code: "CURRENT"}
				kb := localstore.Kanban{Code: "OLD-B"}
				id := int64(3)
				if existing {
					id = 2
				}
				item := tagIdentityItem(id, "A", &current)
				wantLinks := map[string]string{}
				catalog := []localstore.Kanban{}
				var codes []string
				if state == "null" {
					item.Kanban = nil
				} else if state == "inactive" {
					item.Active = false
				} else {
					wantLinks["A"] = "CURRENT"
					catalog = append(catalog, current)
					codes = append(codes, "CURRENT")
				}
				items := []localstore.SyncDataItem{item}
				wantIDs := map[int64]string{id: "A"}
				if !existing {
					items = append(items, tagIdentityItem(2, "B", &kb))
					wantIDs[2] = "B"
					wantLinks["B"] = "OLD-B"
					catalog = append(catalog, kb)
					codes = append(codes, "OLD-B")
				}
				tagIdentityRun(t, s, items, catalog)
				tagIdentityCheck(t, s, db, wantIDs, wantLinks, codes)
			})
		}
	}
}
func TestTagIdentityFullSwapFollowsSnapshotBothOrders(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		for _, aState := range []string{"linked", "null", "inactive"} {
			for _, bState := range []string{"linked", "null", "inactive"} {
				t.Run(fmt.Sprintf("reverse=%v/A=%s/B=%s", reverse, aState, bState), func(t *testing.T) {
					s, db := tagIdentityStore(t)
					ka, kb := localstore.Kanban{Code: "CURRENT-A"}, localstore.Kanban{Code: "CURRENT-B"}
					a, b := tagIdentityItem(2, "A", &ka), tagIdentityItem(1, "B", &kb)
					want := map[string]string{}
					var catalog []localstore.Kanban
					var codes []string
					for i, state := range []string{aState, bState} {
						item := &a
						k := ka
						if i == 1 {
							item = &b
							k = kb
						}
						if state == "null" {
							item.Kanban = nil
						} else if state == "inactive" {
							item.Active = false
						} else {
							want[item.UII] = k.Code
							catalog = append(catalog, k)
							codes = append(codes, k.Code)
						}
					}
					items := []localstore.SyncDataItem{b, a}
					if reverse {
						items = []localstore.SyncDataItem{a, b}
					}
					tagIdentityRun(t, s, items, catalog)
					tagIdentityCheck(t, s, db, map[int64]string{1: "B", 2: "A"}, want, codes)
				})
			}
		}
	}
}

// Combining two requested cases: a skipped (empty-SKU) UII still carries its
// existing kanban in the VPS snapshot while its former id moves to the other UII.
func TestTagIdentityFullSwapWithEmptySKUStillKeepsLegitimateLink(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		t.Run(fmt.Sprint(reverse), func(t *testing.T) {
			s, db := tagIdentityStore(t)
			ka, kb := localstore.Kanban{Code: "OLD-A"}, localstore.Kanban{Code: "OLD-B"}
			a, b := tagIdentityItem(2, "A", &ka), tagIdentityItem(1, "B", &kb)
			a.SKU = " "
			items := []localstore.SyncDataItem{b, a}
			if reverse {
				items = []localstore.SyncDataItem{a, b}
			}
			tagIdentityRun(t, s, items, []localstore.Kanban{ka, kb})
			// Existing skip behavior leaves only id=1 stored; nevertheless A's legitimate
			// link should survive, like the standalone empty-SKU reproduction requires.
			tagIdentityCheck(t, s, db, map[int64]string{1: "B"}, map[string]string{"A": "OLD-A", "B": "OLD-B"}, []string{"OLD-A", "OLD-B"})
		})
	}
}
