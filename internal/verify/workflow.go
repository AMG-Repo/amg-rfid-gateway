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

// Tool represents a tool from local storage.
type Tool struct {
	ID       int64  `json:"id"`
	SKU      string `json:"sku"`
	Name     string `json:"name"`
	UII      string `json:"uii"`
	Location string `json:"location"`
	Status   string `json:"status"`
}

// ToolStore defines the interface for tool lookups.
type ToolStore interface {
	GetToolByUII(uii string) (*Tool, error)
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
// Logic:
// 1. Check antenna zone mapping first
// 2. If no zone, check tool location in localstore
//   - If location contains "Almacen" → suggest "salida"
//   - Else → suggest "entrada"
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

	// 2. Fall back to location-based logic
	tool, err := v.localStore.GetToolByUII(uii)
	if err != nil || tool == nil {
		// Unknown tool - default to entrada (single antenna, unknown tool)
		return SuggestedAction{
			Action: ActionEntrada,
			Reason: "unknown:tool_not_found",
		}
	}

	// Tool found - suggest based on current location
	locationNormalized := normalizeLocation(tool.Location)
	if strings.Contains(locationNormalized, "almacen") {
		// Tool is at warehouse - suggest salida (leaving)
		return SuggestedAction{
			Action:   ActionSalida,
			Reason:   "location:warehouse",
			ToolInfo: tool,
		}
	}

	// Tool is outside or unknown location - suggest entrada (arriving)
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
