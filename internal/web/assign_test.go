package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/amg-rfid/amg-rfid-gateway/internal/config"
	"github.com/amg-rfid/amg-rfid-gateway/internal/httpclient"
	"github.com/amg-rfid/amg-rfid-gateway/internal/localstore"
)

const (
	offlineMessage  = "Sin conexión con el VPS: la asignación requiere estar en línea."
	vpsAssignOKBody = `{"uii":"A0000001","kanban":{"code":"EXP758SF M16","description":"Burlete","standard_pack":250,"unit":"PZ","zones":["C","A"],"customer":"Ford"}}`
)

type fakeSyncTrigger struct{ calls atomic.Int32 }

func (f *fakeSyncTrigger) RequestSync() { f.calls.Add(1) }

// fakeAssignVPS answers POST /api/v1/gateway/tags/assign and records what it received.
type fakeAssignVPS struct {
	*httptest.Server
	calls atomic.Int32
	last  atomic.Value // map[string]any
}

func newFakeAssignVPS(t *testing.T, status int, body string) *fakeAssignVPS {
	t.Helper()
	f := &fakeAssignVPS{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/gateway/tags/assign" || r.Method != http.MethodPost {
			t.Errorf("unexpected VPS request %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer vps-jwt" {
			t.Errorf("expected gateway bearer token, got %q", got)
		}
		var sent map[string]any
		json.NewDecoder(r.Body).Decode(&sent)
		f.last.Store(sent)
		f.calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		w.Write([]byte(body))
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeAssignVPS) sent() map[string]any {
	v, _ := f.last.Load().(map[string]any)
	return v
}

// withVPS points the env's server at a VPS URL and installs a sync trigger spy.
func (e *kanbanTestEnv) withVPS(url string, timeout time.Duration) *fakeSyncTrigger {
	e.server.vpsClient = httpclient.NewVPSClient(url, timeout, "vps-jwt")
	trigger := &fakeSyncTrigger{}
	e.server.SetSyncTrigger(trigger)
	return trigger
}

func postAssign(s *Server, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/tags/assign", strings.NewReader(body))
	s.handleAssignTag(rec, req)
	return rec
}

func decodeBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("invalid JSON %q: %v", rec.Body.String(), err)
	}
	return m
}

// assertNothingStored proves a rejected/failed assignment left no trace locally.
func assertNothingStored(t *testing.T, e *kanbanTestEnv, uii string) {
	t.Helper()
	if k, _ := e.store.GetKanbanByUII(uii); k != nil {
		t.Errorf("expected no local link for %s, got %+v", uii, k)
	}
	if catalog, _ := e.store.ListKanbans(); len(catalog) != 0 {
		t.Errorf("expected no kanban cached, got %+v", catalog)
	}
	if n, _ := e.store.GetPendingConfirmationsCount(); n != 0 {
		t.Errorf("expected nothing queued, got %d pending confirmations", n)
	}
}

