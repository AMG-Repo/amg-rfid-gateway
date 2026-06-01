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
type settingsSaveMsg struct {
	leaveAfterSave bool
}

// settingsDiscardChangesMsg is sent when user discards unsaved changes while leaving.
type settingsDiscardChangesMsg struct{}

// settingsStayMsg is sent when user stays on settings after an unsaved-exit prompt.
type settingsStayMsg struct{}

type settingsPromptMode int

const (
	settingsPromptNone settingsPromptMode = iota
	settingsPromptSaveConfirm
	settingsPromptUnsavedExit
)

type antennaEditorMode int

const (
	antennaEditorModeList antennaEditorMode = iota
	antennaEditorModeForm
	antennaEditorModeDeleteConfirm
)

type antennaFormField int

const (
	antennaFormFieldID antennaFormField = iota
	antennaFormFieldIP
	antennaFormFieldPort
	antennaFormFieldEnabled
	antennaFormFieldZone
	antennaFormFieldProtocol
	antennaFormFieldCount
)

type antennaForm struct {
	id       string
	ip       string
	port     string
	enabled  bool
	zone     string
	protocol config.AntennaProtocol
}

type antennaEditorModel struct {
	mode       antennaEditorMode
	cursor     int
	formCursor antennaFormField
	draft      []config.AntennaConfig
	form       antennaForm
	editingIdx int
	err        string
	hasChanges bool
}

func newAntennaEditorModel(antennas []config.AntennaConfig) antennaEditorModel {
	draft := copyAntennas(antennas)
	return antennaEditorModel{mode: antennaEditorModeList, draft: draft, editingIdx: -1}
}

func copyAntennas(antennas []config.AntennaConfig) []config.AntennaConfig {
	if antennas == nil {
		return nil
	}
	draft := make([]config.AntennaConfig, len(antennas))
	copy(draft, antennas)
	for i := range draft {
		draft[i].Protocol = effectiveAntennaProtocol(draft[i].Protocol)
	}
	return draft
}

func (m antennaEditorModel) Update(msg tea.KeyMsg) antennaEditorModel {
	switch m.mode {
	case antennaEditorModeForm:
		return m.updateForm(msg)
	case antennaEditorModeDeleteConfirm:
		return m.updateDeleteConfirm(msg)
	default:
		return m.updateList(msg)
	}
}

func (m antennaEditorModel) updateList(msg tea.KeyMsg) antennaEditorModel {
	switch msg.Type {
	case tea.KeyUp:
		m.cursor = moveCursor(m.cursor, len(m.draft), -1)
	case tea.KeyDown:
		m.cursor = moveCursor(m.cursor, len(m.draft), 1)
	case tea.KeyEnter:
		m.startEdit()
	case tea.KeyRunes:
		switch string(msg.Runes) {
		case "j":
			m.cursor = moveCursor(m.cursor, len(m.draft), 1)
		case "k":
			m.cursor = moveCursor(m.cursor, len(m.draft), -1)
		case "a":
			m.startAdd()
		case "e":
			m.startEdit()
		case "d":
			if len(m.draft) > 0 {
				m.mode = antennaEditorModeDeleteConfirm
				m.err = ""
			}
		}
	}
	return m
}

func (m antennaEditorModel) updateDeleteConfirm(msg tea.KeyMsg) antennaEditorModel {
	switch msg.Type {
	case tea.KeyEsc:
		m.mode = antennaEditorModeList
	case tea.KeyRunes:
		switch strings.ToLower(string(msg.Runes)) {
		case "y":
			if m.cursor >= 0 && m.cursor < len(m.draft) {
				m.draft = append(m.draft[:m.cursor], m.draft[m.cursor+1:]...)
				if m.cursor >= len(m.draft) {
					m.cursor = len(m.draft) - 1
				}
				if m.cursor < 0 {
					m.cursor = 0
				}
				m.hasChanges = true
			}
			m.mode = antennaEditorModeList
		case "n":
			m.mode = antennaEditorModeList
		}
	}
	return m
}

