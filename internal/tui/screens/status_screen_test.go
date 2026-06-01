package screens

import (
	"testing"
	"time"
)

func TestStatusScreen_GetStatus(t *testing.T) {
	m := NewStatusScreen()
	status := m.GetStatus()

	// NewStatusScreen now starts with placeholder values ("—")
	// Gateway ID should have placeholder
	if status.GatewayID != "—" {
		t.Errorf("gateway ID should be '—' initially, got %q", status.GatewayID)
	}

	// Company ID should have placeholder
	if status.CompanyID != "—" {
		t.Errorf("company ID should be '—' initially, got %q", status.CompanyID)
	}

	// Version should have placeholder
	if status.Version != "—" {
		t.Errorf("version should be '—' initially, got %q", status.Version)
	}

	// SyncStatus should be "waiting"
	if status.SyncStatus != "waiting" {
		t.Errorf("sync status should be 'waiting' initially, got %q", status.SyncStatus)
	}

	// Antenna counts should be 0
	if status.AntennaCount != 0 {
		t.Errorf("antenna count should be 0 initially, got %d", status.AntennaCount)
	}
	if status.ConnectedAntennas != 0 {
		t.Errorf("connected antennas should be 0 initially, got %d", status.ConnectedAntennas)
	}
}

func TestStatusScreen_SetStatus(t *testing.T) {
	m := NewStatusScreen()

	newStatus := SystemStatus{
		GatewayID:         "test-gateway",
		CompanyID:         "test-company",
		Version:           "v1.0.0",
		Uptime:            time.Hour,
		PendingReadings:   100,
		SyncStatus:        "offline",
		LastSync:          time.Now(),
		AntennaCount:      5,
		ConnectedAntennas: 3,
	}

	m.SetStatus(newStatus)

	if m.status.GatewayID != "test-gateway" {
		t.Errorf("GatewayID = %q, want 'test-gateway'", m.status.GatewayID)
	}
	if m.status.PendingReadings != 100 {
		t.Errorf("PendingReadings = %d, want 100", m.status.PendingReadings)
	}
	if m.status.SyncStatus != "offline" {
		t.Errorf("SyncStatus = %q, want 'offline'", m.status.SyncStatus)
	}
}

func TestStatusScreen_SetSize(t *testing.T) {
	m := NewStatusScreen()
	m.SetSize(100, 40)

	if m.width != 100 {
		t.Errorf("width = %d, want 100", m.width)
	}
	if m.height != 40 {
		t.Errorf("height = %d, want 40", m.height)
	}
}

func TestStatusScreen_View(t *testing.T) {
	m := NewStatusScreen()
	view := m.View()

	if view == "" {
		t.Error("view should not be empty")
	}

	// Should contain section headers
	expectedSections := []string{"Identity", "Runtime", "Synchronization"}
	for _, section := range expectedSections {
		if !contains(view, section) {
			t.Errorf("view should contain %q", section)
		}
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		name     string
		duration time.Duration
		expected string
	}{
		{"seconds only", 45 * time.Second, "45s"},
		{"minutes and seconds", 5*time.Minute + 30*time.Second, "5m 30s"},
		{"hours and minutes", 2*time.Hour + 30*time.Minute, "2h 30m"},
		{"days and hours", 25 * time.Hour, "1d 1h"},
		{"multiple days", 50 * time.Hour, "2d 2h"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatDuration(tt.duration)
			if result != tt.expected {
				t.Errorf("formatDuration(%v) = %q, want %q", tt.duration, result, tt.expected)
			}
		})
	}
}

func TestStatusScreen_ViewUsesBackendUptime(t *testing.T) {
	tests := []struct {
		name        string
		uptime      time.Duration
		wantDisplay string
	}{
		{
			name:        "renders backend hour minute uptime",
			uptime:      time.Hour + 23*time.Minute,
			wantDisplay: "1h 23m",
		},
		{
			name:        "renders backend minute second uptime",
			uptime:      5*time.Minute + 30*time.Second,
			wantDisplay: "5m 30s",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewStatusScreen()
			m.SetStatus(SystemStatus{Uptime: tt.uptime})

			view := m.View()

			if !contains(view, tt.wantDisplay) {
				t.Fatalf("view should contain backend uptime %q, got:\n%s", tt.wantDisplay, view)
			}
		})
	}
}
