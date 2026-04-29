package tui

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"testing"
	"time"

	"github.com/amg-rfid/amg-rfid-gateway/internal/antenna"
	"github.com/amg-rfid/amg-rfid-shared-go/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Mock implementations for testing
type mockHealthMonitor struct {
	status models.GatewayHealthStatus
}

func (m *mockHealthMonitor) GetStatus() models.GatewayHealthStatus {
	return m.status
}

func (m *mockHealthMonitor) GetUptime() time.Duration {
	return time.Hour
}

type mockAntennaProvider struct {
	antennas []antenna.AntennaStatus
}

func (m *mockAntennaProvider) GetAntennaStatuses() []antenna.AntennaStatus {
	return m.antennas
}

func TestBridgeServer_StartStop(t *testing.T) {
	socketPath := "/tmp/test-bridge-" + t.Name() + ".sock"
	defer os.Remove(socketPath)

	mockHealth := &mockHealthMonitor{
		status: models.GatewayHealthStatus{
			GatewayID: "test-gateway",
			Status:    "online",
		},
	}

	mockAntennas := &mockAntennaProvider{
		antennas: []antenna.AntennaStatus{
			{ID: "ant-01", Connected: true},
		},
	}

	server := NewBridgeServer(socketPath, mockHealth, mockAntennas)

	// Start server
	ctx, cancel := context.WithCancel(context.Background())
	err := server.Start(ctx)
	require.NoError(t, err)
	assert.True(t, server.IsRunning())

	// Stop server
	cancel()
	err = server.Stop()
	require.NoError(t, err)
	assert.False(t, server.IsRunning())
}

func TestBridgeServer_StopWithoutStart(t *testing.T) {
	socketPath := "/tmp/test-bridge-" + t.Name() + ".sock"
	defer os.Remove(socketPath)

	server := NewBridgeServer(socketPath, &mockHealthMonitor{}, &mockAntennaProvider{})

	// Stop without starting should not panic
	err := server.Stop()
	assert.NoError(t, err)
}

func TestBridgeServer_GetStatusRoute(t *testing.T) {
	socketPath := "/tmp/test-bridge-" + t.Name() + ".sock"
	defer os.Remove(socketPath)

	mockHealth := &mockHealthMonitor{
		status: models.GatewayHealthStatus{
			GatewayID: "test-gateway",
			CompanyID: "test-company",
			Status:    "online",
			CacheSize: 42,
			Antennas:  []models.AntennaStatus{{ID: "ant-01", Connected: true}},
			Version:   "v1.0.0",
			Uptime:    3600,
		},
	}

	server := NewBridgeServer(socketPath, mockHealth, &mockAntennaProvider{})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := server.Start(ctx)
	require.NoError(t, err)
	defer server.Stop()

	// Wait for server to be ready
	time.Sleep(50 * time.Millisecond)

	// Connect and send request
	client := NewBridgeClient(socketPath)
	status, err := client.GetStatus()

	require.NoError(t, err)
	assert.Equal(t, "test-gateway", status.GatewayID)
	assert.Equal(t, "test-company", status.CompanyID)
	assert.Equal(t, "online", status.Status)
	assert.Equal(t, 42, status.CacheSize)
	assert.Equal(t, "v1.0.0", status.Version)
}

func TestBridgeServer_GetAntennasRoute(t *testing.T) {
	socketPath := "/tmp/test-bridge-" + t.Name() + ".sock"
	defer os.Remove(socketPath)

	mockAntennas := &mockAntennaProvider{
		antennas: []antenna.AntennaStatus{
			{
				ID:           "ant-01",
				Connected:    true,
				ReadingCount: 100,
				LastTagEPC:   "EPC123",
				LastTagRSSI:  -45,
				AutoReading:  true,
				ErrorCount:   0,
			},
			{
				ID:           "ant-02",
				Connected:    false,
				ReadingCount: 0,
				AutoReading:  false,
				ErrorCount:   2,
			},
		},
	}

	server := NewBridgeServer(socketPath, &mockHealthMonitor{}, mockAntennas)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := server.Start(ctx)
	require.NoError(t, err)
	defer server.Stop()

	// Wait for server to be ready
	time.Sleep(50 * time.Millisecond)

	// Connect and send request
	client := NewBridgeClient(socketPath)
	antennas, err := client.GetAntennas()

	require.NoError(t, err)
	assert.Len(t, antennas, 2)
	assert.Equal(t, "ant-01", antennas[0].ID)
	assert.True(t, antennas[0].Connected)
	assert.Equal(t, uint64(100), antennas[0].ReadingCount)
	assert.Equal(t, "EPC123", antennas[0].LastTagEPC)
	assert.Equal(t, -45, antennas[0].LastTagRSSI)
	assert.False(t, antennas[1].Connected)
}

