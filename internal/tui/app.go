// Package tui provides the Bubbletea-based terminal UI for gateway configuration.
package tui

import (
	"log"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/amg-rfid/amg-rfid-gateway/internal/antenna"
	"github.com/amg-rfid/amg-rfid-gateway/internal/config"
	"github.com/amg-rfid/amg-rfid-gateway/internal/tui/screens"
)

// PollInterval is the interval between data polls from the bridge (only when screen is active).
const PollInterval = 5 * time.Second

// Screen represents the current active screen in the TUI.
type Screen int

const (
	// ScreenMainMenu is the main menu screen.
	ScreenMainMenu Screen = iota
	// ScreenAntennas shows antenna status.
	ScreenAntennas
	// ScreenNetwork shows network status.
	ScreenNetwork
	// ScreenStatus shows system status.
	ScreenStatus
	// ScreenSplash is the splash screen.
	ScreenSplash
	// ScreenSettings is the settings configuration screen.
	ScreenSettings
)

// App is the main TUI application model.
type App struct {
	// Current active screen
	currentScreen Screen

	// Screen models
	mainMenu screens.MainScreenModel
	antennas screens.AntennasScreenModel
	network  screens.NetworkScreenModel
	status   screens.StatusScreenModel
	splash   screens.SplashScreenModel
	settings screens.SettingsScreenModel

	// Terminal dimensions
	width  int
	height int

	// Config
	cfg *config.GatewayConfig

	// Config path for saving
	configPath string

	// Version string
	version string

	// Styles
	styles *Styles

	// Bridge client for communicating with gateway
	bridgeClient *BridgeClient

	// Last fetched data
	lastStatus   screens.SystemStatus
	lastAntennas []screens.AntennaInfo
}

// Styles holds the lipgloss styles for the TUI.
type Styles struct {
	Title        lipgloss.Style
	Subtitle     lipgloss.Style
	MenuItem     lipgloss.Style
	MenuSelected lipgloss.Style
	StatusOK     lipgloss.Style
	StatusError  lipgloss.Style
	StatusWarn   lipgloss.Style
	TableHeader  lipgloss.Style
	TableCell    lipgloss.Style
	Help         lipgloss.Style
}

// NewStyles creates the default styles for the TUI.
func NewStyles() *Styles {
	return &Styles{
		Title: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#7D56F4")).
			MarginLeft(2).
			MarginBottom(1),
		Subtitle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#B8B8B8")).
			MarginLeft(2).
			MarginBottom(1),
		MenuItem: lipgloss.NewStyle().
			MarginLeft(4).
			Padding(0, 1),
		MenuSelected: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#7D56F4")).
			MarginLeft(4).
			Padding(0, 1).
			Background(lipgloss.Color("#2A2A2A")),
		StatusOK: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#04B575")),
		StatusError: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FF4672")),
		StatusWarn: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#F9D71C")),
		TableHeader: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#7D56F4")),
		TableCell: lipgloss.NewStyle().
			Padding(0, 1),
		Help: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#666666")).
			MarginTop(1).
			MarginLeft(2),
	}
}

// NewApp creates a new TUI application.
func NewApp(cfgPath string, version string) (*App, error) {
	// Try to load config
	cfg, err := config.LoadFromYAML(cfgPath)
	if err != nil {
		// Fallback to env
		cfg = config.LoadFromEnv()
	}

	styles := NewStyles()

	// Create bridge client
	bridgeClient := NewBridgeClient("")

	app := &App{
		currentScreen: ScreenSplash,
		mainMenu:      screens.NewMainScreen(),
		antennas:      screens.NewAntennasScreen(),
		network:       screens.NewNetworkScreen(),
		status:        screens.NewStatusScreen(),
		splash:        screens.NewSplashScreen(),
		settings:      screens.NewSettingsScreen(cfg),
		cfg:           cfg,
		configPath:    cfgPath,
		version:       version,
		styles:        styles,
		bridgeClient:  bridgeClient,
	}

	// Initialize with default data
	app.lastStatus = app.status.GetStatus()
	app.lastAntennas = app.antennas.GetAntennas()

	return app, nil
}

// Init initializes the TUI application.
func (a *App) Init() tea.Cmd {
	// Initialize all screens
	cmds := []tea.Cmd{
		a.mainMenu.Init(),
		a.antennas.Init(),
		a.network.Init(),
		a.status.Init(),
		a.splash.Init(),
		a.settings.Init(),
		// Start polling for data
		a.pollData(),
	}

	return tea.Batch(cmds...)
}

