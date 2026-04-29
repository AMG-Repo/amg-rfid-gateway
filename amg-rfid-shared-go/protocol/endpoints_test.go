package protocol

import (
	"testing"
)

func TestEndpointConstants(t *testing.T) {
	tests := []struct {
		endpoint string
		expected string
	}{
		{EndpointSync, "/api/v1/rfid/sync"},
		{EndpointPending, "/api/v1/rfid/pending"},
		{EndpointHealthGateways, "/api/v1/health/gateways"},
		{EndpointMetrics, "/metrics"},
		{EndpointHealth, "/health"},
	}

	for _, tt := range tests {
		if tt.endpoint != tt.expected {
			t.Errorf("endpoint = %s, want %s", tt.endpoint, tt.expected)
		}
	}
}

func TestFullURLConstruction(t *testing.T) {
	baseURL := "https://api.example.com"

	// Test sync endpoint
	fullSyncURL := baseURL + EndpointSync
	expectedSync := "https://api.example.com/api/v1/rfid/sync"
	if fullSyncURL != expectedSync {
		t.Errorf("sync URL = %s, want %s", fullSyncURL, expectedSync)
	}

	// Test health endpoint
	fullHealthURL := baseURL + EndpointHealthGateways
	expectedHealth := "https://api.example.com/api/v1/health/gateways"
	if fullHealthURL != expectedHealth {
		t.Errorf("health URL = %s, want %s", fullHealthURL, expectedHealth)
	}
}