func TestBridgeServer_UnknownRoute(t *testing.T) {
	socketPath := "/tmp/test-bridge-" + t.Name() + ".sock"
	defer os.Remove(socketPath)

	server := NewBridgeServer(socketPath, &mockHealthMonitor{}, &mockAntennaProvider{})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := server.Start(ctx)
	require.NoError(t, err)
	defer server.Stop()

	// Wait for server to be ready
	time.Sleep(50 * time.Millisecond)

	// Connect and send unknown request
	conn, err := net.Dial("unix", socketPath)
	require.NoError(t, err)
	defer conn.Close()

	req := BridgeRequest{Method: "GET", Path: "/unknown"}
	encoder := json.NewEncoder(conn)
	err = encoder.Encode(req)
	require.NoError(t, err)

	// Read response
	var resp BridgeResponse
	decoder := json.NewDecoder(conn)
	err = decoder.Decode(&resp)
	require.NoError(t, err)

	assert.False(t, resp.Success)
	assert.Contains(t, resp.Error, "not found")
}

func TestBridgeServer_InvalidMethod(t *testing.T) {
	socketPath := "/tmp/test-bridge-" + t.Name() + ".sock"
	defer os.Remove(socketPath)

	server := NewBridgeServer(socketPath, &mockHealthMonitor{}, &mockAntennaProvider{})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := server.Start(ctx)
	require.NoError(t, err)
	defer server.Stop()

	// Wait for server to be ready
	time.Sleep(50 * time.Millisecond)

	// Connect and send POST request (not allowed)
	conn, err := net.Dial("unix", socketPath)
	require.NoError(t, err)
	defer conn.Close()

	req := BridgeRequest{Method: "POST", Path: "/status"}
	encoder := json.NewEncoder(conn)
	err = encoder.Encode(req)
	require.NoError(t, err)

	// Read response
	var resp BridgeResponse
	decoder := json.NewDecoder(conn)
	err = decoder.Decode(&resp)
	require.NoError(t, err)

	assert.False(t, resp.Success)
	assert.Contains(t, resp.Error, "method not allowed")
}

func TestBridgeServer_SocketCleanup(t *testing.T) {
	socketPath := "/tmp/test-bridge-" + t.Name() + ".sock"

	mockHealth := &mockHealthMonitor{}
	mockAntennas := &mockAntennaProvider{}

	server := NewBridgeServer(socketPath, mockHealth, mockAntennas)

	ctx, cancel := context.WithCancel(context.Background())
	err := server.Start(ctx)
	require.NoError(t, err)

	// Socket should exist
	_, err = os.Stat(socketPath)
	assert.NoError(t, err)

	// Stop server
	cancel()
	server.Stop()

	// Socket should be removed
	_, err = os.Stat(socketPath)
	assert.True(t, os.IsNotExist(err))
}

func TestBridgeServer_ConcurrentConnections(t *testing.T) {
	socketPath := "/tmp/test-bridge-" + t.Name() + ".sock"
	defer os.Remove(socketPath)

	mockHealth := &mockHealthMonitor{
		status: models.GatewayHealthStatus{
			GatewayID: "test-gateway",
			Status:    "online",
		},
	}

	server := NewBridgeServer(socketPath, mockHealth, &mockAntennaProvider{})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := server.Start(ctx)
	require.NoError(t, err)
	defer server.Stop()

	// Wait for server to be ready
	time.Sleep(50 * time.Millisecond)

	// Create multiple clients concurrently
	done := make(chan bool, 5)
	for i := 0; i < 5; i++ {
		go func() {
			client := NewBridgeClient(socketPath)
			status, err := client.GetStatus()
			if err == nil && status.GatewayID == "test-gateway" {
				done <- true
			} else {
				done <- false
			}
		}()
	}

	// Wait for all clients
	successCount := 0
	for i := 0; i < 5; i++ {
		if <-done {
			successCount++
		}
	}

	assert.Equal(t, 5, successCount)
}
