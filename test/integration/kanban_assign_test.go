package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	stdsync "sync"
	"testing"
	"time"

	"github.com/amg-rfid/amg-rfid-gateway/internal/cache"
	"github.com/amg-rfid/amg-rfid-gateway/internal/events"
	"github.com/amg-rfid/amg-rfid-gateway/internal/httpclient"
	"github.com/amg-rfid/amg-rfid-gateway/internal/localstore"
	syncpkg "github.com/amg-rfid/amg-rfid-gateway/internal/sync"
	"github.com/amg-rfid/amg-rfid-gateway/internal/verify"
	"github.com/amg-rfid/amg-rfid-gateway/internal/web"
)

// statefulVPS emulates the cloud: assignments change what sync-data returns next.
type statefulVPS struct {
	*httptest.Server
	mu       stdsync.Mutex
	assigned map[string]string // uii -> kanban code ("" = unassigned but known)
	assigns  int
	syncs    int
}

const e2eKanbanJSON = `{"code":"EXP758SF M16","description":"Burlete frontal","standard_pack":250,"unit":"PZ","zones":["C","A"],"customer":"Ford"}`

func newStatefulVPS(t *testing.T) *statefulVPS {
	v := &statefulVPS{assigned: map[string]string{}}
	v.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		v.mu.Lock()
		defer v.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/gateway/sync-data":
			v.syncs++
			items := []string{}
			i := 0
			for uii, code := range v.assigned {
				i++
				kanban := "null"
				if code != "" {
					kanban = e2eKanbanJSON
				}
				items = append(items, fmt.Sprintf(`{"id":%d,"tool_id":10,"uii":%q,"sku":"EXP758SF","name":"Burlete","status":"available","active":true,"kanban":%s}`, i, uii, kanban))
			}
			fmt.Fprintf(w, `{"status":"OK","tools":[%s]}`, strings.Join(items, ","))
		case "/api/v1/gateway/users":
			w.Write([]byte(`[]`))
		case "/api/v1/gateway/kanbans":
			fmt.Fprintf(w, `{"status":"OK","kanbans":[%s]}`, e2eKanbanJSON)
		case "/api/v1/gateway/tags/assign":
			var req map[string]string
			json.NewDecoder(r.Body).Decode(&req)
			v.assigns++
			v.assigned[req["uii"]] = req["kanban_code"]
			fmt.Fprintf(w, `{"uii":%q,"kanban":%s}`, req["uii"], e2eKanbanJSON)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(v.Close)
	return v
}

func (v *statefulVPS) set(uii, code string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.assigned[uii] = code
}

func (v *statefulVPS) syncCount() int {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.syncs
}

func freePort(t *testing.T) int {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("no free port: %v", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

type noTools struct{}

func (noTools) GetToolByUII(string) (*verify.Tool, error) { return nil, nil }

// End to end through the public surface: an unassigned read lands in the inbox,
// POST /api/tags/assign registers it on the (fake) VPS, the cache links it, the
// inbox empties and the triggered sync keeps the link; when the VPS later
// returns kanban:null the stale link is cleared.
func TestKanbanAssign_EndToEnd_InboxAssignSyncAndStaleLink(t *testing.T) {
	vps := newStatefulVPS(t)

	c, err := cache.NewSQLite(filepath.Join(t.TempDir(), "cache.db"))
	if err != nil {
		t.Fatalf("failed to open cache: %v", err)
	}
	defer c.Close()
	store := localstore.New(c.GetDB())
	client := httpclient.NewVPSClient(vps.URL, 2*time.Second, httpclient.StaticToken("jwt"))

	toolsSync := syncpkg.NewToolsSync(client, store, "comp-1", time.Hour, time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := toolsSync.Start(ctx); err != nil {
		t.Fatalf("sync start failed: %v", err)
	}
	defer toolsSync.Stop()
	eventually(t, "first sync", toolsSync.IsVPSOnline)

	bus := events.NewEventBus(100)
	defer bus.Close()
	port := freePort(t)
	srv := web.NewServerWithConfig("127.0.0.1", port, bus, store, verify.NewVerifier(noTools{}, nil), client, "comp-1", nil)
	srv.SetSyncTrigger(toolsSync)
	if err := srv.Start(ctx); err != nil {
		t.Fatalf("web start failed: %v", err)
	}
	defer srv.Stop(context.Background())
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	eventually(t, "web server", func() bool {
		resp, err := http.Get(base + "/api/status")
		if err != nil {
			return false
		}
		resp.Body.Close()
		return true
	})

	// 1. A read of a tag nobody assigned shows up in the inbox.
	bus.Publish(events.TagDetected{EPC: "A000000000000003", AntennaID: "ant-1", RSSI: 50, Timestamp: time.Now()})
	eventually(t, "unassigned tag listed", func() bool {
		var tags []localstore.UnassignedTag
		getJSON(t, base+"/api/unassigned-tags", &tags)
		return len(tags) == 1 && tags[0].EPC == "A000000000000003"
	})

	// 2. Assign it: VPS registers it, cache links it, inbox empties, sync refreshes.
	syncsBefore := vps.syncCount()
	resp, err := http.Post(base+"/api/tags/assign", "application/json", strings.NewReader(`{"uii":"A000000000000003","kanban_code":"EXP758SF M16"}`))
	if err != nil {
		t.Fatalf("assign failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if k, _ := store.GetKanbanByUII("A000000000000003"); k == nil || k.Code != "EXP758SF M16" || k.Customer != "Ford" {
		t.Errorf("expected local link right after assign, got %+v", k)
	}
	var inbox []localstore.UnassignedTag
	getJSON(t, base+"/api/unassigned-tags", &inbox)
	if len(inbox) != 0 {
		t.Errorf("expected empty inbox, got %+v", inbox)
	}
	eventually(t, "immediate sync after assign (no 1h wait)", func() bool { return vps.syncCount() > syncsBefore })
	eventually(t, "tag stored by the refresh sync", func() bool {
		tag, _ := store.GetToolTagByUII("A000000000000003")
		return tag != nil
	})
	if k, _ := store.GetKanbanByUII("A000000000000003"); k == nil {
		t.Error("link must survive the refresh sync")
	}

	// 3. The VPS un-assigns it (kanban:null): the next sync clears the stale link.
	vps.set("A000000000000003", "")
	syncsBefore = vps.syncCount()
	toolsSync.RequestSync()
	eventually(t, "sync after unassign", func() bool { return vps.syncCount() > syncsBefore })
	eventually(t, "stale link cleared", func() bool {
		k, _ := store.GetKanbanByUII("A000000000000003")
		return k == nil
	})
}

func getJSON(t *testing.T, url string, out any) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s failed: %v", url, err)
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		t.Fatalf("decode %s failed: %v", url, err)
	}
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for %s", what)
}
