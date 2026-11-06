package web

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/amg-rfid/amg-rfid-gateway/internal/cache"
	"github.com/amg-rfid/amg-rfid-gateway/internal/config"
	"github.com/amg-rfid/amg-rfid-gateway/internal/events"
	"github.com/amg-rfid/amg-rfid-gateway/internal/localstore"
	"github.com/amg-rfid/amg-rfid-gateway/internal/verify"
)

// kanbanTestEnv wires a Server to the schema the gateway really creates (cache.NewSQLite).
type kanbanTestEnv struct {
	server *Server
	store  *localstore.LocalStore
	bus    *events.EventBus
	db     *sql.DB
}

func newKanbanTestEnv(t *testing.T, cfg *config.GatewayConfig) *kanbanTestEnv {
	t.Helper()
	c, err := cache.NewSQLite(filepath.Join(t.TempDir(), "cache.db"))
	if err != nil {
		t.Fatalf("failed to open cache: %v", err)
	}
	t.Cleanup(func() { c.Close() })

	bus := events.NewEventBus(100)
	t.Cleanup(bus.Close)
	store := localstore.New(c.GetDB())
	verifier := verify.NewVerifier(emptyToolStore{}, nil)

	srv := NewServerWithConfig("127.0.0.1", 0, bus, store, verifier, nil, "test-company", cfg)
	srv.unassignedFlushInterval = 10 * time.Millisecond
	return &kanbanTestEnv{server: srv, store: store, bus: bus, db: c.GetDB()}
}

// emptyToolStore is a verifier ToolStore that knows no tools.
type emptyToolStore struct{}

func (emptyToolStore) GetToolByUII(uii string) (*verify.Tool, error) { return nil, nil }

func (e *kanbanTestEnv) linkKanban(t *testing.T, uii string, k localstore.Kanban) {
	t.Helper()
	item := localstore.SyncDataItem{ID: 1, ToolID: 1, UII: uii, SKU: "S", Name: "N", Active: true, Kanban: &k}
	if err := e.store.ApplyKanbanLinks([]localstore.SyncDataItem{item}); err != nil {
		t.Fatalf("failed to link kanban: %v", err)
	}
}

func testKanban() localstore.Kanban {
	return localstore.Kanban{Code: "EXP758SF M16", Description: "Burlete", StandardPack: 250, Unit: "PZ", Zones: []string{"C", "A"}, Customer: "Ford"}
}

func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for %s", what)
}

func getUnassigned(t *testing.T, s *Server) (*httptest.ResponseRecorder, []localstore.UnassignedTag) {
	t.Helper()
	rec := httptest.NewRecorder()
	s.handleUnassignedTags(rec, httptest.NewRequest(http.MethodGet, "/api/unassigned-tags", nil))
	var tags []localstore.UnassignedTag
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &tags); err != nil {
			t.Fatalf("invalid JSON %q: %v", rec.Body.String(), err)
		}
	}
	return rec, tags
}

func TestHandleUnassignedTags_ListsStoredRows(t *testing.T) {
	env := newKanbanTestEnv(t, nil)
	seen := time.Date(2026, 11, 5, 10, 0, 0, 0, time.UTC)
	env.server.now = func() time.Time { return seen.Add(time.Hour) }
	env.store.UpsertUnassignedTag("A000000000000003", "ant-1", seen, 4)

	rec, tags := getUnassigned(t, env.server)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected JSON content type, got %q", ct)
	}
	if len(tags) != 1 || tags[0].EPC != "A000000000000003" || tags[0].AntennaID != "ant-1" || tags[0].ReadCount != 4 || !tags[0].LastSeen.Equal(seen) {
		t.Errorf("unexpected body: %s", rec.Body.String())
	}
	// Field names are part of the API contract for the panel.
	for _, key := range []string{`"epc"`, `"antenna_id"`, `"first_seen"`, `"last_seen"`, `"read_count"`} {
		if !strings.Contains(rec.Body.String(), key) {
			t.Errorf("expected %s in %s", key, rec.Body.String())
		}
	}
}

func TestHandleUnassignedTags_EmptyIsAnEmptyArray(t *testing.T) {
	env := newKanbanTestEnv(t, nil)
	rec, _ := getUnassigned(t, env.server)
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Errorf("expected 200 [], got %d %q", rec.Code, rec.Body.String())
	}

	nilStore := NewServer("127.0.0.1", 0, events.NewEventBus(1), nil, nil, nil, "c")
	rec, _ = getUnassigned(t, nilStore)
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Errorf("nil store: expected 200 [], got %d %q", rec.Code, rec.Body.String())
	}
}

func TestHandleUnassignedTags_MethodNotAllowed(t *testing.T) {
	env := newKanbanTestEnv(t, nil)
	rec := httptest.NewRecorder()
	env.server.handleUnassignedTags(rec, httptest.NewRequest(http.MethodPost, "/api/unassigned-tags", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rec.Code)
	}
}