func (m antennaEditorModel) updateForm(msg tea.KeyMsg) antennaEditorModel {
	switch msg.Type {
	case tea.KeyEsc:
		m.mode = antennaEditorModeList
		m.err = ""
	case tea.KeyUp:
		m.formCursor = antennaFormField(moveCursor(int(m.formCursor), int(antennaFormFieldCount), -1))
	case tea.KeyDown, tea.KeyTab:
		m.formCursor = antennaFormField(moveCursor(int(m.formCursor), int(antennaFormFieldCount), 1))
	case tea.KeyLeft:
		if m.formCursor == antennaFormFieldProtocol {
			m.form.protocol = cycleAntennaProtocol(m.form.protocol, -1)
		}
	case tea.KeyRight, tea.KeySpace:
		if m.formCursor == antennaFormFieldEnabled {
			m.form.enabled = !m.form.enabled
		} else if m.formCursor == antennaFormFieldProtocol {
			m.form.protocol = cycleAntennaProtocol(m.form.protocol, 1)
		}
	case tea.KeyEnter:
		if m.formCursor == antennaFormFieldEnabled {
			m.form.enabled = !m.form.enabled
			return m
		}
		if m.formCursor == antennaFormFieldProtocol {
			m.form.protocol = cycleAntennaProtocol(m.form.protocol, 1)
			return m
		}
		return m.commitForm()
	case tea.KeyBackspace:
		m.trimCurrentFormField()
	case tea.KeyRunes:
		switch string(msg.Runes) {
		case "j":
			m.formCursor = antennaFormField(moveCursor(int(m.formCursor), int(antennaFormFieldCount), 1))
		case "k":
			m.formCursor = antennaFormField(moveCursor(int(m.formCursor), int(antennaFormFieldCount), -1))
		default:
			m.appendCurrentFormField(string(msg.Runes))
		}
	}
	return m
}

func (m *antennaEditorModel) startAdd() {
	m.mode = antennaEditorModeForm
	m.formCursor = antennaFormFieldID
	m.editingIdx = -1
	m.err = ""
	m.form = antennaForm{port: "8080", enabled: true, protocol: config.ProtocolGeneric}
}

func (m *antennaEditorModel) startEdit() {
	if len(m.draft) == 0 || m.cursor < 0 || m.cursor >= len(m.draft) {
		return
	}
	ant := m.draft[m.cursor]
	m.mode = antennaEditorModeForm
	m.formCursor = antennaFormFieldID
	m.editingIdx = m.cursor
	m.err = ""
	m.form = antennaForm{id: ant.ID, ip: ant.IP, port: strconv.Itoa(ant.Port), enabled: ant.Enabled, zone: ant.Zone, protocol: effectiveAntennaProtocol(ant.Protocol)}
}

func (m antennaEditorModel) commitForm() antennaEditorModel {
	ant, err := m.form.toAntennaConfig()
	if err != nil {
		m.err = err.Error()
		return m
	}
	for i, existing := range m.draft {
		if i != m.editingIdx && existing.ID == ant.ID {
			m.err = fmt.Sprintf("duplicate antenna id %q", ant.ID)
			return m
		}
	}
	if m.editingIdx >= 0 && m.editingIdx < len(m.draft) {
		m.draft[m.editingIdx] = ant
		m.cursor = m.editingIdx
	} else {
		m.draft = append(m.draft, ant)
		m.cursor = len(m.draft) - 1
	}
	m.mode = antennaEditorModeList
	m.err = ""
	m.hasChanges = true
	return m
}

