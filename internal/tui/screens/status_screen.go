package screens

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// SystemStatus holds system information for display.
type SystemStatus struct {
	GatewayID         string
	CompanyID         string
	Version           string
	Uptime            time.Duration
	PendingReadings   int
	SyncStatus        string
	LastSync          time.Time
	AntennaCount      int
	ConnectedAntennas int
}

// StatusScreenModel represents the system status screen.
type StatusScreenModel struct {
	// System status
	status SystemStatus

	// Dimensions
	width  int
	height int

	// Styles
	styles *StatusScreenStyles
}

// StatusScreenStyles holds styles for the status screen.
type StatusScreenStyles struct {
	Title       lipgloss.Style
	Subtitle    lipgloss.Style
	Label       lipgloss.Style
	Value       lipgloss.Style
	Section     lipgloss.Style
	StatusOK    lipgloss.Style
	StatusError lipgloss.Style
	StatusWarn  lipgloss.Style
	Help        lipgloss.Style
}

// NewStatusScreenStyles creates default styles.
func NewStatusScreenStyles() *StatusScreenStyles {
	return &StatusScreenStyles{
		Title: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#7D56F4")).
			MarginLeft(2).
			MarginBottom(1),
		Subtitle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#B8B8B8")).
			MarginLeft(2).
			MarginBottom(2),
		Label: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#B8B8B8")).
			MarginLeft(4).
			Width(20),
		Value: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFFFF")),
		Section: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#7D56F4")).
			MarginLeft(2).
			MarginTop(1).
			MarginBottom(1),
		StatusOK: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#04B575")),
		StatusError: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FF4672")),
		StatusWarn: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#F9D71C")),
		Help: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#666666")).
			MarginTop(2).
			MarginLeft(2),
	}
}

// NewStatusScreen creates a new system status screen.
func NewStatusScreen() StatusScreenModel {
	// Start with empty status — will be populated from bridge data
	return StatusScreenModel{
		status: SystemStatus{
			GatewayID:         "—",
			CompanyID:         "—",
			Version:           "—",
			Uptime:            0,
			PendingReadings:   0,
			SyncStatus:        "waiting",
			AntennaCount:      0,
			ConnectedAntennas: 0,
		},
		styles: NewStatusScreenStyles(),
	}
}

// Init initializes the screen.
func (m StatusScreenModel) Init() tea.Cmd {
	return nil
}

// Update handles messages.
func (m StatusScreenModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Status screen has no navigation, just display
	// In real implementation, could update uptime dynamically
	return m, nil
}

// View renders the screen.
func (m StatusScreenModel) View() string {
	// Build title
	title := m.styles.Title.Render("System Status")
	subtitle := m.styles.Subtitle.Render("Gateway overview and metrics")

	// Build sections
	var content string

	// Identity section
	content += m.styles.Section.Render("Identity")
	content += m.renderRow("Gateway ID:", m.status.GatewayID)
	content += m.renderRow("Company ID:", m.status.CompanyID)
	content += m.renderRow("Version:", m.status.Version)

	// Runtime section
	content += m.styles.Section.Render("Runtime")
	content += m.renderRow("Uptime:", formatDuration(m.status.Uptime))
	content += m.renderRow("Antennas:", fmt.Sprintf("%d/%d connected", m.status.ConnectedAntennas, m.status.AntennaCount))

	// Sync section
	content += m.styles.Section.Render("Synchronization")
	content += m.renderRow("Pending:", fmt.Sprintf("%d readings", m.status.PendingReadings))

	// Sync status with color
	status := m.styles.StatusOK.Render("ONLINE")
	switch m.status.SyncStatus {
	case "offline":
		status = m.styles.StatusError.Render("OFFLINE")
	case "syncing":
		status = m.styles.StatusWarn.Render("SYNCING")
	case "waiting":
		status = m.styles.StatusWarn.Render("WAITING FOR DATA")
	}
	content += m.renderRow("Status:", status)

	lastSync := "Never"
	if !m.status.LastSync.IsZero() {
		lastSync = formatDuration(time.Since(m.status.LastSync)) + " ago"
	}
	content += m.renderRow("Last Sync:", lastSync)

	// Build help
	help := m.styles.Help.Render("ESC back • q/ctrl+c quit")

	// Combine all
	return title + "\n" + subtitle + "\n\n" + content + "\n\n" + help
}

// renderRow renders a label-value row.
func (m StatusScreenModel) renderRow(label, value string) string {
	labelStr := m.styles.Label.Render(label)
	valueStr := m.styles.Value.Render(value)
	return labelStr + valueStr + "\n"
}

// formatDuration formats a duration in a human-readable way.
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm %ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
	}
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	return fmt.Sprintf("%dd %dh", days, hours)
}

// SetSize updates the screen dimensions.
func (m *StatusScreenModel) SetSize(width, height int) {
	m.width = width
	m.height = height
}

// GetStatus returns the current system status.
func (m StatusScreenModel) GetStatus() SystemStatus {
	return m.status
}

// SetStatus updates the system status.
func (m *StatusScreenModel) SetStatus(status SystemStatus) {
	m.status = status
}
