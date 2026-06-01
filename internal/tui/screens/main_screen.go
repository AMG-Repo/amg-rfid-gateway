// Package screens provides the different screens for the TUI.
package screens

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// MenuItem represents a menu item in the main menu.
type MenuItem struct {
	Title       string
	Description string
	Screen      string
}

// MainScreenModel represents the main menu screen.
type MainScreenModel struct {
	// Menu items
	items []MenuItem

	// Current cursor position
	cursor int

	// Selected screen (empty if nothing selected)
	selectedScreen string

	// Dimensions
	width  int
	height int

	// Styles
	styles *MainScreenStyles
}

// MainScreenStyles holds styles for the main screen.
type MainScreenStyles struct {
	Title        lipgloss.Style
	Subtitle     lipgloss.Style
	MenuItem     lipgloss.Style
	MenuSelected lipgloss.Style
	Description  lipgloss.Style
	Help         lipgloss.Style
}

// NewMainScreenStyles creates default styles.
func NewMainScreenStyles() *MainScreenStyles {
	return &MainScreenStyles{
		Title: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#7D56F4")).
			MarginLeft(2).
			MarginBottom(1),
		Subtitle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#B8B8B8")).
			MarginLeft(2).
			MarginBottom(2),
		MenuItem: lipgloss.NewStyle().
			MarginLeft(4).
			Padding(0, 1),
		MenuSelected: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#7D56F4")).
			MarginLeft(4).
			Padding(0, 1).
			Background(lipgloss.Color("#2A2A2A")),
		Description: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#888888")).
			MarginLeft(6),
		Help: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#666666")).
			MarginTop(2).
			MarginLeft(2),
	}
}

// NewMainScreen creates a new main menu screen.
func NewMainScreen() MainScreenModel {
	items := []MenuItem{
		{Title: "Antennas", Description: "View antenna status and connection", Screen: "antennas"},
		{Title: "Network", Description: "View network configuration and status", Screen: "network"},
		{Title: "System", Description: "View system status and sync info", Screen: "status"},
		{Title: "Settings", Description: "Configure gateway settings", Screen: "settings"},
		{Title: "Quit", Description: "Exit the configurator", Screen: "quit"},
	}

	return MainScreenModel{
		items:  items,
		cursor: 0,
		styles: NewMainScreenStyles(),
	}
}

// Init initializes the screen.
func (m MainScreenModel) Init() tea.Cmd {
	return nil
}

// Update handles messages.
func (m MainScreenModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// NEGATIVE: Handle key messages
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}

	// NEGATIVE: Handle navigation
	switch keyMsg.String() {
	case "up", "k":
		m.cursor = moveCursor(m.cursor, len(m.items), -1)
	case "down", "j":
		m.cursor = moveCursor(m.cursor, len(m.items), 1)
	case "enter", " ":
		// Mark selected
		m.selectedScreen = m.items[m.cursor].Screen
		// If quit, return quit command
		if m.selectedScreen == "quit" {
			return m, tea.Quit
		}
	}

	return m, nil
}

// View renders the screen.
func (m MainScreenModel) View() string {
	// Build title
	title := m.styles.Title.Render("AMG RFID Gateway Configurator")
	subtitle := m.styles.Subtitle.Render("Select an option to configure")

	// Build menu
	var menu string
	for i, item := range m.items {
		// Determine if this is the selected item
		isSelected := i == m.cursor

		// Render item
		cursor := "  "
		if isSelected {
			cursor = "▸ "
		}

		itemText := cursor + item.Title
		if isSelected {
			menu += m.styles.MenuSelected.Render(itemText) + "\n"
			menu += m.styles.Description.Render(item.Description) + "\n"
		} else {
			menu += m.styles.MenuItem.Render(itemText) + "\n"
		}
	}

	// Build help
	help := m.styles.Help.Render("↑/k up • ↓/j down • enter select • q/ctrl+c quit")

	// Combine all
	return title + "\n" + subtitle + "\n" + menu + "\n" + help
}

// SetSize updates the screen dimensions.
func (m *MainScreenModel) SetSize(width, height int) {
	m.width = width
	m.height = height
}

// GetSelectedScreen returns the selected screen and resets it.
func (m *MainScreenModel) GetSelectedScreen() string {
	screen := m.selectedScreen
	m.selectedScreen = "" // Reset after reading
	return screen
}
