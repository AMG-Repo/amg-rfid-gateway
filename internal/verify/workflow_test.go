package verify

import (
	"errors"
	"testing"

	"github.com/amg-rfid/amg-rfid-gateway/internal/config"
)

// mockToolStore is a mock implementation of ToolStore for testing.
type mockToolStore struct {
	tools map[string]*Tool
}

func (m *mockToolStore) GetToolByUII(uii string) (*Tool, error) {
	tool, ok := m.tools[uii]
	if !ok {
		return nil, nil
	}
	return tool, nil
}

func newMockToolStore() *mockToolStore {
	return &mockToolStore{
		tools: make(map[string]*Tool),
	}
}

func (m *mockToolStore) AddTool(tool *Tool) {
	m.tools[tool.UII] = tool
}

func TestNewVerifier(t *testing.T) {
	store := newMockToolStore()
	antennas := []config.AntennaConfig{
		{ID: "ant-1", Zone: "entrada"},
	}

	verifier := NewVerifier(store, antennas)

	if verifier == nil {
		t.Fatal("expected verifier to be non-nil")
	}
	if verifier.localStore != store {
		t.Error("expected localStore to match")
	}
	if len(verifier.antennas) != 1 {
		t.Errorf("expected 1 antenna, got %d", len(verifier.antennas))
	}
}

func TestSuggestAction_WithZoneMapping(t *testing.T) {
	store := newMockToolStore()
	antennas := []config.AntennaConfig{
		{ID: "ant-entrada", Zone: "entrada"},
		{ID: "ant-salida", Zone: "salida"},
		{ID: "ant-nozone", Zone: ""},
	}

	verifier := NewVerifier(store, antennas)

	tests := []struct {
		name       string
		antennaID  string
		wantAction Action
		wantReason string
	}{
		{
			name:       "antenna with entrada zone",
			antennaID:  "ant-entrada",
			wantAction: ActionEntrada,
			wantReason: "zone:entrada",
		},
		{
			name:       "antenna with salida zone",
			antennaID:  "ant-salida",
			wantAction: ActionSalida,
			wantReason: "zone:salida",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := verifier.SuggestAction("EPC-123", tt.antennaID)

			if result.Action != tt.wantAction {
				t.Errorf("SuggestAction() Action = %v, want %v", result.Action, tt.wantAction)
			}
			if result.Reason != tt.wantReason {
				t.Errorf("SuggestAction() Reason = %v, want %v", result.Reason, tt.wantReason)
			}
		})
	}
}

func TestSuggestAction_WithoutZone_UnknownTool(t *testing.T) {
	store := newMockToolStore()
	antennas := []config.AntennaConfig{
		{ID: "ant-1", Zone: ""},
	}

	verifier := NewVerifier(store, antennas)

	result := verifier.SuggestAction("UNKNOWN-EPC", "ant-1")

	if result.Action != ActionEntrada {
		t.Errorf("expected ActionEntrada for unknown tool, got %v", result.Action)
	}
	if result.Reason != "unknown:tool_not_found" {
		t.Errorf("expected reason 'unknown:tool_not_found', got %v", result.Reason)
	}
}

func TestSuggestAction_WithoutZone_ToolInAlmacen(t *testing.T) {
	store := newMockToolStore()
	store.AddTool(&Tool{
		ID:       1,
		SKU:      "TOOL-001",
		Name:     "Test Tool",
		UII:      "EPC-123",
		Location: "Almacén A",
		Status:   "active",
	})

	antennas := []config.AntennaConfig{
		{ID: "ant-1", Zone: ""},
	}

	verifier := NewVerifier(store, antennas)

	result := verifier.SuggestAction("EPC-123", "ant-1")

	if result.Action != ActionSalida {
		t.Errorf("expected ActionSalida for tool in Almacén, got %v", result.Action)
	}
	if result.Reason != "location:warehouse" {
		t.Errorf("expected reason 'location:warehouse', got %v", result.Reason)
	}
	if result.ToolInfo == nil {
		t.Error("expected ToolInfo to be set")
	}
	if result.ToolInfo.SKU != "TOOL-001" {
		t.Errorf("expected SKU 'TOOL-001', got %v", result.ToolInfo.SKU)
	}
}

