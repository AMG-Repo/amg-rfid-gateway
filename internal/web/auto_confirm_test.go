package web

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/amg-rfid/amg-rfid-gateway/internal/autoconfirm"
	"github.com/amg-rfid/amg-rfid-gateway/internal/config"
	"github.com/amg-rfid/amg-rfid-gateway/internal/events"
	"github.com/amg-rfid/amg-rfid-gateway/internal/localstore"
	"github.com/amg-rfid/amg-rfid-shared-go/models"
)

func postAutoConfirm(s *Server, method, body, token string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, "/api/auto-confirm", strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	s.routes().ServeHTTP(rec, req)
	return rec
}

func TestAutoConfirm_SwitchPersistsAndUpdatesMode(t *testing.T) {
	env := newKanbanTestEnv(t, nil)

	rec := postAutoConfirm(env.server, http.MethodPost, `{"enabled":true}`, "")
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != `{"auto_confirm":true}` {
		t.Fatalf("enable = %d %s", rec.Code, rec.Body.String())
	}
	if enabled, err := env.store.AutoConfirmEnabled(); err != nil || !enabled {
		t.Errorf("persisted = %v, %v; want true", enabled, err)
	}
	if !env.server.AutoConfirmMode().Enabled() {
		t.Error("in-memory mode not switched on")
	}
	if body := getStatus(t, env.server); body["auto_confirm"] != true {
		t.Errorf("status auto_confirm = %v, want true", body["auto_confirm"])
	}

	rec = postAutoConfirm(env.server, http.MethodPost, `{"enabled":false}`, "")
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != `{"auto_confirm":false}` {
		t.Fatalf("disable = %d %s", rec.Code, rec.Body.String())
	}
	if enabled, _ := env.store.AutoConfirmEnabled(); enabled || env.server.AutoConfirmMode().Enabled() {
		t.Error("mode still automatic after disabling")
	}
}

func TestAutoConfirm_RejectsBadRequests(t *testing.T) {
	env := newKanbanTestEnv(t, nil)
	if rec := postAutoConfirm(env.server, http.MethodGet, "", ""); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET = %d, want 405", rec.Code)
	}
	for _, body := range []string{`nope`, `{}`, `{"enabled":"yes"}`} {
		if rec := postAutoConfirm(env.server, http.MethodPost, body, ""); rec.Code != http.StatusBadRequest {
			t.Errorf("body %s = %d, want 400", body, rec.Code)
		}
	}
	if env.server.AutoConfirmMode().Enabled() {
		t.Error("a rejected request changed the mode")
	}
}

func TestAutoConfirm_LANModeRequiresToken(t *testing.T) {
	env := newKanbanTestEnv(t, &config.GatewayConfig{WebAccessMode: "lan", WebAuthToken: "tok"})
	if rec := postAutoConfirm(env.server, http.MethodPost, `{"enabled":true}`, ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("without token = %d, want 401", rec.Code)
	}
	if env.server.AutoConfirmMode().Enabled() {
		t.Error("unauthorized request changed the mode")
	}
	if rec := postAutoConfirm(env.server, http.MethodPost, `{"enabled":true}`, "tok"); rec.Code != http.StatusOK {
		t.Errorf("with token = %d, want 200", rec.Code)
	}
}

func TestHandleStatus_ReportsAutoConfirmSettings(t *testing.T) {
	cfg := &config.GatewayConfig{AutoConfirmIdle: 90 * time.Second, AutoConfirmMinReads: 4}
	env := newKanbanTestEnv(t, cfg)
	body := getStatus(t, env.server)
	if body["auto_confirm"] != false || body["auto_confirm_idle_seconds"] != float64(90) || body["auto_confirm_min_reads"] != float64(4) {
		t.Errorf("status = %v", body)
	}
}

func TestHandleHistory_MarksAutoConfirmations(t *testing.T) {
	env := newKanbanTestEnv(t, nil)
	now := time.Now()
	if err := env.store.RecordSyncedConfirmationWithIDAuto("E-AUTO", "salida", "ant-1", "id-a", now, true); err != nil {
		t.Fatal(err)
	}
	if err := env.store.RecordSyncedConfirmationWithID("E-MAN", "entrada", "manual", "id-m", now); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	env.server.handleHistory(rec, httptest.NewRequest(http.MethodGet, "/api/history", nil))
	var items []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &items); err != nil {
		t.Fatalf("json: %v", err)
	}
	got := map[any]any{}
	for _, it := range items {
		got[it["uii"]] = it["auto"]
	}
	if got["E-AUTO"] != true || got["E-MAN"] != false {
		t.Errorf("auto markers = %v", got)
	}
}

