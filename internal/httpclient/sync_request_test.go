package httpclient

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/amg-rfid/amg-rfid-shared-go/models"
)

func syncRequestFixture() models.SyncRequest {
	ts := time.Date(2026, 11, 19, 17, 0, 0, 123456789, time.UTC)
	return models.SyncRequest{
		GatewayID: "e2e-gateway",
		CompanyID: "amg",
		Readings: []models.Reading{
			{AntennaID: "UR4-001", GatewayID: "e2e-gateway", EPC: "E2806995000050104CF452C0", RSSI: -60, Timestamp: ts},
		},
		Timestamp: ts,
	}
}

func TestSendSyncRequest_PostsBatchToGatewaySyncWithBearer(t *testing.T) {
	var gotMethod, gotPath, gotAuth, gotType string
	var gotBody models.SyncRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		gotAuth, gotType = r.Header.Get("Authorization"), r.Header.Get("Content-Type")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"accepted":1,"duplicates":0,"errors":[],"next_sync_in":30,"server_time":"2026-11-19T17:00:01Z"}`))
	}))
	defer server.Close()

	client := NewVPSClient(server.URL, 5*time.Second, StaticToken("tok"))
	req := syncRequestFixture()
	resp, err := client.SendSyncRequest(req)
	if err != nil {
		t.Fatalf("SendSyncRequest: %v", err)
	}

	if gotMethod != http.MethodPost || gotPath != "/api/v1/gateway/sync" {
		t.Errorf("request = %s %s; want POST /api/v1/gateway/sync", gotMethod, gotPath)
	}
	if gotAuth != "Bearer tok" || gotType != "application/json" {
		t.Errorf("headers Authorization=%q Content-Type=%q", gotAuth, gotType)
	}
	if gotBody.GatewayID != "e2e-gateway" || gotBody.CompanyID != "amg" || len(gotBody.Readings) != 1 ||
		gotBody.Readings[0].EPC != req.Readings[0].EPC || !gotBody.Readings[0].Timestamp.Equal(req.Readings[0].Timestamp) {
		t.Errorf("body = %+v; want the bare sync request", gotBody)
	}
	if !resp.Success || resp.Accepted != 1 || resp.NextSyncIn != 30 {
		t.Errorf("response = %+v", resp)
	}
}

func TestSendSyncRequest_RenewsTokenOnceAfter401(t *testing.T) {
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Header.Get("Authorization"))
		var body models.SyncRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.Readings) != 1 {
			t.Errorf("request %d body not resent intact: %v", len(seen), err)
		}
		if r.Header.Get("Authorization") != "Bearer tok-2" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"success":true,"accepted":0,"duplicates":1}`))
	}))
	defer server.Close()

	tokens := &rotatingTokens{}
	resp, err := NewVPSClient(server.URL, 5*time.Second, tokens).SendSyncRequest(syncRequestFixture())
	if err != nil {
		t.Fatalf("SendSyncRequest: %v", err)
	}
	if len(seen) != 2 || seen[0] != "Bearer tok-1" || seen[1] != "Bearer tok-2" {
		t.Errorf("Authorization headers = %v; want [Bearer tok-1 Bearer tok-2]", seen)
	}
	if resp.Duplicates != 1 {
		t.Errorf("response = %+v", resp)
	}
}

func TestSendSyncRequest_NonSuccessStatusIsAnError(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{"forbidden ids", http.StatusForbidden, `{"error":"gateway_id does not match token"}`},
		{"too large", http.StatusRequestEntityTooLarge, `{"error":"body too large"}`},
		{"rate limited", http.StatusTooManyRequests, `{"error":"rate limit"}`},
		{"db error", http.StatusInternalServerError, `{"error":"db"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()

			resp, err := NewVPSClient(server.URL, 5*time.Second, StaticToken("tok")).SendSyncRequest(syncRequestFixture())
			if err == nil {
				t.Fatalf("SendSyncRequest succeeded with %d; want error (response %+v)", tc.status, resp)
			}
			if resp != nil {
				t.Errorf("response = %+v; want nil so the engine keeps readings pending", resp)
			}
			if !strings.Contains(err.Error(), http.StatusText(tc.status)) && !strings.Contains(err.Error(), tc.body) {
				t.Errorf("error %q does not name the status or body", err)
			}
		})
	}
}

// The VPS rejects a reading without rssi ("rssi is required"), so a zero
// RSSI must still be serialized.
func TestSendSyncRequest_AlwaysSendsRSSIEvenWhenZero(t *testing.T) {
	var raw map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
			t.Errorf("decode body: %v", err)
		}
		_, _ = w.Write([]byte(`{"success":true,"accepted":1}`))
	}))
	defer server.Close()

	req := syncRequestFixture()
	req.Readings[0].RSSI = 0
	if _, err := NewVPSClient(server.URL, 5*time.Second, StaticToken("tok")).SendSyncRequest(req); err != nil {
		t.Fatalf("SendSyncRequest: %v", err)
	}
	reading := raw["readings"].([]any)[0].(map[string]any)
	if rssi, ok := reading["rssi"]; !ok || rssi != float64(0) {
		t.Errorf("reading rssi = %v (present %v); want 0 serialized", rssi, ok)
	}
}

// The sync engine tells a whole-batch rejection from a transient failure by
// the HTTP status, so a non-2xx error must expose it.
func TestSendSyncRequest_NonSuccessErrorCarriesStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"Invalid request body"}`))
	}))
	defer server.Close()

	_, err := NewVPSClient(server.URL, 5*time.Second, StaticToken("tok")).SendSyncRequest(syncRequestFixture())
	var sc interface{ StatusCode() int }
	if !errors.As(err, &sc) || sc.StatusCode() != http.StatusBadRequest {
		t.Fatalf("error %v does not carry status 400", err)
	}
}
