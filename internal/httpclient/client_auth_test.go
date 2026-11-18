package httpclient

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// rotatingTokens hands out "tok-1", then "tok-2" after Invalidate, and records calls.
type rotatingTokens struct {
	mu          sync.Mutex
	current     int
	invalidated []string
	err         error
}

func (r *rotatingTokens) Token(context.Context) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return "", r.err
	}
	if r.current == 0 {
		r.current = 1
	}
	return "tok-" + string(rune('0'+r.current)), nil
}

func (r *rotatingTokens) Invalidate(token string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.invalidated = append(r.invalidated, token)
	r.current++
}

func syncDataOK(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"status": "OK", "tools": []any{}})
}

func TestAuthedRequest_RenewsAndRetriesOnceAfter401(t *testing.T) {
	var hits atomic.Int32
	var seen []string
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		auth := r.Header.Get("Authorization")
		mu.Lock()
		seen = append(seen, auth)
		mu.Unlock()
		if auth != "Bearer tok-2" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		syncDataOK(w)
	}))
	defer server.Close()

	tokens := &rotatingTokens{}
	client := NewVPSClient(server.URL, 5*time.Second, tokens)
	if _, err := client.FetchSyncData("comp-1"); err != nil {
		t.Fatalf("FetchSyncData after renewal: %v", err)
	}
	if hits.Load() != 2 {
		t.Fatalf("requests = %d; want 2", hits.Load())
	}
	if len(tokens.invalidated) != 1 || tokens.invalidated[0] != "tok-1" {
		t.Fatalf("invalidated = %v; want [tok-1]", tokens.invalidated)
	}
	if seen[0] != "Bearer tok-1" || seen[1] != "Bearer tok-2" {
		t.Fatalf("Authorization headers = %v", seen)
	}
}

func TestAuthedRequest_RetriedPOSTResendsBody(t *testing.T) {
	var bodies []GatewayConfirmRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req GatewayConfirmRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		bodies = append(bodies, req)
		if r.Header.Get("Authorization") != "Bearer tok-2" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(GatewayConfirmResponse{Status: "OK"})
	}))
	defer server.Close()

	client := NewVPSClient(server.URL, 5*time.Second, &rotatingTokens{})
	if err := client.SendConfirmationV2("comp-1", "EPC-1", "salida", "ant-1"); err != nil {
		t.Fatalf("SendConfirmationV2: %v", err)
	}
	if len(bodies) != 2 || bodies[1].UII != "EPC-1" || bodies[1].CompanyID != "comp-1" {
		t.Fatalf("retried body = %+v; want the same payload twice", bodies)
	}
}

func TestAuthedRequest_PersistentUnauthorizedStopsAfterOneRetry(t *testing.T) {
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	client := NewVPSClient(server.URL, 5*time.Second, &rotatingTokens{})
	if _, err := client.FetchUsers("comp-1"); err == nil {
		t.Fatal("FetchUsers succeeded; want 401 error")
	}
	if hits.Load() != 2 {
		t.Fatalf("requests = %d; want 2 (original + one retry)", hits.Load())
	}
}

func TestAuthedRequest_TokenErrorSendsNoRequest(t *testing.T) {
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
	}))
	defer server.Close()

	tokenErr := errors.New("credential rejected")
	client := NewVPSClient(server.URL, 5*time.Second, &rotatingTokens{err: tokenErr})
	_, err := client.FetchSyncData("comp-1")
	if !errors.Is(err, tokenErr) {
		t.Fatalf("err = %v; want wrapped token error", err)
	}
	if hits.Load() != 0 {
		t.Fatalf("requests = %d; want 0", hits.Load())
	}
}
