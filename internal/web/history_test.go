package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/amg-rfid/amg-rfid-gateway/internal/config"
	"github.com/amg-rfid/amg-rfid-gateway/internal/httpclient"
)

func getHistory(t *testing.T, env *kanbanTestEnv) (*httptest.ResponseRecorder, []map[string]any) {
	t.Helper()
	rec := httptest.NewRecorder()
	env.server.routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/history", nil))
	var items []map[string]any
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &items); err != nil {
			t.Fatalf("invalid JSON %q: %v", rec.Body.String(), err)
		}
	}
	return rec, items
}

func startOfToday() time.Time {
	now := time.Now()
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
}

func TestHandleHistory_EmptyIsAnEmptyArray(t *testing.T) {
	env := newKanbanTestEnv(t, nil)

	rec, items := getHistory(t, env)

	if rec.Code != http.StatusOK || rec.Body.String() != "[]\n" || len(items) != 0 {
		t.Errorf("expected 200 [], got %d %q", rec.Code, rec.Body.String())
	}
}

func TestHandleHistory_ListsTodaysConfirmationsNewestFirstWithKanbanDetail(t *testing.T) {
	env := newKanbanTestEnv(t, nil)
	env.linkKanban(t, "A0000001", testKanban())
	now := time.Now()
	// An online confirm (synced) and an older, still pending, offline one.
	if err := env.store.RecordSyncedConfirmation("A0000001", "salida", "ant-2", now.Add(-time.Second)); err != nil {
		t.Fatalf("seed failed: %v", err)
	}
	if _, err := env.store.CreateConfirmation("A0000001", "entrada", "ant-1", now.Add(-2*time.Second), 0); err != nil {
		t.Fatalf("seed failed: %v", err)
	}

	rec, items := getHistory(t, env)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d: %s", len(items), rec.Body.String())
	}
	newest, oldest := items[0], items[1]
	want := map[string]any{
		"uii": "A0000001", "kanban_code": "EXP758SF M16", "description": "Burlete",
		"quantity": float64(250), "unit": "PZ", "action": "salida", "antenna_id": "ant-2", "synced": true,
	}
	for k, v := range want {
		if newest[k] != v {
			t.Errorf("newest[%s] = %#v, want %#v", k, newest[k], v)
		}
	}
	if oldest["action"] != "entrada" || oldest["antenna_id"] != "ant-1" || oldest["synced"] != false {
		t.Errorf("oldest entry unexpected: %+v", oldest)
	}
	for _, item := range items {
		ts, ok := item["time"].(string)
		if !ok {
			t.Fatalf("time = %#v, want an RFC3339 string", item["time"])
		}
		if _, err := time.Parse(time.RFC3339, ts); err != nil {
			t.Errorf("time %q is not RFC3339: %v", ts, err)
		}
	}
	first, _ := time.Parse(time.RFC3339, newest["time"].(string))
	second, _ := time.Parse(time.RFC3339, oldest["time"].(string))
	if !first.After(second) {
		t.Errorf("not newest first: %v then %v", first, second)
	}
}

// Day boundary is the gateway's local midnight and uses the confirmation time,
// not the moment the row was stored.
func TestHandleHistory_OnlyIncludesTheGatewayLocalDay(t *testing.T) {
	env := newKanbanTestEnv(t, nil)
	env.linkKanban(t, "A0000001", testKanban())
	midnight := startOfToday()
	// Stored just now but confirmed before local midnight: yesterday.
	if err := env.store.RecordSyncedConfirmation("A0000001", "entrada", "ant-1", midnight.Add(-time.Minute)); err != nil {
		t.Fatalf("seed failed: %v", err)
	}
	if err := env.store.RecordSyncedConfirmation("A0000001", "salida", "ant-1", time.Now()); err != nil {
		t.Fatalf("seed failed: %v", err)
	}

	_, items := getHistory(t, env)

	if len(items) != 1 || items[0]["action"] != "salida" {
		t.Errorf("expected only today's salida, got %+v", items)
	}
}

// A confirmation whose tag is no longer linked keeps its row with null kanban fields.
func TestHandleHistory_UnlinkedTagHasNullKanbanFields(t *testing.T) {
	env := newKanbanTestEnv(t, nil)
	if err := env.store.RecordSyncedConfirmation("UNLINKED-1", "entrada", "ant-1", time.Now()); err != nil {
		t.Fatalf("seed failed: %v", err)
	}

	_, items := getHistory(t, env)

	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %+v", items)
	}
	item := items[0]
	for _, key := range []string{"kanban_code", "description", "quantity", "unit"} {
		if v, present := item[key]; !present || v != nil {
			t.Errorf("%s = %#v (present %v), want null", key, v, present)
		}
	}
	if item["uii"] != "UNLINKED-1" || item["action"] != "entrada" {
		t.Errorf("unexpected item: %+v", item)
	}
}

// End to end through the confirm endpoint: both modes show up, with the right sync state.
func TestHandleHistory_ReflectsConfirmEndpointInBothModes(t *testing.T) {
	env := newKanbanTestEnv(t, nil)
	env.linkKanban(t, "A0000001", testKanban())
	vps := newFakeConfirmVPS(t)
	env.server.vpsClient = httpclient.NewVPSClient(vps.URL, 2*time.Second, "tok")
	postConfirm(env.server, `{"uii":"A0000001","action":"entrada","antenna_id":"ant-1"}`)
	env.server.vpsClient = httpclient.NewVPSClient("http://localhost:59999", 100*time.Millisecond, "tok")
	postConfirm(env.server, `{"uii":"A0000001","action":"salida","antenna_id":"ant-2"}`)

	_, items := getHistory(t, env)

	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %+v", items)
	}
	byAction := map[any]map[string]any{items[0]["action"]: items[0], items[1]["action"]: items[1]}
	if byAction["entrada"]["synced"] != true || byAction["salida"]["synced"] != false {
		t.Errorf("unexpected sync states: %+v", items)
	}
}

func TestHandleHistory_WithoutStoreIsAnEmptyArray(t *testing.T) {
	srv := NewServer("127.0.0.1", 0, nil, nil, nil, nil, "c")
	rec := httptest.NewRecorder()
	srv.handleHistory(rec, httptest.NewRequest(http.MethodGet, "/api/history", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "[]\n" {
		t.Errorf("expected 200 [], got %d %q", rec.Code, rec.Body.String())
	}
}

func TestHandleHistory_MethodNotAllowed(t *testing.T) {
	env := newKanbanTestEnv(t, nil)
	rec := httptest.NewRecorder()
	env.server.routes().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/history", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rec.Code)
	}
}

// Reads are GET like /api/tags: no bearer token even in LAN mode.
func TestHistoryRoute_ReadDoesNotRequireAuthInLANMode(t *testing.T) {
	env := newKanbanTestEnv(t, &config.GatewayConfig{WebAccessMode: "lan", WebAuthToken: "secret"})
	ts := httptest.NewServer(env.server.routes())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/history")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 without token, got %d", resp.StatusCode)
	}
}