func TestSuggestAction_WithoutZone_ToolOutside(t *testing.T) {
	store := newMockToolStore()
	store.AddTool(&Tool{
		ID:       1,
		SKU:      "TOOL-002",
		Name:     "Test Tool Outside",
		UII:      "EPC-456",
		Location: "Obra Central",
		Status:   "active",
	})

	antennas := []config.AntennaConfig{
		{ID: "ant-1", Zone: ""},
	}

	verifier := NewVerifier(store, antennas)

	result := verifier.SuggestAction("EPC-456", "ant-1")

	if result.Action != ActionEntrada {
		t.Errorf("expected ActionEntrada for tool outside, got %v", result.Action)
	}
	if result.Reason != "location:outside" {
		t.Errorf("expected reason 'location:outside', got %v", result.Reason)
	}
}

func TestSuggestAction_WithoutZone_EmptyLocation(t *testing.T) {
	store := newMockToolStore()
	store.AddTool(&Tool{
		ID:       1,
		SKU:      "TOOL-003",
		Name:     "Test Tool No Location",
		UII:      "EPC-789",
		Location: "",
		Status:   "active",
	})

	antennas := []config.AntennaConfig{
		{ID: "ant-1", Zone: ""},
	}

	verifier := NewVerifier(store, antennas)

	result := verifier.SuggestAction("EPC-789", "ant-1")

	if result.Action != ActionEntrada {
		t.Errorf("expected ActionEntrada for tool with empty location, got %v", result.Action)
	}
}

func TestSuggestAction_LowercaseAlmacen(t *testing.T) {
	store := newMockToolStore()
	store.AddTool(&Tool{
		ID:       1,
		SKU:      "TOOL-004",
		Name:     "Test Tool",
		UII:      "EPC-ABC",
		Location: "almacen central", // lowercase
		Status:   "active",
	})

	antennas := []config.AntennaConfig{
		{ID: "ant-1", Zone: ""},
	}

	verifier := NewVerifier(store, antennas)

	result := verifier.SuggestAction("EPC-ABC", "ant-1")

	if result.Action != ActionSalida {
		t.Errorf("expected ActionSalida for lowercase 'almacen', got %v", result.Action)
	}
}

func TestGetAntennaZone(t *testing.T) {
	antennas := []config.AntennaConfig{
		{ID: "ant-entrada", Zone: "entrada"},
		{ID: "ant-salida", Zone: "salida"},
		{ID: "ant-empty", Zone: ""},
		{ID: "ant-mixed", Zone: "Entrada"}, // mixed case
	}

	verifier := NewVerifier(nil, antennas)

	tests := []struct {
		antennaID string
		wantZone  string
	}{
		{"ant-entrada", "entrada"},
		{"ant-salida", "salida"},
		{"ant-empty", ""},
		{"ant-mixed", "entrada"}, // should normalize to lowercase
		{"ant-nonexistent", ""},
	}

	for _, tt := range tests {
		t.Run(tt.antennaID, func(t *testing.T) {
			got := verifier.GetAntennaZone(tt.antennaID)
			if got != tt.wantZone {
				t.Errorf("GetAntennaZone(%q) = %q, want %q", tt.antennaID, got, tt.wantZone)
			}
		})
	}
}

func TestValidateAction(t *testing.T) {
	verifier := NewVerifier(nil, nil)

	tests := []struct {
		name    string
		action  string
		wantErr bool
	}{
		{"valid entrada", "entrada", false},
		{"valid salida", "salida", false},
		{"valid ENTRADA uppercase", "ENTRADA", false},
		{"valid SALIDA uppercase", "SALIDA", false},
		{"valid Entrada mixed", "Entrada", false},
		{"invalid empty", "", true},
		{"invalid ambos", "ambos", true},
		{"invalid random", "random", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := verifier.ValidateAction(tt.action)
			if tt.wantErr && err == nil {
				t.Errorf("ValidateAction(%q) expected error, got nil", tt.action)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("ValidateAction(%q) unexpected error: %v", tt.action, err)
			}
		})
	}
}

