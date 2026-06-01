package screens

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// AntennaInfo holds information about an antenna for display.
type AntennaInfo struct {
	ID           string
	Type         string
	IP           string
	Port         int
	Connected    bool
	ReadingCount uint64
	LastTagEPC   string
	LastTagRSSI  int
	AutoReading  bool
	ErrorCount   uint64
}

// AntennasScreenModel represents the antenna status screen.
type AntennasScreenModel struct {
	// List of antennas
	antennas []AntennaInfo

	// Cursor position
	cursor int

	// Dimensions
	width  int
	height int

	// Styles
	styles *AntennasScreenStyles
}

// AntennasScreenStyles holds styles for the antennas screen.
type AntennasScreenStyles struct {
	Title        lipgloss.Style
	Subtitle     lipgloss.Style
	TableHeader  lipgloss.Style
	TableRow     lipgloss.Style
	Connected    lipgloss.Style
	Disconnected lipgloss.Style
	Help         lipgloss.Style
}

// NewAntennasScreenStyles creates default styles.
func NewAntennasScreenStyles() *AntennasScreenStyles {
	return &AntennasScreenStyles{
		Title: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#7D56F4")).
			MarginLeft(2).
			MarginBottom(1),
		Subtitle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#B8B8B8")).
			MarginLeft(2).
			MarginBottom(1),
		TableHeader: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#7D56F4")).
			Padding(0, 1),
		TableRow: lipgloss.NewStyle().
			Padding(0, 1),
		Connected: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#04B575")),
		Disconnected: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FF4672")),
		Help: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#666666")).
			MarginTop(1).
			MarginLeft(2),
	}
}

// NewAntennasScreen creates a new antenna status screen.
func NewAntennasScreen() AntennasScreenModel {
	// Start with empty list — will be populated from bridge data
	return AntennasScreenModel{
		antennas: []AntennaInfo{},
		cursor:   0,
		styles:   NewAntennasScreenStyles(),
	}
}

// Init initializes the screen.
func (m AntennasScreenModel) Init() tea.Cmd {
	return nil
}

// Update handles messages.
func (m AntennasScreenModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// NEGATIVE: Handle key messages
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}

	// NEGATIVE: Handle navigation
	switch keyMsg.String() {
	case "up", "k":
		m.cursor = moveCursor(m.cursor, len(m.antennas), -1)
	case "down", "j":
		m.cursor = moveCursor(m.cursor, len(m.antennas), 1)
	}

	return m, nil
}

// View renders the screen.
func (m AntennasScreenModel) View() string {
	// Build title
	title := m.styles.Title.Render("Antenna Status")
	subtitle := m.styles.Subtitle.Render(fmt.Sprintf("%d antennas configured", len(m.antennas)))

	if len(m.antennas) == 0 {
		return title + "\n" + subtitle + "\n\n" +
			m.styles.Help.Render("  No antennas detected. Waiting for gateway data...") + "\n\n" +
			m.styles.Help.Render("ESC back • q/ctrl+c quit")
	}

	// Build table header
	header := m.styles.TableHeader.Render(
		fmt.Sprintf("%-10s %-12s %-6s %-8s %-10s %-12s", "ID", "Status", "Reads", "Errors", "Auto", "Last Tag"),
	)

	// Build table rows
	var rows string
	for i, ant := range m.antennas {
		// Determine status display
		status := m.styles.Disconnected.Render("OFFLINE")
		if ant.Connected {
			status = m.styles.Connected.Render("ONLINE")
		}

		// Auto-reading indicator
		autoStr := "—"
		if ant.AutoReading {
			autoStr = "ACTIVE"
		}

		// Last tag (truncate EPC for display)
		lastTag := "—"
		if ant.LastTagEPC != "" {
			epc := ant.LastTagEPC
			if len(epc) > 12 {
				epc = epc[:12] + "..."
			}
			lastTag = fmt.Sprintf("%s (%d)", epc, ant.LastTagRSSI)
		}

		// Cursor indicator
		cursor := "  "
		if i == m.cursor {
			cursor = "▸ "
		}

		row := fmt.Sprintf("%-10s %-12s %-6d %-8d %-10s %-12s",
			ant.ID, status, ant.ReadingCount, ant.ErrorCount, autoStr, lastTag)

		if i == m.cursor {
			rows += cursor + m.styles.TableRow.Background(lipgloss.Color("#2A2A2A")).Render(row) + "\n"
		} else {
			rows += cursor + m.styles.TableRow.Render(row) + "\n"
		}
	}

	// Build help
	help := m.styles.Help.Render("↑/k up • ↓/j down • ESC back • q/ctrl+c quit")

	// Combine all
	return title + "\n" + subtitle + "\n\n" + header + "\n" + rows + "\n" + help
}

// SetSize updates the screen dimensions.
func (m *AntennasScreenModel) SetSize(width, height int) {
	m.width = width
	m.height = height
}

// GetAntennas returns the current antenna list.
func (m AntennasScreenModel) GetAntennas() []AntennaInfo {
	return m.antennas
}

// SetAntennas updates the antenna list.
func (m *AntennasScreenModel) SetAntennas(antennas []AntennaInfo) {
	m.antennas = antennas
	// Reset cursor if out of bounds
	if m.cursor >= len(antennas) {
		m.cursor = 0
	}
}