func TestHandleAssignTag_SuccessUpdatesCacheInboxAndTriggersSync(t *testing.T) {
	env := newKanbanTestEnv(t, nil)
	env.store.UpsertUnassignedTag("A0000001", "ant-2", time.Now(), 5)
	env.store.UpsertUnassignedTag("A0000002", "ant-1", time.Now(), 1)
	vps := newFakeAssignVPS(t, http.StatusOK, vpsAssignOKBody)
	trigger := env.withVPS(vps.URL, 2*time.Second)

	rec := postAssign(env.server, `{"uii":"A0000001","kanban_code":"EXP758SF M16"}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := decodeBody(t, rec)
	if body["success"] != true || body["uii"] != "A0000001" {
		t.Errorf("unexpected body: %s", rec.Body.String())
	}
	kanban, _ := body["kanban"].(map[string]any)
	if kanban["code"] != "EXP758SF M16" || kanban["customer"] != "Ford" {
		t.Errorf("expected the kanban in the response, got %s", rec.Body.String())
	}

	// What the VPS received: company, UII, kanban and the antenna that read the tag (audit).
	sent := vps.sent()
	if vps.calls.Load() != 1 || sent["company_id"] != "test-company" || sent["uii"] != "A0000001" || sent["kanban_code"] != "EXP758SF M16" || sent["antenna_id"] != "ant-2" {
		t.Errorf("unexpected VPS request (%d calls): %+v", vps.calls.Load(), sent)
	}

	// Local cache: linked, catalog row stored, removed from the inbox, other tags untouched.
	got, _ := env.store.GetKanbanByUII("A0000001")
	if got == nil || got.Code != "EXP758SF M16" || got.StandardPack != 250 {
		t.Errorf("expected local link, got %+v", got)
	}
	pending, _ := env.store.ListUnassignedTags()
	if len(pending) != 1 || pending[0].EPC != "A0000002" {
		t.Errorf("expected only A0000002 left in the inbox, got %+v", pending)
	}
	if trigger.calls.Load() != 1 {
		t.Errorf("expected exactly one immediate sync request, got %d", trigger.calls.Load())
	}
	// Assigning creates no movement.
	if n, _ := env.store.GetPendingConfirmationsCount(); n != 0 {
		t.Errorf("expected no pending confirmations, got %d", n)
	}
}

func TestHandleAssignTag_AntennaFromRequestAndManualFallback(t *testing.T) {
	env := newKanbanTestEnv(t, nil)
	vps := newFakeAssignVPS(t, http.StatusOK, vpsAssignOKBody)
	env.withVPS(vps.URL, 2*time.Second)

	postAssign(env.server, `{"uii":"A0000001","kanban_code":"K","antenna_id":"ant-9"}`)
	if got := vps.sent()["antenna_id"]; got != "ant-9" {
		t.Errorf("expected antenna from the request, got %v", got)
	}

	// Not in the inbox and no antenna given: audit value "manual", like /api/confirm.
	postAssign(env.server, `{"uii":"A0000003","kanban_code":"K"}`)
	if got := vps.sent()["antenna_id"]; got != "manual" {
		t.Errorf("expected manual fallback, got %v", got)
	}
}

func TestHandleAssignTag_IsIdempotentForTheSameKanban(t *testing.T) {
	env := newKanbanTestEnv(t, nil)
	vps := newFakeAssignVPS(t, http.StatusOK, vpsAssignOKBody)
	env.withVPS(vps.URL, 2*time.Second)

	for i := 0; i < 2; i++ {
		if rec := postAssign(env.server, `{"uii":"A0000001","kanban_code":"EXP758SF M16"}`); rec.Code != http.StatusOK {
			t.Fatalf("call %d: expected 200, got %d", i, rec.Code)
		}
	}
	if catalog, _ := env.store.ListKanbans(); len(catalog) != 1 {
		t.Errorf("expected one kanban row, got %+v", catalog)
	}
}

func TestHandleAssignTag_OfflineVPSIs503AndStoresNothing(t *testing.T) {
	env := newKanbanTestEnv(t, nil)
	env.store.UpsertUnassignedTag("A0000001", "ant-1", time.Now(), 2)
	dead := httptest.NewServer(http.NotFoundHandler())
	dead.Close() // connection refused
	trigger := env.withVPS(dead.URL, 500*time.Millisecond)

	rec := postAssign(env.server, `{"uii":"A0000001","kanban_code":"EXP758SF M16"}`)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", rec.Code, rec.Body.String())
	}
	if body := decodeBody(t, rec); body["error"] != offlineMessage {
		t.Errorf("expected message %q, got %v", offlineMessage, body["error"])
	}
	assertNothingStored(t, env, "A0000001")
	pending, _ := env.store.ListUnassignedTags()
	if len(pending) != 1 || pending[0].EPC != "A0000001" {
		t.Errorf("expected the tag to stay in the inbox, got %+v", pending)
	}
	if trigger.calls.Load() != 0 {
		t.Errorf("expected no sync request on failure, got %d", trigger.calls.Load())
	}
}

func TestHandleAssignTag_VPSTimeoutIs503(t *testing.T) {
	env := newKanbanTestEnv(t, nil)
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(400 * time.Millisecond)
	}))
	defer slow.Close()
	env.withVPS(slow.URL, 50*time.Millisecond)

	rec := postAssign(env.server, `{"uii":"A0000001","kanban_code":"K"}`)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", rec.Code, rec.Body.String())
	}
	if body := decodeBody(t, rec); body["error"] != offlineMessage {
		t.Errorf("unexpected message %v", body["error"])
	}
	assertNothingStored(t, env, "A0000001")
}

func TestHandleAssignTag_NoVPSClientIs503(t *testing.T) {
	env := newKanbanTestEnv(t, nil) // vpsClient nil
	rec := postAssign(env.server, `{"uii":"A0000001","kanban_code":"K"}`)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", rec.Code)
	}
	assertNothingStored(t, env, "A0000001")
}

func TestHandleAssignTag_MapsVPSRejections(t *testing.T) {
	tests := []struct {
		name         string
		vpsStatus    int
		vpsBody      string
		wantStatus   int
		wantCode     string
		wantCurrent  string
		wantContains string
	}{
		{"kanban not found", http.StatusNotFound, `{"error":"kanban_not_found"}`, 404, "kanban_not_found", "", "kanban no existe"},
		{"uii assigned to another kanban", http.StatusConflict, `{"error":"uii_assigned","current_kanban":"TOY-001"}`, 409, "uii_assigned", "TOY-001", "TOY-001"},
		{"vps rejects the request", http.StatusBadRequest, `{"error":"invalid uii"}`, 400, "invalid_request", "", "rechazó"},
		{"vps internal error", http.StatusInternalServerError, `boom`, 502, "vps_error", "", "VPS"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newKanbanTestEnv(t, nil)
			env.store.UpsertUnassignedTag("A0000001", "ant-1", time.Now(), 1)
			vps := newFakeAssignVPS(t, tt.vpsStatus, tt.vpsBody)
			trigger := env.withVPS(vps.URL, 2*time.Second)

			rec := postAssign(env.server, `{"uii":"A0000001","kanban_code":"K1"}`)

			if rec.Code != tt.wantStatus {
				t.Fatalf("expected %d, got %d: %s", tt.wantStatus, rec.Code, rec.Body.String())
			}
			body := decodeBody(t, rec)
			if body["code"] != tt.wantCode {
				t.Errorf("expected code %q, got %v", tt.wantCode, body["code"])
			}
			if msg, _ := body["error"].(string); !strings.Contains(msg, tt.wantContains) {
				t.Errorf("expected message containing %q, got %q", tt.wantContains, msg)
			}
			if tt.wantCurrent != "" && body["current_kanban"] != tt.wantCurrent {
				t.Errorf("expected current_kanban %q, got %v", tt.wantCurrent, body["current_kanban"])
			}
			assertNothingStored(t, env, "A0000001")
			if pending, _ := env.store.ListUnassignedTags(); len(pending) != 1 {
				t.Errorf("expected the tag to stay in the inbox, got %+v", pending)
			}
			if trigger.calls.Load() != 0 {
				t.Errorf("expected no sync request, got %d", trigger.calls.Load())
			}
		})
	}
}

// Decision: the gateway does not validate the kanban against its local catalog
// (it may be stale); the VPS is authoritative and a 404 is mapped above.
func TestHandleAssignTag_ForwardsKanbanCodeMissingFromLocalCatalog(t *testing.T) {
	env := newKanbanTestEnv(t, nil)
	vps := newFakeAssignVPS(t, http.StatusOK, vpsAssignOKBody)
	env.withVPS(vps.URL, 2*time.Second)

	rec := postAssign(env.server, `{"uii":"A0000001","kanban_code":"NOT-IN-LOCAL-CATALOG"}`)

	if rec.Code != http.StatusOK || vps.calls.Load() != 1 || vps.sent()["kanban_code"] != "NOT-IN-LOCAL-CATALOG" {
		t.Errorf("expected the code forwarded to the VPS, got %d (%d calls)", rec.Code, vps.calls.Load())
	}
}

func TestHandleAssignTag_RejectsInvalidRequestsWithoutCallingTheVPS(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"invalid json", `{"uii":`},
		{"missing uii", `{"kanban_code":"K"}`},
		{"blank uii", `{"uii":"  ","kanban_code":"K"}`},
		{"missing kanban_code", `{"uii":"A1"}`},
		{"blank kanban_code", `{"uii":"A1","kanban_code":" "}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newKanbanTestEnv(t, nil)
			vps := newFakeAssignVPS(t, http.StatusOK, vpsAssignOKBody)
			env.withVPS(vps.URL, 2*time.Second)

			rec := postAssign(env.server, tt.body)

			if rec.Code != http.StatusBadRequest {
				t.Errorf("expected 400, got %d: %s", rec.Code, rec.Body.String())
			}
			if vps.calls.Load() != 0 {
				t.Errorf("the VPS must not be called, got %d calls", vps.calls.Load())
			}
			assertNothingStored(t, env, "A1")
		})
	}
}