func (f antennaForm) toAntennaConfig() (config.AntennaConfig, error) {
	id := strings.TrimSpace(f.id)
	ip := strings.TrimSpace(f.ip)
	zone := strings.TrimSpace(f.zone)
	port, err := strconv.Atoi(strings.TrimSpace(f.port))
	if err != nil {
		return config.AntennaConfig{}, errors.New("antenna port must be a number")
	}
	ant := config.AntennaConfig{ID: id, IP: ip, Port: port, Enabled: f.enabled, Zone: zone, Protocol: effectiveAntennaProtocol(f.protocol)}
	if !config.IsSupportedAntennaProtocol(ant.Protocol) {
		return config.AntennaConfig{}, errors.New("antenna protocol must be: generic, zebra")
	}
	if err := ant.Validate(); err != nil {
		return config.AntennaConfig{}, err
	}
	return ant, nil
}

func (m *antennaEditorModel) appendCurrentFormField(value string) {
	switch m.formCursor {
	case antennaFormFieldID:
		m.form.id += value
	case antennaFormFieldIP:
		m.form.ip += value
	case antennaFormFieldPort:
		m.form.port += value
	case antennaFormFieldZone:
		m.form.zone += value
	}
}

func (m *antennaEditorModel) trimCurrentFormField() {
	trim := func(value string) string {
		if len(value) == 0 {
			return value
		}
		return value[:len(value)-1]
	}
	switch m.formCursor {
	case antennaFormFieldID:
		m.form.id = trim(m.form.id)
	case antennaFormFieldIP:
		m.form.ip = trim(m.form.ip)
	case antennaFormFieldPort:
		m.form.port = trim(m.form.port)
	case antennaFormFieldZone:
		m.form.zone = trim(m.form.zone)
	}
}

func cycleAntennaProtocol(current config.AntennaProtocol, delta int) config.AntennaProtocol {
	protocols := []config.AntennaProtocol{config.ProtocolGeneric, config.ProtocolZebra}
	idx := 0
	for i, protocol := range protocols {
		if effectiveAntennaProtocol(current) == protocol {
			idx = i
			break
		}
	}
	return protocols[moveCursor(idx, len(protocols), delta)]
}

// IsSettingsSaveMsg reports whether msg is a settings save confirmation.
func IsSettingsSaveMsg(msg tea.Msg) bool {
	_, ok := msg.(settingsSaveMsg)
	return ok
}

// SettingsSaveLeavesAfterSave reports whether a save confirmation should navigate away after persisting.
func SettingsSaveLeavesAfterSave(msg tea.Msg) bool {
	saveMsg, ok := msg.(settingsSaveMsg)
	return ok && saveMsg.leaveAfterSave
}

// IsSettingsBackMsg reports whether msg requests leaving settings without unsaved changes.
func IsSettingsBackMsg(msg tea.Msg) bool {
	_, ok := msg.(settingsBackMsg)
	return ok
}

// IsSettingsDiscardChangesMsg reports whether msg requests discarding unsaved settings changes.
func IsSettingsDiscardChangesMsg(msg tea.Msg) bool {
	_, ok := msg.(settingsDiscardChangesMsg)
	return ok
}

// IsSettingsStayMsg reports whether msg cancels leaving settings.
func IsSettingsStayMsg(msg tea.Msg) bool {
	_, ok := msg.(settingsStayMsg)
	return ok
}

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

	// Operator prompt state
	promptMode settingsPromptMode

	// Change tracking
	hasChanges bool

	// Staged antenna CRUD editor
	antennaEditor antennaEditorModel

	// Last save/validation error shown to operator
	saveError string

	// Styles
	styles *SettingsScreenStyles
}

// SettingsScreenStyles holds styles for the settings screen.
type SettingsScreenStyles struct {
	Title      lipgloss.Style
	Subtitle   lipgloss.Style
	FieldLabel lipgloss.Style
	FieldValue lipgloss.Style
	FieldEdit  lipgloss.Style
	FieldError lipgloss.Style
	Selected   lipgloss.Style
	Help       lipgloss.Style
	Confirm    lipgloss.Style
	Changed    lipgloss.Style
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
			Width(28),
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

	webAccessMode := cfg.WebAccessMode
	if webAccessMode == "" {
		webAccessMode = "local"
	}

	fields, values := settingsFieldsAndValues(cfg, queueCap, warningThreshold, webAccessMode)

	return SettingsScreenModel{
		config:        cfg,
		original:      original,
		fields:        fields,
		values:        values,
		cursor:        0,
		editing:       false,
		antennaEditor: newAntennaEditorModel(cfg.Antennas),
		styles:        NewSettingsScreenStyles(),
	}
}