// Reads are GET like /api/tags: no bearer token even in LAN mode.
func TestUnassignedTagsRoute_ReadDoesNotRequireAuthInLANMode(t *testing.T) {
	cfg := &config.GatewayConfig{WebAccessMode: "lan", WebAuthToken: "secret"}
	env := newKanbanTestEnv(t, cfg)

	ts := httptest.NewServer(env.server.routes())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/unassigned-tags")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 without token, got %d", resp.StatusCode)
	}
}

func TestUnassignedRecorder_RecordsReadsOfUnassignedTagsOnly(t *testing.T) {
	env := newKanbanTestEnv(t, nil)
	env.linkKanban(t, "ASSIGNED", testKanban())
	env.store.UpsertUsers([]localstore.User{{ID: 1, CompanyID: "c", Name: "Op", RFIDTag: "BADGE-1", Active: true}})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	env.server.startUnassignedRecorder(ctx)

	now := time.Now()
	for i := 0; i < 3; i++ {
		env.bus.Publish(events.TagDetected{EPC: "NEWTAG", AntennaID: "ant-1", RSSI: 60, Timestamp: now.Add(time.Duration(i) * time.Second)})
	}
	env.bus.Publish(events.TagDetected{EPC: "ASSIGNED", AntennaID: "ant-1", Timestamp: now})
	env.bus.Publish(events.TagDetected{EPC: "BADGE-1", AntennaID: "ant-1", Timestamp: now})

	waitUntil(t, "NEWTAG recorded with 3 reads", func() bool {
		tags, _ := env.store.ListUnassignedTags()
		return len(tags) == 1 && tags[0].ReadCount == 3
	})
	// Give the recorder a further flush to prove the other two EPCs are not added later.
	time.Sleep(50 * time.Millisecond)

	tags, _ := env.store.ListUnassignedTags()
	if len(tags) != 1 || tags[0].EPC != "NEWTAG" || tags[0].AntennaID != "ant-1" {
		t.Errorf("expected only NEWTAG unassigned (assigned tag and user badge excluded), got %+v", tags)
	}
	// Unassigned reads never create movements.
	if n, _ := env.store.GetPendingConfirmationsCount(); n != 0 {
		t.Errorf("expected no pending confirmations, got %d", n)
	}
}

func TestUnassignedRecorder_ReadsAccumulateAcrossFlushes(t *testing.T) {
	env := newKanbanTestEnv(t, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	env.server.startUnassignedRecorder(ctx)

	env.bus.Publish(events.TagDetected{EPC: "NEWTAG", AntennaID: "ant-1", Timestamp: time.Now()})
	waitUntil(t, "first read", func() bool {
		tags, _ := env.store.ListUnassignedTags()
		return len(tags) == 1 && tags[0].ReadCount == 1
	})
	env.bus.Publish(events.TagDetected{EPC: "NEWTAG", AntennaID: "ant-2", Timestamp: time.Now().Add(time.Minute)})
	waitUntil(t, "second read", func() bool {
		tags, _ := env.store.ListUnassignedTags()
		return len(tags) == 1 && tags[0].ReadCount == 2 && tags[0].AntennaID == "ant-2"
	})
}

func TestUnassignedRecorder_NilStoreOrBusIsNoop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Must not panic.
	NewServer("127.0.0.1", 0, nil, nil, nil, nil, "c").startUnassignedRecorder(ctx)
	NewServer("127.0.0.1", 0, events.NewEventBus(1), nil, nil, nil, "c").startUnassignedRecorder(ctx)
}

// sseEvents connects to the SSE handler and returns a channel of decoded tag_detected payloads.
func sseTagEvents(t *testing.T, s *Server) (chan map[string]any, func()) {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(s.SSEHandler))
	resp, err := http.Get(ts.URL)
	if err != nil {
		t.Fatalf("SSE connect failed: %v", err)
	}
	out := make(chan map[string]any, 10)
	go func() {
		defer close(out)
		sc := bufio.NewScanner(resp.Body)
		var event string
		for sc.Scan() {
			line := sc.Text()
			switch {
			case strings.HasPrefix(line, "event: "):
				event = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: ") && event == "tag_detected":
				var payload map[string]any
				if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &payload); err == nil {
					out <- payload
				}
			}
		}
	}()
	return out, func() { resp.Body.Close(); ts.Close() }
}

func nextSSE(t *testing.T, ch chan map[string]any) map[string]any {
	t.Helper()
	select {
	case p, ok := <-ch:
		if !ok {
			t.Fatal("SSE stream closed")
		}
		return p
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for SSE tag_detected")
	}
	return nil
}

