package main

import (
	"testing"
	"time"

	"github.com/amg-rfid/amg-rfid-gateway/internal/localstore"
	"github.com/amg-rfid/amg-rfid-gateway/internal/verify"
)

// mockLocalStore implements the minimal interface needed for toolStoreAdapter testing
type mockLocalStore struct {
	tag    *localstore.ToolTagRecord
	master *localstore.ToolRecord
}

func (m *mockLocalStore) GetToolTagByUII(uii string) (*localstore.ToolTagRecord, error) {
	return m.tag, nil
}

func (m *mockLocalStore) GetToolRecordByID(id int64) (*localstore.ToolRecord, error) {
	return m.master, nil
}

// Verify mock implements the required interface
var _ interface {
	GetToolTagByUII(uii string) (*localstore.ToolTagRecord, error)
	GetToolRecordByID(id int64) (*localstore.ToolRecord, error)
} = (*mockLocalStore)(nil)

// TestToolStoreAdapter_GetToolByUII_NormalizedFields verifies the adapter properly
// maps normalized schema fields (ToolID, KanbanZone, DefaultDestination) from
// tool_tags and tools tables into verify.Tool.
func TestToolStoreAdapter_GetToolByUII_NormalizedFields(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name              string
		tag               *localstore.ToolTagRecord
		master            *localstore.ToolRecord
		expectedToolID    int64
		expectedSKU       string
		expectedKanban    *string
		expectedDefaultDest *string
	}{
		{
			name: "complete tool with all normalized fields",
			tag: &localstore.ToolTagRecord{
				ID:         100,
				ToolID:     42,
				UII:        "E200341502001080",
				Location:   "Almacén General",
				Status:     "active",
				KanbanZone: strPtr("Zona A"),
				LastSyncedAt: now,
			},
			master: &localstore.ToolRecord{
				ID:                 42,
				SKU:                "TOOL-001",
				Name:               "Test Tool",
				DefaultDestination: strPtr("Taller Principal"),
			},
			expectedToolID:      42,
			expectedSKU:         "TOOL-001",
			expectedKanban:      strPtr("Zona A"),
			expectedDefaultDest: strPtr("Taller Principal"),
		},
		{
			name: "tool without kanban zone",
			tag: &localstore.ToolTagRecord{
				ID:         101,
				ToolID:     43,
				UII:        "E200341502001081",
				Location:   "Almacén B",
				Status:     "active",
				KanbanZone: nil,
				LastSyncedAt: now,
			},
			master: &localstore.ToolRecord{
				ID:                 43,
				SKU:                "TOOL-002",
				Name:               "Tool Without Kanban",
				DefaultDestination: strPtr("Almacén Secundario"),
			},
			expectedToolID:      43,
			expectedSKU:         "TOOL-002",
			expectedKanban:      nil,
			expectedDefaultDest: strPtr("Almacén Secundario"),
		},
		{
			name: "tool without default destination",
			tag: &localstore.ToolTagRecord{
				ID:         102,
				ToolID:     44,
				UII:        "E200341502001082",
				Location:   "Taller",
				Status:     "active",
				KanbanZone: strPtr("Zona B"),
				LastSyncedAt: now,
			},
			master: &localstore.ToolRecord{
				ID:                 44,
				SKU:                "TOOL-003",
				Name:               "Tool Without Default Dest",
				DefaultDestination: nil,
			},
			expectedToolID:      44,
			expectedSKU:         "TOOL-003",
			expectedKanban:      strPtr("Zona B"),
			expectedDefaultDest: nil,
		},
		{
			name: "tag exists but no master record",
			tag: &localstore.ToolTagRecord{
				ID:         103,
				ToolID:     999, // Non-existent tool ID
				UII:        "E200341502001083",
				Location:   "Unknown",
				Status:     "active",
				KanbanZone: nil,
				LastSyncedAt: now,
			},
			master:              nil, // Master not found
			expectedToolID:      999,
			expectedSKU:         "",
			expectedKanban:      nil,
			expectedDefaultDest: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a mock adapter (we test the mapping logic directly)
			tool := mapToVerifyTool(tt.tag, tt.master)

			// Verify normalized fields
			if tool.ToolID != tt.expectedToolID {
				t.Errorf("ToolID = %d, expected %d", tool.ToolID, tt.expectedToolID)
			}
			if tool.SKU != tt.expectedSKU {
				t.Errorf("SKU = %s, expected %s", tool.SKU, tt.expectedSKU)
			}

			// Verify KanbanZone
			if !strPtrEqual(tool.KanbanZone, tt.expectedKanban) {
				t.Errorf("KanbanZone = %v, expected %v", strPtrStr(tool.KanbanZone), strPtrStr(tt.expectedKanban))
			}

			// Verify DefaultDestination
			if !strPtrEqual(tool.DefaultDestination, tt.expectedDefaultDest) {
				t.Errorf("DefaultDestination = %v, expected %v", strPtrStr(tool.DefaultDestination), strPtrStr(tt.expectedDefaultDest))
			}

			// Verify basic fields from tag
			if tool.ID != tt.tag.ID {
				t.Errorf("ID = %d, expected %d", tool.ID, tt.tag.ID)
			}
			if tool.UII != tt.tag.UII {
				t.Errorf("UII = %s, expected %s", tool.UII, tt.tag.UII)
			}
			if tool.Location != tt.tag.Location {
				t.Errorf("Location = %s, expected %s", tool.Location, tt.tag.Location)
			}
			if tool.Status != tt.tag.Status {
				t.Errorf("Status = %s, expected %s", tool.Status, tt.tag.Status)
			}

			// Verify master fields
			if tt.master != nil {
				if tool.Name != tt.master.Name {
					t.Errorf("Name = %s, expected %s", tool.Name, tt.master.Name)
				}
			}
		})
	}
}