func TestHandleAssignTag_MethodNotAllowed(t *testing.T) {
	env := newKanbanTestEnv(t, nil)
	rec := httptest.NewRecorder()
	env.server.handleAssignTag(rec, httptest.NewRequest(http.MethodGet, "/api/tags/assign", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rec.Code)
	}
}

func TestHandleAssignTag_NoLocalStoreDoesNotCallTheVPS(t *testing.T) {
	vps := newFakeAssignVPS(t, http.StatusOK, vpsAssignOKBody)
	srv := NewServer("127.0.0.1", 0, nil, nil, nil, httpclient.NewVPSClient(vps.URL, time.Second, "vps-jwt"), "test-company")

	rec := postAssign(srv, `{"uii":"A1","kanban_code":"K"}`)

	if rec.Code != http.StatusInternalServerError || vps.calls.Load() != 0 {
		t.Errorf("expected 500 without calling the VPS, got %d (%d calls)", rec.Code, vps.calls.Load())
	}
}

func TestAssignRoute_RequiresWriteAuthInLANMode(t *testing.T) {
	cfg := &config.GatewayConfig{WebAccessMode: "lan", WebAuthToken: "secret"}
	env := newKanbanTestEnv(t, cfg)
	vps := newFakeAssignVPS(t, http.StatusOK, vpsAssignOKBody)
	env.withVPS(vps.URL, 2*time.Second)
	ts := httptest.NewServer(env.server.routes())
	defer ts.Close()

	do := func(token string) int {
		req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/tags/assign", strings.NewReader(`{"uii":"A0000001","kanban_code":"K"}`))
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}

	if got := do(""); got != http.StatusUnauthorized {
		t.Errorf("without token: expected 401, got %d", got)
	}
	if got := do("wrong"); got != http.StatusUnauthorized {
		t.Errorf("wrong token: expected 401, got %d", got)
	}
	if vps.calls.Load() != 0 {
		t.Errorf("the VPS must not be called when unauthorized, got %d", vps.calls.Load())
	}
	if got := do("secret"); got != http.StatusOK {
		t.Errorf("valid token: expected 200, got %d", got)
	}
}