func settingsFieldsAndValues(cfg *config.GatewayConfig, queueCap, warningThreshold, webAccessMode string) ([]settingsField, []string) {
	fields := []settingsField{
		{label: "Gateway ID", key: "gateway_id", required: true, validator: validateNotEmpty},
		{label: "Company ID", key: "company_id", required: false, validator: nil},
		{label: "Cloud URL", key: "cloud_url", required: true, validator: validateWSSURL},
		{label: "Web UI Access", key: "web_access_mode", required: true, validator: validateWebAccessMode},
		{label: "Web UI Token", key: "web_auth_token", required: false, validator: nil},
		{label: "Log Level", key: "log_level", required: false, validator: validateLogLevel},
		{label: "Queue Cap", key: "queue_cap", required: false, validator: validateQueueCap},
		{label: "Warning Threshold", key: "warning_threshold", required: false, validator: validateQueueCap},
	}

	values := []string{
		cfg.GatewayID,
		cfg.CompanyID,
		cfg.CloudURL,
		webAccessMode,
		cfg.WebAuthToken,
		cfg.LogLevel,
		queueCap,
		warningThreshold,
	}

	for _, ant := range cfg.Antennas {
		fields = append(fields, settingsField{
			label:     fmt.Sprintf("Antenna %s Protocol", ant.ID),
			key:       antennaProtocolFieldKey(ant.ID),
			required:  true,
			validator: validateAntennaProtocol,
		})
		values = append(values, string(effectiveAntennaProtocol(ant.Protocol)))
	}

	return fields, values
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
		WebAccessMode:             cfg.WebAccessMode,
		WebListenAddr:             cfg.WebListenAddr,
		WebAuthToken:              cfg.WebAuthToken,
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
	if m.promptMode == settingsPromptSaveConfirm {
		switch msg := msg.(type) {
		case tea.KeyMsg:
			switch {
			case msg.Type == tea.KeyRunes && (string(msg.Runes) == "y" || string(msg.Runes) == "Y"):
				m.promptMode = settingsPromptNone
				return m, func() tea.Msg { return settingsSaveMsg{} }
			case msg.Type == tea.KeyRunes && (string(msg.Runes) == "n" || string(msg.Runes) == "N"):
				m.promptMode = settingsPromptNone
				return m, nil
			case msg.Type == tea.KeyEsc:
				m.promptMode = settingsPromptNone
				return m, nil
			}
		}
		return m, nil
	}

	if m.promptMode == settingsPromptUnsavedExit {
		switch msg := msg.(type) {
		case tea.KeyMsg:
			switch {
			case msg.Type == tea.KeyRunes && (string(msg.Runes) == "y" || string(msg.Runes) == "Y"):
				m.promptMode = settingsPromptNone
				return m, func() tea.Msg { return settingsSaveMsg{leaveAfterSave: true} }
			case msg.Type == tea.KeyRunes && (string(msg.Runes) == "d" || string(msg.Runes) == "D"):
				m.promptMode = settingsPromptNone
				return m, func() tea.Msg { return settingsDiscardChangesMsg{} }
			case msg.Type == tea.KeyRunes && (string(msg.Runes) == "n" || string(msg.Runes) == "N"):
				m.promptMode = settingsPromptNone
				return m, func() tea.Msg { return settingsStayMsg{} }
			case msg.Type == tea.KeyEsc:
				m.promptMode = settingsPromptNone
				return m, func() tea.Msg { return settingsStayMsg{} }
			}
		}
		return m, nil
	}

	if saveErr, ok := msg.(error); ok {
		m.saveError = saveErr.Error()
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
		if m.shouldRouteToAntennaEditor(msg) {
			before := m.antennaEditor.hasChanges
			m.antennaEditor = m.antennaEditor.Update(msg)
			if !before && m.antennaEditor.hasChanges {
				m.hasChanges = true
			}
			return m, nil
		}

		switch msg.Type {
		case tea.KeyUp:
			m.cursor = moveCursor(m.cursor, len(m.fields), -1)
		case tea.KeyDown:
			m.cursor = moveCursor(m.cursor, len(m.fields), 1)
		case tea.KeyTab:
			m.cursor = moveCursor(m.cursor, len(m.fields), 1)
		case tea.KeyShiftTab:
			m.cursor = moveCursor(m.cursor, len(m.fields), -1)
		case tea.KeyLeft:
			if m.isAntennaProtocolField() {
				m.cycleCurrentAntennaProtocol(-1)
			}
		case tea.KeyRight, tea.KeySpace:
			if m.isAntennaProtocolField() {
				m.cycleCurrentAntennaProtocol(1)
			}
		case tea.KeyEnter:
			if m.isAntennaProtocolField() {
				m.cycleCurrentAntennaProtocol(1)
				return m, nil
			}
			// Start editing the current field
			m.editing = true
			m.editBuffer = m.values[m.cursor]
			m.editError = ""
		case tea.KeyEsc:
			if m.hasChanges {
				m.promptMode = settingsPromptUnsavedExit
				return m, nil
			}
			m.saveError = ""
			// Go back to main menu
			return m, func() tea.Msg { return settingsBackMsg{} }
		default:
			// Handle rune keys (like 's' for save)
			if msg.Type == tea.KeyRunes && string(msg.Runes) == "j" {
				m.cursor = moveCursor(m.cursor, len(m.fields), 1)
			} else if msg.Type == tea.KeyRunes && string(msg.Runes) == "k" {
				m.cursor = moveCursor(m.cursor, len(m.fields), -1)
			} else if msg.Type == tea.KeyRunes && string(msg.Runes) == "s" {
				// Show save confirmation if there are changes
				if m.hasChanges {
					m.promptMode = settingsPromptSaveConfirm
				}
			}
		}
	}

	return m, nil
}