// errorStore simulates a store error
type errorStore struct{}

func (e *errorStore) GetToolByUII(uii string) (*Tool, error) {
	return nil, errors.New("database error")
}

func TestSuggestAction_StoreError(t *testing.T) {
	store := &errorStore{}
	antennas := []config.AntennaConfig{
		{ID: "ant-1", Zone: ""},
	}

	verifier := NewVerifier(store, antennas)

	result := verifier.SuggestAction("EPC-123", "ant-1")

	// Should default to entrada when store errors
	if result.Action != ActionEntrada {
		t.Errorf("expected ActionEntrada on store error, got %v", result.Action)
	}
	if result.Reason != "unknown:tool_not_found" {
		t.Errorf("expected reason 'unknown:tool_not_found' on error, got %v", result.Reason)
	}
}

// Test SuggestAction with unknown UII (tool not in DB)
func TestSuggestAction_UnknownUII(t *testing.T) {
	store := newMockToolStore()
	antennas := []config.AntennaConfig{
		{ID: "ant-1", Zone: ""},
	}

	verifier := NewVerifier(store, antennas)

	result := verifier.SuggestAction("UNKNOWN-EPC-123", "ant-1")

	if result.Action != ActionEntrada {
		t.Errorf("expected ActionEntrada for unknown tool, got %v", result.Action)
	}
	if result.Reason != "unknown:tool_not_found" {
		t.Errorf("expected reason 'unknown:tool_not_found', got %v", result.Reason)
	}
	if result.ToolInfo != nil {
		t.Error("expected ToolInfo to be nil for unknown tool")
	}
}

// Test SuggestAction with antenna zone override (zone takes precedence over location)
func TestSuggestAction_ZoneOverridesLocation(t *testing.T) {
	store := newMockToolStore()
	// Tool is in warehouse (would suggest salida based on location)
	store.AddTool(&Tool{
		ID:       1,
		SKU:      "TOOL-001",
		Name:     "Test Tool",
		UII:      "EPC-123",
		Location: "Almacén Central",
		Status:   "active",
	})

	// But antenna has entrada zone
	antennas := []config.AntennaConfig{
		{ID: "ant-1", Zone: "entrada"},
	}

	verifier := NewVerifier(store, antennas)

	result := verifier.SuggestAction("EPC-123", "ant-1")

	// Zone should override location-based logic
	if result.Action != ActionEntrada {
		t.Errorf("expected ActionEntrada when zone is 'entrada', got %v", result.Action)
	}
	if result.Reason != "zone:entrada" {
		t.Errorf("expected reason 'zone:entrada', got %v", result.Reason)
	}
}

// Test SuggestAction with antenna zone salida (takes precedence)
func TestSuggestAction_ZoneSalidaOverridesOutside(t *testing.T) {
	store := newMockToolStore()
	// Tool is outside (would suggest entrada based on location)
	store.AddTool(&Tool{
		ID:       1,
		SKU:      "TOOL-001",
		Name:     "Test Tool",
		UII:      "EPC-123",
		Location: "Obra Central",
		Status:   "active",
	})

	// But antenna has salida zone
	antennas := []config.AntennaConfig{
		{ID: "ant-1", Zone: "salida"},
	}

	verifier := NewVerifier(store, antennas)

	result := verifier.SuggestAction("EPC-123", "ant-1")

	// Zone should override location-based logic
	if result.Action != ActionSalida {
		t.Errorf("expected ActionSalida when zone is 'salida', got %v", result.Action)
	}
	if result.Reason != "zone:salida" {
		t.Errorf("expected reason 'zone:salida', got %v", result.Reason)
	}
}

