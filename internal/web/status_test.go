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

func getStatus(t *testing.T, s *Server) map[string]any {
	t.Helper()
	rec := httptest.NewRecorder()
	s.handleStatus(rec, httptest.NewRequest(http.MethodGet, "/api/status", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON %q: %v", rec.Body.String(), err)
	}
	return body
}

func parseTime(t *testing.T, field string, v any) time.Time {
	t.Helper()
	str, ok := v.(string)
	if !ok {
		t.Fatalf("%s = %#v, want an RFC3339 string", field, v)
	}
	parsed, err := time.Parse(time.RFC3339, str)
	if err != nil {
		t.Fatalf("%s = %q is not RFC3339: %v", field, str, err)
	}
	return parsed
}

func TestHandleStatus_EmptyQueueHasNullTimesAndDefaultThreshold(t *testing.T) {
	env := newKanbanTestEnv(t, nil)

	body := getStatus(t, env.server)

	if v, present := body["oldest_pending_at"]; !present || v != nil {
		t.Errorf("oldest_pending_at = %#v (present %v), want null", v, present)
	}
	if v, present := body["last_sync_at"]; !present || v != nil {
		t.Errorf("last_sync_at = %#v (present %v), want null", v, present)
	}
	if v := body["pending_warning_threshold"]; v != float64(1000) {
		t.Errorf("pending_warning_threshold = %#v, want 1000", v)
	}
}

func TestHandleStatus_ReportsConfiguredThreshold(t *testing.T) {
	threshold := 25
	env := newKanbanTestEnv(t, &config.GatewayConfig{PendingWarningThreshold: &threshold})

	if v := getStatus(t, env.server)["pending_warning_threshold"]; v != float64(25) {
		t.Errorf("pending_warning_threshold = %#v, want 25", v)
	}
}

func TestHandleStatus_ReportsOldestPendingAndLastSync(t *testing.T) {
	env := newKanbanTestEnv(t, nil)
	env.linkKanban(t, "A0000001", testKanban())
	older := time.Now().UTC().Add(-3 * time.Hour).Truncate(time.Second)
	newer := time.Now().UTC().Add(-1 * time.Hour).Truncate(time.Second)
	syncedEarly := time.Now().UTC().Add(-48 * time.Hour).Truncate(time.Second)
	syncedLate := time.Now().UTC().Add(-2 * time.Minute).Truncate(time.Second)
	f := "2006-01-02 15:04:05"
	for _, row := range []struct {
		synced  bool
		created string
		syncAt  any
	}{
		{false, newer.Format(f), nil},
		{false, older.Format(f), nil},                                       // the oldest pending one
		{true, older.Add(-24 * time.Hour).Format(f), syncedEarly.Format(f)}, // synced rows are not "pending"
		{true, older.Format(f), syncedLate.Format(f)},
	} {
		if _, err := env.db.Exec(`INSERT INTO pending_confirmations (uii, action, antenna_id, synced, created_at, synced_at) VALUES ('A0000001', 'entrada', 'a', ?, ?, ?)`,
			row.synced, row.created, row.syncAt); err != nil {
			t.Fatalf("seed failed: %v", err)
		}
	}

	body := getStatus(t, env.server)

	if got := parseTime(t, "oldest_pending_at", body["oldest_pending_at"]); !got.Equal(older) {
		t.Errorf("oldest_pending_at = %v, want %v", got, older)
	}
	if got := parseTime(t, "last_sync_at", body["last_sync_at"]); !got.Equal(syncedLate) {
		t.Errorf("last_sync_at = %v, want %v", got, syncedLate)
	}
	if body["pending_count"] != float64(2) {
		t.Errorf("pending_count = %#v, want 2", body["pending_count"])
	}
}

// An online confirm is a successful send to the VPS, so it moves last_sync_at.
func TestHandleStatus_LastSyncFollowsOnlineConfirm(t *testing.T) {
	env := newKanbanTestEnv(t, nil)
	vps := newFakeConfirmVPS(t)
	env.server.vpsClient = httpclient.NewVPSClient(vps.URL, 2*time.Second, "tok")
	env.linkKanban(t, "A0000001", testKanban())
	postConfirm(env.server, `{"uii":"A0000001","action":"entrada"}`)

	body := getStatus(t, env.server)

	got := parseTime(t, "last_sync_at", body["last_sync_at"])
	if d := time.Since(got); d < -2*time.Second || d > time.Minute {
		t.Errorf("last_sync_at %v is not about now", got)
	}
	if body["oldest_pending_at"] != nil {
		t.Errorf("oldest_pending_at = %#v, want null", body["oldest_pending_at"])
	}
}

// Without a local store the new fields degrade to null / default and the old ones stay.
func TestHandleStatus_WithoutStoreKeepsExistingFields(t *testing.T) {
	srv := NewServer("127.0.0.1", 0, nil, nil, nil, nil, "c")

	body := getStatus(t, srv)

	for _, key := range []string{"vps_online", "pending_count", "tools_count", "pending_warning"} {
		if _, ok := body[key]; !ok {
			t.Errorf("existing field %q is missing", key)
		}
	}
	if body["oldest_pending_at"] != nil || body["last_sync_at"] != nil || body["pending_warning_threshold"] != float64(1000) {
		t.Errorf("unexpected new fields: %+v", body)
	}
}
