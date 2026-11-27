package main

import (
	"context"
	"testing"
	"time"
)

// A reader that stays down for days must keep waiting the max backoff: the
// shift overflowed and fell to 0 s (prod: "reconnecting in 0s (attempt 14550)").
func TestCalculateBackoff_SaturatesAtMaxForLargeAttempts(t *testing.T) {
	const initial, max = time.Second, 30 * time.Second
	prev := time.Duration(0)
	for attempt := 0; attempt <= 20000; attempt++ {
		got := calculateBackoff(attempt, initial, max)
		if got < prev || got > max || got <= 0 {
			t.Fatalf("calculateBackoff(%d) = %v after %v; want monotonic in (0, %v]", attempt, got, prev, max)
		}
		prev = got
	}
	for _, attempt := range []int{34, 63, 64, 65, 14550} {
		if got := calculateBackoff(attempt, initial, max); got != max {
			t.Errorf("calculateBackoff(%d) = %v, want %v", attempt, got, max)
		}
	}
}

// After a successful connect the next drop starts again from the initial
// backoff, not from where the earlier outage left it.
func TestReconnectLoop_ResetsBackoffAfterConnect(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 4 failed connects, 1 connect that later drops, then 2 failed connects.
	results := []bool{false, false, false, false, true, false, false}
	var waits []time.Duration
	run := func(context.Context, bool) bool {
		r := results[0]
		results = results[1:]
		if len(results) == 0 {
			cancel()
		}
		return r
	}
	wait := func(_ context.Context, d time.Duration) bool {
		waits = append(waits, d)
		return true
	}

	reconnectLoop(ctx, "ant-1", time.Second, 30*time.Second, run, wait)

	want := []time.Duration{1 * time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 1 * time.Second, 2 * time.Second}
	if len(waits) != len(want) {
		t.Fatalf("waits = %v, want %v", waits, want)
	}
	for i := range want {
		if waits[i] != want[i] {
			t.Fatalf("waits = %v, want %v", waits, want)
		}
	}
}

// Once the backoff is at its max, attempts are logged only now and then, so a
// reader down for a day does not write tens of thousands of lines.
func TestReconnectLoop_LogsAttemptsSparselyAtMaxBackoff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	const attempts = 200
	n := 0
	logged := 0
	run := func(_ context.Context, logAttempt bool) bool {
		if logAttempt {
			logged++
		}
		n++
		if n == attempts {
			cancel()
		}
		return false
	}
	wait := func(context.Context, time.Duration) bool { return true }

	reconnectLoop(ctx, "ant-1", time.Second, 30*time.Second, run, wait)

	// 1s..16s grow (attempts 1-6 logged), then about one in reconnectLogEvery.
	if logged < 6 || logged > 6+attempts/reconnectLogEvery+1 {
		t.Errorf("logged %d of %d attempts; want growth attempts plus ~1 in %d at max", logged, attempts, reconnectLogEvery)
	}
}
