package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/amg-rfid/amg-rfid-gateway/internal/antenna"
	"github.com/amg-rfid/amg-rfid-gateway/internal/config"
)

type fakeAntennaProvider struct {
	statuses []antenna.AntennaStatus
}

func (f fakeAntennaProvider) GetAntennaStatuses() []antenna.AntennaStatus { return f.statuses }

func TestHandleAntennas_ReportsEveryConfiguredAntenna(t *testing.T) {
	lastRead := time.Date(2026, 11, 6, 14, 38, 28, 0, time.UTC)
	cfg := &config.GatewayConfig{Antennas: []config.AntennaConfig{
		{ID: "UR4-001", Enabled: true, Zone: "Entrada"},
		{ID: "ANT-002", Enabled: true, Zone: "salida"},
		{ID: "ANT-003", Enabled: true},
		{ID: "ANT-004", Enabled: false, Zone: "salida"},
	}}
	env := newKanbanTestEnv(t, cfg)
	env.server.SetAntennaProvider(fakeAntennaProvider{statuses: []antenna.AntennaStatus{
		{ID: "UR4-001", Connected: true, LastReadAt: &lastRead},
		{ID: "ANT-002", Connected: false},
	}})

	rec := httptest.NewRecorder()
	env.server.routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/antennas", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var got []AntennaHealth
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	want := []AntennaHealth{
		{ID: "UR4-001", Zone: "entrada", State: "reading", LastReadAt: &lastRead},
		{ID: "ANT-002", Zone: "salida", State: "reconnecting"},
		{ID: "ANT-003", Zone: "", State: "reconnecting"},
		{ID: "ANT-004", Zone: "salida", State: "stopped"},
	}
	if len(got) != len(want) {
		t.Fatalf("expected %d antennas, got %d: %+v", len(want), len(got), got)
	}
	for i := range want {
		g, w := got[i], want[i]
		if g.ID != w.ID || g.Zone != w.Zone || g.State != w.State {
			t.Errorf("antenna %d: expected %+v, got %+v", i, w, g)
		}
		if (w.LastReadAt == nil) != (g.LastReadAt == nil) || (w.LastReadAt != nil && !w.LastReadAt.Equal(*g.LastReadAt)) {
			t.Errorf("antenna %s: expected last_read_at %v, got %v", w.ID, w.LastReadAt, g.LastReadAt)
		}
	}
	if !json.Valid(rec.Body.Bytes()) || !containsKey(rec.Body.Bytes(), "last_read_at") {
		t.Errorf("expected last_read_at key in JSON, got %s", rec.Body.String())
	}
}

func TestHandleAntennas_WithoutProviderReportsConfiguredAntennasAsReconnecting(t *testing.T) {
	cfg := &config.GatewayConfig{Antennas: []config.AntennaConfig{{ID: "UR4-001", Enabled: true}}}
	env := newKanbanTestEnv(t, cfg)

	rec := httptest.NewRecorder()
	env.server.routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/antennas", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var got []AntennaHealth
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(got) != 1 || got[0].State != "reconnecting" || got[0].LastReadAt != nil {
		t.Errorf("expected one reconnecting antenna without reads, got %+v", got)
	}
}

func TestHandleAntennas_MethodNotAllowed(t *testing.T) {
	env := newKanbanTestEnv(t, &config.GatewayConfig{})

	rec := httptest.NewRecorder()
	env.server.routes().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/antennas", nil))

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rec.Code)
	}
}

func containsKey(body []byte, key string) bool {
	var items []map[string]json.RawMessage
	if err := json.Unmarshal(body, &items); err != nil {
		return false
	}
	for _, item := range items {
		if _, ok := item[key]; ok {
			return true
		}
	}
	return false
}