// pollDataMsg is sent when data is polled from the bridge.
type pollDataMsg struct {
	status   screens.SystemStatus
	antennas []screens.AntennaInfo
	err      error
}

type settingsSaveErrorMsg struct {
	err error
}

// pollData fetches data from the bridge server.
func (a *App) pollData() tea.Cmd {
	return func() tea.Msg {
		// Check if bridge is available
		if !a.bridgeClient.IsAvailable() {
			return pollDataMsg{err: nil} // Silently skip if bridge not available
		}

		// Fetch status
		healthStatus, err := a.bridgeClient.GetStatus()
		if err != nil {
			return pollDataMsg{err: err}
		}

		// Fetch antennas
		antennaStatuses, err := a.bridgeClient.GetAntennas()
		if err != nil {
			return pollDataMsg{err: err}
		}

		// Convert to screen types
		status := screens.SystemStatus{
			GatewayID:         healthStatus.GatewayID,
			CompanyID:         healthStatus.CompanyID,
			Version:           healthStatus.Version,
			Uptime:            time.Duration(healthStatus.Uptime) * time.Second,
			PendingReadings:   healthStatus.CacheSize,
			AntennaCount:      len(antennaStatuses),
			ConnectedAntennas: countConnectedAntennas(antennaStatuses),
		}

		// Determine sync status
		switch healthStatus.Status {
		case "online":
			status.SyncStatus = "online"
		case "offline":
			status.SyncStatus = "offline"
		case "syncing":
			status.SyncStatus = "syncing"
		default:
			status.SyncStatus = "unknown"
		}

		// Convert antennas to screen format
		antennas := make([]screens.AntennaInfo, len(antennaStatuses))
		for i, ant := range antennaStatuses {
			antennas[i] = screens.AntennaInfo{
				ID:           ant.ID,
				Connected:    ant.Connected,
				ReadingCount: ant.ReadingCount,
				LastTagEPC:   ant.LastTagEPC,
				LastTagRSSI:  ant.LastTagRSSI,
				AutoReading:  ant.AutoReading,
				ErrorCount:   ant.ErrorCount,
			}
		}

		return pollDataMsg{
			status:   status,
			antennas: antennas,
		}
	}
}

// countConnectedAntennas counts the number of connected antennas.
func countConnectedAntennas(antennas []antenna.AntennaStatus) int {
	count := 0
	for _, ant := range antennas {
		if ant.Connected {
			count++
		}
	}
	return count
}

