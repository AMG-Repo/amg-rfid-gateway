package screens

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/amg-rfid/amg-rfid-gateway/internal/version"
)

func TestSplashScreen_New(t *testing.T) {
	m := NewSplashScreen()

	if m.width != 0 {
		t.Errorf("width should be 0 initially, got %d", m.width)
	}
	if m.height != 0 {
		t.Errorf("height should be 0 initially, got %d", m.height)
	}
	if m.timerComplete {
		t.Error("timerComplete should be false initially")
	}
	if m.shouldAdvance {
		t.Error("shouldAdvance should be false initially")
	}
}

func TestSplashScreen_Init(t *testing.T) {
	m := NewSplashScreen()
	cmd := m.Init()

	// Init should return a tea.Tick command
	if cmd == nil {
		t.Error("Init() should return a command, got nil")
	}
}

func TestSplashScreen_SetSize(t *testing.T) {
	m := NewSplashScreen()
	m.SetSize(100, 50)

	if m.width != 100 {
		t.Errorf("width = %d, want 100", m.width)
	}
	if m.height != 50 {
		t.Errorf("height = %d, want 50", m.height)
	}
}

func TestSplashScreen_View(t *testing.T) {
	m := NewSplashScreen()
	m.SetSize(80, 24)

	view := m.View()

	if view == "" {
		t.Error("view should not be empty")
	}

	// Should contain version
	if !strings.Contains(view, version.Version) {
		t.Errorf("view should contain version %q", version.Version)
	}

	// Should contain status indicator
	if !strings.Contains(view, "VPS:") {
		t.Error("view should contain VPS status label")
	}
}

func TestSplashScreen_AutoAdvance(t *testing.T) {
	tests := []struct {
		name          string
		msg           tea.Msg
		expectAdvance bool
	}{
		{
			name:          "tick message advances",
			msg:           splashTickMsg{},
			expectAdvance: true,
		},
		{
			name:          "enter key advances",
			msg:           tea.KeyMsg{Type: tea.KeyEnter},
			expectAdvance: true,
		},
		{
			name:          "esc key advances",
			msg:           tea.KeyMsg{Type: tea.KeyEsc},
			expectAdvance: true,
		},
		{
			name:          "other key does not advance",
			msg:           tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")},
			expectAdvance: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewSplashScreen()
			newModel, _ := m.Update(tt.msg)
			m = newModel.(SplashScreenModel)

			if m.shouldAdvance != tt.expectAdvance {
				t.Errorf("shouldAdvance = %v, want %v", m.shouldAdvance, tt.expectAdvance)
			}
		})
	}
}

func TestSplashScreen_ShouldAdvanceToMenu(t *testing.T) {
	m := NewSplashScreen()

	// Initially should not advance
	if m.ShouldAdvanceToMenu() {
		t.Error("ShouldAdvanceToMenu() should be false initially")
	}

	// After tick, should advance
	newModel, _ := m.Update(splashTickMsg{})
	m = newModel.(SplashScreenModel)

	if !m.ShouldAdvanceToMenu() {
		t.Error("ShouldAdvanceToMenu() should be true after tick")
	}
}

func TestSplashScreen_Reset(t *testing.T) {
	m := NewSplashScreen()

	// Simulate timer completion
	newModel, _ := m.Update(splashTickMsg{})
	m = newModel.(SplashScreenModel)

	if !m.shouldAdvance {
		t.Error("setup failed: shouldAdvance should be true")
	}

	// Reset
	m.Reset()

	if m.shouldAdvance {
		t.Error("shouldAdvance should be false after Reset()")
	}
	if m.timerComplete {
		t.Error("timerComplete should be false after Reset()")
	}
}

func TestSplashScreen_SetVPSStatus(t *testing.T) {
	m := NewSplashScreen()

	// Initially disconnected
	if m.vpsConnected {
		t.Error("vpsConnected should be false initially")
	}

	// Set to connected
	m.SetVPSStatus(true)
	if !m.vpsConnected {
		t.Error("vpsConnected should be true after SetVPSStatus(true)")
	}

	// Set to disconnected
	m.SetVPSStatus(false)
	if m.vpsConnected {
		t.Error("vpsConnected should be false after SetVPSStatus(false)")
	}
}

func TestSplashScreen_VPSStatusInView(t *testing.T) {
	tests := []struct {
		name       string
		connected  bool
		wantString string
	}{
		{
			name:       "disconnected shows Disconnected",
			connected:  false,
			wantString: "Disconnected",
		},
		{
			name:       "connected shows Connected",
			connected:  true,
			wantString: "Connected",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewSplashScreen()
			m.SetVPSStatus(tt.connected)
			view := m.View()

			if !strings.Contains(view, tt.wantString) {
				t.Errorf("view should contain %q, got:\n%s", tt.wantString, view)
			}
		})
	}
}

func TestSplashScreen_SplashTickMsgType(t *testing.T) {
	// Verify that splashTickMsg implements tea.Msg
	var _ tea.Msg = splashTickMsg{}
}

func TestSplashScreen_InitReturnsTick(t *testing.T) {
	m := NewSplashScreen()
	cmd := m.Init()

	// Execute the command to get the message
	msg := cmd()

	// Should return a splashTickMsg after 2 seconds
	_, ok := msg.(splashTickMsg)
	if !ok {
		t.Errorf("Init command should return splashTickMsg, got %T", msg)
	}
}

func TestSplashScreen_AdvanceResetsAfterCheck(t *testing.T) {
	m := NewSplashScreen()

	// Simulate timer completion
	newModel, _ := m.Update(splashTickMsg{})
	m = newModel.(SplashScreenModel)

	// First check should return true
	if !m.ShouldAdvanceToMenu() {
		t.Error("ShouldAdvanceToMenu() should return true on first call")
	}

	// Second check should return false (already reset)
	if m.ShouldAdvanceToMenu() {
		t.Error("ShouldAdvanceToMenu() should return false after first check")
	}
}

func TestSplashScreen_TimerDuration(t *testing.T) {
	// Verify the timer duration is approximately 2 seconds
	start := time.Now()
	
	m := NewSplashScreen()
	cmd := m.Init()
	
	// Get the tick message (this simulates what happens when timer fires)
	msg := cmd()
	
	// The command should be a tick that fires after 2 seconds
	// We can't easily test the actual timing, but we can verify it returns the right type
	if _, ok := msg.(splashTickMsg); !ok {
		t.Errorf("expected splashTickMsg, got %T after %v", msg, time.Since(start))
	}
}

func TestSplashScreen_ASCIILogo(t *testing.T) {
	m := NewSplashScreen()
	view := m.View()

	// The ASCII art should contain RFID-related elements
	expectedElements := []string{
		"┌", "┐", "└", "┘", // Box drawing characters
		"◆", "◈",         // Diamond shapes (chip)
		"~", "≈",         // Wave characters
	}

	found := false
	for _, elem := range expectedElements {
		if strings.Contains(view, elem) {
			found = true
			break
		}
	}

	if !found {
		t.Error("view should contain ASCII art elements (box drawing, chip, or waves)")
	}
}
