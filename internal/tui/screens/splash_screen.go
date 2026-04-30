package screens

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/amg-rfid/amg-rfid-gateway/internal/version"
)

// splashTickMsg is sent when the splash screen timer completes.
type splashTickMsg struct{}

// SplashScreenModel represents the splash screen.
type SplashScreenModel struct {
	// Terminal dimensions
	width  int
	height int

	// VPS connection status
	vpsConnected bool

	// Timer state
	timerComplete bool
	shouldAdvance bool

	// Styles
	styles *SplashScreenStyles
}

// SplashScreenStyles holds styles for the splash screen.
type SplashScreenStyles struct {
	Logo         lipgloss.Style
	Version      lipgloss.Style
	StatusOK     lipgloss.Style
	StatusError  lipgloss.Style
	Help         lipgloss.Style
	Connecting   lipgloss.Style
}

// NewSplashScreenStyles creates default styles.
func NewSplashScreenStyles() *SplashScreenStyles {
	return &SplashScreenStyles{
		Logo: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#00D4FF")).
			Bold(true).
			Align(lipgloss.Center),
		Version: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#B8B8B8")).
			Align(lipgloss.Center).
			MarginTop(1),
		StatusOK: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#04B575")),
		StatusError: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FF4672")),
		Help: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#666666")).
			Align(lipgloss.Center).
			MarginTop(2),
		Connecting: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#F9D71C")),
	}
}

// NewSplashScreen creates a new splash screen.
func NewSplashScreen() SplashScreenModel {
	return SplashScreenModel{
		vpsConnected:  false,
		timerComplete: false,
		shouldAdvance: false,
		styles:        NewSplashScreenStyles(),
	}
}

// Init initializes the screen and starts the auto-advance timer.
func (m SplashScreenModel) Init() tea.Cmd {
	return tea.Tick(2*time.Second, func(time.Time) tea.Msg {
		return splashTickMsg{}
	})
}

// Update handles messages.
func (m SplashScreenModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case splashTickMsg:
		m.timerComplete = true
		m.shouldAdvance = true
		return m, nil

	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyEnter, tea.KeyEsc:
			m.shouldAdvance = true
			return m, nil
		}
	}

	return m, nil
}

// View renders the screen.
func (m SplashScreenModel) View() string {
	// Build the ASCII logo
	logo := m.styles.Logo.Render(m.renderLogo())

	// Version string
	versionStr := m.styles.Version.Render("Version " + version.Version)

	// VPS Status with colored indicator
	var statusStr string
	if m.vpsConnected {
		statusStr = m.styles.StatusOK.Render("● Connected")
	} else {
		statusStr = m.styles.StatusError.Render("○ Disconnected")
	}
	status := lipgloss.NewStyle().
		Align(lipgloss.Center).
		MarginTop(1).
		Render("VPS: " + statusStr)

	// Help text
	help := m.styles.Help.Render("Press Enter to continue")

	// Combine all elements
	content := logo + "\n" + versionStr + "\n" + status + "\n" + help

	// Center vertically and horizontally
	return lipgloss.Place(m.width, m.height,
		lipgloss.Center, lipgloss.Center,
		content)
}

// renderLogo returns the ASCII art logo (radar style with concentric rings).
func (m SplashScreenModel) renderLogo() string {
	return `          ·
       ·  │  ·
     · ───┼─── ·
    ·╭────┼────╮·
   ·╭┤ ╭──┴──╮ ├╮·
   ·│ │  AMG  │ │·
   ·╰┤ ╰─────╯ ├╯·
    ·╰─────────╯·
     · ─────── ·
       ·  │  ·
          ·`
}

// SetSize updates the screen dimensions.
func (m *SplashScreenModel) SetSize(width, height int) {
	m.width = width
	m.height = height
}

// SetVPSStatus updates the VPS connection status.
func (m *SplashScreenModel) SetVPSStatus(connected bool) {
	m.vpsConnected = connected
}

// ShouldAdvanceToMenu returns true if the splash should advance to main menu.
// This also resets the advance flag to prevent multiple triggers.
func (m *SplashScreenModel) ShouldAdvanceToMenu() bool {
	if m.shouldAdvance {
		m.shouldAdvance = false
		return true
	}
	return false
}

// Reset resets the screen state for next display.
func (m *SplashScreenModel) Reset() {
	m.timerComplete = false
	m.shouldAdvance = false
}

// IsTimerComplete returns whether the auto-advance timer has fired.
func (m SplashScreenModel) IsTimerComplete() bool {
	return m.timerComplete
}