func (m SettingsScreenModel) shouldRouteToAntennaEditor(msg tea.KeyMsg) bool {
	if m.antennaEditor.mode != antennaEditorModeList {
		return true
	}
	switch msg.Type {
	case tea.KeyUp, tea.KeyDown, tea.KeyEnter:
		return m.isAntennaProtocolField() && len(m.antennaEditor.draft) > 0
	case tea.KeyRunes:
		switch string(msg.Runes) {
		case "a":
			return true
		case "e", "d", "j", "k":
			return m.isAntennaProtocolField() && len(m.antennaEditor.draft) > 0
		}
	}
	return false
}

// View renders the screen.
func (m SettingsScreenModel) View() string {
	if m.promptMode == settingsPromptSaveConfirm {
		return m.renderConfirmDialog()
	}
	if m.promptMode == settingsPromptUnsavedExit {
		return m.renderUnsavedExitDialog()
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

	antennaSection := m.renderAntennaEditor()

	saveError := ""
	if m.saveError != "" {
		saveError = "\n" + m.styles.FieldError.Render("Save failed: "+m.saveError) + "\n"
	}

	// Build help
	help := m.styles.Help.Render(m.renderHelp())

	// Combine all
	return title + "\n" + subtitle + "\n" + fields + antennaSection + saveError + "\n" + help
}

func (m SettingsScreenModel) renderAntennaEditor() string {
	section := "\n" + m.styles.Subtitle.Render("Antenna Editor") + "\n"
	switch m.antennaEditor.mode {
	case antennaEditorModeForm:
		form := m.antennaEditor.form
		section += fmt.Sprintf("  ID: %s\n  IP: %s\n  Port: %s\n  Enabled: %t\n  Zone: %s\n  Protocol: %s\n", form.id, form.ip, form.port, form.enabled, form.zone, effectiveAntennaProtocol(form.protocol))
		if m.antennaEditor.err != "" {
			section += m.styles.FieldError.Render("  "+m.antennaEditor.err) + "\n"
		}
	case antennaEditorModeDeleteConfirm:
		if len(m.antennaEditor.draft) > 0 {
			section += m.styles.Confirm.Render(fmt.Sprintf("Delete antenna %s? (y/n)", m.antennaEditor.draft[m.antennaEditor.cursor].ID)) + "\n"
		}
	default:
		if len(m.antennaEditor.draft) == 0 {
			section += "  No antennas configured\n"
		}
		for i, ant := range m.antennaEditor.draft {
			status := "disabled"
			if ant.Enabled {
				status = "enabled"
			}
			cursor := "  "
			if i == m.antennaEditor.cursor {
				cursor = "▸ "
			}
			section += cursor + fmt.Sprintf("%s %s:%d (%s) Zone: %s Protocol: %s\n", ant.ID, ant.IP, ant.Port, status, ant.Zone, effectiveAntennaProtocol(ant.Protocol))
		}
	}
	return section
}

// renderHelp returns the help text based on current state.
func (m SettingsScreenModel) renderHelp() string {
	if m.antennaEditor.mode == antennaEditorModeForm {
		return "↑/k previous field • ↓/j next field • type edit • ←/→ cycle protocol • space toggle/cycle • enter commit • esc cancel"
	}
	if m.antennaEditor.mode == antennaEditorModeDeleteConfirm {
		return "y delete • n/esc cancel"
	}
	if m.editing {
		return "enter save • esc cancel • type to edit"
	}
	if m.isAntennaProtocolField() {
		return "↑/k antenna up • ↓/j antenna down • ←/→ cycle protocol • space cycle protocol • a add • e/enter edit • d delete • s save • esc back"
	}
	return "↑/k up • ↓/j down • tab/shift+tab navigate • enter edit • a add • e/enter edit • d delete • s save • esc back"
}

func (m SettingsScreenModel) isAntennaProtocolField() bool {
	if m.cursor < 0 || m.cursor >= len(m.fields) {
		return false
	}
	return strings.HasPrefix(m.fields[m.cursor].key, "antenna_protocol:")
}

func (m *SettingsScreenModel) cycleCurrentAntennaProtocol(delta int) {
	if !m.isAntennaProtocolField() {
		return
	}

	protocols := []string{string(config.ProtocolGeneric), string(config.ProtocolZebra)}
	current := strings.ToLower(strings.TrimSpace(m.values[m.cursor]))
	currentIndex := 0
	for i, protocol := range protocols {
		if current == protocol {
			currentIndex = i
			break
		}
	}

	next := moveCursor(currentIndex, len(protocols), delta)
	if m.values[m.cursor] != protocols[next] {
		m.values[m.cursor] = protocols[next]
		m.hasChanges = true
	}
}

// renderConfirmDialog renders the save confirmation dialog.
func (m SettingsScreenModel) renderConfirmDialog() string {
	content := m.styles.Confirm.Render("Save changes? (y/n)")
	return lipgloss.Place(m.width, m.height,
		lipgloss.Center, lipgloss.Center,
		content)
}

func (m SettingsScreenModel) renderUnsavedExitDialog() string {
	content := m.styles.Confirm.Render("Unsaved changes. Save and leave? (y save / d discard / n stay)")
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

	if idx < 0 || idx >= len(m.fields) {
		return ""
	}

	field := m.fields[idx]
	switch field.key {
	case "gateway_id":
		return m.original.GatewayID
	case "company_id":
		return m.original.CompanyID
	case "cloud_url":
		return m.original.CloudURL
	case "web_access_mode":
		return m.original.WebAccessMode
	case "web_auth_token":
		return m.original.WebAuthToken
	case "log_level":
		return m.original.LogLevel
	case "queue_cap":
		if m.original.MaxPendingConfirmations != nil {
			return strconv.Itoa(*m.original.MaxPendingConfirmations)
		}
		return "10000"
	case "warning_threshold":
		if m.original.PendingWarningThreshold != nil {
			return strconv.Itoa(*m.original.PendingWarningThreshold)
		}
		return "1000"
	default:
		if antennaID, ok := strings.CutPrefix(field.key, "antenna_protocol:"); ok {
			for _, ant := range m.original.Antennas {
				if ant.ID == antennaID {
					return string(effectiveAntennaProtocol(ant.Protocol))
				}
			}
		}
	}
	return ""
}

// HasChanges returns true if any field has been modified.
func (m SettingsScreenModel) HasChanges() bool {
	return m.hasChanges || m.antennaEditor.hasChanges
}

// GetConfig returns the modified configuration.
func (m SettingsScreenModel) GetConfig() *config.GatewayConfig {
	cfg := copyConfig(m.original)

	cfg.GatewayID = m.values[0]
	cfg.CompanyID = m.values[1]
	cfg.CloudURL = m.values[2]
	cfg.WebAccessMode = m.values[3]
	cfg.WebAuthToken = m.values[4]
	cfg.LogLevel = m.values[5]

	if cfg.WebAccessMode == "local" {
		cfg.WebListenAddr = "127.0.0.1"
	} else if cfg.WebAccessMode == "lan" {
		cfg.WebListenAddr = "0.0.0.0"
	}

	// Parse queue cap
	if val, err := parseQueueCap(m.values[6]); err == nil {
		cfg.MaxPendingConfirmations = val
	}

	// Parse warning threshold
	if val, err := parseQueueCap(m.values[7]); err == nil {
		cfg.PendingWarningThreshold = val
	}

	cfg.Antennas = copyAntennas(m.antennaEditor.draft)

	for i, field := range m.fields {
		antennaID, ok := strings.CutPrefix(field.key, "antenna_protocol:")
		if !ok {
			continue
		}
		for antennaIdx := range cfg.Antennas {
			if cfg.Antennas[antennaIdx].ID == antennaID {
				cfg.Antennas[antennaIdx].Protocol = config.AntennaProtocol(strings.ToLower(strings.TrimSpace(m.values[i])))
				break
			}
		}
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

	webAccessMode := cfg.WebAccessMode
	if webAccessMode == "" {
		webAccessMode = "local"
	}
	m.fields, m.values = settingsFieldsAndValues(cfg, queueCap, warningThreshold, webAccessMode)
	m.antennaEditor = newAntennaEditorModel(cfg.Antennas)
	if m.cursor >= len(m.fields) {
		m.cursor = len(m.fields) - 1
	}

	m.hasChanges = false
	m.editing = false
	m.editBuffer = ""
	m.editError = ""
	m.promptMode = settingsPromptNone
	m.saveError = ""
}

// SetSaveError sets the operator-visible save error message.
func (m *SettingsScreenModel) SetSaveError(err error) {
	if err == nil {
		m.saveError = ""
		return
	}
	m.saveError = err.Error()
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

func validateWebAccessMode(value string) error {
	mode := strings.ToLower(strings.TrimSpace(value))
	if mode == "local" || mode == "lan" {
		return nil
	}
	return errors.New("must be: local, lan")
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

func validateAntennaProtocol(value string) error {
	protocol := config.AntennaProtocol(strings.ToLower(strings.TrimSpace(value)))
	if config.IsSupportedAntennaProtocol(protocol) {
		return nil
	}
	return errors.New("must be: generic, zebra")
}

func antennaProtocolFieldKey(antennaID string) string {
	return "antenna_protocol:" + antennaID
}

func effectiveAntennaProtocol(protocol config.AntennaProtocol) config.AntennaProtocol {
	if protocol == "" {
		return config.ProtocolGeneric
	}
	return protocol
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
