package screens

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/amg-rfid/amg-rfid-gateway/internal/config"
)

// settingsBackMsg is sent when user presses Esc to go back.
type settingsBackMsg struct{}

// settingsSaveMsg is sent when user confirms saving config.
type settingsSaveMsg struct{}

// settingsField represents a single editable field.
type settingsField struct {
	label       string
	key         string
	required    bool
	validator   func(string) error
	placeholder string
}

// SettingsScreenModel represents the settings configuration screen.
type SettingsScreenModel struct {
	// Terminal dimensions
	width  int
	height int

	// Configuration
	config   *config.GatewayConfig
	original *config.GatewayConfig

	// Field definitions and values
	fields []settingsField
	values []string

	// Cursor position
	cursor int

	// Editing state
	editing    bool
	editBuffer string
	editError  string

	// Save confirmation
	showConfirm bool

	// Change tracking
	hasChanges bool

	// Styles
	styles *SettingsScreenStyles
}

// SettingsScreenStyles holds styles for the settings screen.
type SettingsScreenStyles struct {
	Title       lipgloss.Style
	Subtitle    lipgloss.Style
	FieldLabel  lipgloss.Style
	FieldValue  lipgloss.Style
	FieldEdit   lipgloss.Style
	FieldError  lipgloss.Style
	Selected    lipgloss.Style
	Help        lipgloss.Style
	Confirm     lipgloss.Style
	Changed     lipgloss.Style
}

// NewSettingsScreenStyles creates default styles.
func NewSettingsScreenStyles() *SettingsScreenStyles {
	return &SettingsScreenStyles{
		Title: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#7D56F4")).
			MarginLeft(2).
			MarginBottom(1),
		Subtitle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#B8B8B8")).
			MarginLeft(2).
			MarginBottom(2),
		FieldLabel: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#B8B8B8")).
			MarginLeft(4).
			Width(20),
		FieldValue: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFFFF")).
			Width(40),
		FieldEdit: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(lipgloss.Color("#2A2A2A")).
			Width(40),
		FieldError: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FF4672")).
			MarginLeft(4),
		Selected: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#7D56F4")).
			MarginLeft(4),
		Help: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#666666")).
			MarginTop(2).
			MarginLeft(2),
		Confirm: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#F9D71C")).
			MarginLeft(2).
			MarginTop(1),
		Changed: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#F9D71C")),
	}
}

// NewSettingsScreen creates a new settings screen.
func NewSettingsScreen(cfg *config.GatewayConfig) SettingsScreenModel {
	if cfg == nil {
		cfg = &config.GatewayConfig{}
	}

	// Deep copy the config to preserve original
	original := copyConfig(cfg)

	// Get queue cap values as strings
	queueCap := "10000"
	if cfg.MaxPendingConfirmations != nil {
		if *cfg.MaxPendingConfirmations == 0 {
			queueCap = "0 (unlimited)"
		} else {
			queueCap = strconv.Itoa(*cfg.MaxPendingConfirmations)
		}
	}

	warningThreshold := "1000"
	if cfg.PendingWarningThreshold != nil {
		if *cfg.PendingWarningThreshold == 0 {
			warningThreshold = "0 (never)"
		} else {
			warningThreshold = strconv.Itoa(*cfg.PendingWarningThreshold)
		}
	}

	// Define fields
	fields := []settingsField{
		{label: "Gateway ID", key: "gateway_id", required: true, validator: validateNotEmpty},
		{label: "Company ID", key: "company_id", required: false, validator: nil},
		{label: "Cloud URL", key: "cloud_url", required: true, validator: validateWSSURL},
		{label: "Log Level", key: "log_level", required: false, validator: validateLogLevel},
		{label: "Queue Cap", key: "queue_cap", required: false, validator: validateQueueCap},
		{label: "Warning Threshold", key: "warning_threshold", required: false, validator: validateQueueCap},
	}

	// Initialize values from config
	values := []string{
		cfg.GatewayID,
		cfg.CompanyID,
		cfg.CloudURL,
		cfg.LogLevel,
		queueCap,
		warningThreshold,
	}

	return SettingsScreenModel{
		config:   cfg,
		original: original,
		fields:   fields,
		values:   values,
		cursor:   0,
		editing:  false,
		styles:   NewSettingsScreenStyles(),
	}
}

