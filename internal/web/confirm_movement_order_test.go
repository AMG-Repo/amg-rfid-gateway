package web

import (
	"encoding/json"
	"github.com/amg-rfid/amg-rfid-gateway/internal/httpclient"
	"github.com/amg-rfid/amg-rfid-gateway/internal/localstore"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func movementLocOffline(t *testing.T, e *kanbanTestEnv) {
	t.Helper()
	v := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }))
	t.Cleanup(v.Close)
	e.server.vpsClient = httpclient.NewVPSClient(v.URL, time.Second, httpclient.StaticToken("tok"))
}
func movementLocAccepted(t *testing.T, r *httptest.ResponseRecorder, mode string) {
	t.Helper()
	var x ConfirmResponse
	if e := json.Unmarshal(r.Body.Bytes(), &x); e != nil {
		t.Fatal(e)
	}
	if r.Code != 200 || !x.Success || x.Mode != mode {
		t.Fatalf("not accepted: %d %+v", r.Code, x)
	}
}
func TestMovementLocationRejectionsNeverChangeLocation(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
	}{{"body", "{", 400}, {"action", `{"uii":"A","action":"bad"}`, 400}, {"unassigned", `{"uii":"A","action":"entrada"}`, 422}, {"full", `{"uii":"A","action":"entrada"}`, 507}, {"lookup", `{"uii":"A","action":"entrada"}`, 500}, {"insert", `{"uii":"A","action":"entrada"}`, 200}} {
		t.Run(tc.name, func(t *testing.T) {
			e := newKanbanTestEnv(t, queueCapConfig(1))
			seedAssignedTag(t, e, "A", "C")
			movementLocOffline(t, e)
			var err error
			switch tc.name {
			case "unassigned":
				_, err = e.db.Exec(`DELETE FROM tag_kanban_links`)
			case "full":
				_, err = e.store.CreateConfirmation("OTHER", "entrada", "ant", time.Now(), 0)
			case "lookup":
				_, err = e.db.Exec(`DROP TABLE tag_kanban_links`)
			case "insert":
				_, err = e.db.Exec(`DROP TABLE pending_confirmations`)
			}
			if err != nil {
				t.Fatal(err)
			}
			r := postConfirm(e.server, tc.body)
			got := cachedLocation(t, e, "A")
			t.Logf("status=%d body=%s location=%q", r.Code, r.Body.String(), got)
			if r.Code != tc.status || got != "C" {
				t.Fatalf("rejection changed location or status")
			}
		})
	}
}
func TestMovementLocationOlderPendingMustNotRollbackNewOnlineMovement(t *testing.T) {
	e := newKanbanTestEnv(t, nil)
	seedAssignedTag(t, e, "A", "C")
	movementLocOffline(t, e)
	movementLocAccepted(t, postConfirm(e.server, `{"uii":"A","action":"entrada"}`), "offline")
	v := newFakeConfirmVPS(t)
	e.server.vpsClient = httpclient.NewVPSClient(v.URL, time.Second, httpclient.StaticToken("tok"))
	movementLocAccepted(t, postConfirm(e.server, `{"uii":"A","action":"salida"}`), "online")
	before := cachedLocation(t, e, "A")
	all := e.allConfirmations(t)
	if len(all) != 2 || all[0].Synced || !all[1].Synced {
		t.Fatalf("unexpected history %+v", all)
	}
	// The VPS sync truth is C; then the sync's replay runs.
	if err := e.store.UpsertToolTags([]localstore.ToolTagRecord{{ID: 1, ToolID: 1, UII: "A", Active: true, Location: &before}}); err != nil {
		t.Fatal(err)
	}
	if err := e.store.ReapplyPendingMovements(); err != nil {
		t.Fatal(err)
	}
	after := cachedLocation(t, e, "A")
	t.Logf("offline entrada pending; newer online salida synced; before sync=%q after sync=%q", before, after)
	if after != before {
		t.Fatalf("S13: newer online salida rolled back to older pending entrada")
	}
}
func TestMovementLocationAcceptedUppercaseSalidaMustMoveOutside(t *testing.T) {
	e := newKanbanTestEnv(t, nil)
	seedAssignedTag(t, e, "A", localstore.WarehouseLocation)
	v := newFakeConfirmVPS(t)
	e.server.vpsClient = httpclient.NewVPSClient(v.URL, time.Second, httpclient.StaticToken("tok"))
	movementLocAccepted(t, postConfirm(e.server, `{"uii":"A","action":"SALIDA"}`), "online")
	got := cachedLocation(t, e, "A")
	t.Logf("accepted SALIDA location=%q", got)
	if got != "C" {
		t.Fatalf("accepted salida should set C, got %q", got)
	}
}
