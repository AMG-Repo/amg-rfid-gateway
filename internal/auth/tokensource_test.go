package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const testSecret = "s3cr3t-device-value"

// fakeTokenServer issues sequential tokens ("tok-1", "tok-2", ...) and counts hits.
type fakeTokenServer struct {
	srv       *httptest.Server
	hits      atomic.Int32
	status    atomic.Int32 // 0 means 200
	expiresIn int
	lastBody  map[string]string
	mu        sync.Mutex
}

func newFakeTokenServer(t *testing.T, expiresIn int) *fakeTokenServer {
	t.Helper()
	f := &fakeTokenServer{expiresIn: expiresIn}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := f.hits.Add(1)
		if r.Method != http.MethodPost || r.URL.Path != TokenPath {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		f.lastBody = body
		f.mu.Unlock()
		if s := f.status.Load(); s != 0 {
			w.WriteHeader(int(s))
			_, _ = w.Write([]byte(`{"error":"invalid credentials"}`))
			return
		}
		time.Sleep(20 * time.Millisecond) // widen the race window for single-flight checks
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "tok-" + strconv.Itoa(int(n)),
			"token_type":   "Bearer",
			"expires_in":   f.expiresIn,
		})
	}))
	t.Cleanup(f.srv.Close)
	return f
}

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time          { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *fakeClock) Advance(d time.Duration) { c.mu.Lock(); c.now = c.now.Add(d); c.mu.Unlock() }

func newSource(f *fakeTokenServer, clock *fakeClock) *TokenSource {
	ts := NewTokenSource(f.srv.URL+"/", Credential{GatewayID: "gw-1", Secret: testSecret}, nil)
	ts.now = clock.Now
	return ts
}

func TestTokenSource_ExchangesCredentialAndCaches(t *testing.T) {
	f := newFakeTokenServer(t, 43200)
	clock := &fakeClock{now: time.Unix(1_000_000, 0)}
	ts := newSource(f, clock)

	tok, err := ts.Token(context.Background())
	if err != nil || tok != "tok-1" {
		t.Fatalf("Token() = %q, %v; want tok-1, nil", tok, err)
	}
	tok, err = ts.Token(context.Background())
	if err != nil || tok != "tok-1" {
		t.Fatalf("second Token() = %q, %v; want cached tok-1", tok, err)
	}
	if got := f.hits.Load(); got != 1 {
		t.Fatalf("server hits = %d; want 1", got)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.lastBody["gateway_id"] != "gw-1" || f.lastBody["secret"] != testSecret {
		t.Fatalf("request body = %v; want gateway_id and secret", f.lastBody)
	}
}

func TestTokenSource_RenewsAtEightyPercentOfLifetime(t *testing.T) {
	f := newFakeTokenServer(t, 43200) // 12h
	clock := &fakeClock{now: time.Unix(1_000_000, 0)}
	ts := newSource(f, clock)

	if _, err := ts.Token(context.Background()); err != nil {
		t.Fatal(err)
	}
	clock.Advance(9*time.Hour + 30*time.Minute) // < 80% of 12h (9h36m)
	if tok, _ := ts.Token(context.Background()); tok != "tok-1" {
		t.Fatalf("before 80%%: token = %q; want tok-1", tok)
	}
	clock.Advance(10 * time.Minute) // past 80%
	if tok, _ := ts.Token(context.Background()); tok != "tok-2" {
		t.Fatalf("after 80%%: token = %q; want tok-2", tok)
	}
}

func TestTokenSource_ConcurrentCallersShareOneRenewal(t *testing.T) {
	f := newFakeTokenServer(t, 43200)
	clock := &fakeClock{now: time.Unix(1_000_000, 0)}
	ts := newSource(f, clock)

	var wg sync.WaitGroup
	tokens := make([]string, 20)
	for i := range tokens {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			tokens[i], _ = ts.Token(context.Background())
		}(i)
	}
	wg.Wait()
	if got := f.hits.Load(); got != 1 {
		t.Fatalf("server hits = %d; want 1", got)
	}
	for i, tok := range tokens {
		if tok != "tok-1" {
			t.Fatalf("caller %d got %q; want tok-1", i, tok)
		}
	}
}

func TestTokenSource_InvalidateForcesRenewalOnlyForStaleToken(t *testing.T) {
	f := newFakeTokenServer(t, 43200)
	clock := &fakeClock{now: time.Unix(1_000_000, 0)}
	ts := newSource(f, clock)

	old, _ := ts.Token(context.Background())
	ts.Invalidate(old)
	fresh, err := ts.Token(context.Background())
	if err != nil || fresh != "tok-2" {
		t.Fatalf("after Invalidate: %q, %v; want tok-2", fresh, err)
	}
	ts.Invalidate(old) // a late 401 for the old token must not discard the fresh one
	if tok, _ := ts.Token(context.Background()); tok != "tok-2" {
		t.Fatalf("stale Invalidate dropped fresh token: got %q", tok)
	}
	if got := f.hits.Load(); got != 2 {
		t.Fatalf("server hits = %d; want 2", got)
	}
}

