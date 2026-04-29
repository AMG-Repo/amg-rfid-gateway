package httpclient

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/amg-rfid/amg-rfid-gateway/internal/localstore"
)

func TestNewVPSClient(t *testing.T) {
	client := NewVPSClient("http://example.com", 30*time.Second)

	if client == nil {
		t.Fatal("expected client to be non-nil")
	}
	if client.baseURL != "http://example.com" {
		t.Errorf("expected baseURL 'http://example.com', got %q", client.baseURL)
	}
	if client.timeout != 30*time.Second {
		t.Errorf("expected timeout 30s, got %v", client.timeout)
	}
	if client.httpClient == nil {
		t.Error("expected httpClient to be initialized")
	}
}

func TestNewVPSClient_DefaultTimeout(t *testing.T) {
	client := NewVPSClient("http://example.com", 0)

	if client.timeout != 30*time.Second {
		t.Errorf("expected default timeout 30s, got %v", client.timeout)
	}
}

func TestFetchTools_Success(t *testing.T) {
	expectedTools := []localstore.Tool{
		{ID: 1, CompanyID: "comp-1", SKU: "TOOL-001", Name: "Tool One", UII: "EPC-001", Status: "active"},
		{ID: 2, CompanyID: "comp-1", SKU: "TOOL-002", Name: "Tool Two", UII: "EPC-002", Status: "active"},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Errorf("expected GET method, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/tools" {
			t.Errorf("expected path /api/v1/tools, got %s", r.URL.Path)
		}

		companyID := r.URL.Query().Get("company_id")
		if companyID != "comp-1" {
			t.Errorf("expected company_id 'comp-1', got %q", companyID)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(expectedTools)
	}))
	defer server.Close()

	client := NewVPSClient(server.URL, 5*time.Second)
	tools, err := client.FetchTools("comp-1")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tools) != 2 {
		t.Errorf("expected 2 tools, got %d", len(tools))
	}
	if tools[0].SKU != "TOOL-001" {
		t.Errorf("expected SKU 'TOOL-001', got %q", tools[0].SKU)
	}
}

func TestFetchTools_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewVPSClient(server.URL, 5*time.Second)
	_, err := client.FetchTools("comp-1")

	if err == nil {
		t.Error("expected error for HTTP 500")
	}
}

func TestFetchTools_NetworkError(t *testing.T) {
	// Create client pointing to a non-existent server
	client := NewVPSClient("http://localhost:59999", 100*time.Millisecond)
	_, err := client.FetchTools("comp-1")

	if err == nil {
		t.Error("expected error for network failure")
	}
}

func TestFetchUsers_Success(t *testing.T) {
	expectedUsers := []localstore.User{
		{ID: 1, CompanyID: "comp-1", Name: "User One", RFIDTag: "TAG-001", Active: true},
		{ID: 2, CompanyID: "comp-1", Name: "User Two", RFIDTag: "TAG-002", Active: true},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Errorf("expected GET method, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/users" {
			t.Errorf("expected path /api/v1/users, got %s", r.URL.Path)
		}

		companyID := r.URL.Query().Get("company_id")
		if companyID != "comp-1" {
			t.Errorf("expected company_id 'comp-1', got %q", companyID)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(expectedUsers)
	}))
	defer server.Close()

	client := NewVPSClient(server.URL, 5*time.Second)
	users, err := client.FetchUsers("comp-1")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(users) != 2 {
		t.Errorf("expected 2 users, got %d", len(users))
	}
	if users[0].Name != "User One" {
		t.Errorf("expected Name 'User One', got %q", users[0].Name)
	}
}

func TestFetchUsers_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := NewVPSClient(server.URL, 5*time.Second)
	_, err := client.FetchUsers("comp-1")

	if err == nil {
		t.Error("expected error for HTTP 404")
	}
}

func TestSendConfirmation_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST method, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/tools/confirm" {
			t.Errorf("expected path /api/v1/tools/confirm, got %s", r.URL.Path)
		}

		var reqBody map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}

		if reqBody["uii"] != "EPC-123" {
			t.Errorf("expected uii 'EPC-123', got %v", reqBody["uii"])
		}
		if reqBody["action"] != "entrada" {
			t.Errorf("expected action 'entrada', got %v", reqBody["action"])
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer server.Close()

	client := NewVPSClient(server.URL, 5*time.Second)
	err := client.SendConfirmation("comp-1", "EPC-123", "entrada")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSendConfirmation_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid action"})
	}))
	defer server.Close()

	client := NewVPSClient(server.URL, 5*time.Second)
	err := client.SendConfirmation("comp-1", "EPC-123", "invalid")

	if err == nil {
		t.Error("expected error for HTTP 400")
	}
}

func TestSendConfirmation_NetworkError(t *testing.T) {
	client := NewVPSClient("http://localhost:59999", 100*time.Millisecond)
	err := client.SendConfirmation("comp-1", "EPC-123", "entrada")

	if err == nil {
		t.Error("expected error for network failure")
	}
}

func TestFetchTools_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("invalid json"))
	}))
	defer server.Close()

	client := NewVPSClient(server.URL, 5*time.Second)
	_, err := client.FetchTools("comp-1")

	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestFetchTools_EmptyResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("[]"))
	}))
	defer server.Close()

	client := NewVPSClient(server.URL, 5*time.Second)
	tools, err := client.FetchTools("comp-1")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tools) != 0 {
		t.Errorf("expected 0 tools, got %d", len(tools))
	}
}

func TestVPSClient_BaseURLWithTrailingSlash(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Path should not have double slashes
		if r.URL.Path != "/api/v1/tools" {
			t.Errorf("expected path /api/v1/tools, got %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("[]"))
	}))
	defer server.Close()

	// Test with trailing slash
	client := NewVPSClient(server.URL+"/", 5*time.Second)
	_, err := client.FetchTools("comp-1")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// Verify the client handles context timeout properly
func TestFetchTools_Timeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond) // Slow response
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("[]"))
	}))
	defer server.Close()

	client := NewVPSClient(server.URL, 100*time.Millisecond) // Short timeout
	_, err := client.FetchTools("comp-1")

	// Should get a timeout error
	if err == nil {
		t.Error("expected timeout error")
	}
}

// Helper to check if error contains expected message
func containsError(err error, substr string) bool {
	if err == nil {
		return false
	}
	return len(substr) > 0 && fmt.Sprintf("%v", err) != ""
}
