package wsclient

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/gorilla/websocket"
)

// countingTokens returns "tok-N", where N grows on every Invalidate or Rotate.
type countingTokens struct {
	mu          sync.Mutex
	n           int
	invalidated []string
	err         error
}

func (c *countingTokens) Token(context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil {
		return "", c.err
	}
	return "tok-" + strconv.Itoa(c.n+1), nil
}

func (c *countingTokens) Invalidate(token string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.invalidated = append(c.invalidated, token)
	c.n++
}

func (c *countingTokens) Rotate() { c.mu.Lock(); c.n++; c.mu.Unlock() }

func authServer(t *testing.T, accept func(auth string) bool) (string, *[]string) {
	t.Helper()
	var mu sync.Mutex
	var seen []string
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		mu.Lock()
		seen = append(seen, auth)
		mu.Unlock()
		if !accept(auth) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}))
	t.Cleanup(srv.Close)
	return "ws" + strings.TrimPrefix(srv.URL, "http"), &seen
}

func TestConnect_UsesCurrentTokenOnEveryConnect(t *testing.T) {
	url, seen := authServer(t, func(string) bool { return true })
	tokens := &countingTokens{}
	client := NewClient(url, tokens)
	client.reconnectDelay = 0

	if err := client.Connect(); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	tokens.Rotate()
	if err := client.Reconnect(); err != nil {
		t.Fatalf("Reconnect: %v", err)
	}
	defer client.Disconnect()

	if len(*seen) != 2 || (*seen)[0] != "Bearer tok-1" || (*seen)[1] != "Bearer tok-2" {
		t.Fatalf("Authorization headers = %v; want tok-1 then tok-2", *seen)
	}
}

func TestConnect_UnauthorizedHandshakeInvalidatesToken(t *testing.T) {
	url, _ := authServer(t, func(auth string) bool { return auth == "Bearer tok-2" })
	tokens := &countingTokens{}
	client := NewClient(url, tokens)
	client.reconnectDelay = 0

	if err := client.Connect(); err == nil {
		t.Fatal("Connect with rejected token succeeded")
	}
	if len(tokens.invalidated) != 1 || tokens.invalidated[0] != "tok-1" {
		t.Fatalf("invalidated = %v; want [tok-1]", tokens.invalidated)
	}
	if err := client.Reconnect(); err != nil {
		t.Fatalf("Reconnect with renewed token: %v", err)
	}
	client.Disconnect()
}

func TestConnect_ForbiddenHandshakeInvalidatesToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	t.Cleanup(srv.Close)
	tokens := &countingTokens{}
	client := NewClient("ws"+strings.TrimPrefix(srv.URL, "http"), tokens)

	if err := client.Connect(); err == nil {
		t.Fatal("Connect with forbidden token succeeded")
	}
	if len(tokens.invalidated) != 1 || tokens.invalidated[0] != "tok-1" {
		t.Fatalf("invalidated = %v; want [tok-1]", tokens.invalidated)
	}
}

func TestConnect_TokenErrorDoesNotDial(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1) }))
	t.Cleanup(srv.Close)

	tokenErr := errors.New("credential rejected")
	client := NewClient("ws"+strings.TrimPrefix(srv.URL, "http"), &countingTokens{err: tokenErr})
	if err := client.Connect(); !errors.Is(err, tokenErr) {
		t.Fatalf("err = %v; want wrapped token error", err)
	}
	if hits.Load() != 0 {
		t.Fatalf("dials = %d; want 0", hits.Load())
	}
}