func TestSSE_TagDetected_AssignedTagCarriesKanbanFields(t *testing.T) {
	env := newKanbanTestEnv(t, nil)
	env.linkKanban(t, "ASSIGNED", testKanban())
	ch, closeSSE := sseTagEvents(t, env.server)
	defer closeSSE()
	waitUntil(t, "SSE subscriber", func() bool {
		env.server.mu.RLock()
		defer env.server.mu.RUnlock()
		return len(env.server.sseClients) == 1
	})

	env.bus.Publish(events.TagDetected{EPC: "ASSIGNED", AntennaID: "ant-1", RSSI: 55, Timestamp: time.Now()})
	p := nextSSE(t, ch)

	kanban, ok := p["kanban"].(map[string]any)
	if !ok {
		t.Fatalf("expected kanban object in payload, got %+v", p)
	}
	if kanban["code"] != "EXP758SF M16" || kanban["description"] != "Burlete" || kanban["standard_pack"] != float64(250) || kanban["unit"] != "PZ" || kanban["customer"] != "Ford" {
		t.Errorf("unexpected kanban fields: %+v", kanban)
	}
	if zones, _ := kanban["zones"].([]any); len(zones) != 2 {
		t.Errorf("expected zones in kanban, got %+v", kanban["zones"])
	}
	if _, present := p["unassigned"]; present {
		t.Errorf("assigned tag must not carry the unassigned flag, got %+v", p)
	}
}

func TestSSE_TagDetected_UnassignedTagIsFlagged(t *testing.T) {
	env := newKanbanTestEnv(t, nil)
	ch, closeSSE := sseTagEvents(t, env.server)
	defer closeSSE()
	waitUntil(t, "SSE subscriber", func() bool {
		env.server.mu.RLock()
		defer env.server.mu.RUnlock()
		return len(env.server.sseClients) == 1
	})

	env.bus.Publish(events.TagDetected{EPC: "NEWTAG", AntennaID: "ant-1", RSSI: 55, Timestamp: time.Now()})
	p := nextSSE(t, ch)

	if p["unassigned"] != true {
		t.Errorf("expected unassigned:true, got %+v", p)
	}
	if _, present := p["kanban"]; present {
		t.Errorf("unassigned tag must not carry a kanban object, got %+v", p)
	}
}

// Regression: the pre-existing tag_detected fields are unchanged.
func TestSSE_TagDetected_KeepsExistingFields(t *testing.T) {
	env := newKanbanTestEnv(t, nil)
	ch, closeSSE := sseTagEvents(t, env.server)
	defer closeSSE()
	waitUntil(t, "SSE subscriber", func() bool {
		env.server.mu.RLock()
		defer env.server.mu.RUnlock()
		return len(env.server.sseClients) == 1
	})

	ts := time.Date(2026, 11, 5, 10, 0, 0, 0, time.UTC)
	env.bus.Publish(events.TagDetected{EPC: "NEWTAG", AntennaID: "ant-1", RSSI: 55, Timestamp: ts})
	p := nextSSE(t, ch)

	if p["epc"] != "NEWTAG" || p["antenna_id"] != "ant-1" || p["rssi"] != float64(55) || p["timestamp"] != "2026-11-05T10:00:00Z" {
		t.Errorf("event fields changed: %+v", p)
	}
	if p["suggested_action"] != "entrada" || p["suggested_reason"] != "unknown:tool_not_found" {
		t.Errorf("suggestion fields changed: %+v", p)
	}
}

// SSE without a store (existing tests) keeps working and does not claim anything about kanbans.
func TestSSE_TagDetected_WithoutStoreHasNoKanbanFlags(t *testing.T) {
	bus := events.NewEventBus(10)
	defer bus.Close()
	srv := NewServer("127.0.0.1", 0, bus, nil, nil, nil, "c")
	ch, closeSSE := sseTagEvents(t, srv)
	defer closeSSE()
	waitUntil(t, "SSE subscriber", func() bool { srv.mu.RLock(); defer srv.mu.RUnlock(); return len(srv.sseClients) == 1 })

	bus.Publish(events.TagDetected{EPC: "X", AntennaID: "a", Timestamp: time.Now()})
	p := nextSSE(t, ch)

	if _, present := p["unassigned"]; present {
		t.Errorf("unexpected unassigned flag without a store: %+v", p)
	}
	if p["suggested_action"] != "unknown" {
		t.Errorf("expected legacy suggestion, got %+v", p)
	}
}

// Tags not read for 24h leave the inbox; a tag read again reappears.
func TestHandleUnassignedTags_HidesTagsNotSeenFor24h(t *testing.T) {
	env := newKanbanTestEnv(t, nil)
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	env.server.now = func() time.Time { return now }
	env.store.UpsertUnassignedTag("A000000000000003", "ant-1", now.Add(-25*time.Hour), 2)
	env.store.UpsertUnassignedTag("A000000000000004", "ant-1", now.Add(-time.Hour), 3)

	rec, tags := getUnassigned(t, env.server)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if len(tags) != 1 || tags[0].EPC != "A000000000000004" {
		t.Fatalf("expected only the tag seen within 24h, got %s", rec.Body.String())
	}

	env.store.UpsertUnassignedTag("A000000000000003", "ant-1", now, 1)
	_, tags = getUnassigned(t, env.server)
	if len(tags) != 2 {
		t.Errorf("expected the re-read tag to reappear, got %d tags", len(tags))
	}
}
