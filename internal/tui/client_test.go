package tui

import (
	"encoding/json"
	"errors"
	"net"
	"os"
	"testing"
	"time"

	"github.com/amg-rfid/amg-rfid-gateway/internal/antenna"
	"github.com/amg-rfid/amg-rfid-gateway/internal/config"
	"github.com/amg-rfid/amg-rfid-shared-go/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBridgeClient_New(t *testing.T) {
	client := NewBridgeClient("/tmp/test.sock")
	assert.NotNil(t, client)
	assert.Equal(t, "/tmp/test.sock", client.socketPath)
}

func TestBridgeClient_GetStatus_Success(t *testing.T) {
	socketPath := "/tmp/test-client-" + t.Name() + ".sock"
	defer os.Remove(socketPath)

	// Start a mock server
	listener, err := net.Listen("unix", socketPath)
	require.NoError(t, err)
	defer listener.Close()

	// Server response
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		// Read request
		var req BridgeRequest
		decoder := json.NewDecoder(conn)
		if err := decoder.Decode(&req); err != nil {
			return
		}

		// Send response
		resp := BridgeResponse{
			Success: true,
			Data: models.GatewayHealthStatus{
				GatewayID: "client-test-gateway",
				Status:    "online",
			},
		}
		encoder := json.NewEncoder(conn)
		encoder.Encode(resp)
	}()

	// Wait for server to be ready
	time.Sleep(50 * time.Millisecond)

	client := NewBridgeClient(socketPath)
	status, err := client.GetStatus()

	require.NoError(t, err)
	assert.Equal(t, "client-test-gateway", status.GatewayID)
	assert.Equal(t, "online", status.Status)
}

func TestBridgeClient_GetStatus_Timeout(t *testing.T) {
	socketPath := "/tmp/test-client-" + t.Name() + ".sock"
	defer os.Remove(socketPath)

	// Start a server that never responds
	listener, err := net.Listen("unix", socketPath)
	require.NoError(t, err)
	defer listener.Close()

	go func() {
		conn, _ := listener.Accept()
		if conn != nil {
			// Don't respond, just hold the connection
			time.Sleep(10 * time.Second)
			conn.Close()
		}
	}()

	// Wait for server to be ready
	time.Sleep(50 * time.Millisecond)

	client := NewBridgeClient(socketPath)
	// Override timeout for faster test
	client.timeout = 100 * time.Millisecond

	_, err = client.GetStatus()

	assert.Error(t, err)
	assert.True(t, errors.Is(err, os.ErrDeadlineExceeded) || err.Error() == "i/o timeout")
}

func TestBridgeClient_GetStatus_ConnectionRefused(t *testing.T) {
	socketPath := "/tmp/test-client-nonexistent-" + t.Name() + ".sock"

	client := NewBridgeClient(socketPath)
	_, err := client.GetStatus()

	assert.Error(t, err)
}

func TestBridgeClient_GetAntennas_Success(t *testing.T) {
	socketPath := "/tmp/test-client-" + t.Name() + ".sock"
	defer os.Remove(socketPath)

	// Start a mock server
	listener, err := net.Listen("unix", socketPath)
	require.NoError(t, err)
	defer listener.Close()

	// Server response
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		// Read request
		var req BridgeRequest
		decoder := json.NewDecoder(conn)
		if err := decoder.Decode(&req); err != nil {
			return
		}

		// Send response
		antennas := []antenna.AntennaStatus{
			{ID: "ant-01", Connected: true, ReadingCount: 50},
			{ID: "ant-02", Connected: false, ErrorCount: 1},
		}
		resp := BridgeResponse{
			Success: true,
			Data:    antennas,
		}
		encoder := json.NewEncoder(conn)
		encoder.Encode(resp)
	}()

	// Wait for server to be ready
	time.Sleep(50 * time.Millisecond)

	client := NewBridgeClient(socketPath)
	antennas, err := client.GetAntennas()

	require.NoError(t, err)
	assert.Len(t, antennas, 2)
	assert.Equal(t, "ant-01", antennas[0].ID)
	assert.True(t, antennas[0].Connected)
	assert.Equal(t, uint64(50), antennas[0].ReadingCount)
	assert.False(t, antennas[1].Connected)
}

