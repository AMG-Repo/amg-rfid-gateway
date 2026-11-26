package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"

	"github.com/amg-rfid/amg-rfid-gateway/internal/httpclient"
)

var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func TestHandleConfirm_OnlineSendCarriesConfirmationIDAndOccurredAt(t *testing.T) {
	env := newKanbanTestEnv(t, nil)
	vps := newFakeConfirmVPS(t)
	env.server.vpsClient = httpclient.NewVPSClient(vps.URL, 2*time.Second, httpclient.StaticToken("tok"))
	env.linkKanban(t, "A0000001", testKanban())
	before := time.Now().UTC()

	postConfirm(env.server, `{"uii":"A0000001","action":"entrada"}`)

	sent, _ := vps.last.Load().(map[string]any)
	id, _ := sent["confirmation_id"].(string)
	if !uuidPattern.MatchString(id) {
		t.Errorf("confirmation_id = %v, want a UUID", sent["confirmation_id"])
	}
	raw, _ := sent["occurred_at"].(string)
	occurredAt, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil || occurredAt.Location() != time.UTC || occurredAt.Before(before.Add(-time.Second)) {
		t.Errorf("occurred_at = %q (err %v), want the confirm instant in UTC", raw, err)
	}
}

// A failed online send queues the same id and instant it sent.
func TestHandleConfirm_FailedOnlineSendQueuesSentIDAndOccurredAt(t *testing.T) {
	env := newKanbanTestEnv(t, nil)
	var sent map[string]any
	vps := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			return
		}
		json.NewDecoder(r.Body).Decode(&sent)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(vps.Close)
	env.server.vpsClient = httpclient.NewVPSClient(vps.URL, 2*time.Second, httpclient.StaticToken("tok"))
	env.linkKanban(t, "A0000001", testKanban())

	postConfirm(env.server, `{"uii":"A0000001","action":"salida"}`)

	queue := env.pendingConfirmations(t)
	if len(queue) != 1 {
		t.Fatalf("queued = %d, want 1", len(queue))
	}
	if sent["confirmation_id"] == nil || queue[0].ConfirmationID != sent["confirmation_id"] {
		t.Errorf("queued id %q, sent %v", queue[0].ConfirmationID, sent["confirmation_id"])
	}
	if got := queue[0].OccurredAt.Format(time.RFC3339Nano); got != sent["occurred_at"] {
		t.Errorf("queued occurred_at %q, sent %v", got, sent["occurred_at"])
	}
}

// The online-success record keeps the id and instant the VPS received.
func TestHandleConfirm_OnlineSuccessRecordsSentID(t *testing.T) {
	env := newKanbanTestEnv(t, nil)
	vps := newFakeConfirmVPS(t)
	env.server.vpsClient = httpclient.NewVPSClient(vps.URL, 2*time.Second, httpclient.StaticToken("tok"))
	env.linkKanban(t, "A0000001", testKanban())

	postConfirm(env.server, `{"uii":"A0000001","action":"entrada"}`)

	sent, _ := vps.last.Load().(map[string]any)
	var id, occurredAt string
	if err := env.db.QueryRow(`SELECT confirmation_id, occurred_at FROM pending_confirmations WHERE uii = 'A0000001'`).Scan(&id, &occurredAt); err != nil {
		t.Fatalf("read record: %v", err)
	}
	if id != sent["confirmation_id"] || occurredAt != sent["occurred_at"] {
		t.Errorf("recorded %q/%q, sent %v/%v", id, occurredAt, sent["confirmation_id"], sent["occurred_at"])
	}
}