// Update handles messages and updates the application state.
func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// NEGATIVE: Handle quit first
	if msg, ok := msg.(tea.KeyMsg); ok {
		if msg.Type == tea.KeyCtrlC {
			return a, tea.Quit
		}

		// Global ESC to go back to main menu. Settings owns Esc so it can protect unsaved edits.
		if msg.Type == tea.KeyEsc && a.currentScreen != ScreenMainMenu && a.currentScreen != ScreenSettings {
			a.currentScreen = ScreenMainMenu
			return a, nil
		}
	}

	if a.currentScreen == ScreenSettings {
		if saveErr, ok := msg.(settingsSaveErrorMsg); ok {
			a.settings.SetSaveError(saveErr.err)
			return a, nil
		}
		if screens.IsSettingsSaveMsg(msg) {
			return a.handleSettingsSave(msg)
		}
		if screens.IsSettingsBackMsg(msg) {
			a.currentScreen = ScreenMainMenu
			return a, nil
		}
		if screens.IsSettingsDiscardChangesMsg(msg) {
			a.settings.SetConfig(a.cfg)
			a.settings.SetSaveError(nil)
			a.currentScreen = ScreenMainMenu
			return a, nil
		}
		if screens.IsSettingsStayMsg(msg) {
			return a, nil
		}
	}

	// NEGATIVE: Handle window resize
	if msg, ok := msg.(tea.WindowSizeMsg); ok {
		a.width = msg.Width
		a.height = msg.Height
		// Propagate to all screens
		a.mainMenu.SetSize(msg.Width, msg.Height)
		a.antennas.SetSize(msg.Width, msg.Height)
		a.network.SetSize(msg.Width, msg.Height)
		a.status.SetSize(msg.Width, msg.Height)
		a.settings.SetSize(msg.Width, msg.Height)
		return a, nil
	}

	// Handle poll data message
	if pollMsg, ok := msg.(pollDataMsg); ok {
		if pollMsg.err == nil {
			// Update last fetched data
			a.lastStatus = pollMsg.status
			a.lastAntennas = pollMsg.antennas

			// Update screens with new data
			a.status.SetStatus(pollMsg.status)
			a.antennas.SetAntennas(pollMsg.antennas)
		}

		// Schedule next poll ONLY if we're on a data screen
		if a.currentScreen == ScreenAntennas || a.currentScreen == ScreenStatus {
			return a, tea.Tick(PollInterval, func(time.Time) tea.Msg {
				return a.pollData()()
			})
		}
		// Don't poll if we're on main menu or network screen
		return a, nil
	}

	// HAPPY PATH: Route messages to current screen
	switch a.currentScreen {
	case ScreenMainMenu:
		newModel, cmd := a.mainMenu.Update(msg)
		a.mainMenu = newModel.(screens.MainScreenModel)

		// Check for navigation
		if screen := a.mainMenu.GetSelectedScreen(); screen != "" {
			navCmd := a.navigateTo(screen)
			return a, tea.Batch(cmd, navCmd)
		}

		return a, cmd

	case ScreenAntennas:
		newModel, cmd := a.antennas.Update(msg)
		a.antennas = newModel.(screens.AntennasScreenModel)
		return a, cmd

	case ScreenNetwork:
		newModel, cmd := a.network.Update(msg)
		a.network = newModel.(screens.NetworkScreenModel)
		return a, cmd

	case ScreenStatus:
		newModel, cmd := a.status.Update(msg)
		a.status = newModel.(screens.StatusScreenModel)
		return a, cmd

	case ScreenSplash:
		newModel, cmd := a.splash.Update(msg)
		a.splash = newModel.(screens.SplashScreenModel)

		// Check if splash is done and should advance to main menu
		if a.splash.ShouldAdvanceToMenu() {
			a.currentScreen = ScreenMainMenu
		}

		return a, cmd

	case ScreenSettings:
		newModel, cmd := a.settings.Update(msg)
		a.settings = newModel.(screens.SettingsScreenModel)
		return a, cmd

	default:
		return a, nil
	}
}

func (a *App) handleSettingsSave(msg tea.Msg) (tea.Model, tea.Cmd) {
	newCfg := a.settings.GetConfig()
	if err := newCfg.Validate(); err != nil {
		log.Printf("[TUI] Config validation failed: %v", err)
		a.settings.SetSaveError(err)
		return a, func() tea.Msg { return settingsSaveErrorMsg{err: err} }
	}

	if err := newCfg.SaveToYAML(a.configPath); err != nil {
		log.Printf("[TUI] Failed to save config: %v", err)
		a.settings.SetSaveError(err)
		return a, func() tea.Msg { return settingsSaveErrorMsg{err: err} }
	}

	log.Printf("[TUI] Config saved to %s", a.configPath)
	a.cfg = newCfg
	a.settings.SetConfig(newCfg)
	a.settings.SetSaveError(nil)
	if screens.SettingsSaveLeavesAfterSave(msg) {
		a.currentScreen = ScreenMainMenu
	}
	return a, nil
}

// View renders the current screen.
func (a *App) View() string {
	if a.width == 0 || a.height == 0 {
		return "Loading..."
	}

	switch a.currentScreen {
	case ScreenMainMenu:
		return a.mainMenu.View()
	case ScreenAntennas:
		return a.antennas.View()
	case ScreenNetwork:
		return a.network.View()
	case ScreenStatus:
		return a.status.View()
	case ScreenSplash:
		return a.splash.View()
	case ScreenSettings:
		return a.settings.View()
	default:
		return "Unknown screen"
	}
}

// navigateTo changes to the specified screen and starts polling if needed.
func (a *App) navigateTo(screen string) tea.Cmd {
	prevScreen := a.currentScreen

	switch screen {
	case "antennas":
		a.currentScreen = ScreenAntennas
	case "network":
		a.currentScreen = ScreenNetwork
	case "status":
		a.currentScreen = ScreenStatus
	case "settings":
		a.currentScreen = ScreenSettings
	case "quit":
		// Quit is handled by returning tea.Quit from Update
	default:
		a.currentScreen = ScreenMainMenu
	}

	// Start immediate poll when entering a data screen
	if a.currentScreen != prevScreen &&
		(a.currentScreen == ScreenAntennas || a.currentScreen == ScreenStatus) {
		return a.pollData()
	}
	return nil
}