func TestBridgeClient_GetAntennas_ServerError(t *testing.T) {
	socketPath := "/tmp/test-client-" + t.Name() + ".sock"
	defer os.Remove(socketPath)

	// Start a mock server that returns error
	listener, err := net.Listen("unix", socketPath)
	require.NoError(t, err)
	defer listener.Close()

	// Server response
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		// Read request
		var req BridgeRequest
		decoder := json.NewDecoder(conn)
		if err := decoder.Decode(&req); err != nil {
			return
		}

		// Send error response
		resp := BridgeResponse{
			Success: false,
			Error:   "internal server error",
		}
		encoder := json.NewEncoder(conn)
		encoder.Encode(resp)
	}()

	// Wait for server to be ready
	time.Sleep(50 * time.Millisecond)

	client := NewBridgeClient(socketPath)
	_, err = client.GetAntennas()

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "internal server error")
}

func TestBridgeClient_InvalidResponse(t *testing.T) {
	socketPath := "/tmp/test-client-" + t.Name() + ".sock"
	defer os.Remove(socketPath)

	// Start a mock server that sends invalid JSON
	listener, err := net.Listen("unix", socketPath)
	require.NoError(t, err)
	defer listener.Close()

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		// Read request (discard)
		buf := make([]byte, 1024)
		conn.Read(buf)

		// Send invalid JSON
		conn.Write([]byte("not valid json"))
	}()

	// Wait for server to be ready
	time.Sleep(50 * time.Millisecond)

	client := NewBridgeClient(socketPath)
	_, err = client.GetStatus()

	assert.Error(t, err)
}

func TestBridgeClient_MultipleRequests(t *testing.T) {
	socketPath := "/tmp/test-client-" + t.Name() + ".sock"
	defer os.Remove(socketPath)

	// Start a mock server
	listener, err := net.Listen("unix", socketPath)
	require.NoError(t, err)
	defer listener.Close()

	requestCount := 0

	// Server response
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}

			go func(c net.Conn) {
				defer c.Close()

				// Read request
				var req BridgeRequest
				decoder := json.NewDecoder(c)
				if err := decoder.Decode(&req); err != nil {
					return
				}

				requestCount++

				// Send response
				var resp BridgeResponse
				if req.Path == "/status" {
					resp = BridgeResponse{
						Success: true,
						Data:    models.GatewayHealthStatus{GatewayID: "gateway-" + string(rune(requestCount))},
					}
				} else {
					resp = BridgeResponse{
						Success: true,
						Data:    []antenna.AntennaStatus{{ID: "ant-" + string(rune(requestCount))}},
					}
				}
				encoder := json.NewEncoder(c)
				encoder.Encode(resp)
			}(conn)
		}
	}()

	// Wait for server to be ready
	time.Sleep(50 * time.Millisecond)

	client := NewBridgeClient(socketPath)

	// Make multiple requests
	for i := 0; i < 3; i++ {
		status, err := client.GetStatus()
		require.NoError(t, err)
		assert.NotEmpty(t, status.GatewayID)
	}

	antennas, err := client.GetAntennas()
	require.NoError(t, err)
	assert.NotEmpty(t, antennas)
}

func TestBridgeClient_GetConfig_Success(t *testing.T) {
	socketPath := "/tmp/test-client-" + t.Name() + ".sock"
	defer os.Remove(socketPath)

	// Start a mock server
	listener, err := net.Listen("unix", socketPath)
	require.NoError(t, err)
	defer listener.Close()

	// Server response
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		// Read request
		var req BridgeRequest
		decoder := json.NewDecoder(conn)
		if err := decoder.Decode(&req); err != nil {
			return
		}

		// Verify request
		assert.Equal(t, "GET", req.Method)
		assert.Equal(t, "/config", req.Path)

		// Send response
		cfg := config.GatewayConfig{
			GatewayID: "client-test-gateway",
			CompanyID: "test-company",
			CloudURL:  "wss://test.example.com",
		}
		resp := BridgeResponse{
			Success: true,
			Data:    cfg,
		}
		encoder := json.NewEncoder(conn)
		encoder.Encode(resp)
	}()

	// Wait for server to be ready
	time.Sleep(50 * time.Millisecond)

	client := NewBridgeClient(socketPath)
	cfg, err := client.GetConfig()

	require.NoError(t, err)
	assert.NotNil(t, cfg)
	assert.Equal(t, "client-test-gateway", cfg.GatewayID)
	assert.Equal(t, "test-company", cfg.CompanyID)
	assert.Equal(t, "wss://test.example.com", cfg.CloudURL)
}