// copyConfig creates a deep copy of the config, preserving ALL fields.
func copyConfig(cfg *config.GatewayConfig) *config.GatewayConfig {
	if cfg == nil {
		return nil
	}

	// Copy all scalar fields
	cp := &config.GatewayConfig{
		GatewayID:                 cfg.GatewayID,
		CompanyID:                 cfg.CompanyID,
		CloudURL:                  cfg.CloudURL,
		JWTSecret:                 cfg.JWTSecret,
		SyncInterval:              cfg.SyncInterval,
		BatchSize:                 cfg.BatchSize,
		MaxRetries:                cfg.MaxRetries,
		HealthPort:                cfg.HealthPort,
		DataPath:                  cfg.DataPath,
		ListenMode:                cfg.ListenMode,
		HeartbeatInterval:         cfg.HeartbeatInterval,
		HeartbeatSilenceThreshold: cfg.HeartbeatSilenceThreshold,
		AdaptiveDelayRecent:       cfg.AdaptiveDelayRecent,
		AdaptiveDelayRecentWindow: cfg.AdaptiveDelayRecentWindow,
		AdaptiveDelayStale:        cfg.AdaptiveDelayStale,
		AdaptiveDelayStaleWindow:  cfg.AdaptiveDelayStaleWindow,
		AdaptiveDelayAutoReading:  cfg.AdaptiveDelayAutoReading,
		ReconnectInitialBackoff:   cfg.ReconnectInitialBackoff,
		ReconnectMaxBackoff:       cfg.ReconnectMaxBackoff,
		SocketPath:                cfg.SocketPath,
		WebEnabled:                cfg.WebEnabled,
		WebPort:                   cfg.WebPort,
		WebListenAddr:             cfg.WebListenAddr,
		VPSAPIURL:                 cfg.VPSAPIURL,
		SyncToolsInterval:         cfg.SyncToolsInterval,
		ConfirmationRetryInterval: cfg.ConfirmationRetryInterval,
		LogLevel:                  cfg.LogLevel,
	}

	// Deep copy pointer fields
	if cfg.MaxPendingConfirmations != nil {
		val := *cfg.MaxPendingConfirmations
		cp.MaxPendingConfirmations = &val
	}
	if cfg.PendingWarningThreshold != nil {
		val := *cfg.PendingWarningThreshold
		cp.PendingWarningThreshold = &val
	}

	// Deep copy antennas slice
	if cfg.Antennas != nil {
		cp.Antennas = make([]config.AntennaConfig, len(cfg.Antennas))
		copy(cp.Antennas, cfg.Antennas)
	}

	return cp
}

// Init initializes the screen.
func (m SettingsScreenModel) Init() tea.Cmd {
	return nil
}

// Update handles messages.
func (m SettingsScreenModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Handle save confirmation dialog
	if m.showConfirm {
		switch msg := msg.(type) {
		case tea.KeyMsg:
			switch {
			case msg.Type == tea.KeyRunes && (string(msg.Runes) == "y" || string(msg.Runes) == "Y"):
				m.showConfirm = false
				return m, func() tea.Msg { return settingsSaveMsg{} }
			case msg.Type == tea.KeyRunes && (string(msg.Runes) == "n" || string(msg.Runes) == "N"):
				m.showConfirm = false
				return m, nil
			case msg.Type == tea.KeyEsc:
				m.showConfirm = false
				return m, nil
			}
		}
		return m, nil
	}

	// Handle editing mode
	if m.editing {
		switch msg := msg.(type) {
		case tea.KeyMsg:
			switch msg.Type {
			case tea.KeyEsc:
				// Cancel editing, restore original value
				m.editing = false
				m.editBuffer = m.values[m.cursor]
				m.editError = ""
				return m, nil

			case tea.KeyEnter:
				// Validate and save
				if err := m.validateCurrentField(); err != nil {
					m.editError = err.Error()
					return m, nil
				}
				// Save the value
				if m.values[m.cursor] != m.editBuffer {
					m.values[m.cursor] = m.editBuffer
					m.hasChanges = true
				}
				m.editing = false
				m.editError = ""
				return m, nil

			case tea.KeyBackspace:
				if len(m.editBuffer) > 0 {
					m.editBuffer = m.editBuffer[:len(m.editBuffer)-1]
				}
				return m, nil

			default:
				// Add character to buffer
				if msg.Type == tea.KeyRunes {
					m.editBuffer += string(msg.Runes)
				}
				return m, nil
			}
		}
		return m, nil
	}

	// Handle normal navigation
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyUp:
			if m.cursor > 0 {
				m.cursor--
			} else {
				m.cursor = len(m.fields) - 1
			}
		case tea.KeyDown:
			if m.cursor < len(m.fields)-1 {
				m.cursor++
			} else {
				m.cursor = 0
			}
		case tea.KeyTab:
			if m.cursor < len(m.fields)-1 {
				m.cursor++
			} else {
				m.cursor = 0
			}
		case tea.KeyShiftTab:
			if m.cursor > 0 {
				m.cursor--
			} else {
				m.cursor = len(m.fields) - 1
			}
		case tea.KeyEnter:
			// Start editing the current field
			m.editing = true
			m.editBuffer = m.values[m.cursor]
			m.editError = ""
		case tea.KeyEsc:
			// Go back to main menu
			return m, func() tea.Msg { return settingsBackMsg{} }
		default:
			// Handle rune keys (like 's' for save)
			if msg.Type == tea.KeyRunes && string(msg.Runes) == "s" {
				// Show save confirmation if there are changes
				if m.hasChanges {
					m.showConfirm = true
				}
			}
		}
	}

	return m, nil
}