func TestTokenSource_RejectedCredentialIsReportedWithoutSecretAndBacksOff(t *testing.T) {
	f := newFakeTokenServer(t, 43200)
	f.status.Store(http.StatusUnauthorized)
	clock := &fakeClock{now: time.Unix(1_000_000, 0)}
	ts := newSource(f, clock)

	_, err := ts.Token(context.Background())
	if !errors.Is(err, ErrCredentialRejected) {
		t.Fatalf("err = %v; want ErrCredentialRejected", err)
	}
	if strings.Contains(err.Error(), testSecret) {
		t.Fatalf("error leaks the secret: %v", err)
	}
	if _, err := ts.Token(context.Background()); !errors.Is(err, ErrCredentialRejected) {
		t.Fatalf("second err = %v; want ErrCredentialRejected during backoff", err)
	}
	if got := f.hits.Load(); got != 1 {
		t.Fatalf("server hits during backoff = %d; want 1", got)
	}
	clock.Advance(time.Hour)
	_, _ = ts.Token(context.Background())
	if got := f.hits.Load(); got != 2 {
		t.Fatalf("server hits after backoff = %d; want 2", got)
	}
}

func TestTokenSource_OutageKeepsValidTokenAndFailsAfterExpiry(t *testing.T) {
	f := newFakeTokenServer(t, 43200)
	clock := &fakeClock{now: time.Unix(1_000_000, 0)}
	ts := newSource(f, clock)

	if _, err := ts.Token(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.status.Store(http.StatusServiceUnavailable)
	clock.Advance(10 * time.Hour) // renewal due, server down, token still valid
	tok, err := ts.Token(context.Background())
	if err != nil || tok != "tok-1" {
		t.Fatalf("during outage: %q, %v; want still-valid tok-1", tok, err)
	}
	clock.Advance(3 * time.Hour) // past expiry
	if _, err := ts.Token(context.Background()); err == nil {
		t.Fatal("expired token returned during outage; want error")
	}
	f.status.Store(0)
	clock.Advance(time.Hour) // past any backoff
	if tok, err := ts.Token(context.Background()); err != nil || !strings.HasPrefix(tok, "tok-") || tok == "tok-1" {
		t.Fatalf("after recovery: %q, %v; want a new token", tok, err)
	}
}

func TestTokenSource_RateLimitedHonorsRetryAfter(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Retry-After", "120")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	t.Cleanup(srv.Close)
	clock := &fakeClock{now: time.Unix(1_000_000, 0)}
	ts := NewTokenSource(srv.URL, Credential{GatewayID: "gw-1", Secret: testSecret}, nil)
	ts.now = clock.Now

	if _, err := ts.Token(context.Background()); err == nil {
		t.Fatal("429 returned no error")
	}
	clock.Advance(60 * time.Second) // past the default 5s backoff, inside Retry-After
	_, _ = ts.Token(context.Background())
	if got := hits.Load(); got != 1 {
		t.Fatalf("hits inside Retry-After = %d; want 1", got)
	}
	clock.Advance(61 * time.Second)
	_, _ = ts.Token(context.Background())
	if got := hits.Load(); got != 2 {
		t.Fatalf("hits after Retry-After = %d; want 2", got)
	}
}

func TestTokenSource_RetryAfterIsCapped(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Retry-After", "86400")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	t.Cleanup(srv.Close)
	clock := &fakeClock{now: time.Unix(1_000_000, 0)}
	ts := NewTokenSource(srv.URL, Credential{GatewayID: "gw-1", Secret: testSecret}, nil)
	ts.now = clock.Now

	_, _ = ts.Token(context.Background())
	clock.Advance(15*time.Minute + time.Second)
	_, _ = ts.Token(context.Background())
	if got := hits.Load(); got != 2 {
		t.Fatalf("hits after 15m cap = %d; want 2", got)
	}
}

func TestTokenSource_CancelledCallerDoesNotBackOffOthers(t *testing.T) {
	f := newFakeTokenServer(t, 43200)
	clock := &fakeClock{now: time.Unix(1_000_000, 0)}
	ts := newSource(f, clock)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ts.Token(ctx); err == nil {
		t.Fatal("cancelled ctx returned no error")
	}
	tok, err := ts.Token(context.Background())
	if err != nil || tok == "" {
		t.Fatalf("fresh ctx Token = %q, %v; want token without backoff", tok, err)
	}
}

func TestLoadCredentialFile(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string, mode os.FileMode) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), mode); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(p, mode); err != nil {
			t.Fatal(err)
		}
		return p
	}

	secret, err := LoadCredentialFile(write("ok", testSecret+"\n", 0o600))
	if err != nil || secret != testSecret {
		t.Fatalf("0600 file: %q, %v; want trimmed secret", secret, err)
	}
	if _, err := LoadCredentialFile(write("open", testSecret, 0o644)); err == nil {
		t.Fatal("0644 file accepted; want permission error")
	}
	if _, err := LoadCredentialFile(write("empty", "\n", 0o600)); err == nil {
		t.Fatal("empty file accepted; want error")
	}
	if _, err := LoadCredentialFile(filepath.Join(dir, "missing")); err == nil {
		t.Fatal("missing file accepted; want error")
	}
}