// Test SuggestAction with unknown antenna (falls back to location-based since zone is empty)
func TestSuggestAction_UnknownAntenna(t *testing.T) {
	store := newMockToolStore()
	store.AddTool(&Tool{
		ID:       1,
		SKU:      "TOOL-001",
		Name:     "Test Tool",
		UII:      "EPC-123",
		Location: "Almacén A",
		Status:   "active",
	})

	antennas := []config.AntennaConfig{
		{ID: "ant-1", Zone: ""},
	}

	verifier := NewVerifier(store, antennas)

	// Use unknown antenna ID - zone lookup returns empty, falls back to location
	result := verifier.SuggestAction("EPC-123", "ant-unknown")

	// Since zone is empty and tool is in Almacén, should suggest salida (leaving warehouse)
	if result.Action != ActionSalida {
		t.Errorf("expected ActionSalida when unknown antenna has no zone and tool in Almacén, got %v", result.Action)
	}
	if result.Reason != "location:warehouse" {
		t.Errorf("expected reason 'location:warehouse', got %v", result.Reason)
	}
}

// Test SuggestAction with tool having special characters in location
func TestSuggestAction_SpecialCharactersInLocation(t *testing.T) {
	store := newMockToolStore()
	store.AddTool(&Tool{
		ID:       1,
		SKU:      "TOOL-001",
		Name:     "Test Tool",
		UII:      "EPC-123",
		Location: "Almacén_Norte-123",
		Status:   "active",
	})

	antennas := []config.AntennaConfig{
		{ID: "ant-1", Zone: ""},
	}

	verifier := NewVerifier(store, antennas)

	result := verifier.SuggestAction("EPC-123", "ant-1")

	// Location contains "almacen" (normalized), should suggest salida
	if result.Action != ActionSalida {
		t.Errorf("expected ActionSalida for tool in Almacén, got %v", result.Action)
	}
	if result.Reason != "location:warehouse" {
		t.Errorf("expected reason 'location:warehouse', got %v", result.Reason)
	}
}

// Test SuggestAction with inactive tool
func TestSuggestAction_InactiveTool(t *testing.T) {
	store := newMockToolStore()
	store.AddTool(&Tool{
		ID:       1,
		SKU:      "TOOL-001",
		Name:     "Test Tool",
		UII:      "EPC-123",
		Location: "Almacén A",
		Status:   "inactive", // Inactive status
	})

	antennas := []config.AntennaConfig{
		{ID: "ant-1", Zone: ""},
	}

	verifier := NewVerifier(store, antennas)

	// Should still suggest based on location regardless of status
	result := verifier.SuggestAction("EPC-123", "ant-1")

	if result.Action != ActionSalida {
		t.Errorf("expected ActionSalida for inactive tool in Almacén, got %v", result.Action)
	}
	if result.ToolInfo == nil {
		t.Error("expected ToolInfo to be populated")
	}
	if result.ToolInfo.Status != "inactive" {
		t.Errorf("expected Status 'inactive', got %s", result.ToolInfo.Status)
	}
}

// Test ValidateAction edge cases
func TestValidateAction_EdgeCases(t *testing.T) {
	verifier := NewVerifier(nil, nil)

	tests := []struct {
		name    string
		action  string
		wantErr bool
	}{
		{"empty string", "", true},
		{"whitespace only", "   ", true},
		{"mixed case entrada", "EnTrAdA", false},
		{"mixed case salida", "SaLiDa", false},
		{"uppercase ENTRADA", "ENTRADA", false},
		{"uppercase SALIDA", "SALIDA", false},
		{"similar word entrada-s", "entradas", true},
		{"similar word salida-s", "salidas", true},
		{"entrada with spaces", " entrada ", true}, // Should fail due to spaces
		{"salida with newline", "salida\n", true},  // Should fail due to newline
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := verifier.ValidateAction(tt.action)
			if tt.wantErr && err == nil {
				t.Errorf("ValidateAction(%q) expected error, got nil", tt.action)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("ValidateAction(%q) unexpected error: %v", tt.action, err)
			}
		})
	}
}

