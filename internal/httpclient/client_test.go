package httpclient

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/amg-rfid/amg-rfid-gateway/internal/localstore"
	"github.com/amg-rfid/amg-rfid-gateway/internal/sync"
)

func TestNewVPSClient(t *testing.T) {
	client := NewVPSClient("http://example.com", 30*time.Second, "test-jwt-token")

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
	if client.jwtToken != "test-jwt-token" {
		t.Errorf("expected jwtToken 'test-jwt-token', got %q", client.jwtToken)
	}
}

func TestNewVPSClient_DefaultTimeout(t *testing.T) {
	client := NewVPSClient("http://example.com", 0, "test-token")

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

	client := NewVPSClient(server.URL, 5*time.Second, "test-token")
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

	client := NewVPSClient(server.URL, 5*time.Second, "test-token")
	_, err := client.FetchTools("comp-1")

	if err == nil {
		t.Error("expected error for HTTP 500")
	}
}

func TestFetchTools_NetworkError(t *testing.T) {
	// Create client pointing to a non-existent server
	client := NewVPSClient("http://localhost:59999", 100*time.Millisecond, "test-token")
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

	client := NewVPSClient(server.URL, 5*time.Second, "test-token")
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

	client := NewVPSClient(server.URL, 5*time.Second, "test-token")
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
		// SendConfirmation now delegates to SendConfirmationV2 which uses gateway endpoint
		if r.URL.Path != "/api/v1/gateway/confirm" {
			t.Errorf("expected path /api/v1/gateway/confirm, got %s", r.URL.Path)
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

	client := NewVPSClient(server.URL, 5*time.Second, "test-token")
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

	client := NewVPSClient(server.URL, 5*time.Second, "test-token")
	err := client.SendConfirmation("comp-1", "EPC-123", "invalid")

	if err == nil {
		t.Error("expected error for HTTP 400")
	}
}

func TestSendConfirmation_NetworkError(t *testing.T) {
	client := NewVPSClient("http://localhost:59999", 100*time.Millisecond, "test-token")
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

	client := NewVPSClient(server.URL, 5*time.Second, "test-token")
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

	client := NewVPSClient(server.URL, 5*time.Second, "test-token")
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
	client := NewVPSClient(server.URL+"/", 5*time.Second, "test-token")
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

	client := NewVPSClient(server.URL, 100*time.Millisecond, "test-token") // Short timeout
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

func strPtr(s string) *string {
	return &s
}

func TestFetchSyncData_Success(t *testing.T) {
	expectedItems := []localstore.SyncDataItem{
		{ID: 1, ToolID: 10, UII: "EPC-001", SKU: "SKU-001", Name: "Tool 1", Status: "available", Location: strPtr("Almacén General")},
		{ID: 2, ToolID: 10, UII: "EPC-002", SKU: "SKU-001", Name: "Tool 1", Status: "in_use", Location: strPtr("Línea 1")},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Errorf("expected GET method, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/gateway/sync-data" {
			t.Errorf("expected path /api/v1/gateway/sync-data, got %s", r.URL.Path)
		}

		companyID := r.URL.Query().Get("company_id")
		if companyID != "comp-1" {
			t.Errorf("expected company_id 'comp-1', got %q", companyID)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(sync.SyncDataResponse{
			Status: "OK",
			Tools:  expectedItems,
		})
	}))
	defer server.Close()

	client := NewVPSClient(server.URL, 5*time.Second, "test-token")
	resp, err := client.FetchSyncData("comp-1")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if len(resp.Tools) != 2 {
		t.Errorf("expected 2 items, got %d", len(resp.Tools))
	}
	if resp.Tools[0].UII != "EPC-001" {
		t.Errorf("expected UII 'EPC-001', got %q", resp.Tools[0].UII)
	}
	if resp.Tools[0].SKU != "SKU-001" {
		t.Errorf("expected SKU 'SKU-001', got %q", resp.Tools[0].SKU)
	}
}

func TestFetchSyncData_NonOKStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(sync.SyncDataResponse{
			Status: "ERROR",
			Tools:  nil,
		})
	}))
	defer server.Close()

	client := NewVPSClient(server.URL, 5*time.Second, "test-token")
	_, err := client.FetchSyncData("comp-1")

	if err == nil {
		t.Error("expected error for non-OK status")
	}
}

