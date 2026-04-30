package screens

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestMainScreen_Navigation(t *testing.T) {
	tests := []struct {
		name       string
		startPos   int
		key        string
		expectPos  int
		numOptions int
	}{
		{"down from 0", 0, "down", 1, 5},
		{"down with j", 0, "j", 1, 5},
		{"up from 1", 1, "up", 0, 5},
		{"up with k", 1, "k", 0, 5},
		{"down at bottom", 4, "down", 4, 5}, // stays at bottom
		{"up at top", 0, "up", 0, 5},        // stays at top
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewMainScreen()
			m.cursor = tt.startPos

			newModel, _ := m.Update(tea.KeyMsg{
				Type:  tea.KeyRunes,
				Runes: []rune(tt.key),
			})
			m = newModel.(MainScreenModel)

			if m.cursor != tt.expectPos {
				t.Errorf("cursor = %d, want %d", m.cursor, tt.expectPos)
			}
		})
	}
}

func TestMainScreen_Selection(t *testing.T) {
	tests := []struct {
		name         string
		cursor       int
		expectScreen string
	}{
		{"select antennas", 0, "antennas"},
		{"select network", 1, "network"},
		{"select status", 2, "status"},
		{"select settings", 3, "settings"},
		{"select quit", 4, "quit"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewMainScreen()
			m.cursor = tt.cursor

			newModel, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
			m = newModel.(MainScreenModel)

			screen := m.GetSelectedScreen()
			if screen != tt.expectScreen {
				t.Errorf("selected screen = %q, want %q", screen, tt.expectScreen)
			}
		})
	}
}

func TestMainScreen_QuitCommand(t *testing.T) {
	m := NewMainScreen()
	m.cursor = 4 // Quit option

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// The command should be tea.Quit
	if cmd == nil {
		t.Error("expected quit command, got nil")
	}
}

func TestMainScreen_SetSize(t *testing.T) {
	m := NewMainScreen()
	m.SetSize(100, 50)

	if m.width != 100 {
		t.Errorf("width = %d, want 100", m.width)
	}
	if m.height != 50 {
		t.Errorf("height = %d, want 50", m.height)
	}
}

func TestMainScreen_View(t *testing.T) {
	m := NewMainScreen()
	view := m.View()

	// Check that view contains expected elements
	if view == "" {
		t.Error("view should not be empty")
	}

	// Should contain menu items
	expectedItems := []string{"Antennas", "Network", "System", "Settings", "Quit"}
	for _, item := range expectedItems {
		if !contains(view, item) {
			t.Errorf("view should contain %q", item)
		}
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
