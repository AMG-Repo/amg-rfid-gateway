// Package verify provides verification workflow logic for entrada/salida decisions.
package verify

import (
	"errors"
	"strings"

	"github.com/amg-rfid/amg-rfid-gateway/internal/config"
)

// Action represents the verification action type.
type Action string

const (
	ActionEntrada Action = "entrada"
	ActionSalida  Action = "salida"
)

// SuggestedAction contains the recommended action and reasoning.
type SuggestedAction struct {
	Action   Action `json:"action"`
	Reason   string `json:"reason"`
	ToolInfo *Tool  `json:"tool_info,omitempty"`
}

// Tool represents a tool from local storage (normalized schema view).
// Combines data from tool_tags (per-tag state) and tools (SKU master).
type Tool struct {
	ID                 int64   `json:"id"`
	ToolID             int64   `json:"tool_id"`
	SKU                string  `json:"sku"`
	Name               string  `json:"name"`
	UII                string  `json:"uii"`
	Location           string  `json:"location"`
	Status             string  `json:"status"`
	KanbanZone         *string `json:"kanban_zone,omitempty"`
	DefaultDestination *string `json:"default_destination,omitempty"`
}

// ToolStore defines the interface for tool lookups.
type ToolStore interface {
	GetToolByUII(uii string) (*Tool, error)
}

// NormalizedStore defines the interface for normalized schema lookups.
// Supports the tools + tool_tags JOIN pattern for offline resolution.
type NormalizedStore interface {
	ToolStore
	GetToolTagByUII(uii string) (*ToolTagInfo, error)
	GetToolRecordByID(id int64) (*ToolMasterInfo, error)
}

// ToolTagInfo holds per-tag state from tool_tags table.
type ToolTagInfo struct {
	ID         int64   `json:"id"`
	ToolID     int64   `json:"tool_id"`
	UII        string  `json:"uii"`
	Location   string  `json:"location"`
	Status     string  `json:"status"`
	KanbanZone *string `json:"kanban_zone,omitempty"`
}

// ToolMasterInfo holds SKU-level data from tools table.
type ToolMasterInfo struct {
	ID                 int64   `json:"id"`
	SKU                string  `json:"sku"`
	Name               string  `json:"name"`
	DefaultDestination *string `json:"default_destination,omitempty"`
}

// Verifier handles verification workflow logic.
type Verifier struct {
	localStore ToolStore
	antennas   []config.AntennaConfig
}

// NewVerifier creates a new verifier.
func NewVerifier(store ToolStore, antennas []config.AntennaConfig) *Verifier {
	return &Verifier{
		localStore: store,
		antennas:   antennas,
	}
}

// SuggestAction determines the suggested action for a tag detection.
// Resolution order (per design.md):
// 1. Check antenna zone mapping first (explicit configuration)
// 2. If no zone, load local joined tag/tool snapshot
//   - If tag location is "Almacén General" → suggest "salida" (exit)
//   - If tag location is outside warehouse or empty → suggest "entrada" (entry)
// 3. If no local metadata → fallback to antenna mapping or default to "entrada" with warning
//
// Domain rules:
// - ENTRY action always goes to "Almacén General"
// - EXIT action resolves destination from tool record (kanban_zone first, then default_destination fallback)
func (v *Verifier) SuggestAction(uii string, antennaID string) SuggestedAction {
	// 1. Check antenna zone mapping first
	zone := v.GetAntennaZone(antennaID)
	if zone != "" {
		action := Action(zone)
		return SuggestedAction{
			Action: action,
			Reason: "zone:" + zone,
		}
	}

	// 2. Fall back to normalized local schema lookup
	tool, err := v.localStore.GetToolByUII(uii)
	if err != nil || tool == nil {
		// Unknown tool or database error - default to entrada (single antenna, unknown tool)
		// The gateway should remain functional even with local store issues
		return SuggestedAction{
			Action: ActionEntrada,
			Reason: "unknown:tool_not_found",
		}
	}

	// Tool found - suggest based on current tag location (normalized schema)
	locationNormalized := normalizeLocation(tool.Location)
	isInWarehouse := isWarehouseLocation(locationNormalized)

	if isInWarehouse {
		// Tool is at warehouse (Almacén General) - suggest salida (leaving)
		// EXIT destination will be resolved by VPS from kanban_zone or default_destination
		return SuggestedAction{
			Action:   ActionSalida,
			Reason:   "location:warehouse",
			ToolInfo: tool,
		}
	}

	// Tool is outside or unknown location - suggest entrada (arriving)
	// ENTRY destination is always "Almacén General" (resolved by VPS)
	return SuggestedAction{
		Action:   ActionEntrada,
		Reason:   "location:outside",
		ToolInfo: tool,
	}
}