func TestFetchSyncData_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	client := NewVPSClient(server.URL, 5*time.Second, "test-token")
	_, err := client.FetchSyncData("comp-1")

	if err == nil {
		t.Error("expected error for HTTP 401")
	}
}

func TestSendGatewayConfirmation_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST method, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/gateway/confirm" {
			t.Errorf("expected path /api/v1/gateway/confirm, got %s", r.URL.Path)
		}

		var reqBody map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}

		if reqBody["uii"] != "EPC-123" {
			t.Errorf("expected uii 'EPC-123', got %v", reqBody["uii"])
		}
		if reqBody["action"] != "salida" {
			t.Errorf("expected action 'salida', got %v", reqBody["action"])
		}
		if reqBody["antenna_id"] != "ant-01" {
			t.Errorf("expected antenna_id 'ant-01', got %v", reqBody["antenna_id"])
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(GatewayConfirmResponse{
			Status:   "OK",
			UII:      "EPC-123",
			Action:   "EXIT",
			Location: "Línea 4",
		})
	}))
	defer server.Close()

	client := NewVPSClient(server.URL, 5*time.Second, "test-token")
	err := client.SendGatewayConfirmation("comp-1", "EPC-123", "salida", "ant-01")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSendGatewayConfirmation_WithoutAntennaID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}

		if reqBody["uii"] != "EPC-456" {
			t.Errorf("expected uii 'EPC-456', got %v", reqBody["uii"])
		}
		if _, exists := reqBody["antenna_id"]; exists {
			t.Error("expected antenna_id to not be present when empty")
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "OK"})
	}))
	defer server.Close()

	client := NewVPSClient(server.URL, 5*time.Second, "test-token")
	err := client.SendGatewayConfirmation("comp-1", "EPC-456", "entrada", "")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSendGatewayConfirmation_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "tag not found"})
	}))
	defer server.Close()

	client := NewVPSClient(server.URL, 5*time.Second, "test-token")
	err := client.SendGatewayConfirmation("comp-1", "UNKNOWN-EPC", "entrada", "ant-01")

	if err == nil {
		t.Error("expected error for HTTP 404")
	}
}

// TestFetchSyncData_SendsAuthorizationHeader verifies Blocker 2 fix:
// Gateway client sends proper JWT Bearer token to gateway endpoints
func TestFetchSyncData_SendsAuthorizationHeader(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify the endpoint is correct
		if r.URL.Path != "/api/v1/gateway/sync-data" {
			t.Errorf("expected path /api/v1/gateway/sync-data, got %s", r.URL.Path)
		}

		// Verify Authorization header is present and correct (Blocker 2)
		authHeader := r.Header.Get("Authorization")
		expectedAuth := "Bearer test-jwt-token"
		if authHeader != expectedAuth {
			t.Errorf("expected Authorization header %q, got %q", expectedAuth, authHeader)
		}

		// Return valid response
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "OK",
			"tools": []map[string]interface{}{
				{
					"id":       1,
					"tool_id":  1,
					"uii":      "EPC-001",
					"sku":      "TOOL-001",
					"name":     "Test Tool",
					"status":   "available",
					"location": "Almacén General",
					"active":   true,
				},
			},
			"timestamp": time.Now().UTC().Format(time.RFC3339),
		})
	}))
	defer server.Close()

	client := NewVPSClient(server.URL, 5*time.Second, "test-jwt-token")
	resp, err := client.FetchSyncData("comp-1")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected response to be non-nil")
	}
	if len(resp.Tools) != 1 {
		t.Errorf("expected 1 tool, got %d", len(resp.Tools))
	}
}

