package screens

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amg-rfid/amg-rfid-gateway/internal/config"
)

func TestSettingsScreen_New(t *testing.T) {
	cfg := &config.GatewayConfig{
		GatewayID: "test-gateway",
		CompanyID: "test-company",
		CloudURL:  "wss://example.com",
		LogLevel:  "info",
	}

	m := NewSettingsScreen(cfg)

	if m.width != 0 {
		t.Errorf("width should be 0 initially, got %d", m.width)
	}
	if m.height != 0 {
		t.Errorf("height should be 0 initially, got %d", m.height)
	}
	if m.cursor != 0 {
		t.Errorf("cursor should be 0 initially, got %d", m.cursor)
	}
	if m.editing {
		t.Error("editing should be false initially")
	}
	if m.showConfirm {
		t.Error("showConfirm should be false initially")
	}
	if m.hasChanges {
		t.Error("hasChanges should be false initially")
	}
}

func TestSettingsScreen_SetSize(t *testing.T) {
	cfg := &config.GatewayConfig{GatewayID: "test"}
	m := NewSettingsScreen(cfg)
	m.SetSize(100, 50)

	if m.width != 100 {
		t.Errorf("width = %d, want 100", m.width)
	}
	if m.height != 50 {
		t.Errorf("height = %d, want 50", m.height)
	}
}

func TestSettingsScreen_Init(t *testing.T) {
	cfg := &config.GatewayConfig{GatewayID: "test"}
	m := NewSettingsScreen(cfg)
	cmd := m.Init()

	if cmd != nil {
		t.Error("Init() should return nil for settings screen")
	}
}

func TestSettingsScreen_View(t *testing.T) {
	cfg := &config.GatewayConfig{
		GatewayID: "test-gateway",
		CompanyID: "test-company",
		CloudURL:  "wss://example.com",
		LogLevel:  "info",
	}

	m := NewSettingsScreen(cfg)
	view := m.View()

	if view == "" {
		t.Error("view should not be empty")
	}

	// Should contain title
	if !strings.Contains(view, "Settings") {
		t.Error("view should contain 'Settings' title")
	}

	// Should contain field labels
	expectedLabels := []string{"Gateway ID", "Company ID", "Cloud URL", "Log Level"}
	for _, label := range expectedLabels {
		if !strings.Contains(view, label) {
			t.Errorf("view should contain label %q", label)
		}
	}

	// Should contain values
	if !strings.Contains(view, "test-gateway") {
		t.Error("view should contain gateway ID value")
	}
}

func TestSettingsScreen_Navigation(t *testing.T) {
	tests := []struct {
		name      string
		startPos  int
		keyMsg    tea.KeyMsg
		expectPos int
		numFields int
	}{
		{
			name:      "down from 0",
			startPos:  0,
			keyMsg:    tea.KeyMsg{Type: tea.KeyDown},
			expectPos: 1,
			numFields: 6,
		},
		{
			name:      "up from 1",
			startPos:  1,
			keyMsg:    tea.KeyMsg{Type: tea.KeyUp},
			expectPos: 0,
			numFields: 6,
		},
		{
			name:      "tab moves down",
			startPos:  0,
			keyMsg:    tea.KeyMsg{Type: tea.KeyTab},
			expectPos: 1,
			numFields: 6,
		},
		{
			name:      "shift+tab moves up",
			startPos:  1,
			keyMsg:    tea.KeyMsg{Type: tea.KeyShiftTab},
			expectPos: 0,
			numFields: 6,
		},
		{
			name:      "down at bottom wraps",
			startPos:  5,
			keyMsg:    tea.KeyMsg{Type: tea.KeyDown},
			expectPos: 0,
			numFields: 6,
		},
		{
			name:      "up at top wraps",
			startPos:  0,
			keyMsg:    tea.KeyMsg{Type: tea.KeyUp},
			expectPos: 5,
			numFields: 6,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.GatewayConfig{
				GatewayID: "test",
				CompanyID: "test",
				CloudURL:  "wss://test.com",
				LogLevel:  "info",
				MaxPendingConfirmations: intPtr(1000),
				PendingWarningThreshold:   intPtr(100),
			}
			m := NewSettingsScreen(cfg)
			m.cursor = tt.startPos

			newModel, _ := m.Update(tt.keyMsg)
			m = newModel.(SettingsScreenModel)

			if m.cursor != tt.expectPos {
				t.Errorf("cursor = %d, want %d", m.cursor, tt.expectPos)
			}
		})
	}
}

func TestSettingsScreen_EnterStartsEditing(t *testing.T) {
	cfg := &config.GatewayConfig{
		GatewayID: "test-gateway",
		CompanyID: "test-company",
		CloudURL:  "wss://example.com",
		LogLevel:  "info",
	}

	m := NewSettingsScreen(cfg)
	m.cursor = 0 // GatewayID field

	if m.editing {
		t.Error("should not be editing initially")
	}

	newModel, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = newModel.(SettingsScreenModel)

	if !m.editing {
		t.Error("should be editing after Enter")
	}
}

