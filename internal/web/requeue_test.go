package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/amg-rfid/amg-rfid-gateway/internal/config"
	syncpkg "github.com/amg-rfid/amg-rfid-gateway/internal/sync"
)

func postRequeue(env *kanbanTestEnv, method, authorization string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "/api/confirmations/requeue", nil)
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	rec := httptest.NewRecorder()
	env.server.routes().ServeHTTP(rec, req)
	return rec
}

func retryCountOf(t *testing.T, env *kanbanTestEnv, id int64) int {
	t.Helper()
	var n int
	if err := env.db.QueryRow(`SELECT retry_count FROM pending_confirmations WHERE id = ?`, id).Scan(&n); err != nil {
		t.Fatalf("read retry_count: %v", err)
	}
	return n
}

func seedWithRetries(t *testing.T, env *kanbanTestEnv, uii string, retries int) int64 {
	t.Helper()
	conf, err := env.store.CreateConfirmation(uii, "entrada", "ant-1", time.Now(), 0)
	if err != nil {
		t.Fatalf("seed failed: %v", err)
	}
	if _, err := env.db.Exec(`UPDATE pending_confirmations SET retry_count = ? WHERE id = ?`, retries, conf.ID); err != nil {
		t.Fatalf("set retries: %v", err)
	}
	return conf.ID
}

func TestHandleRequeue_ResetsOnlyStuckPendingConfirmations(t *testing.T) {
	env := newKanbanTestEnv(t, nil)
	stuckA := seedWithRetries(t, env, "STUCK-A", syncpkg.MaxRetries)
	stuckB := seedWithRetries(t, env, "STUCK-B", syncpkg.MaxRetries+3)
	retrying := seedWithRetries(t, env, "RETRYING", syncpkg.MaxRetries-1)
	fresh := seedWithRetries(t, env, "FRESH", 0)
	// A synced row with a high retry count is history, not queue.
	synced := seedWithRetries(t, env, "SYNCED", syncpkg.MaxRetries)
	if err := env.store.MarkConfirmationSynced(synced); err != nil {
		t.Fatalf("mark synced: %v", err)
	}

	rec := postRequeue(env, http.MethodPost, "")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON %q: %v", rec.Body.String(), err)
	}
	if body["requeued"] != float64(2) {
		t.Errorf("requeued = %#v, want 2", body["requeued"])
	}
	for name, id := range map[string]int64{"stuckA": stuckA, "stuckB": stuckB, "fresh": fresh} {
		if got := retryCountOf(t, env, id); got != 0 {
			t.Errorf("%s retry_count = %d, want 0", name, got)
		}
	}
	if got := retryCountOf(t, env, retrying); got != syncpkg.MaxRetries-1 {
		t.Errorf("non-stuck retry_count = %d, want it untouched at %d", got, syncpkg.MaxRetries-1)
	}
	if got := retryCountOf(t, env, synced); got != syncpkg.MaxRetries {
		t.Errorf("synced retry_count = %d, want it untouched at %d", got, syncpkg.MaxRetries)
	}
	if n, err := env.store.GetStuckConfirmationsCount(syncpkg.MaxRetries); err != nil || n != 0 {
		t.Errorf("stuck count = %d (err %v), want 0 after requeue", n, err)
	}
}

func TestHandleRequeue_NothingStuckAnswersZero(t *testing.T) {
	env := newKanbanTestEnv(t, nil)
	seedWithRetries(t, env, "FRESH", 0)

	rec := postRequeue(env, http.MethodPost, "")

	if rec.Code != http.StatusOK || rec.Body.String() != "{\"requeued\":0}\n" {
		t.Errorf("expected 200 {\"requeued\":0}, got %d %q", rec.Code, rec.Body.String())
	}
}

func TestHandleRequeue_RejectsOtherMethods(t *testing.T) {
	env := newKanbanTestEnv(t, nil)
	id := seedWithRetries(t, env, "STUCK", syncpkg.MaxRetries)

	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		if rec := postRequeue(env, method, ""); rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s: expected 405, got %d", method, rec.Code)
		}
	}
	if got := retryCountOf(t, env, id); got != syncpkg.MaxRetries {
		t.Errorf("retry_count = %d, want it untouched at %d", got, syncpkg.MaxRetries)
	}
}

func TestHandleRequeue_LANModeRequiresToken(t *testing.T) {
	env := newKanbanTestEnv(t, &config.GatewayConfig{WebAccessMode: "lan", WebAuthToken: "test-web-token"})
	id := seedWithRetries(t, env, "STUCK", syncpkg.MaxRetries)

	rec := postRequeue(env, http.MethodPost, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without token, got %d", rec.Code)
	}
	if rec.Header().Get("WWW-Authenticate") == "" {
		t.Error("expected WWW-Authenticate header on 401")
	}
	if got := retryCountOf(t, env, id); got != syncpkg.MaxRetries {
		t.Errorf("retry_count = %d, want it untouched at %d without auth", got, syncpkg.MaxRetries)
	}

	rec = postRequeue(env, http.MethodPost, "Bearer test-web-token")
	if rec.Code != http.StatusOK || retryCountOf(t, env, id) != 0 {
		t.Errorf("expected 200 and reset with token, got %d (retry_count %d)", rec.Code, retryCountOf(t, env, id))
	}
}