// TestSendConfirmationV2_SendsAuthorizationHeader verifies Blocker 2 fix:
// Gateway client sends proper JWT Bearer token to gateway confirm endpoint
func TestSendConfirmationV2_SendsAuthorizationHeader(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify the endpoint is correct
		if r.URL.Path != "/api/v1/gateway/confirm" {
			t.Errorf("expected path /api/v1/gateway/confirm, got %s", r.URL.Path)
		}

		// Verify Authorization header is present and correct (Blocker 2)
		authHeader := r.Header.Get("Authorization")
		expectedAuth := "Bearer gateway-jwt-123"
		if authHeader != expectedAuth {
			t.Errorf("expected Authorization header %q, got %q", expectedAuth, authHeader)
		}

		// Verify Content-Type
		contentType := r.Header.Get("Content-Type")
		if contentType != "application/json" {
			t.Errorf("expected Content-Type application/json, got %q", contentType)
		}

		// Return success response
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":    "OK",
			"uii":       "EPC-123",
			"action":    "ENTRY",
			"location":  "Almacén General",
			"timestamp": time.Now().UTC().Format(time.RFC3339),
		})
	}))
	defer server.Close()

	client := NewVPSClient(server.URL, 5*time.Second, "gateway-jwt-123")
	err := client.SendConfirmationV2("comp-1", "EPC-123", "entrada", "ant-01")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestSendGatewayConfirmation_SendsAuthorizationHeader verifies JWT auth on gateway confirm
func TestSendGatewayConfirmation_SendsAuthorizationHeader(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify Authorization header is present and correct
		authHeader := r.Header.Get("Authorization")
		expectedAuth := "Bearer gateway-auth-token"
		if authHeader != expectedAuth {
			t.Errorf("expected Authorization header %q, got %q", expectedAuth, authHeader)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(GatewayConfirmResponse{
			Status:   "OK",
			UII:      "EPC-123",
			Action:   "ENTRY",
			Location: "Almacén General",
		})
	}))
	defer server.Close()

	client := NewVPSClient(server.URL, 5*time.Second, "gateway-auth-token")
	err := client.SendGatewayConfirmation("comp-1", "EPC-123", "entrada", "ant-01")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestFetchSyncData_WithoutToken_DoesNotSendAuthHeader verifies that when
// no JWT token is provided, no Authorization header is sent
func TestFetchSyncData_WithoutToken_DoesNotSendAuthHeader(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify no Authorization header when token is empty
		authHeader := r.Header.Get("Authorization")
		if authHeader != "" {
			t.Errorf("expected no Authorization header when token is empty, got %q", authHeader)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "OK",
			"tools":  []map[string]interface{}{},
		})
	}))
	defer server.Close()

	client := NewVPSClient(server.URL, 5*time.Second, "") // Empty token
	_, err := client.FetchSyncData("comp-1")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// === Nullability Tests for SyncDataItem ===

// TestFetchSyncData_NullableFields_NullValue verifies that JSON null deserializes to Go nil
func TestFetchSyncData_NullableFields_NullValue(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// Return sync data with explicit JSON null values
		w.Write([]byte(`{
			"status": "OK",
			"tools": [
				{
					"id": 1,
					"tool_id": 10,
					"uii": "EPC-001",
					"sku": "SKU-001",
					"name": "Test Tool",
					"description": null,
					"status": "available",
					"location": null,
					"unit_number": null,
					"display_name": null,
					"active": true
				}
			]
		}`))
	}))
	defer server.Close()

	client := NewVPSClient(server.URL, 5*time.Second, "test-token")
	resp, err := client.FetchSyncData("comp-1")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if len(resp.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(resp.Tools))
	}

	tool := resp.Tools[0]
	if tool.Description != nil {
		t.Errorf("expected Description to be nil for JSON null, got %v", *tool.Description)
	}
	if tool.Location != nil {
		t.Errorf("expected Location to be nil for JSON null, got %v", *tool.Location)
	}
	if tool.UnitNumber != nil {
		t.Errorf("expected UnitNumber to be nil for JSON null, got %v", *tool.UnitNumber)
	}
	if tool.DisplayName != nil {
		t.Errorf("expected DisplayName to be nil for JSON null, got %v", *tool.DisplayName)
	}
}

