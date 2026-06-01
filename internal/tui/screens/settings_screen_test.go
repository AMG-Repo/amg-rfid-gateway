package screens

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
	if m.promptMode != settingsPromptNone {
		t.Error("prompt mode should be none initially")
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
			numFields: 8,
		},
		{
			name:      "up from 1",
			startPos:  1,
			keyMsg:    tea.KeyMsg{Type: tea.KeyUp},
			expectPos: 0,
			numFields: 8,
		},
		{
			name:      "tab moves down",
			startPos:  0,
			keyMsg:    tea.KeyMsg{Type: tea.KeyTab},
			expectPos: 1,
			numFields: 8,
		},
		{
			name:      "shift+tab moves up",
			startPos:  1,
			keyMsg:    tea.KeyMsg{Type: tea.KeyShiftTab},
			expectPos: 0,
			numFields: 8,
		},
		{
			name:      "down at bottom wraps",
			startPos:  7,
			keyMsg:    tea.KeyMsg{Type: tea.KeyDown},
			expectPos: 0,
			numFields: 8,
		},
		{
			name:      "up at top wraps",
			startPos:  0,
			keyMsg:    tea.KeyMsg{Type: tea.KeyUp},
			expectPos: 7,
			numFields: 8,
		},
		{
			name:      "j matches down arrow",
			startPos:  0,
			keyMsg:    tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")},
			expectPos: 1,
			numFields: 8,
		},
		{
			name:      "k matches up arrow and wraps",
			startPos:  0,
			keyMsg:    tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")},
			expectPos: 7,
			numFields: 8,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.GatewayConfig{
				GatewayID:               "test",
				CompanyID:               "test",
				CloudURL:                "wss://test.com",
				LogLevel:                "info",
				MaxPendingConfirmations: intPtr(1000),
				PendingWarningThreshold: intPtr(100),
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
			name:      "Web access mode local is valid",
			fieldIdx:  3,
			value:     "local",
			wantValid: true,
		},
		{
			name:      "Web access mode invalid",
			fieldIdx:  3,
			value:     "internet",
			wantValid: false,
			wantError: "local, lan",
		},
		{
			name:      "LogLevel valid",
			fieldIdx:  5,
			value:     "debug",
			wantValid: true,
		},
		{
			name:      "LogLevel invalid",
			fieldIdx:  5,
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

	if m.promptMode != settingsPromptNone {
		t.Error("should not show confirm initially")
	}

	// Press 's' for save
	newModel, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	m = newModel.(SettingsScreenModel)

	if m.promptMode != settingsPromptSaveConfirm {
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
	m.promptMode = settingsPromptSaveConfirm

	// Press 'y' to confirm
	newModel, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m = newModel.(SettingsScreenModel)

	if m.promptMode != settingsPromptNone {
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
	m.promptMode = settingsPromptSaveConfirm

	// Press 'n' to cancel
	newModel, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	m = newModel.(SettingsScreenModel)

	if m.promptMode != settingsPromptNone {
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
	m.values[3] = "lan"
	m.values[4] = "tablet-token"

	result := m.GetConfig()

	if result.GatewayID != "modified-gateway" {
		t.Errorf("GatewayID = %q, want 'modified-gateway'", result.GatewayID)
	}
	if result.CompanyID != "modified-company" {
		t.Errorf("CompanyID = %q, want 'modified-company'", result.CompanyID)
	}
	if result.WebAccessMode != "lan" {
		t.Errorf("WebAccessMode = %q, want 'lan'", result.WebAccessMode)
	}
	if result.WebListenAddr != "0.0.0.0" {
		t.Errorf("WebListenAddr = %q, want '0.0.0.0'", result.WebListenAddr)
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
		"Web UI Access",
		"Web UI Token",
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

func TestSettingsScreen_RendersAntennaProtocol(t *testing.T) {
	tests := []struct {
		name         string
		protocol     config.AntennaProtocol
		wantProtocol string
	}{
		{name: "default protocol displays as generic", protocol: "", wantProtocol: "generic"},
		{name: "explicit protocol displays unchanged", protocol: config.ProtocolZebra, wantProtocol: "zebra"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.GatewayConfig{
				GatewayID: "test",
				Antennas: []config.AntennaConfig{
					{ID: "dock", IP: "192.168.1.10", Port: 8080, Enabled: true, Zone: "entrada", Protocol: tt.protocol},
				},
			}

			m := NewSettingsScreen(cfg)
			view := m.View()

			assert.Contains(t, view, "Antenna dock Protocol")
			assert.Contains(t, view, tt.wantProtocol)
		})
	}
}

func TestSettingsScreen_EditsAntennaProtocolPerAntenna(t *testing.T) {
	cfg := protocolSettingsConfig(config.ProtocolGeneric)
	cfg.GatewayID = "gateway-original"
	cfg.CompanyID = "company-original"
	cfg.JWTSecret = "secret"
	cfg.ListenMode = "auto"
	cfg.WebAccessMode = "local"
	cfg.WebListenAddr = "127.0.0.1"
	cfg.WebAuthToken = "keep-token"
	cfg.SyncInterval = 30
	cfg.BatchSize = 100
	cfg.MaxRetries = 5
	cfg.HealthPort = 8080
	cfg.DataPath = "/keep/data"
	cfg.MaxPendingConfirmations = intPtr(222)
	cfg.PendingWarningThreshold = intPtr(111)
	cfg.Antennas = []config.AntennaConfig{
		{ID: "dock", IP: "192.168.1.10", Port: 8080, Enabled: true, Zone: "entrada", Protocol: config.ProtocolGeneric},
		{ID: "exit", IP: "192.168.1.11", Port: 8081, Enabled: false, Zone: "salida", Protocol: config.ProtocolGeneric},
	}
	m := NewSettingsScreen(cfg)

	protocolField := requireFieldIndex(t, m, "antenna_protocol:dock")
	m.cursor = protocolField

	m = updateSettingsModel(t, m, tea.KeyMsg{Type: tea.KeyRight})

	require.True(t, m.HasChanges())
	result := m.GetConfig()

	assert.Equal(t, config.ProtocolZebra, result.Antennas[0].Protocol)
	assert.Equal(t, config.ProtocolGeneric, result.Antennas[1].Protocol)
	assert.Equal(t, "gateway-original", result.GatewayID)
	assert.Equal(t, "192.168.1.10", result.Antennas[0].IP)
	assert.Equal(t, 8080, result.Antennas[0].Port)
	assert.True(t, result.Antennas[0].Enabled)
	assert.Equal(t, "entrada", result.Antennas[0].Zone)
	assert.Equal(t, "keep-token", result.WebAuthToken)
	assert.Equal(t, "/keep/data", result.DataPath)
}

func TestSettingsScreen_ProtocolSelectorCyclesSupportedValues(t *testing.T) {
	tests := []struct {
		name      string
		start     config.AntennaProtocol
		key       tea.KeyMsg
		wantValue string
	}{
		{name: "space cycles generic forward to zebra", start: config.ProtocolGeneric, key: tea.KeyMsg{Type: tea.KeySpace}, wantValue: "zebra"},
		{name: "right cycles generic forward to zebra", start: config.ProtocolGeneric, key: tea.KeyMsg{Type: tea.KeyRight}, wantValue: "zebra"},
		{name: "left cycles generic backward to zebra", start: config.ProtocolGeneric, key: tea.KeyMsg{Type: tea.KeyLeft}, wantValue: "zebra"},
		{name: "right wraps zebra forward to generic", start: config.ProtocolZebra, key: tea.KeyMsg{Type: tea.KeyRight}, wantValue: "generic"},
		{name: "left wraps zebra backward to generic", start: config.ProtocolZebra, key: tea.KeyMsg{Type: tea.KeyLeft}, wantValue: "generic"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newProtocolSettingsModel(tt.start)
			protocolField := requireFieldIndex(t, m, "antenna_protocol:dock")
			m.cursor = protocolField

			m = updateSettingsModel(t, m, tt.key)

			require.False(t, m.editing)
			assert.True(t, m.HasChanges())
			assert.Equal(t, tt.wantValue, m.values[protocolField])
			assert.Equal(t, config.AntennaProtocol(tt.wantValue), m.GetConfig().Antennas[0].Protocol)
		})
	}
}

func TestSettingsScreen_ProtocolSelectorPreventsFreeTextInput(t *testing.T) {
	m := newProtocolSettingsModel(config.ProtocolGeneric)
	protocolField := requireFieldIndex(t, m, "antenna_protocol:dock")
	m.cursor = protocolField

	m = updateSettingsModel(t, m, tea.KeyMsg{Type: tea.KeyRight})
	require.False(t, m.editing)
	require.Equal(t, "zebra", m.values[protocolField])

	for _, r := range "alien" {
		m = updateSettingsModel(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}

	assert.Equal(t, "zebra", m.values[protocolField])
	assert.NotEqual(t, config.AntennaProtocol("alien"), m.GetConfig().Antennas[0].Protocol)
}

func TestSettingsScreen_ProtocolSelectorDefaultsLegacyProtocolToGeneric(t *testing.T) {
	m := newProtocolSettingsModel("")
	protocolField := requireFieldIndex(t, m, "antenna_protocol:dock")

	assert.Equal(t, "generic", m.values[protocolField])
}

func TestSettingsScreen_ProtocolSelectorHelpMatchesControls(t *testing.T) {
	m := newProtocolSettingsModel(config.ProtocolGeneric)
	m.cursor = requireFieldIndex(t, m, "antenna_protocol:dock")

	view := m.View()

	assert.Contains(t, view, "↑/k up")
	assert.Contains(t, view, "↓/j down")
	assert.Contains(t, view, "a add antenna")
	assert.Contains(t, view, "e/enter edit")
}

func TestSettingsScreen_RejectsUnsupportedAntennaProtocol(t *testing.T) {
	cfg := &config.GatewayConfig{
		GatewayID: "gateway-original",
		Antennas: []config.AntennaConfig{
			{ID: "dock", IP: "192.168.1.10", Port: 8080, Enabled: true, Protocol: config.ProtocolGeneric},
		},
	}
	m := NewSettingsScreen(cfg)
	m.cursor = requireFieldIndex(t, m, "antenna_protocol:dock")

	m = updateSettingsModel(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m.editBuffer = "alien"

	err := m.validateCurrentField()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "generic, zebra")
}

func updateSettingsModel(t *testing.T, m SettingsScreenModel, key tea.KeyMsg) SettingsScreenModel {
	t.Helper()
	newModel, _ := m.Update(key)
	updated, ok := newModel.(SettingsScreenModel)
	require.Truef(t, ok, "expected SettingsScreenModel, got %T", newModel)
	return updated
}

func requireFieldIndex(t *testing.T, m SettingsScreenModel, key string) int {
	t.Helper()
	for i, field := range m.fields {
		if field.key == key {
			return i
		}
	}
	t.Fatalf("field %q not found", key)
	return -1
}

func newProtocolSettingsModel(protocol config.AntennaProtocol) SettingsScreenModel {
	return NewSettingsScreen(protocolSettingsConfig(protocol))
}

func protocolSettingsConfig(protocol config.AntennaProtocol) *config.GatewayConfig {
	return &config.GatewayConfig{
		GatewayID: "test",
		CloudURL:  "wss://test.com",
		LogLevel:  "info",
		Antennas: []config.AntennaConfig{
			{ID: "dock", IP: "192.168.1.10", Port: 8080, Enabled: true, Protocol: protocol},
		},
	}
}

// Helper function
func intPtr(i int) *int {
	return &i
}
