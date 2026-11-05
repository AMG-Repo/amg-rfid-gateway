package integration

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/amg-rfid/amg-rfid-gateway/internal/cache"
	"github.com/amg-rfid/amg-rfid-gateway/internal/httpclient"
	"github.com/amg-rfid/amg-rfid-gateway/internal/localstore"
	syncpkg "github.com/amg-rfid/amg-rfid-gateway/internal/sync"
)

// fakeVPS serves the gateway endpoints ToolsSync uses. kanbansStatus lets a test
// emulate a cloud that does not expose /kanbans yet.
func fakeVPS(t *testing.T, syncBody string, kanbansStatus int, kanbansBody string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/gateway/sync-data":
			w.Write([]byte(syncBody))
		case "/api/v1/gateway/users":
			w.Write([]byte(`[]`))
		case "/api/v1/gateway/kanbans":
			w.WriteHeader(kanbansStatus)
			w.Write([]byte(kanbansBody))
		default:
			http.NotFound(w, r)
		}
	}))
}

// syncOnce runs ToolsSync against the fake VPS and a real SQLite cache and waits for the first sync.
func syncOnce(t *testing.T, vps *httptest.Server) *localstore.LocalStore {
	t.Helper()
	c, err := cache.NewSQLite(filepath.Join(t.TempDir(), "cache.db"))
	if err != nil {
		t.Fatalf("failed to open cache: %v", err)
	}
	t.Cleanup(func() { c.Close() })
	store := localstore.New(c.GetDB())

	client := httpclient.NewVPSClient(vps.URL, 2*time.Second, "jwt")
	ts := syncpkg.NewToolsSync(client, store, "comp-1", time.Hour, time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if err := ts.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	t.Cleanup(ts.Stop)

	deadline := time.Now().Add(3 * time.Second)
	for !ts.IsVPSOnline() {
		if time.Now().After(deadline) {
			t.Fatal("timeout waiting for first sync")
		}
		time.Sleep(10 * time.Millisecond)
	}
	return store
}

func TestKanbanSync_EndToEnd_AssignedTagExposesKanbanAndCatalogIsCached(t *testing.T) {
	vps := fakeVPS(t, `{"status":"OK","tools":[
		{"id":1,"tool_id":10,"uii":"A0000001","sku":"EXP758SF","name":"Burlete","status":"available","active":true,"kanban_zone":"A",
		 "kanban":{"code":"EXP758SF M16","description":"Burlete frontal","standard_pack":250,"unit":"PZ","zones":["C","A"],"extrusora":"M16","position":"A4","area":"RAMFG","customer":"Ford","card_type":"produccion","sku":"EXP758SF"}},
		{"id":2,"tool_id":11,"uii":"A0000002","sku":"OLD-1","name":"Legacy","status":"available","active":true,"kanban":null},
		{"id":3,"tool_id":10,"uii":"A0000003","sku":"EXP758SF","name":"Burlete","status":"available","active":false,
		 "kanban":{"code":"EXP758SF M16","description":"Burlete frontal"}}
	]}`, http.StatusOK, `{"status":"OK","kanbans":[{"code":"EXP758SF M16","description":"Burlete frontal","standard_pack":250,"unit":"PZ","zones":["C","A"],"extrusora":"M16","position":"A4","area":"RAMFG","customer":"Ford","card_type":"produccion","sku":"EXP758SF"},{"code":"TOY-001","description":"Sello","standard_pack":10,"unit":"KG","zones":["H"]}]}`)
	defer vps.Close()

	store := syncOnce(t, vps)

	got, err := store.GetKanbanByUII("A0000001")
	if err != nil || got == nil {
		t.Fatalf("expected kanban for A0000001, got (%+v, %v)", got, err)
	}
	if got.Code != "EXP758SF M16" || got.StandardPack != 250 || got.Unit != "PZ" || got.Customer != "Ford" || got.Position != "A4" {
		t.Errorf("unexpected kanban: %+v", got)
	}
	for _, uii := range []string{"A0000002", "A0000003"} {
		if k, _ := store.GetKanbanByUII(uii); k != nil {
			t.Errorf("expected %s (kanban:null / inactive) to have no link, got %+v", uii, k)
		}
	}

	catalog, err := store.ListKanbans()
	if err != nil || len(catalog) != 2 || catalog[0].Code != "EXP758SF M16" || catalog[1].Code != "TOY-001" {
		t.Errorf("expected cached catalog of 2 kanbans, got (%+v, %v)", catalog, err)
	}
}

// Regression: a VPS that predates kanbans (no kanban fields, no /kanbans) syncs exactly as before.
func TestKanbanSync_EndToEnd_OldVPSStillSyncsTools(t *testing.T) {
	vps := fakeVPS(t, `{"status":"OK","tools":[
		{"id":1,"tool_id":10,"uii":"E200AAAA","sku":"SKU-1","name":"Hammer","status":"available","location":"Almacén General","active":true,"kanban_zone":"Zona A","tool_destination":"Taller"}
	]}`, http.StatusNotFound, `{"error":"not found"}`)
	defer vps.Close()

	store := syncOnce(t, vps)

	tag, err := store.GetToolTagByUII("E200AAAA")
	if err != nil || tag == nil {
		t.Fatalf("expected tag stored, got (%+v, %v)", tag, err)
	}
	if tag.ToolID != 10 || !tag.Active || tag.KanbanZone == nil || *tag.KanbanZone != "Zona A" || tag.Location == nil || *tag.Location != "Almacén General" {
		t.Errorf("tag stored differently than before: %+v", tag)
	}
	tool, err := store.GetToolByUII("E200AAAA")
	if err != nil || tool == nil || tool.SKU != "SKU-1" || tool.Name != "Hammer" {
		t.Errorf("expected tool lookup unchanged, got (%+v, %v)", tool, err)
	}
	if n, _ := store.GetToolsCount(); n != 1 {
		t.Errorf("expected 1 active tag, got %d", n)
	}
	if k, _ := store.GetKanbanByUII("E200AAAA"); k != nil {
		t.Errorf("expected no kanban for old payload, got %+v", k)
	}
	if catalog, _ := store.ListKanbans(); len(catalog) != 0 {
		t.Errorf("expected empty catalog when /kanbans is missing, got %+v", catalog)
	}
}
