package screens

import (
	"testing"
)

func TestNetworkScreen_GetNetworkInfo(t *testing.T) {
	m := NewNetworkScreen()
	info := m.GetNetworkInfo()

	// Backend URL should be set
	if info.BackendURL == "" {
		t.Error("backend URL should not be empty")
	}

	// Backend status should be set
	if info.BackendStatus == "" {
		t.Error("backend status should not be empty")
	}
}

func TestNetworkScreen_SetNetworkInfo(t *testing.T) {
	m := NewNetworkScreen()

	newInfo := NetworkInfo{
		SSID:          "TestWiFi",
		IPAddress:     "192.168.1.100",
		Gateway:       "192.168.1.1",
		BackendStatus: "offline",
		BackendURL:    "wss://test.example.com",
	}

	m.SetNetworkInfo(newInfo)

	if m.info.SSID != "TestWiFi" {
		t.Errorf("SSID = %q, want 'TestWiFi'", m.info.SSID)
	}
	if m.info.IPAddress != "192.168.1.100" {
		t.Errorf("IPAddress = %q, want '192.168.1.100'", m.info.IPAddress)
	}
	if m.info.BackendStatus != "offline" {
		t.Errorf("BackendStatus = %q, want 'offline'", m.info.BackendStatus)
	}
}

func TestNetworkScreen_SetSize(t *testing.T) {
	m := NewNetworkScreen()
	m.SetSize(100, 40)

	if m.width != 100 {
		t.Errorf("width = %d, want 100", m.width)
	}
	if m.height != 40 {
		t.Errorf("height = %d, want 40", m.height)
	}
}

func TestNetworkScreen_View(t *testing.T) {
	m := NewNetworkScreen()
	view := m.View()

	if view == "" {
		t.Error("view should not be empty")
	}

	// Should contain network labels
	expectedLabels := []string{"WiFi SSID:", "IP Address:", "Gateway:", "Backend"}
	for _, label := range expectedLabels {
		if !contains(view, label) {
			t.Errorf("view should contain %q", label)
		}
	}
}

func TestNetworkScreen_DetectNetworkInfo(t *testing.T) {
	// Test that detectNetworkInfo returns valid info
	info := detectNetworkInfo()

	// Should have some backend URL
	if info.BackendURL == "" {
		t.Error("backend URL should not be empty")
	}

	// Backend status should be set
	if info.BackendStatus == "" {
		t.Error("backend status should not be empty")
	}
}