// Test GetAntennaZone with case variations
func TestGetAntennaZone_CaseVariations(t *testing.T) {
	antennas := []config.AntennaConfig{
		{ID: "ant-1", Zone: "ENTRADA"},
		{ID: "ant-2", Zone: "Salida"},
		{ID: "ant-3", Zone: "EnTrAdA"},
	}

	verifier := NewVerifier(nil, antennas)

	tests := []struct {
		antennaID string
		expected  string
	}{
		{"ant-1", "entrada"}, // Uppercase
		{"ant-2", "salida"},  // Mixed case
		{"ant-3", "entrada"}, // Mixed case
	}

	for _, tt := range tests {
		t.Run(tt.antennaID, func(t *testing.T) {
			result := verifier.GetAntennaZone(tt.antennaID)
			if result != tt.expected {
				t.Errorf("GetAntennaZone(%q) = %q, want %q", tt.antennaID, result, tt.expected)
			}
		})
	}
}

// Test GetAntennaZone with invalid zones
func TestGetAntennaZone_InvalidZones(t *testing.T) {
	antennas := []config.AntennaConfig{
		{ID: "ant-1", Zone: "invalid"},
		{ID: "ant-2", Zone: "ambos"},
		{ID: "ant-3", Zone: "none"},
		{ID: "ant-4", Zone: ""},
	}

	verifier := NewVerifier(nil, antennas)

	for _, ant := range antennas {
		t.Run(ant.ID, func(t *testing.T) {
			result := verifier.GetAntennaZone(ant.ID)
			if result != "" {
				t.Errorf("GetAntennaZone(%q) = %q, want empty string for invalid zone", ant.ID, result)
			}
		})
	}
}

// Test SuggestAction with multiple antennas, different zones
func TestSuggestAction_MultipleAntennas(t *testing.T) {
	store := newMockToolStore()
	store.AddTool(&Tool{
		ID:       1,
		SKU:      "TOOL-001",
		Name:     "Test Tool",
		UII:      "EPC-123",
		Location: "Almacén A",
		Status:   "active",
	})

	antennas := []config.AntennaConfig{
		{ID: "ant-entrada", Zone: "entrada"},
		{ID: "ant-salida", Zone: "salida"},
		{ID: "ant-neutral", Zone: ""},
	}

	verifier := NewVerifier(store, antennas)

	// Test with entrada antenna
	result := verifier.SuggestAction("EPC-123", "ant-entrada")
	if result.Action != ActionEntrada {
		t.Errorf("expected ActionEntrada for entrada antenna, got %v", result.Action)
	}

	// Test with salida antenna
	result = verifier.SuggestAction("EPC-123", "ant-salida")
	if result.Action != ActionSalida {
		t.Errorf("expected ActionSalida for salida antenna, got %v", result.Action)
	}

	// Test with neutral antenna (falls back to location)
	result = verifier.SuggestAction("EPC-123", "ant-neutral")
	if result.Action != ActionSalida {
		t.Errorf("expected ActionSalida for neutral antenna (tool in Almacén), got %v", result.Action)
	}
}

// Test Verifier with empty antennas
func TestVerifier_EmptyAntennas(t *testing.T) {
	store := newMockToolStore()
	store.AddTool(&Tool{
		ID:       1,
		SKU:      "TOOL-001",
		Name:     "Test Tool",
		UII:      "EPC-123",
		Location: "Almacén A",
		Status:   "active",
	})

	verifier := NewVerifier(store, []config.AntennaConfig{})

	// With no antennas configured, falls back to location-based
	result := verifier.SuggestAction("EPC-123", "ant-1")

	if result.Action != ActionSalida {
		t.Errorf("expected ActionSalida with empty antennas, got %v", result.Action)
	}
	if result.Reason != "location:warehouse" {
		t.Errorf("expected reason 'location:warehouse', got %v", result.Reason)
	}
}
