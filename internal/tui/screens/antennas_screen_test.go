package screens

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestAntennasScreen_Navigation(t *testing.T) {
	// Create test antennas for navigation tests
	testAntennas := []AntennaInfo{
		{ID: "ant-01", Type: "MTI", IP: "192.168.1.101", Port: 10001, Connected: true, ReadingCount: 150, LastTagEPC: "E2001234567890ABCDEF", LastTagRSSI: -45, AutoReading: true, ErrorCount: 0},
		{ID: "ant-02", Type: "MTI", IP: "192.168.1.102", Port: 10002, Connected: true, ReadingCount: 89, LastTagEPC: "E2009876543210FEDCBA", LastTagRSSI: -52, AutoReading: true, ErrorCount: 2},
		{ID: "ant-03", Type: "MTI", IP: "192.168.1.103", Port: 10003, Connected: false, ReadingCount: 0, LastTagEPC: "", LastTagRSSI: 0, AutoReading: false, ErrorCount: 5},
	}

	tests := []struct {
		name        string
		startPos    int
		key         string
		expectPos   int
		numAntennas int
	}{
		{"down from 0", 0, "down", 1, 3},
		{"down with j", 0, "j", 1, 3},
		{"up from 1", 1, "up", 0, 3},
		{"up with k", 1, "k", 0, 3},
		{"down at bottom", 2, "down", 2, 3}, // stays at bottom
		{"up at top", 0, "up", 0, 3},        // stays at top
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewAntennasScreen()
			// Set test antennas first
			m.SetAntennas(testAntennas)
			m.cursor = tt.startPos

			newModel, _ := m.Update(tea.KeyMsg{
				Type:  tea.KeyRunes,
				Runes: []rune(tt.key),
			})
			m = newModel.(AntennasScreenModel)

			if m.cursor != tt.expectPos {
				t.Errorf("cursor = %d, want %d", m.cursor, tt.expectPos)
			}
		})
	}
}

func TestAntennasScreen_GetAntennas(t *testing.T) {
	m := NewAntennasScreen()
	antennas := m.GetAntennas()

	// NewAntennasScreen now starts with empty list
	if len(antennas) != 0 {
		t.Errorf("expected 0 antennas initially, got %d", len(antennas))
	}

	// Set test antennas and verify
	testAntennas := []AntennaInfo{
		{ID: "ant-01", Type: "MTI", IP: "192.168.1.101", Port: 10001, Connected: true, ReadingCount: 150, LastTagEPC: "E2001234567890ABCDEF", LastTagRSSI: -45, AutoReading: true, ErrorCount: 0},
		{ID: "ant-02", Type: "MTI", IP: "192.168.1.102", Port: 10002, Connected: true, ReadingCount: 89, LastTagEPC: "E2009876543210FEDCBA", LastTagRSSI: -52, AutoReading: true, ErrorCount: 2},
		{ID: "ant-03", Type: "MTI", IP: "192.168.1.103", Port: 10003, Connected: false, ReadingCount: 0, LastTagEPC: "", LastTagRSSI: 0, AutoReading: false, ErrorCount: 5},
	}
	m.SetAntennas(testAntennas)

	antennas = m.GetAntennas()
	if len(antennas) != 3 {
		t.Errorf("expected 3 antennas after SetAntennas, got %d", len(antennas))
	}

	// Check first antenna
	if antennas[0].ID != "ant-01" {
		t.Errorf("expected first antenna ID 'ant-01', got %s", antennas[0].ID)
	}
}

func TestAntennasScreen_SetAntennas(t *testing.T) {
	m := NewAntennasScreen()

	newAntennas := []AntennaInfo{
		{ID: "test-01", Type: "Test", IP: "10.0.0.1", Port: 1234, Connected: true, ReadingCount: 42, LastTagEPC: "E200TEST1234567890", LastTagRSSI: -35, AutoReading: true, ErrorCount: 0},
	}

	m.SetAntennas(newAntennas)

	if len(m.antennas) != 1 {
		t.Errorf("expected 1 antenna, got %d", len(m.antennas))
	}

	if m.antennas[0].ID != "test-01" {
		t.Errorf("expected antenna ID 'test-01', got %s", m.antennas[0].ID)
	}

	// Verify enriched fields are preserved
	if m.antennas[0].ReadingCount != 42 {
		t.Errorf("expected ReadingCount 42, got %d", m.antennas[0].ReadingCount)
	}
	if m.antennas[0].LastTagEPC != "E200TEST1234567890" {
		t.Errorf("expected LastTagEPC 'E200TEST1234567890', got %s", m.antennas[0].LastTagEPC)
	}
}

