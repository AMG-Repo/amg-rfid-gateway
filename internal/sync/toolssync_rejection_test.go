package sync

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

// statusErr is an HTTP status failure as the VPS client reports it.
type statusErr int

func (e statusErr) Error() string   { return fmt.Sprintf("unexpected status code: %d", int(e)) }
func (e statusErr) StatusCode() int { return int(e) }

// A confirmation that keeps failing for transient reasons never runs out of
// retries: after MaxRetries failures it is still retried, not stuck.
func TestToolsSync_TransientFailuresNeverExhaustRetries(t *testing.T) {
	e := newBackoffEnv(t, time.Minute)
	id := e.seed(t, "EPC-1")
	if _, err := e.db.Exec(`UPDATE pending_confirmations SET retry_count = ? WHERE id = ?`, MaxRetries-1, id); err != nil {
		t.Fatalf("seed retries: %v", err)
	}

	for i := 0; i < 3; i++ {
		e.makeDue(t, id)
		e.ts.performFlush()
	}
	if len(*e.sent) != 3 {
		t.Fatalf("sends = %d, want 3 (a transient failure stopped the retries)", len(*e.sent))
	}
	if n, err := e.store.GetStuckConfirmationsCount(MaxRetries); err != nil || n != 0 {
		t.Fatalf("stuck count = %d (err %v), want 0", n, err)
	}

	*e.fail = false
	e.makeDue(t, id)
	e.ts.performFlush()
	if n, err := e.store.GetPendingConfirmationsCount(); err != nil || n != 0 {
		t.Fatalf("pending = %d (err %v), want 0 once the VPS answers", n, err)
	}
}

// Statuses that can succeed later are transient; only other 4xx park the row.
func TestToolsSync_TransientStatusesAreRetried(t *testing.T) {
	for _, err := range []error{
		errors.New("request failed: dial tcp: i/o timeout"),
		statusErr(401), statusErr(408), statusErr(429), statusErr(500), statusErr(503),
	} {
		t.Run(err.Error(), func(t *testing.T) {
			e := newBackoffEnv(t, time.Minute)
			id := e.seed(t, "EPC-1")
			*e.failErr = err

			e.ts.performFlush()

			if got := e.retryCount(t, id); got != 1 {
				t.Errorf("retry_count = %d, want 1", got)
			}
			if n, _ := e.store.GetStuckConfirmationsCount(MaxRetries); n != 0 {
				t.Errorf("stuck count = %d, want 0", n)
			}
			if e.ts.IsVPSOnline() {
				t.Errorf("VPS reported online after a transient failure")
			}
		})
	}
}

// A permanent rejection parks the confirmation on the first attempt and does
// not mark the VPS offline: it answered.
func TestToolsSync_PermanentRejectionParksImmediately(t *testing.T) {
	for _, code := range []int{400, 404, 409, 422} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			e := newBackoffEnv(t, time.Minute)
			id := e.seed(t, "EPC-1")
			*e.failErr = statusErr(code)

			e.ts.performFlush()

			if got := e.retryCount(t, id); got < MaxRetries {
				t.Errorf("retry_count = %d, want >= %d (parked)", got, MaxRetries)
			}
			if n, _ := e.store.GetStuckConfirmationsCount(MaxRetries); n != 1 {
				t.Errorf("stuck count = %d, want 1", n)
			}
			if !e.ts.IsVPSOnline() {
				t.Errorf("VPS reported offline after it answered %d", code)
			}
		})
	}
}
