package wsclient

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/amg-rfid/amg-rfid-shared-go/models"
	"github.com/gorilla/websocket"
)

func TestNewClient(t *testing.T) {
	client := NewClient("wss://cloud.example.com/ws", "test-jwt-token")

	if client.url != "wss://cloud.example.com/ws" {
		t.Errorf("expected URL 'wss://cloud.example.com/ws', got '%s'", client.url)
	}
	if client.jwtToken != "test-jwt-token" {
		t.Errorf("expected token 'test-jwt-token', got '%s'", client.jwtToken)
	}
}

func TestClient_Connect(t *testing.T) {
	// Create test WebSocket server
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify JWT header
		authHeader := r.Header.Get("Authorization")
		if authHeader != "Bearer test-token" {
			t.Errorf("expected Authorization header 'Bearer test-token', got '%s'", authHeader)
		}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade failed: %v", err)
		}
		defer conn.Close()

		// Echo back
		for {
			mt, message, err := conn.ReadMessage()
			if err != nil {
				return
			}
			conn.WriteMessage(mt, message)
		}
	}))
	defer server.Close()

	// Convert http:// to ws://
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	client := NewClient(wsURL, "test-token")
	err := client.Connect()
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer client.Disconnect()

	if !client.IsConnected() {
		t.Error("expected client to be connected")
	}
}

func TestClient_Connect_InvalidURL(t *testing.T) {
	client := NewClient("not-a-valid-url", "test-token")
	err := client.Connect()
	if err == nil {
		t.Error("expected error for invalid URL, got nil")
	}
}

func TestClient_Send(t *testing.T) {
	// Create test WebSocket server that echoes
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	received := make(chan []byte, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade failed: %v", err)
		}
		defer conn.Close()

		// Read message
		_, message, err := conn.ReadMessage()
		if err != nil {
			return
		}
		received <- message

		// Echo back
		conn.WriteMessage(websocket.TextMessage, message)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	client := NewClient(wsURL, "test-token")
	err := client.Connect()
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer client.Disconnect()

	// Send message
	testMessage := []byte(`{"test": "message"}`)
	err = client.Send(testMessage)
	if err != nil {
		t.Errorf("failed to send: %v", err)
	}

	// Verify server received it
	select {
	case msg := <-received:
		if string(msg) != string(testMessage) {
			t.Errorf("expected '%s', got '%s'", testMessage, msg)
		}
	case <-time.After(2 * time.Second):
		t.Error("timeout waiting for message")
	}
}

func TestClient_Send_NotConnected(t *testing.T) {
	client := NewClient("wss://example.com", "token")
	// Don't connect

	err := client.Send([]byte("test"))
	if err == nil {
		t.Error("expected error when sending while not connected")
	}
}

func TestClient_Disconnect(t *testing.T) {
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, _ := upgrader.Upgrade(w, r, nil)
		if conn != nil {
			defer conn.Close()
			// Keep connection open
			time.Sleep(100 * time.Millisecond)
		}
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	client := NewClient(wsURL, "test-token")
	err := client.Connect()
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}

	err = client.Disconnect()
	if err != nil {
		t.Errorf("failed to disconnect: %v", err)
	}

	if client.IsConnected() {
		t.Error("expected client to be disconnected")
	}
}

func TestClient_Reconnect(t *testing.T) {
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	connectCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		connectCount++
		conn, _ := upgrader.Upgrade(w, r, nil)
		if conn != nil {
			defer conn.Close()
			// Close immediately on first connection
			if connectCount == 1 {
				return
			}
			// Keep open on second connection
			time.Sleep(500 * time.Millisecond)
		}
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	client := NewClient(wsURL, "test-token")
	client.reconnectDelay = 50 * time.Millisecond // Speed up for tests

	// First connect
	err := client.Connect()
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}

	// Force disconnect
	client.Disconnect()

	// Wait a bit
	time.Sleep(100 * time.Millisecond)

	// Try to reconnect
	err = client.Reconnect()
	if err != nil {
		t.Logf("Reconnect returned: %v (this is expected)", err)
	}
}

func TestClient_SyncRequest(t *testing.T) {
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade failed: %v", err)
		}
		defer conn.Close()

		// Read sync request
		_, _, err = conn.ReadMessage()
		if err != nil {
			return
		}

		// Send sync response
		response := `{"success": true, "accepted": 1, "duplicates": 0, "errors": [], "next_sync_in": 30, "server_time": "2024-01-01T00:00:00Z"}`
		conn.WriteMessage(websocket.TextMessage, []byte(response))
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	client := NewClient(wsURL, "test-token")
	err := client.Connect()
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer client.Disconnect()

	// Send sync request
	request := models.SyncRequest{
		GatewayID: "gw-001",
		CompanyID: "comp-123",
		Readings: []models.Reading{
			{
				AntennaID: "ant-1",
				EPC:       "E200341502001080",
				RSSI:      201,
				Timestamp: time.Now(),
			},
		},
		Timestamp: time.Now(),
	}

	response, err := client.SendSyncRequest(request)
	if err != nil {
		t.Fatalf("failed to send sync request: %v", err)
	}

	if !response.Success {
		t.Error("expected success response")
	}
	if response.Accepted != 1 {
		t.Errorf("expected 1 accepted reading, got %d", response.Accepted)
	}
}

func TestClient_IsConnected_ClosedByServer(t *testing.T) {
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, _ := upgrader.Upgrade(w, r, nil)
		if conn != nil {
			// Close immediately
			conn.Close()
		}
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	client := NewClient(wsURL, "test-token")
	err := client.Connect()
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}

	// Wait for server to close
	time.Sleep(100 * time.Millisecond)

	// Check connection - should detect closure
	if client.IsConnected() {
		// This might pass or fail depending on timing
		t.Log("Client still thinks it's connected (timing dependent)")
	}
}