// View renders the screen.
func (m SettingsScreenModel) View() string {
	if m.showConfirm {
		return m.renderConfirmDialog()
	}

	// Build title
	title := m.styles.Title.Render("Settings")
	subtitle := m.styles.Subtitle.Render("Configure gateway settings")

	// Build fields
	var fields string
	for i, field := range m.fields {
		isSelected := i == m.cursor
		isChanged := m.values[i] != m.getOriginalValue(i)

		label := m.styles.FieldLabel.Render(field.label + ":")

		var value string
		if m.editing && isSelected {
			// Show edit buffer with cursor
			value = m.styles.FieldEdit.Render(m.editBuffer + "█")
		} else if isChanged {
			// Highlight changed values
			value = m.styles.Changed.Render(m.values[i])
		} else {
			value = m.styles.FieldValue.Render(m.values[i])
		}

		cursor := "  "
		if isSelected && !m.editing {
			cursor = "▸ "
		}

		fields += cursor + label + " " + value + "\n"

		// Show validation error for current field
		if isSelected && m.editing && m.editError != "" {
			fields += m.styles.FieldError.Render("  "+m.editError) + "\n"
		}
	}

	// Build antenna section if antennas exist
	var antennaSection string
	if len(m.config.Antennas) > 0 {
		antennaSection = "\n" + m.styles.Subtitle.Render("Antennas") + "\n"
		for _, ant := range m.config.Antennas {
			status := "disabled"
			if ant.Enabled {
				status = "enabled"
			}
			antennaSection += m.styles.FieldLabel.Render(fmt.Sprintf("  %s:", ant.ID))
			antennaSection += fmt.Sprintf(" %s:%d (%s) Zone: %s\n", ant.IP, ant.Port, status, ant.Zone)
		}
	}

	// Build help
	help := m.styles.Help.Render(m.renderHelp())

	// Combine all
	return title + "\n" + subtitle + "\n" + fields + antennaSection + "\n" + help
}

// renderHelp returns the help text based on current state.
func (m SettingsScreenModel) renderHelp() string {
	if m.editing {
		return "enter save • esc cancel • type to edit"
	}
	return "↑/k up • ↓/j down • tab/shift+tab navigate • enter edit • s save • esc back"
}

// renderConfirmDialog renders the save confirmation dialog.
func (m SettingsScreenModel) renderConfirmDialog() string {
	content := m.styles.Confirm.Render("Save changes? (y/n)")
	return lipgloss.Place(m.width, m.height,
		lipgloss.Center, lipgloss.Center,
		content)
}

// validateCurrentField validates the current edit buffer.
func (m SettingsScreenModel) validateCurrentField() error {
	if m.cursor >= len(m.fields) {
		return nil
	}

	field := m.fields[m.cursor]

	// Check required
	if field.required && strings.TrimSpace(m.editBuffer) == "" {
		return errors.New("cannot be empty")
	}

	// Run custom validator if present
	if field.validator != nil {
		return field.validator(m.editBuffer)
	}

	return nil
}