func TestAntennasScreen_SetAntennas_ResetsCursor(t *testing.T) {
	m := NewAntennasScreen()
	m.cursor = 2 // At last item

	newAntennas := []AntennaInfo{
		{ID: "test-01", Type: "Test", IP: "10.0.0.1", Port: 1234, Connected: true},
	}

	m.SetAntennas(newAntennas)

	if m.cursor != 0 {
		t.Errorf("cursor should reset to 0, got %d", m.cursor)
	}
}

func TestAntennasScreen_SetSize(t *testing.T) {
	m := NewAntennasScreen()
	m.SetSize(120, 60)

	if m.width != 120 {
		t.Errorf("width = %d, want 120", m.width)
	}
	if m.height != 60 {
		t.Errorf("height = %d, want 60", m.height)
	}
}

func TestAntennasScreen_View(t *testing.T) {
	m := NewAntennasScreen()
	view := m.View()

	if view == "" {
		t.Error("view should not be empty")
	}

	// With empty antennas, should show "No antennas detected" message
	if !contains(view, "No antennas detected") {
		t.Error("view should show 'No antennas detected' message when empty")
	}

	// Set test antennas and verify table headers
	testAntennas := []AntennaInfo{
		{ID: "ant-01", Type: "MTI", IP: "192.168.1.101", Port: 10001, Connected: true, ReadingCount: 150, LastTagEPC: "E2001234567890ABCDEF", LastTagRSSI: -45, AutoReading: true, ErrorCount: 0},
		{ID: "ant-02", Type: "MTI", IP: "192.168.1.102", Port: 10002, Connected: true, ReadingCount: 89, LastTagEPC: "E2009876543210FEDCBA", LastTagRSSI: -52, AutoReading: true, ErrorCount: 2},
	}
	m.SetAntennas(testAntennas)

	view = m.View()

	// Should contain table headers (actual view uses "ID" and "Status" in header row)
	expectedHeaders := []string{"ID", "Status", "Reads", "Errors", "Auto", "Last Tag"}
	for _, header := range expectedHeaders {
		if !contains(view, header) {
			t.Errorf("view should contain %q", header)
		}
	}
}

func TestAntennasScreen_ConnectedStatus(t *testing.T) {
	m := NewAntennasScreen()

	// Set test antennas with mixed connection status
	testAntennas := []AntennaInfo{
		{ID: "ant-01", Type: "MTI", IP: "192.168.1.101", Port: 10001, Connected: true, ReadingCount: 150, LastTagEPC: "E2001234567890ABCDEF", LastTagRSSI: -45, AutoReading: true, ErrorCount: 0},
		{ID: "ant-02", Type: "MTI", IP: "192.168.1.102", Port: 10002, Connected: true, ReadingCount: 89, LastTagEPC: "E2009876543210FEDCBA", LastTagRSSI: -52, AutoReading: true, ErrorCount: 2},
		{ID: "ant-03", Type: "MTI", IP: "192.168.1.103", Port: 10003, Connected: false, ReadingCount: 0, LastTagEPC: "", LastTagRSSI: 0, AutoReading: false, ErrorCount: 5},
	}
	m.SetAntennas(testAntennas)

	// Check that we have both connected and disconnected antennas
	connected := 0
	disconnected := 0

	for _, ant := range m.antennas {
		if ant.Connected {
			connected++
		} else {
			disconnected++
		}
	}

	// Sample data has 2 connected, 1 disconnected
	if connected != 2 {
		t.Errorf("expected 2 connected antennas, got %d", connected)
	}
	if disconnected != 1 {
		t.Errorf("expected 1 disconnected antenna, got %d", disconnected)
	}
}