// normalizeLocation normalizes location string for matching.
// Converts to lowercase and removes common accents.
func normalizeLocation(loc string) string {
	// Convert to lowercase
	loc = strings.ToLower(loc)
	// Replace common accented characters
	replacements := map[string]string{
		"á": "a",
		"é": "e",
		"í": "i",
		"ó": "o",
		"ú": "u",
		"ñ": "n",
	}
	for accented, plain := range replacements {
		loc = strings.ReplaceAll(loc, accented, plain)
	}
	return loc
}

// GetAntennaZone returns the zone for an antenna ("entrada", "salida", or empty).
func (v *Verifier) GetAntennaZone(antennaID string) string {
	for _, ant := range v.antennas {
		if ant.ID == antennaID {
			zone := strings.ToLower(ant.Zone)
			if zone == "entrada" || zone == "salida" {
				return zone
			}
		}
	}
	return ""
}

// ValidateAction validates the action string.
// Returns error if action is not "entrada" or "salida".
func (v *Verifier) ValidateAction(action string) error {
	actionLower := strings.ToLower(action)
	if actionLower != "entrada" && actionLower != "salida" {
		return errors.New("action must be one of: entrada, salida")
	}
	return nil
}

// isWarehouseLocation checks if a normalized location represents a warehouse location.
// Per spec: If tag location contains "almacen" (e.g., "Almacén General", "Almacén A"),
// the tool is considered to be in the warehouse and should exit (salida).
// If outside warehouse or empty, tool should enter (entrada).
func isWarehouseLocation(normalizedLoc string) bool {
	// Empty location is treated as outside (needs entrada)
	if normalizedLoc == "" {
		return false
	}
	// Match any location containing "almacen" (after normalization)
	// This covers: "almacen general", "almacen a", "almacen central", etc.
	return strings.Contains(normalizedLoc, "almacen")
}

// ResolveExitDestination determines the destination for an EXIT action based on tool data.
// Resolution order per design.md:
// 1. If kanban_zone is present on the tag → use kanban_zone
// 2. Else if tool has default_destination → use default_destination
// 3. Else return error (destination_not_configured)
//
// This is used for offline resolution and informational purposes.
// The VPS will perform the authoritative resolution on confirm.
func ResolveExitDestination(tool *Tool) (string, error) {
	if tool == nil {
		return "", errors.New("tool is nil")
	}

	// 1. Primary: kanban_zone from the tag
	if tool.KanbanZone != nil && *tool.KanbanZone != "" {
		return *tool.KanbanZone, nil
	}

	// 2. Fallback: default_destination from the tool master
	if tool.DefaultDestination != nil && *tool.DefaultDestination != "" {
		return *tool.DefaultDestination, nil
	}

	// 3. No destination configured
	return "", errors.New("destination_not_configured: no kanban_zone or default_destination available")
}

// IsEntryToWarehouse checks if the action is ENTRY (goes to Almacén General).
// Per spec: ENTRY action always sets location to "Almacén General".
func IsEntryToWarehouse(action Action) bool {
	return action == ActionEntrada
}

// IsExitFromWarehouse checks if the action is EXIT (resolves destination from tool).
// Per spec: EXIT action resolves destination from kanban_zone or tool default.
func IsExitFromWarehouse(action Action) bool {
	return action == ActionSalida
}