// getOriginalValue gets the original value for a field.
func (m SettingsScreenModel) getOriginalValue(idx int) string {
	if m.original == nil {
		return ""
	}

	switch idx {
	case 0:
		return m.original.GatewayID
	case 1:
		return m.original.CompanyID
	case 2:
		return m.original.CloudURL
	case 3:
		return m.original.LogLevel
	case 4:
		if m.original.MaxPendingConfirmations != nil {
			return strconv.Itoa(*m.original.MaxPendingConfirmations)
		}
		return "10000"
	case 5:
		if m.original.PendingWarningThreshold != nil {
			return strconv.Itoa(*m.original.PendingWarningThreshold)
		}
		return "1000"
	}
	return ""
}

// HasChanges returns true if any field has been modified.
func (m SettingsScreenModel) HasChanges() bool {
	return m.hasChanges
}

// GetConfig returns the modified configuration.
func (m SettingsScreenModel) GetConfig() *config.GatewayConfig {
	cfg := copyConfig(m.original)

	cfg.GatewayID = m.values[0]
	cfg.CompanyID = m.values[1]
	cfg.CloudURL = m.values[2]
	cfg.LogLevel = m.values[3]

	// Parse queue cap
	if val, err := parseQueueCap(m.values[4]); err == nil {
		cfg.MaxPendingConfirmations = val
	}

	// Parse warning threshold
	if val, err := parseQueueCap(m.values[5]); err == nil {
		cfg.PendingWarningThreshold = val
	}

	return cfg
}

// GetOriginalConfig returns the original unmodified configuration.
func (m SettingsScreenModel) GetOriginalConfig() *config.GatewayConfig {
	return m.original
}

// SetConfig updates the screen with a new configuration.
func (m *SettingsScreenModel) SetConfig(cfg *config.GatewayConfig) {
	if cfg == nil {
		return
	}

	m.config = cfg
	m.original = copyConfig(cfg)

	// Update values
	m.values[0] = cfg.GatewayID
	m.values[1] = cfg.CompanyID
	m.values[2] = cfg.CloudURL
	m.values[3] = cfg.LogLevel

	// Update queue caps
	if cfg.MaxPendingConfirmations != nil {
		if *cfg.MaxPendingConfirmations == 0 {
			m.values[4] = "0 (unlimited)"
		} else {
			m.values[4] = strconv.Itoa(*cfg.MaxPendingConfirmations)
		}
	} else {
		m.values[4] = "10000"
	}

	if cfg.PendingWarningThreshold != nil {
		if *cfg.PendingWarningThreshold == 0 {
			m.values[5] = "0 (never)"
		} else {
			m.values[5] = strconv.Itoa(*cfg.PendingWarningThreshold)
		}
	} else {
		m.values[5] = "1000"
	}

	m.hasChanges = false
}

// SetSize updates the screen dimensions.
func (m *SettingsScreenModel) SetSize(width, height int) {
	m.width = width
	m.height = height
}

// Validator functions

func validateNotEmpty(value string) error {
	if strings.TrimSpace(value) == "" {
		return errors.New("cannot be empty")
	}
	return nil
}

func validateWSSURL(value string) error {
	u, err := url.Parse(value)
	if err != nil {
		return errors.New("invalid URL")
	}
	if u.Scheme != "wss" {
		return errors.New("must use wss:// scheme")
	}
	return nil
}

func validateLogLevel(value string) error {
	validLevels := []string{"debug", "info", "warn", "error", ""}
	value = strings.ToLower(strings.TrimSpace(value))
	for _, level := range validLevels {
		if value == level {
			return nil
		}
	}
	return errors.New("must be: debug, info, warn, error")
}

func validateQueueCap(value string) error {
	// Handle special cases
	value = strings.ToLower(strings.TrimSpace(value))
	if strings.Contains(value, "unlimited") || strings.Contains(value, "never") {
		return nil
	}

	// Try to parse as integer
	if _, err := strconv.Atoi(value); err != nil {
		return errors.New("must be a number")
	}
	return nil
}

// parseQueueCap parses a queue cap value string.
func parseQueueCap(value string) (*int, error) {
	value = strings.ToLower(strings.TrimSpace(value))

	// Handle special cases
	if strings.Contains(value, "unlimited") || value == "0 (never)" {
		zero := 0
		return &zero, nil
	}

	// Remove any parenthetical text
	if idx := strings.Index(value, " "); idx != -1 {
		value = value[:idx]
	}

	// Parse integer
	val, err := strconv.Atoi(value)
	if err != nil {
		return nil, err
	}

	return &val, nil
}