func TestSettingsScreen_EscCancelsEditing(t *testing.T) {
	cfg := &config.GatewayConfig{
		GatewayID: "test-gateway",
		CompanyID: "test-company",
		CloudURL:  "wss://example.com",
		LogLevel:  "info",
	}

	m := NewSettingsScreen(cfg)
	m.cursor = 0

	// Start editing
	newModel, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = newModel.(SettingsScreenModel)

	if !m.editing {
		t.Fatal("setup failed: should be editing")
	}

	// Type something
	m.editBuffer = "modified-value"

	// Cancel with Esc
	newModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = newModel.(SettingsScreenModel)

	if m.editing {
		t.Error("should not be editing after Esc")
	}

	// Value should be restored
	if m.values[0] != "test-gateway" {
		t.Errorf("value should be restored, got %q", m.values[0])
	}
}

func TestSettingsScreen_EscGoesBack(t *testing.T) {
	cfg := &config.GatewayConfig{GatewayID: "test"}
	m := NewSettingsScreen(cfg)

	// Not editing, Esc should signal to go back
	newModel, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = newModel.(SettingsScreenModel)

	// Check that we got a back command
	if cmd == nil {
		t.Error("Esc when not editing should return a command")
	}

	// Execute command to verify it returns the right message
	msg := cmd()
	if _, ok := msg.(settingsBackMsg); !ok {
		t.Errorf("Esc should return settingsBackMsg, got %T", msg)
	}
}

func TestSettingsScreen_HasChanges(t *testing.T) {
	cfg := &config.GatewayConfig{
		GatewayID: "original",
		CompanyID: "test-company",
		CloudURL:  "wss://example.com",
		LogLevel:  "info",
	}

	m := NewSettingsScreen(cfg)

	if m.HasChanges() {
		t.Error("should not have changes initially")
	}

	// Start editing and change value
	m.cursor = 0
	newModel, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = newModel.(SettingsScreenModel)

	// Type new value
	newModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	m = newModel.(SettingsScreenModel)

	// Still no changes until we save
	if m.HasChanges() {
		t.Error("should not have changes until saved")
	}

	// Save the value
	newModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = newModel.(SettingsScreenModel)

	if !m.HasChanges() {
		t.Error("should have changes after saving modified value")
	}
}

func TestSettingsScreen_Validation(t *testing.T) {
	tests := []struct {
		name      string
		fieldIdx  int
		value     string
		wantValid bool
		wantError string
	}{
		{
			name:      "GatewayID empty is invalid",
			fieldIdx:  0,
			value:     "",
			wantValid: false,
			wantError: "cannot be empty",
		},
		{
			name:      "GatewayID with value is valid",
			fieldIdx:  0,
			value:     "gateway-1",
			wantValid: true,
		},
		{
			name:      "CloudURL not wss is invalid",
			fieldIdx:  2,
			value:     "http://example.com",
			wantValid: false,
			wantError: "wss://",
		},
		{
			name:      "CloudURL wss is valid",
			fieldIdx:  2,
			value:     "wss://example.com",
			wantValid: true,
		},
		{
			name:      "LogLevel valid",
			fieldIdx:  3,
			value:     "debug",
			wantValid: true,
		},
		{
			name:      "LogLevel invalid",
			fieldIdx:  3,
			value:     "invalid",
			wantValid: false,
			wantError: "debug, info, warn, error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.GatewayConfig{
				GatewayID: "test",
				CompanyID: "test",
				CloudURL:  "wss://test.com",
				LogLevel:  "info",
			}

			m := NewSettingsScreen(cfg)
			m.cursor = tt.fieldIdx

			// Start editing
			newModel, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
			m = newModel.(SettingsScreenModel)

			// Clear and type new value
			m.editBuffer = tt.value

			// Validate
			err := m.validateCurrentField()
			isValid := err == nil

			if isValid != tt.wantValid {
				t.Errorf("validation = %v, want %v", isValid, tt.wantValid)
			}

			if !tt.wantValid && !strings.Contains(err.Error(), tt.wantError) {
				t.Errorf("error message should contain %q, got %q", tt.wantError, err.Error())
			}
		})
	}
}

func TestSettingsScreen_SaveShowsConfirmation(t *testing.T) {
	cfg := &config.GatewayConfig{
		GatewayID: "test",
		CompanyID: "test",
		CloudURL:  "wss://test.com",
		LogLevel:  "info",
	}

	m := NewSettingsScreen(cfg)
	m.cursor = 0

	// Change a value
	m.values[0] = "modified"
	m.hasChanges = true

	if m.showConfirm {
		t.Error("should not show confirm initially")
	}

	// Press 's' for save
	newModel, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	m = newModel.(SettingsScreenModel)

	if !m.showConfirm {
		t.Error("should show confirm after pressing 's'")
	}
}