func TestHandleKanbans_ListsCachedCatalog(t *testing.T) {
	env := newKanbanTestEnv(t, nil)
	env.store.ReplaceKanbanCatalog([]localstore.Kanban{
		{Code: "B-2", Description: "second", Zones: []string{"A"}},
		{Code: "A-1", Description: "first", StandardPack: 10, Unit: "KG"},
	})

	rec := httptest.NewRecorder()
	env.server.handleKanbans(rec, httptest.NewRequest(http.MethodGet, "/api/kanbans", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var list []localstore.Kanban
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(list) != 2 || list[0].Code != "A-1" || list[0].Unit != "KG" || list[1].Code != "B-2" {
		t.Errorf("unexpected catalog: %+v", list)
	}
}

func TestHandleKanbans_EmptyMethodAndNilStore(t *testing.T) {
	env := newKanbanTestEnv(t, nil)
	rec := httptest.NewRecorder()
	env.server.handleKanbans(rec, httptest.NewRequest(http.MethodGet, "/api/kanbans", nil))
	if strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Errorf("expected [], got %q", rec.Body.String())
	}

	rec = httptest.NewRecorder()
	env.server.handleKanbans(rec, httptest.NewRequest(http.MethodPost, "/api/kanbans", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	NewServer("127.0.0.1", 0, nil, nil, nil, nil, "c").handleKanbans(rec, httptest.NewRequest(http.MethodGet, "/api/kanbans", nil))
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Errorf("nil store: expected 200 [], got %d %q", rec.Code, rec.Body.String())
	}
}
