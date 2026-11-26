package sync

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/amg-rfid/amg-rfid-gateway/internal/cache"
	"github.com/amg-rfid/amg-rfid-gateway/internal/localstore"
)

type sentConfirmation struct {
	id         string
	occurredAt time.Time
}

type idempotencyEnv struct {
	*backoffEnv
	sends  *[]sentConfirmation
	status *error
}

// newIdempotencyEnv is a real store whose VPS records the idempotency fields
// of each send and answers with *status.
func newIdempotencyEnv(t *testing.T) *idempotencyEnv {
	t.Helper()
	c, err := cache.NewSQLite(filepath.Join(t.TempDir(), "cache.db"))
	if err != nil {
		t.Fatalf("open cache: %v", err)
	}
	t.Cleanup(func() { c.Close() })
	store := localstore.New(c.GetDB())
	var sends []sentConfirmation
	var status error
	vps := &mockVPSClient{sendConfirmIDFunc: func(_, _, _, _, id string, occurredAt time.Time) error {
		sends = append(sends, sentConfirmation{id, occurredAt})
		return status
	}}
	return &idempotencyEnv{
		backoffEnv: &backoffEnv{db: c.GetDB(), store: store, ts: NewToolsSync(vps, store, "company123", time.Hour, time.Minute)},
		sends:      &sends,
		status:     &status,
	}
}

const queuedID = "3f2b8c1e-9a4d-4e6f-8b1a-2c3d4e5f6a7b"

var queuedAt = time.Date(2026, 11, 26, 18, 30, 1, 123456789, time.UTC)

func (e *idempotencyEnv) queue(t *testing.T) int64 {
	t.Helper()
	conf, err := e.store.CreateConfirmationWithID("EPC-1", "entrada", "ant-1", queuedID, queuedAt, 0)
	if err != nil {
		t.Fatalf("queue: %v", err)
	}
	return conf.ID
}

func (e *idempotencyEnv) assertEverySend(t *testing.T, n int, id string, occurredAt time.Time) {
	t.Helper()
	if len(*e.sends) != n {
		t.Fatalf("sends = %d, want %d", len(*e.sends), n)
	}
	for i, s := range *e.sends {
		if s.id != id || !s.occurredAt.Equal(occurredAt) {
			t.Errorf("send %d = %q/%v, want %q/%v", i, s.id, s.occurredAt, id, occurredAt)
		}
	}
}

func TestToolsSync_RetriesResendSameConfirmationIDAndOccurredAt(t *testing.T) {
	e := newIdempotencyEnv(t)
	id := e.queue(t)
	*e.status = statusErr(503)

	e.ts.performFlush()
	e.makeDue(t, id)
	e.ts.performFlush()

	e.assertEverySend(t, 2, queuedID, queuedAt)
}

func TestToolsSync_RequeuedConfirmationKeepsIDAndOccurredAt(t *testing.T) {
	e := newIdempotencyEnv(t)
	id := e.queue(t)
	*e.status = statusErr(409) // parks it
	e.ts.performFlush()
	if _, err := e.store.RequeueStuckConfirmations(MaxRetries); err != nil {
		t.Fatalf("requeue: %v", err)
	}
	*e.status = nil
	e.makeDue(t, id)
	e.ts.performFlush()

	e.assertEverySend(t, 2, queuedID, queuedAt)
}

// The VPS answers a replay of an applied confirmation with 200: synced.
func TestToolsSync_ReplayAcceptedMarksSynced(t *testing.T) {
	e := newIdempotencyEnv(t)
	e.queue(t)

	e.ts.performFlush()

	if n, err := e.store.GetPendingConfirmationsCount(); err != nil || n != 0 {
		t.Fatalf("pending = %d (err %v), want 0", n, err)
	}
}

// A row queued before the upgrade has no occurred_at: it is omitted (the VPS
// stamps the movement), never replaced by a timestamp that may be out of range.
func TestToolsSync_LegacyRowOmitsOccurredAt(t *testing.T) {
	e := newIdempotencyEnv(t)
	ts := time.Date(2026, 11, 20, 8, 0, 0, 0, time.UTC)
	if _, err := e.db.Exec(`INSERT INTO pending_confirmations (uii, action, antenna_id, timestamp, confirmation_id) VALUES ('EPC-OLD', 'entrada', 'ant-1', ?, ?)`, ts, queuedID); err != nil {
		t.Fatalf("seed: %v", err)
	}

	e.ts.performFlush()

	e.assertEverySend(t, 1, queuedID, time.Time{})
}

// New VPS error codes are 4xx rejections: parked like any other.
func TestToolsSync_IdempotencyRejectionsPark(t *testing.T) {
	for _, code := range []int{409, 400} { // confirmation_id_conflict, occurred_at_out_of_range
		e := newIdempotencyEnv(t)
		e.queue(t)
		*e.status = statusErr(code)

		e.ts.performFlush()

		if n, _ := e.store.GetStuckConfirmationsCount(MaxRetries); n != 1 {
			t.Errorf("%d: stuck = %d, want 1 (parked)", code, n)
		}
	}
}