func TestSSE_ForwardsAutoConfirmed(t *testing.T) {
	env := newKanbanTestEnv(t, nil)
	ts := httptest.NewServer(http.HandlerFunc(env.server.SSEHandler))
	t.Cleanup(ts.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer resp.Body.Close()
	r := bufio.NewReader(resp.Body)
	readUntil(t, r, "event: connected")

	env.bus.PublishAutoConfirmed(events.AutoConfirmed{UII: "E1", Action: "salida", ConfirmationID: "id-1"})

	readUntil(t, r, "event: auto_confirmed")
	data := readUntil(t, r, "data: ")
	var got map[string]any
	if err := json.Unmarshal([]byte(strings.TrimPrefix(data, "data: ")), &got); err != nil {
		t.Fatalf("data %q: %v", data, err)
	}
	if got["uii"] != "E1" || got["action"] != "salida" {
		t.Errorf("data = %v", got)
	}
}

func readUntil(t *testing.T, r *bufio.Reader, prefix string) string {
	t.Helper()
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				t.Fatalf("stream ended before %q", prefix)
			}
			t.Fatalf("read before %q: %v", prefix, err)
		}
		if strings.HasPrefix(line, prefix) {
			return strings.TrimSpace(line)
		}
	}
}

func TestAutoConfirm_ConcurrentSwitchesKeepStoredAndMemoryEqual(t *testing.T) {
	env := newKanbanTestEnv(t, nil)
	for round := 0; round < 20; round++ {
		var wg sync.WaitGroup
		for i := 0; i < 8; i++ {
			body := `{"enabled":true}`
			if i%2 == 1 {
				body = `{"enabled":false}`
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				postAutoConfirm(env.server, http.MethodPost, body, "")
			}()
		}
		wg.Wait()
		stored, err := env.store.AutoConfirmEnabled()
		if err != nil {
			t.Fatalf("stored: %v", err)
		}
		if mem := env.server.AutoConfirmMode().Enabled(); mem != stored {
			t.Fatalf("round %d: memory %v, stored %v", round, mem, stored)
		}
	}
}

// busyClaimer refuses manual claims, as while an automatic confirm of the tag
// is in flight.
type busyClaimer struct{ manual int }

func (c *busyClaimer) ClaimAuto(string, uint64) bool { return false }
func (c *busyClaimer) ClaimManual(string) bool       { c.manual++; return false }
func (c *busyClaimer) Release(uint64)                {}

func TestHandleConfirm_AutoInFlightAnswers409(t *testing.T) {
	env := newKanbanTestEnv(t, nil)
	env.linkKanban(t, "E1", localstore.Kanban{Code: "K1", Description: "Burlete", StandardPack: 10, Unit: "PZ"})
	c := &busyClaimer{}
	env.server.SetConfirmClaimer(c)

	rec := postConfirm(env.server, `{"uii":"E1","action":"entrada"}`)

	if rec.Code != http.StatusConflict {
		t.Fatalf("confirm = %d %s, want 409", rec.Code, rec.Body.String())
	}
	if got := strings.TrimSpace(rec.Body.String()); got != `{"error":"Este movimiento ya se está confirmando automáticamente."}` {
		t.Errorf("body = %s", got)
	}
	if c.manual != 1 {
		t.Errorf("manual claims = %d, want 1", c.manual)
	}
	if q, _ := env.store.GetUnsyncedConfirmations(10); len(q) != 0 {
		t.Errorf("queue = %+v, want nothing written", q)
	}
}

func TestHandleConfirm_ManualClaimConsumesTrackedPass(t *testing.T) {
	env := newKanbanTestEnv(t, nil)
	env.linkKanban(t, "E1", localstore.Kanban{Code: "K1", Description: "Burlete", StandardPack: 10, Unit: "PZ"})
	mode := &autoconfirm.Mode{}
	mode.Set(true)
	tracker := autoconfirm.NewTracker(autoconfirm.Config{Idle: time.Minute, MinReads: 1}, nopSink{}, mode)
	env.server.SetConfirmClaimer(tracker)

	if rec := postConfirm(env.server, `{"uii":"E1","action":"entrada"}`); rec.Code != http.StatusOK {
		t.Fatalf("confirm = %d %s", rec.Code, rec.Body.String())
	}
	if tracker.ClaimAuto("E1", 1) {
		t.Error("auto claim of the manually confirmed pass succeeded")
	}
}

type nopSink struct{}

func (nopSink) Store(models.Reading) error { return nil }