func TestBridgeClient_UpdateConfig_Success(t *testing.T) {
	socketPath := "/tmp/test-client-" + t.Name() + ".sock"
	defer os.Remove(socketPath)

	// Start a mock server
	listener, err := net.Listen("unix", socketPath)
	require.NoError(t, err)
	defer listener.Close()

	var receivedBody json.RawMessage

	// Server response
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		// Read request
		var req BridgeRequest
		decoder := json.NewDecoder(conn)
		if err := decoder.Decode(&req); err != nil {
			return
		}

		// Verify request
		assert.Equal(t, "POST", req.Method)
		assert.Equal(t, "/config", req.Path)
		receivedBody = req.Body

		// Send success response
		resp := BridgeResponse{
			Success: true,
		}
		encoder := json.NewEncoder(conn)
		encoder.Encode(resp)
	}()

	// Wait for server to be ready
	time.Sleep(50 * time.Millisecond)

	cfg := &config.GatewayConfig{
		GatewayID: "updated-gateway",
		CompanyID: "updated-company",
		CloudURL:  "wss://updated.example.com",
	}

	client := NewBridgeClient(socketPath)
	err = client.UpdateConfig(cfg)

	require.NoError(t, err)
	require.NotNil(t, receivedBody)

	// Verify the body was sent correctly
	var receivedCfg config.GatewayConfig
	err = json.Unmarshal(receivedBody, &receivedCfg)
	require.NoError(t, err)
	assert.Equal(t, "updated-gateway", receivedCfg.GatewayID)
	assert.Equal(t, "updated-company", receivedCfg.CompanyID)
}

func TestBridgeClient_UpdateConfig_ServerError(t *testing.T) {
	socketPath := "/tmp/test-client-" + t.Name() + ".sock"
	defer os.Remove(socketPath)

	// Start a mock server
	listener, err := net.Listen("unix", socketPath)
	require.NoError(t, err)
	defer listener.Close()

	// Server response
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		// Read request
		var req BridgeRequest
		decoder := json.NewDecoder(conn)
		if err := decoder.Decode(&req); err != nil {
			return
		}

		// Send error response
		resp := BridgeResponse{
			Success: false,
			Error:   "validation failed",
		}
		encoder := json.NewEncoder(conn)
		encoder.Encode(resp)
	}()

	// Wait for server to be ready
	time.Sleep(50 * time.Millisecond)

	cfg := &config.GatewayConfig{
		GatewayID: "test-gateway",
		CompanyID: "test-company",
		CloudURL:  "wss://test.example.com",
	}

	client := NewBridgeClient(socketPath)
	err = client.UpdateConfig(cfg)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "validation failed")
}

func TestBridgeClient_ReloadConfig_Success(t *testing.T) {
	socketPath := "/tmp/test-client-" + t.Name() + ".sock"
	defer os.Remove(socketPath)

	// Start a mock server
	listener, err := net.Listen("unix", socketPath)
	require.NoError(t, err)
	defer listener.Close()

	// Server response
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		// Read request
		var req BridgeRequest
		decoder := json.NewDecoder(conn)
		if err := decoder.Decode(&req); err != nil {
			return
		}

		// Verify request
		assert.Equal(t, "POST", req.Method)
		assert.Equal(t, "/config/reload", req.Path)

		// Send success response
		resp := BridgeResponse{
			Success: true,
		}
		encoder := json.NewEncoder(conn)
		encoder.Encode(resp)
	}()

	// Wait for server to be ready
	time.Sleep(50 * time.Millisecond)

	client := NewBridgeClient(socketPath)
	err = client.ReloadConfig()

	require.NoError(t, err)
}

func TestBridgeClient_ReloadConfig_ServerError(t *testing.T) {
	socketPath := "/tmp/test-client-" + t.Name() + ".sock"
	defer os.Remove(socketPath)

	// Start a mock server
	listener, err := net.Listen("unix", socketPath)
	require.NoError(t, err)
	defer listener.Close()

	// Server response
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		// Read request
		var req BridgeRequest
		decoder := json.NewDecoder(conn)
		if err := decoder.Decode(&req); err != nil {
			return
		}

		// Send error response
		resp := BridgeResponse{
			Success: false,
			Error:   "reload failed",
		}
		encoder := json.NewEncoder(conn)
		encoder.Encode(resp)
	}()

	// Wait for server to be ready
	time.Sleep(50 * time.Millisecond)

	client := NewBridgeClient(socketPath)
	err = client.ReloadConfig()

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "reload failed")
}