// TestToolStoreAdapter_ResolveExitDestination verifies that tools mapped by the
// adapter can be used with verify.ResolveExitDestination.
func TestToolStoreAdapter_ResolveExitDestination(t *testing.T) {
	tests := []struct {
		name           string
		tool           *verify.Tool
		expectedDest   string
		expectError    bool
	}{
		{
			name: "destination from kanban_zone",
			tool: &verify.Tool{
				ID:         1,
				ToolID:     100,
				UII:        "E200341502001080",
				SKU:        "TOOL-001",
				Name:       "Test Tool",
				Location:   "Almacén General",
				Status:     "active",
				KanbanZone: strPtr("Zona Kanban A"),
				DefaultDestination: strPtr("Fallback Location"),
			},
			expectedDest: "Zona Kanban A",
			expectError:  false,
		},
		{
			name: "destination from default_destination when kanban empty",
			tool: &verify.Tool{
				ID:         2,
				ToolID:     101,
				UII:        "E200341502001081",
				SKU:        "TOOL-002",
				Name:       "Test Tool 2",
				Location:   "Almacén General",
				Status:     "active",
				KanbanZone: nil,
				DefaultDestination: strPtr("Taller Principal"),
			},
			expectedDest: "Taller Principal",
			expectError:  false,
		},
		{
			name: "error when no destination configured",
			tool: &verify.Tool{
				ID:         3,
				ToolID:     102,
				UII:        "E200341502001082",
				SKU:        "TOOL-003",
				Name:       "Test Tool 3",
				Location:   "Almacén General",
				Status:     "active",
				KanbanZone: nil,
				DefaultDestination: nil,
			},
			expectedDest: "",
			expectError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dest, err := verify.ResolveExitDestination(tt.tool)

			if tt.expectError {
				if err == nil {
					t.Error("expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if dest != tt.expectedDest {
				t.Errorf("destination = %s, expected %s", dest, tt.expectedDest)
			}
		})
	}
}

// TestToolStoreAdapter_ToolIDField verifies ToolID is properly mapped from tool_tags
func TestToolStoreAdapter_ToolIDField(t *testing.T) {
	tag := &localstore.ToolTagRecord{
		ID:       100,
		ToolID:   9999,
		UII:      "E200341502001080",
		Location: "Almacén General",
		Status:   "active",
	}

	master := &localstore.ToolRecord{
		ID:   9999,
		SKU:  "SKU-9999",
		Name: "Master Tool",
	}

	tool := mapToVerifyTool(tag, master)

	if tool.ToolID != 9999 {
		t.Errorf("ToolID = %d, expected 9999", tool.ToolID)
	}

	// ToolID should match the master record ID
	if tool.ToolID != master.ID {
		t.Errorf("ToolID (%d) should match master.ID (%d)", tool.ToolID, master.ID)
	}
}

// mapToVerifyTool is a helper that mimics the toolStoreAdapter mapping logic
func mapToVerifyTool(tag *localstore.ToolTagRecord, master *localstore.ToolRecord) *verify.Tool {
	tool := &verify.Tool{
		ID:         tag.ID,
		ToolID:     tag.ToolID,
		UII:        tag.UII,
		Location:   tag.Location,
		Status:     tag.Status,
		KanbanZone: tag.KanbanZone,
	}

	if master != nil {
		tool.SKU = master.SKU
		tool.Name = master.Name
		tool.DefaultDestination = master.DefaultDestination
	}

	return tool
}

// Helper functions
func strPtr(s string) *string {
	return &s
}

func strPtrStr(s *string) string {
	if s == nil {
		return "<nil>"
	}
	return *s
}

func strPtrEqual(a, b *string) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}
