package web

import (
	"database/sql"
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

const notAssignedMessage = "No se puede registrar un movimiento hasta asignar la tag a un kanban."

// fakeConfirmVPS is a reachable VPS (health + gateway confirm) that records confirmations.
type fakeConfirmVPS struct {
	*httptest.Server
	confirms atomic.Int32
	last     atomic.Value // map[string]any
}

func newFakeConfirmVPS(t *testing.T) *fakeConfirmVPS {
	t.Helper()
	f := &fakeConfirmVPS{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.WriteHeader(http.StatusOK)
		case "/api/v1/gateway/confirm":
			var sent map[string]any
			json.NewDecoder(r.Body).Decode(&sent)
			f.last.Store(sent)
			f.confirms.Add(1)
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"status":"OK"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.Close)
	return f
}

// seedKanbanLink adds the kanban cache tables to a hand-built test DB and links a tag.
func seedKanbanLink(t *testing.T, db *sql.DB, uii string) {
	t.Helper()
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS kanbans (
			code TEXT PRIMARY KEY, description TEXT, standard_pack REAL, unit TEXT, zones TEXT,
			extrusora TEXT, position TEXT, area TEXT, customer TEXT, card_type TEXT, sku TEXT,
			last_synced_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE IF NOT EXISTS tag_kanban_links (
			uii TEXT PRIMARY KEY, kanban_code TEXT NOT NULL, last_synced_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		INSERT OR REPLACE INTO kanbans (code, description, zones) VALUES ('K-1', 'Test kanban', '[]');
	`)
	if err != nil {
		t.Fatalf("failed to create kanban tables: %v", err)
	}
	if _, err := db.Exec(`INSERT OR REPLACE INTO tag_kanban_links (uii, kanban_code) VALUES (?, 'K-1')`, uii); err != nil {
		t.Fatalf("failed to link tag: %v", err)
	}
}

func postConfirm(s *Server, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	s.handleConfirm(rec, httptest.NewRequest(http.MethodPost, "/api/confirm", strings.NewReader(body)))
	return rec
}

func (e *kanbanTestEnv) pendingConfirmations(t *testing.T) []localstore.PendingConfirmation {
	t.Helper()
	list, err := e.store.GetUnsyncedConfirmations(100)
	if err != nil {
		t.Fatalf("failed to read the queue: %v", err)
	}
	return list
}

func assertRejectedNotAssigned(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp ConfirmResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON %q: %v", rec.Body.String(), err)
	}
	if resp.Success || resp.Message != notAssignedMessage || resp.Code != "tag_not_assigned" {
		t.Errorf("unexpected rejection body: %+v", resp)
	}
}

func TestHandleConfirm_RejectsUnassignedTagAndQueuesNothing(t *testing.T) {
	env := newKanbanTestEnv(t, nil)
	vps := newFakeConfirmVPS(t)
	env.server.vpsClient = httpclient.NewVPSClient(vps.URL, 2*time.Second, "tok")

	rec := postConfirm(env.server, `{"uii":"UNKNOWN-EPC","action":"entrada"}`)

	assertRejectedNotAssigned(t, rec)
	if n := len(env.pendingConfirmations(t)); n != 0 {
		t.Errorf("expected nothing queued, got %d", n)
	}
	if vps.confirms.Load() != 0 {
		t.Errorf("the VPS must not receive a confirmation, got %d", vps.confirms.Load())
	}
}

func TestHandleConfirm_RejectsUnassignedTagWhileOfflineToo(t *testing.T) {
	env := newKanbanTestEnv(t, nil)
	env.server.vpsClient = httpclient.NewVPSClient("http://localhost:59999", 100*time.Millisecond, "tok")

	rec := postConfirm(env.server, `{"uii":"UNKNOWN-EPC","action":"salida"}`)

	assertRejectedNotAssigned(t, rec)
	if n := len(env.pendingConfirmations(t)); n != 0 {
		t.Errorf("expected nothing queued offline, got %d", n)
	}
}

// A legacy tool tag synced without a kanban is "unassigned" as well.
func TestHandleConfirm_RejectsKnownToolTagWithoutKanban(t *testing.T) {
	env := newKanbanTestEnv(t, nil)
	env.server.vpsClient = httpclient.NewVPSClient("http://localhost:59999", 100*time.Millisecond, "tok")
	if err := env.store.UpsertToolsFromSync([]localstore.SyncDataItem{
		{ID: 1, ToolID: 1, UII: "LEGACY-1", SKU: "S", Name: "Hammer", Status: "active", Active: true},
	}); err != nil {
		t.Fatalf("seed failed: %v", err)
	}

	assertRejectedNotAssigned(t, postConfirm(env.server, `{"uii":"LEGACY-1","action":"entrada"}`))
	if n := len(env.pendingConfirmations(t)); n != 0 {
		t.Errorf("expected nothing queued, got %d", n)
	}
}

func TestHandleConfirm_RejectsTagWhoseLinkWasClearedBySync(t *testing.T) {
	env := newKanbanTestEnv(t, nil)
	env.server.vpsClient = httpclient.NewVPSClient("http://localhost:59999", 100*time.Millisecond, "tok")
	k := testKanban()
	env.linkKanban(t, "A0000001", k)
	// Next sync returns the tag inactive.
	env.store.ApplyKanbanLinks([]localstore.SyncDataItem{{ID: 1, ToolID: 1, UII: "A0000001", SKU: "S", Name: "N", Active: false, Kanban: &k}})

	assertRejectedNotAssigned(t, postConfirm(env.server, `{"uii":"A0000001","action":"entrada"}`))
	if n := len(env.pendingConfirmations(t)); n != 0 {
		t.Errorf("expected nothing queued, got %d", n)
	}
}

// Regression: an assigned tag confirms exactly as before (online path).
func TestHandleConfirm_AssignedTagConfirmsOnlineAsBefore(t *testing.T) {
	env := newKanbanTestEnv(t, nil)
	vps := newFakeConfirmVPS(t)
	env.server.vpsClient = httpclient.NewVPSClient(vps.URL, 2*time.Second, "tok")
	env.linkKanban(t, "A0000001", testKanban())

	rec := postConfirm(env.server, `{"uii":"A0000001","action":"entrada","antenna_id":"ant-2"}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp ConfirmResponse
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if !resp.Success || resp.Mode != "online" || resp.Message != "Confirmación enviada al VPS" || resp.Code != "" {
		t.Errorf("response changed: %+v", resp)
	}
	sent, _ := vps.last.Load().(map[string]any)
	if vps.confirms.Load() != 1 || sent["uii"] != "A0000001" || sent["action"] != "entrada" || sent["antenna_id"] != "ant-2" || sent["company_id"] != "test-company" {
		t.Errorf("unexpected VPS confirmation (%d): %+v", vps.confirms.Load(), sent)
	}
	if n := len(env.pendingConfirmations(t)); n != 0 {
		t.Errorf("online confirmation must not be queued, got %d", n)
	}
}

// Regression: an assigned tag confirms exactly as before (offline queue path).
func TestHandleConfirm_AssignedTagIsQueuedOfflineAsBefore(t *testing.T) {
	env := newKanbanTestEnv(t, nil)
	env.server.vpsClient = httpclient.NewVPSClient("http://localhost:59999", 100*time.Millisecond, "tok")
	env.linkKanban(t, "A0000001", testKanban())

	rec := postConfirm(env.server, `{"uii":"A0000001","action":"salida"}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp ConfirmResponse
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if !resp.Success || resp.Mode != "offline" || resp.Message != "Confirmación guardada localmente (modo sin conexión)" {
		t.Errorf("response changed: %+v", resp)
	}
	queue := env.pendingConfirmations(t)
	if len(queue) != 1 || queue[0].UII != "A0000001" || queue[0].Action != "salida" || queue[0].AntennaID != "manual" || queue[0].Synced || queue[0].RetryCount != 0 {
		t.Errorf("unexpected queue: %+v", queue)
	}
}

func TestHandleConfirm_EmptyUIIIsBadRequest(t *testing.T) {
	env := newKanbanTestEnv(t, nil)
	for _, body := range []string{`{"action":"entrada"}`, `{"uii":"  ","action":"entrada"}`} {
		rec := postConfirm(env.server, body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: expected 400, got %d", body, rec.Code)
		}
	}
	if n := len(env.pendingConfirmations(t)); n != 0 {
		t.Errorf("expected nothing queued, got %d", n)
	}
}

// Fail closed: if the assignment cannot be verified, nothing is confirmed or queued.
func TestHandleConfirm_FailsClosedWhenAssignmentCannotBeVerified(t *testing.T) {
	t.Run("no local store", func(t *testing.T) {
		vps := newFakeConfirmVPS(t)
		srv := NewServer("127.0.0.1", 0, nil, nil, nil, httpclient.NewVPSClient(vps.URL, time.Second, "tok"), "c")
		rec := postConfirm(srv, `{"uii":"A1","action":"entrada"}`)
		if rec.Code != http.StatusInternalServerError || vps.confirms.Load() != 0 {
			t.Errorf("expected 500 and no VPS call, got %d (%d calls)", rec.Code, vps.confirms.Load())
		}
	})

	t.Run("lookup error", func(t *testing.T) {
		env := newKanbanTestEnv(t, nil)
		env.linkKanban(t, "A0000001", testKanban())
		env.server.vpsClient = httpclient.NewVPSClient("http://localhost:59999", 100*time.Millisecond, "tok")
		if _, err := env.db.Exec(`DROP TABLE tag_kanban_links`); err != nil {
			t.Fatalf("drop failed: %v", err)
		}

		rec := postConfirm(env.server, `{"uii":"A0000001","action":"entrada"}`)

		if rec.Code != http.StatusInternalServerError {
			t.Errorf("expected 500, got %d: %s", rec.Code, rec.Body.String())
		}
		if n := len(env.pendingConfirmations(t)); n != 0 {
			t.Errorf("expected nothing queued, got %d", n)
		}
	})
}

type storedConfirmation struct {
	UII, Action, Antenna string
	Synced               bool
	SyncedAt             *time.Time
}

func (e *kanbanTestEnv) allConfirmations(t *testing.T) []storedConfirmation {
	t.Helper()
	rows, err := e.db.Query(`SELECT uii, action, antenna_id, synced, synced_at FROM pending_confirmations ORDER BY id`)
	if err != nil {
		t.Fatalf("failed to read confirmations: %v", err)
	}
	defer rows.Close()
	var list []storedConfirmation
	for rows.Next() {
		var c storedConfirmation
		if err := rows.Scan(&c.UII, &c.Action, &c.Antenna, &c.Synced, &c.SyncedAt); err != nil {
			t.Fatalf("scan failed: %v", err)
		}
		list = append(list, c)
	}
	return list
}

// Every confirmation is recorded locally: an online success is kept as synced
// with the send time, and is never queued for a second send.
func TestHandleConfirm_OnlineSuccessIsRecordedAsSynced(t *testing.T) {
	env := newKanbanTestEnv(t, nil)
	vps := newFakeConfirmVPS(t)
	env.server.vpsClient = httpclient.NewVPSClient(vps.URL, 2*time.Second, "tok")
	env.linkKanban(t, "A0000001", testKanban())
	before := time.Now().UTC().Add(-time.Second)

	rec := postConfirm(env.server, `{"uii":"A0000001","action":"salida","antenna_id":"ant-2"}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	all := env.allConfirmations(t)
	if len(all) != 1 {
		t.Fatalf("expected 1 stored confirmation, got %+v", all)
	}
	got := all[0]
	if got.UII != "A0000001" || got.Action != "salida" || got.Antenna != "ant-2" || !got.Synced || got.SyncedAt == nil {
		t.Fatalf("unexpected stored confirmation: %+v", got)
	}
	if got.SyncedAt.Before(before.Truncate(time.Second)) || got.SyncedAt.After(time.Now().Add(time.Second)) {
		t.Errorf("synced_at %v is not the send time", *got.SyncedAt)
	}
	if n := len(env.pendingConfirmations(t)); n != 0 {
		t.Errorf("online confirmation must not be pending, got %d", n)
	}
	if vps.confirms.Load() != 1 {
		t.Errorf("the VPS must receive exactly one confirmation, got %d", vps.confirms.Load())
	}
}

// Offline confirmations stay pending (not synced, no synced_at).
func TestHandleConfirm_OfflineStaysPendingWithoutSyncedAt(t *testing.T) {
	env := newKanbanTestEnv(t, nil)
	env.server.vpsClient = httpclient.NewVPSClient("http://localhost:59999", 100*time.Millisecond, "tok")
	env.linkKanban(t, "A0000001", testKanban())

	postConfirm(env.server, `{"uii":"A0000001","action":"entrada"}`)

	all := env.allConfirmations(t)
	if len(all) != 1 || all[0].Synced || all[0].SyncedAt != nil {
		t.Errorf("expected one pending row without synced_at, got %+v", all)
	}
}

// A local record failure must not turn a confirmation the VPS already accepted into an error.
func TestHandleConfirm_OnlineSuccessSurvivesLocalRecordFailure(t *testing.T) {
	env := newKanbanTestEnv(t, nil)
	vps := newFakeConfirmVPS(t)
	env.server.vpsClient = httpclient.NewVPSClient(vps.URL, 2*time.Second, "tok")
	env.linkKanban(t, "A0000001", testKanban())
	if _, err := env.db.Exec(`DROP TABLE pending_confirmations`); err != nil {
		t.Fatalf("drop failed: %v", err)
	}

	rec := postConfirm(env.server, `{"uii":"A0000001","action":"entrada"}`)

	var resp ConfirmResponse
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if rec.Code != http.StatusOK || !resp.Success || resp.Mode != "online" {
		t.Errorf("VPS-accepted confirmation must still succeed: %d %+v", rec.Code, resp)
	}
}

// The panel shows these messages to operators, so even failures are Spanish.
func TestHandleConfirm_QueueFailureMessageIsSpanish(t *testing.T) {
	env := newKanbanTestEnv(t, nil)
	env.server.vpsClient = httpclient.NewVPSClient("http://localhost:59999", 100*time.Millisecond, "tok")
	env.linkKanban(t, "A0000001", testKanban())
	if _, err := env.db.Exec(`DROP TABLE pending_confirmations`); err != nil {
		t.Fatalf("drop failed: %v", err)
	}

	rec := postConfirm(env.server, `{"uii":"A0000001","action":"entrada"}`)

	var resp ConfirmResponse
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Success || resp.Mode != "offline" || resp.Message != "No se pudo guardar la confirmación en el gateway." {
		t.Errorf("unexpected failure response: %+v", resp)
	}
}

func TestHandleConfirm_ValidationErrorsAreSpanish(t *testing.T) {
	env := newKanbanTestEnv(t, nil)
	cases := []struct {
		name, body, want string
	}{
		{"malformed body", `{"uii":`, "Solicitud inválida."},
		{"invalid action", `{"uii":"E2806995000040104CF4558B","action":"bogus"}`, "Movimiento inválido: debe ser entrada o salida."},
		{"missing uii", `{"uii":"  ","action":"entrada"}`, "Falta el código de la tag (uii)."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := postConfirm(env.server, tc.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d", rec.Code)
			}
			var body map[string]string
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("invalid JSON: %v", err)
			}
			if body["error"] != tc.want {
				t.Errorf("error = %q, want %q", body["error"], tc.want)
			}
			if got := env.pendingConfirmations(t); len(got) != 0 {
				t.Errorf("rejected input must not queue anything, got %d", len(got))
			}
		})
	}
}

func TestHandleConfirm_RejectsWithQueueFullAtCapWithoutDroppingQueued(t *testing.T) {
	env := newKanbanTestEnv(t, queueCapConfig(1))
	env.server.vpsClient = httpclient.NewVPSClient("http://localhost:59999", 100*time.Millisecond, "tok")
	env.linkKanban(t, "A0000001", testKanban())
	if _, err := env.store.CreateConfirmation("OLDER-EPC", "entrada", "ant-1", time.Now(), 0); err != nil {
		t.Fatalf("seed failed: %v", err)
	}

	rec := postConfirm(env.server, `{"uii":"A0000001","action":"salida","antenna_id":"ant-1"}`)

	if rec.Code != http.StatusInsufficientStorage {
		t.Fatalf("expected 507, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp ConfirmResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	want := ConfirmResponse{
		Success: false,
		Message: "La cola del gateway está llena: no se guardó el movimiento. Revisá la conexión con el VPS.",
		Mode:    "offline",
		Code:    "queue_full",
	}
	if resp != want {
		t.Errorf("response = %+v, want %+v", resp, want)
	}
	queued := env.pendingConfirmations(t)
	if len(queued) != 1 || queued[0].UII != "OLDER-EPC" {
		t.Errorf("queue must keep only OLDER-EPC, got %+v", queued)
	}
}

func queueCapConfig(maxPending int) *config.GatewayConfig {
	return &config.GatewayConfig{MaxPendingConfirmations: &maxPending}
}
