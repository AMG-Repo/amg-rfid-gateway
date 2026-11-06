package sync

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/amg-rfid/amg-rfid-gateway/internal/cache"
	"github.com/amg-rfid/amg-rfid-gateway/internal/localstore"
)

type backoffEnv struct {
	db    *sql.DB
	store *localstore.LocalStore
	ts    *ToolsSync
	sent  *[]string
	fail  *bool
}

func newBackoffEnv(t *testing.T, retryInterval time.Duration) *backoffEnv {
	t.Helper()
	c, err := cache.NewSQLite(filepath.Join(t.TempDir(), "cache.db"))
	if err != nil {
		t.Fatalf("open cache: %v", err)
	}
	t.Cleanup(func() { c.Close() })
	store := localstore.New(c.GetDB())

	var sent []string
	fail := true
	vps := &mockVPSClient{sendConfirmV2Func: func(_, uii, _, _ string) error {
		sent = append(sent, uii)
		if fail {
			return errors.New("vps down")
		}
		return nil
	}}
	return &backoffEnv{
		db:    c.GetDB(),
		store: store,
		ts:    NewToolsSync(vps, store, "company123", time.Hour, retryInterval),
		sent:  &sent,
		fail:  &fail,
	}
}

func (e *backoffEnv) seed(t *testing.T, uii string) int64 {
	t.Helper()
	conf, err := e.store.CreateConfirmation(uii, "entrada", "ant-1", time.Now(), 0)
	if err != nil {
		t.Fatalf("seed failed: %v", err)
	}
	return conf.ID
}

// makeDue moves the backoff of a row into the past, as if its wait elapsed.
func (e *backoffEnv) makeDue(t *testing.T, id int64) {
	t.Helper()
	if _, err := e.db.Exec(`UPDATE pending_confirmations SET next_attempt_at = datetime('now', '-1 second') WHERE id = ?`, id); err != nil {
		t.Fatalf("make due: %v", err)
	}
}

func (e *backoffEnv) retryCount(t *testing.T, id int64) int {
	t.Helper()
	var n int
	if err := e.db.QueryRow(`SELECT retry_count FROM pending_confirmations WHERE id = ?`, id).Scan(&n); err != nil {
		t.Fatalf("read retry_count: %v", err)
	}
	return n
}

// A just-failed confirmation waits out its backoff: the next tick does not
// resend it, and it is sent again once it is due.
func TestToolsSync_FailedConfirmationIsNotResentUntilDue(t *testing.T) {
	e := newBackoffEnv(t, time.Minute)
	id := e.seed(t, "EPC-1")

	e.ts.performFlush()
	if len(*e.sent) != 1 {
		t.Fatalf("first flush sent %d, want 1", len(*e.sent))
	}

	e.ts.performFlush()
	if len(*e.sent) != 1 {
		t.Fatalf("next tick resent a just-failed confirmation (%d sends); it must wait its backoff", len(*e.sent))
	}

	e.makeDue(t, id)
	e.ts.performFlush()
	if len(*e.sent) != 2 {
		t.Fatalf("a due confirmation was not resent (%d sends)", len(*e.sent))
	}
	if got := e.retryCount(t, id); got != 2 {
		t.Errorf("retry_count = %d, want 2", got)
	}
}

// The wait doubles with every failure: retryInterval * 2^(retry_count-1).
func TestToolsSync_BackoffGrowsWithEachFailure(t *testing.T) {
	e := newBackoffEnv(t, time.Minute)
	id := e.seed(t, "EPC-1")

	for attempt := 1; attempt <= 3; attempt++ {
		before := time.Now()
		e.ts.performFlush()
		after := time.Now()

		var secs float64
		if err := e.db.QueryRow(`SELECT CAST(strftime('%s', next_attempt_at) AS REAL) FROM pending_confirmations WHERE id = ?`, id).Scan(&secs); err != nil {
			t.Fatalf("attempt %d: read next_attempt_at: %v", attempt, err)
		}
		want := time.Minute * time.Duration(1<<(attempt-1))
		lo := before.Add(want).Add(-2 * time.Second).Unix()
		hi := after.Add(want).Add(2 * time.Second).Unix()
		if int64(secs) < lo || int64(secs) > hi {
			t.Errorf("attempt %d: next attempt at %d, want within [%d, %d] (now + %s)", attempt, int64(secs), lo, hi, want)
		}
		e.makeDue(t, id)
	}
}

// Rows waiting out a backoff never fill the batch; rows never tried go out.
func TestToolsSync_WaitingRowsDoNotStarveNewConfirmations(t *testing.T) {
	e := newBackoffEnv(t, time.Minute)
	for i := 0; i < 150; i++ {
		id := e.seed(t, "WAITING")
		if _, err := e.db.Exec(`UPDATE pending_confirmations SET retry_count = 1, created_at = datetime('now', '-1 hour'), next_attempt_at = datetime('now', '+1 hour') WHERE id = ?`, id); err != nil {
			t.Fatalf("mark waiting: %v", err)
		}
	}
	e.seed(t, "FRESH")
	*e.fail = false

	e.ts.performFlush()

	if len(*e.sent) != 1 || (*e.sent)[0] != "FRESH" {
		t.Fatalf("sent = %v, want only FRESH", *e.sent)
	}
}

// Backoff does not change what stuck means: the failure that reaches MaxRetries
// leaves the row queued and counted as stuck, and it is never sent again.
func TestToolsSync_StuckSemanticsHoldWithBackoff(t *testing.T) {
	e := newBackoffEnv(t, time.Minute)
	id := e.seed(t, "EPC-1")
	if _, err := e.db.Exec(`UPDATE pending_confirmations SET retry_count = ? WHERE id = ?`, MaxRetries-1, id); err != nil {
		t.Fatalf("seed retries: %v", err)
	}

	e.ts.performFlush() // the last allowed attempt fails
	if got := e.retryCount(t, id); got != MaxRetries {
		t.Fatalf("retry_count = %d, want %d", got, MaxRetries)
	}
	if n, err := e.store.GetStuckConfirmationsCount(MaxRetries); err != nil || n != 1 {
		t.Fatalf("stuck count = %d (err %v), want 1", n, err)
	}

	e.makeDue(t, id)
	e.ts.performFlush()
	if len(*e.sent) != 1 {
		t.Errorf("a stuck confirmation was sent again (%d sends)", len(*e.sent))
	}

	// Requeue makes it sendable immediately, with no leftover wait.
	if n, err := e.store.RequeueStuckConfirmations(MaxRetries); err != nil || n != 1 {
		t.Fatalf("requeued = %d (err %v), want 1", n, err)
	}
	*e.fail = false
	e.ts.performFlush()
	if len(*e.sent) != 2 {
		t.Fatalf("a requeued confirmation was not sent right away (%d sends)", len(*e.sent))
	}
}
