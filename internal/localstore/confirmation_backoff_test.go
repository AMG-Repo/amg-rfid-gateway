package localstore

import (
	"testing"
	"time"
)

func seedPendingWithRetries(t *testing.T, s *LocalStore, uii string, retries int) int64 {
	t.Helper()
	conf, err := s.CreateConfirmation(uii, "entrada", "ant-1", time.Now(), 0)
	if err != nil {
		t.Fatalf("seed failed: %v", err)
	}
	if _, err := s.db.Exec(`UPDATE pending_confirmations SET retry_count = ? WHERE id = ?`, retries, conf.ID); err != nil {
		t.Fatalf("set retries: %v", err)
	}
	return conf.ID
}

func nextAttemptOf(t *testing.T, s *LocalStore, id int64) *string {
	t.Helper()
	var next *string
	if err := s.db.QueryRow(`SELECT strftime('%Y-%m-%d %H:%M:%S', next_attempt_at) FROM pending_confirmations WHERE id = ?`, id).Scan(&next); err != nil {
		t.Fatalf("read next_attempt_at: %v", err)
	}
	return next
}

func TestScheduleConfirmationRetry_DoublesFromBaseAndCapsAtThirtyMinutes(t *testing.T) {
	s := setupKanbanStore(t)
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	base := 30 * time.Second

	cases := []struct {
		retries int
		want    time.Duration
	}{
		{1, 30 * time.Second},
		{2, time.Minute},
		{3, 2 * time.Minute},
		{4, 4 * time.Minute},
		{7, 30 * time.Minute}, // 30s * 2^6 = 32m, capped
		{9, 30 * time.Minute},
		{200, 30 * time.Minute}, // the exponent must not overflow
	}
	for _, tc := range cases {
		id := seedPendingWithRetries(t, s, "EPC", tc.retries)
		if err := s.ScheduleConfirmationRetry(id, base, now); err != nil {
			t.Fatalf("retries=%d: schedule failed: %v", tc.retries, err)
		}
		next := nextAttemptOf(t, s, id)
		want := now.Add(tc.want).Format(sqliteTimestamp)
		if next == nil || *next != want {
			t.Errorf("retries=%d: next_attempt_at = %v, want %s (now + %s)", tc.retries, deref(next), want, tc.want)
		}
	}
}

func TestGetRetryableConfirmations_SkipsNotYetDueRowsWithoutFillingTheBatch(t *testing.T) {
	s := setupKanbanStore(t)
	// 150 older rows waiting out a backoff must not crowd out newer due rows.
	for i := 0; i < 150; i++ {
		id := seedPendingWithRetries(t, s, "WAITING", 1)
		if _, err := s.db.Exec(`UPDATE pending_confirmations SET created_at = datetime('now', '-1 hour'), next_attempt_at = datetime('now', '+1 hour') WHERE id = ?`, id); err != nil {
			t.Fatalf("mark waiting: %v", err)
		}
	}
	never := seedPendingWithRetries(t, s, "NEVER-TRIED", 0)
	pastDue := seedPendingWithRetries(t, s, "PAST-DUE", 2)
	if _, err := s.db.Exec(`UPDATE pending_confirmations SET next_attempt_at = datetime('now', '-1 minute') WHERE id = ?`, pastDue); err != nil {
		t.Fatalf("mark past due: %v", err)
	}

	got, err := s.GetRetryableConfirmations(100, 10)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("got %d confirmations, want only the NULL and past-due rows", len(got))
	}
	ids := map[int64]bool{got[0].ID: true, got[1].ID: true}
	if !ids[never] || !ids[pastDue] {
		t.Errorf("got ids %v, want %d (next_attempt_at NULL) and %d (past due)", ids, never, pastDue)
	}
}

func TestGetRetryableConfirmations_StuckRowsStayExcludedEvenWhenDue(t *testing.T) {
	s := setupKanbanStore(t)
	stuck := seedPendingWithRetries(t, s, "STUCK", 10)
	if _, err := s.db.Exec(`UPDATE pending_confirmations SET next_attempt_at = datetime('now', '-1 hour') WHERE id = ?`, stuck); err != nil {
		t.Fatalf("mark due: %v", err)
	}

	got, err := s.GetRetryableConfirmations(100, 10)
	if err != nil || len(got) != 0 {
		t.Fatalf("got %d (err %v), want the stuck row excluded", len(got), err)
	}
	if n, err := s.GetStuckConfirmationsCount(10); err != nil || n != 1 {
		t.Errorf("stuck count = %d (err %v), want 1: backoff must not change what stuck means", n, err)
	}
}

func TestRequeueStuckConfirmations_ClearsTheBackoff(t *testing.T) {
	s := setupKanbanStore(t)
	stuck := seedPendingWithRetries(t, s, "STUCK", 10)
	waiting := seedPendingWithRetries(t, s, "WAITING", 3)
	for _, id := range []int64{stuck, waiting} {
		if _, err := s.db.Exec(`UPDATE pending_confirmations SET next_attempt_at = datetime('now', '+30 minutes') WHERE id = ?`, id); err != nil {
			t.Fatalf("set backoff: %v", err)
		}
	}

	n, err := s.RequeueStuckConfirmations(10)
	if err != nil || n != 1 {
		t.Fatalf("requeued = %d (err %v), want 1", n, err)
	}

	if next := nextAttemptOf(t, s, stuck); next != nil {
		t.Errorf("requeued row keeps next_attempt_at %q, want NULL (due now)", *next)
	}
	if next := nextAttemptOf(t, s, waiting); next == nil {
		t.Error("a row still under the retry limit must keep its backoff")
	}
}

func deref(s *string) string {
	if s == nil {
		return "NULL"
	}
	return *s
}