// TestFetchSyncData_NullableFields_EmptyString verifies that JSON empty string deserializes to non-nil pointer to ""
func TestFetchSyncData_NullableFields_EmptyString(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// Return sync data with explicit empty strings
		w.Write([]byte(`{
			"status": "OK",
			"tools": [
				{
					"id": 1,
					"tool_id": 10,
					"uii": "EPC-001",
					"sku": "SKU-001",
					"name": "Test Tool",
					"description": "",
					"status": "available",
					"location": "",
					"unit_number": "",
					"display_name": "",
					"active": true
				}
			]
		}`))
	}))
	defer server.Close()

	client := NewVPSClient(server.URL, 5*time.Second, "test-token")
	resp, err := client.FetchSyncData("comp-1")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(resp.Tools))
	}

	tool := resp.Tools[0]
	if tool.Description == nil {
		t.Error("expected Description to be non-nil for empty string JSON")
	} else if *tool.Description != "" {
		t.Errorf("expected Description to be empty string, got %q", *tool.Description)
	}
	if tool.Location == nil {
		t.Error("expected Location to be non-nil for empty string JSON")
	} else if *tool.Location != "" {
		t.Errorf("expected Location to be empty string, got %q", *tool.Location)
	}
	if tool.UnitNumber == nil {
		t.Error("expected UnitNumber to be non-nil for empty string JSON")
	} else if *tool.UnitNumber != "" {
		t.Errorf("expected UnitNumber to be empty string, got %q", *tool.UnitNumber)
	}
	if tool.DisplayName == nil {
		t.Error("expected DisplayName to be non-nil for empty string JSON")
	} else if *tool.DisplayName != "" {
		t.Errorf("expected DisplayName to be empty string, got %q", *tool.DisplayName)
	}
}

// TestFetchSyncData_NullableFields_Omitted verifies that omitted fields deserialize to nil
func TestFetchSyncData_NullableFields_Omitted(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// Return sync data with omitted optional fields
		w.Write([]byte(`{
			"status": "OK",
			"tools": [
				{
					"id": 1,
					"tool_id": 10,
					"uii": "EPC-001",
					"sku": "SKU-001",
					"name": "Test Tool",
					"status": "available",
					"active": true
				}
			]
		}`))
	}))
	defer server.Close()

	client := NewVPSClient(server.URL, 5*time.Second, "test-token")
	resp, err := client.FetchSyncData("comp-1")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(resp.Tools))
	}

	tool := resp.Tools[0]
	if tool.Description != nil {
		t.Errorf("expected Description to be nil for omitted field, got %v", *tool.Description)
	}
	if tool.Location != nil {
		t.Errorf("expected Location to be nil for omitted field, got %v", *tool.Location)
	}
	if tool.UnitNumber != nil {
		t.Errorf("expected UnitNumber to be nil for omitted field, got %v", *tool.UnitNumber)
	}
	if tool.DisplayName != nil {
		t.Errorf("expected DisplayName to be nil for omitted field, got %v", *tool.DisplayName)
	}
}

// TestFetchSyncData_NullableFields_ValidValues verifies that valid strings deserialize correctly
func TestFetchSyncData_NullableFields_ValidValues(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{
			"status": "OK",
			"tools": [
				{
					"id": 1,
					"tool_id": 10,
					"uii": "EPC-001",
					"sku": "SKU-001",
					"name": "Test Tool",
					"description": "A useful tool",
					"status": "available",
					"location": "Warehouse A",
					"unit_number": "UNIT-001",
					"display_name": "Tool Display Name",
					"active": true
				}
			]
		}`))
	}))
	defer server.Close()

	client := NewVPSClient(server.URL, 5*time.Second, "test-token")
	resp, err := client.FetchSyncData("comp-1")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(resp.Tools))
	}

	tool := resp.Tools[0]
	if tool.Description == nil {
		t.Error("expected Description to be non-nil")
	} else if *tool.Description != "A useful tool" {
		t.Errorf("expected Description 'A useful tool', got %q", *tool.Description)
	}
	if tool.Location == nil {
		t.Error("expected Location to be non-nil")
	} else if *tool.Location != "Warehouse A" {
		t.Errorf("expected Location 'Warehouse A', got %q", *tool.Location)
	}
	if tool.UnitNumber == nil {
		t.Error("expected UnitNumber to be non-nil")
	} else if *tool.UnitNumber != "UNIT-001" {
		t.Errorf("expected UnitNumber 'UNIT-001', got %q", *tool.UnitNumber)
	}
	if tool.DisplayName == nil {
		t.Error("expected DisplayName to be non-nil")
	} else if *tool.DisplayName != "Tool Display Name" {
		t.Errorf("expected DisplayName 'Tool Display Name', got %q", *tool.DisplayName)
	}
}