func TestSettingsScreen_ConfirmSave(t *testing.T) {
	cfg := &config.GatewayConfig{
		GatewayID: "original",
		CompanyID: "test",
		CloudURL:  "wss://test.com",
		LogLevel:  "info",
	}

	m := NewSettingsScreen(cfg)
	m.cursor = 0
	m.values[0] = "modified"
	m.hasChanges = true
	m.showConfirm = true

	// Press 'y' to confirm
	newModel, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m = newModel.(SettingsScreenModel)

	if m.showConfirm {
		t.Error("confirm dialog should close after 'y'")
	}

	// Check that we got a save message
	if cmd == nil {
		t.Error("should return save command after confirm")
	} else {
		msg := cmd()
		if _, ok := msg.(settingsSaveMsg); !ok {
			t.Errorf("should return settingsSaveMsg, got %T", msg)
		}
	}
}

func TestSettingsScreen_CancelSave(t *testing.T) {
	cfg := &config.GatewayConfig{
		GatewayID: "original",
		CompanyID: "test",
		CloudURL:  "wss://test.com",
		LogLevel:  "info",
	}

	m := NewSettingsScreen(cfg)
	m.showConfirm = true

	// Press 'n' to cancel
	newModel, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	m = newModel.(SettingsScreenModel)

	if m.showConfirm {
		t.Error("confirm dialog should close after 'n'")
	}
}

func TestSettingsScreen_GetConfig(t *testing.T) {
	cfg := &config.GatewayConfig{
		GatewayID: "original",
		CompanyID: "test-company",
		CloudURL:  "wss://example.com",
		LogLevel:  "info",
	}

	m := NewSettingsScreen(cfg)
	m.values[0] = "modified-gateway"
	m.values[1] = "modified-company"

	result := m.GetConfig()

	if result.GatewayID != "modified-gateway" {
		t.Errorf("GatewayID = %q, want 'modified-gateway'", result.GatewayID)
	}
	if result.CompanyID != "modified-company" {
		t.Errorf("CompanyID = %q, want 'modified-company'", result.CompanyID)
	}
}

func TestSettingsScreen_GetOriginalConfig(t *testing.T) {
	cfg := &config.GatewayConfig{
		GatewayID: "original",
		CompanyID: "test",
		CloudURL:  "wss://test.com",
		LogLevel:  "info",
	}

	m := NewSettingsScreen(cfg)
	m.values[0] = "modified"

	result := m.GetOriginalConfig()

	if result.GatewayID != "original" {
		t.Errorf("original GatewayID should be unchanged, got %q", result.GatewayID)
	}
}

func TestSettingsScreen_FieldLabels(t *testing.T) {
	cfg := &config.GatewayConfig{
		GatewayID: "test",
		CompanyID: "test",
		CloudURL:  "wss://test.com",
		LogLevel:  "info",
	}

	m := NewSettingsScreen(cfg)

	expectedFields := []string{
		"Gateway ID",
		"Company ID",
		"Cloud URL",
		"Log Level",
		"Queue Cap",
		"Warning Threshold",
	}

	if len(m.fields) != len(expectedFields) {
		t.Errorf("expected %d fields, got %d", len(expectedFields), len(m.fields))
	}

	for i, expected := range expectedFields {
		if i >= len(m.fields) {
			break
		}
		if m.fields[i].label != expected {
			t.Errorf("field[%d].label = %q, want %q", i, m.fields[i].label, expected)
		}
	}
}

func TestSettingsScreen_SetConfig(t *testing.T) {
	cfg := &config.GatewayConfig{
		GatewayID: "test",
		CompanyID: "test",
		CloudURL:  "wss://test.com",
		LogLevel:  "info",
	}

	m := NewSettingsScreen(cfg)

	// Set a new config
	newCfg := &config.GatewayConfig{
		GatewayID: "new-gateway",
		CompanyID: "new-company",
		CloudURL:  "wss://new.com",
		LogLevel:  "debug",
	}

	m.SetConfig(newCfg)

	if m.values[0] != "new-gateway" {
		t.Errorf("values[0] = %q, want 'new-gateway'", m.values[0])
	}
	if m.values[1] != "new-company" {
		t.Errorf("values[1] = %q, want 'new-company'", m.values[1])
	}
}

func TestSettingsScreen_AntennaField(t *testing.T) {
	cfg := &config.GatewayConfig{
		GatewayID: "test",
		Antennas: []config.AntennaConfig{
			{ID: "ant1", IP: "192.168.1.10", Port: 8080, Enabled: true, Zone: "entrada"},
			{ID: "ant2", IP: "192.168.1.11", Port: 8080, Enabled: false, Zone: "salida"},
		},
	}

	m := NewSettingsScreen(cfg)

	// The view should contain antenna information
	view := m.View()
	if !strings.Contains(view, "ant1") {
		t.Error("view should contain antenna ID 'ant1'")
	}
	if !strings.Contains(view, "192.168.1.10") {
		t.Error("view should contain antenna IP '192.168.1.10'")
	}
}

// Helper function
func intPtr(i int) *int {
	return &i
}
