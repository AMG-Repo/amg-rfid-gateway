package screens

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/amg-rfid/amg-rfid-gateway/internal/config"
)

func TestAntennaEditorModel_StateTransitions(t *testing.T) {
	tests := []struct {
		name       string
		start      []config.AntennaConfig
		keys       []tea.KeyMsg
		wantMode   antennaEditorMode
		wantCursor int
		wantDraft  []string
	}{
		{
			name:       "list navigation wraps down",
			start:      testEditorAntennas(),
			keys:       []tea.KeyMsg{{Type: tea.KeyDown}},
			wantMode:   antennaEditorModeList,
			wantCursor: 1,
			wantDraft:  []string{"dock", "exit"},
		},
		{
			name:       "add opens blank form without changing draft",
			start:      testEditorAntennas(),
			keys:       []tea.KeyMsg{{Type: tea.KeyRunes, Runes: []rune("a")}},
			wantMode:   antennaEditorModeForm,
			wantCursor: 0,
			wantDraft:  []string{"dock", "exit"},
		},
		{
			name:       "enter edits selected antenna",
			start:      testEditorAntennas(),
			keys:       []tea.KeyMsg{{Type: tea.KeyDown}, {Type: tea.KeyEnter}},
			wantMode:   antennaEditorModeForm,
			wantCursor: 1,
			wantDraft:  []string{"dock", "exit"},
		},
		{
			name:       "delete cancel returns to list",
			start:      testEditorAntennas(),
			keys:       []tea.KeyMsg{{Type: tea.KeyRunes, Runes: []rune("d")}, {Type: tea.KeyRunes, Runes: []rune("n")}},
			wantMode:   antennaEditorModeList,
			wantCursor: 0,
			wantDraft:  []string{"dock", "exit"},
		},
		{
			name:       "delete confirm removes selected antenna",
			start:      testEditorAntennas(),
			keys:       []tea.KeyMsg{{Type: tea.KeyRunes, Runes: []rune("d")}, {Type: tea.KeyRunes, Runes: []rune("y")}},
			wantMode:   antennaEditorModeList,
			wantCursor: 0,
			wantDraft:  []string{"exit"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			editor := newAntennaEditorModel(tt.start)
			for _, key := range tt.keys {
				editor = editor.Update(key)
			}

			assert.Equal(t, tt.wantMode, editor.mode)
			assert.Equal(t, tt.wantCursor, editor.cursor)
			assert.Equal(t, tt.wantDraft, antennaIDs(editor.draft))
		})
	}
}

func TestAntennaEditorModel_FormCommitValidation(t *testing.T) {
	tests := []struct {
		name      string
		start     []config.AntennaConfig
		form      antennaForm
		wantIDs   []string
		wantError string
	}{
		{
			name:    "valid new antenna is staged",
			start:   testEditorAntennas(),
			form:    antennaForm{id: "packing", ip: "192.168.1.20", port: "9090", enabled: true, zone: "packing", protocol: config.ProtocolZebra},
			wantIDs: []string{"dock", "exit", "packing"},
		},
		{
			name:      "empty id is rejected",
			start:     testEditorAntennas(),
			form:      antennaForm{id: "", ip: "192.168.1.20", port: "9090", enabled: true, zone: "packing", protocol: config.ProtocolGeneric},
			wantIDs:   []string{"dock", "exit"},
			wantError: "id",
		},
		{
			name:      "invalid port is rejected",
			start:     testEditorAntennas(),
			form:      antennaForm{id: "packing", ip: "192.168.1.20", port: "70000", enabled: true, zone: "packing", protocol: config.ProtocolGeneric},
			wantIDs:   []string{"dock", "exit"},
			wantError: "port",
		},
		{
			name:      "duplicate id is rejected before staging",
			start:     testEditorAntennas(),
			form:      antennaForm{id: "dock", ip: "192.168.1.20", port: "9090", enabled: true, zone: "packing", protocol: config.ProtocolGeneric},
			wantIDs:   []string{"dock", "exit"},
			wantError: "duplicate",
		},
		{
			name:      "unsupported protocol is rejected",
			start:     testEditorAntennas(),
			form:      antennaForm{id: "packing", ip: "192.168.1.20", port: "9090", enabled: true, zone: "packing", protocol: config.AntennaProtocol("alien")},
			wantIDs:   []string{"dock", "exit"},
			wantError: "protocol",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			editor := newAntennaEditorModel(tt.start)
			editor.startAdd()
			editor.form = tt.form

			editor = editor.commitForm()

			assert.Equal(t, tt.wantIDs, antennaIDs(editor.draft))
			if tt.wantError == "" {
				assert.Empty(t, editor.err)
				assert.Equal(t, antennaEditorModeList, editor.mode)
			} else {
				assert.Contains(t, strings.ToLower(editor.err), tt.wantError)
				assert.Equal(t, antennaEditorModeForm, editor.mode)
			}
		})
	}
}

func TestAntennaEditorModel_FormProtocolSelector(t *testing.T) {
	editor := newAntennaEditorModel(testEditorAntennas())
	editor.startAdd()
	editor.form.protocol = config.ProtocolGeneric
	editor.formCursor = antennaFormFieldProtocol

	editor = editor.Update(tea.KeyMsg{Type: tea.KeyRight})
	require.Equal(t, config.ProtocolZebra, editor.form.protocol)

	editor = editor.Update(tea.KeyMsg{Type: tea.KeyLeft})
	assert.Equal(t, config.ProtocolGeneric, editor.form.protocol)
}

func TestSettingsScreen_AntennaEditorStagedDrafts(t *testing.T) {
	m := NewSettingsScreen(&config.GatewayConfig{
		GatewayID: "gateway",
		CloudURL:  "wss://example.com",
		Antennas:  testEditorAntennas(),
	})

	m.antennaEditor.startAdd()
	m.antennaEditor.form = antennaForm{id: "packing", ip: "192.168.1.20", port: "9090", enabled: true, zone: "packing", protocol: config.ProtocolZebra}
	m.antennaEditor = m.antennaEditor.commitForm()
	m.hasChanges = true

	result := m.GetConfig()

	require.Len(t, result.Antennas, 3)
	assert.Equal(t, []string{"dock", "exit", "packing"}, antennaIDs(result.Antennas))
	assert.Len(t, m.config.Antennas, 2, "staged edits must not mutate the source config before save")
}

func TestSettingsScreen_SetConfigResetsAntennaDrafts(t *testing.T) {
	m := NewSettingsScreen(&config.GatewayConfig{GatewayID: "gateway", Antennas: testEditorAntennas()})
	m.antennaEditor.startAdd()
	m.antennaEditor.form = antennaForm{id: "packing", ip: "192.168.1.20", port: "9090", enabled: true, zone: "packing", protocol: config.ProtocolZebra}
	m.antennaEditor = m.antennaEditor.commitForm()
	m.hasChanges = true

	m.SetConfig(&config.GatewayConfig{GatewayID: "gateway", Antennas: []config.AntennaConfig{{ID: "fresh", IP: "10.0.0.1", Port: 8080, Enabled: true, Protocol: config.ProtocolGeneric}}})

	assert.False(t, m.HasChanges())
	assert.Equal(t, []string{"fresh"}, antennaIDs(m.antennaEditor.draft))
}

func TestSettingsScreen_AntennaEditorViewAndHelp(t *testing.T) {
	m := NewSettingsScreen(&config.GatewayConfig{GatewayID: "gateway", Antennas: testEditorAntennas()})
	m.cursor = requireFieldIndex(t, m, "antenna_protocol:dock")

	view := m.View()

	assert.Contains(t, view, "Antenna Editor")
	assert.Contains(t, view, "dock")
	assert.Contains(t, view, "a add")
	assert.Contains(t, view, "e/enter edit")
	assert.Contains(t, view, "d delete")
}

func TestSettingsScreen_AntennaEditorFormViewShowsActiveFieldAndMode(t *testing.T) {
	tests := []struct {
		name          string
		setup         func(*SettingsScreenModel)
		activeField   string
		inactiveField string
		wantStatus    string
	}{
		{
			name: "new antenna highlights active id field",
			setup: func(m *SettingsScreenModel) {
				m.antennaEditor.startAdd()
				m.antennaEditor.formCursor = antennaFormFieldID
			},
			activeField:   "▸ ID:",
			inactiveField: "  IP:",
			wantStatus:    "Editing new antenna",
		},
		{
			name: "existing antenna names edited id and highlights active ip field",
			setup: func(m *SettingsScreenModel) {
				m.antennaEditor.cursor = 1
				m.antennaEditor.startEdit()
				m.antennaEditor.formCursor = antennaFormFieldIP
			},
			activeField:   "▸ IP:",
			inactiveField: "  ID:",
			wantStatus:    "Editing antenna exit",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewSettingsScreen(&config.GatewayConfig{GatewayID: "gateway", Antennas: testEditorAntennas()})
			tt.setup(&m)

			view := m.View()

			assert.Contains(t, view, tt.wantStatus)
			assert.Contains(t, view, tt.activeField)
			assert.Contains(t, view, tt.inactiveField)
		})
	}
}

func TestSettingsScreen_AntennaEditorFormActiveRowUsesSettingsSelectedTreatment(t *testing.T) {
	m := NewSettingsScreen(&config.GatewayConfig{GatewayID: "gateway", Antennas: testEditorAntennas()})
	m.antennaEditor.startEdit()
	m.antennaEditor.formCursor = antennaFormFieldIP

	activeRow := m.renderAntennaFormField(antennaFormFieldIP, "IP", m.antennaEditor.form.ip)
	inactiveRow := m.renderAntennaFormField(antennaFormFieldID, "ID", m.antennaEditor.form.id)

	require.Contains(t, activeRow, "▸ IP:")
	assert.Contains(t, activeRow, m.antennaEditor.form.ip)
	assert.NotContains(t, activeRow, "> IP:")
	assert.Equal(t, m.styles.Selected.Render("▸ IP: "+m.antennaEditor.form.ip)+"\n", activeRow)
	assert.Equal(t, "  ID: "+m.antennaEditor.form.id+"\n", inactiveRow)
	assert.NotContains(t, inactiveRow, "▸")

	view := m.View()
	assert.Contains(t, view, "↑/k previous • ↓/j next • type edit • ←/→ cycle protocol • space toggle/cycle • enter save • esc cancel")
}

func TestSettingsScreen_AntennaEditorContextualHelp(t *testing.T) {
	tests := []struct {
		name     string
		config   *config.GatewayConfig
		setup    func(*SettingsScreenModel)
		wantHelp string
	}{
		{
			name:     "empty list shows minimal add help",
			config:   &config.GatewayConfig{GatewayID: "gateway"},
			wantHelp: "a add antenna • Esc back",
		},
		{
			name:     "list shows only antenna list actions",
			config:   &config.GatewayConfig{GatewayID: "gateway", Antennas: testEditorAntennas()},
			wantHelp: "↑/k up • ↓/j down • a add antenna • e/enter edit • d delete • Esc back",
		},
		{
			name:   "delete confirmation shows only confirmation actions",
			config: &config.GatewayConfig{GatewayID: "gateway", Antennas: testEditorAntennas()},
			setup: func(m *SettingsScreenModel) {
				m.antennaEditor.mode = antennaEditorModeDeleteConfirm
			},
			wantHelp: "y confirm • n/esc cancel",
		},
		{
			name:   "form shows focused edit actions",
			config: &config.GatewayConfig{GatewayID: "gateway", Antennas: testEditorAntennas()},
			setup: func(m *SettingsScreenModel) {
				m.antennaEditor.startAdd()
			},
			wantHelp: "↑/k previous • ↓/j next • type edit • ←/→ cycle protocol • space toggle/cycle • enter save • esc cancel",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewSettingsScreen(tt.config)
			if len(tt.config.Antennas) > 0 {
				m.cursor = requireFieldIndex(t, m, "antenna_protocol:dock")
			}
			if tt.setup != nil {
				tt.setup(&m)
			}

			assert.Equal(t, tt.wantHelp, m.renderHelp())
		})
	}
}

func TestSettingsScreen_RoutesAntennaEditorListKeys(t *testing.T) {
	tests := []struct {
		name               string
		key                tea.KeyMsg
		wantSettingsCursor int
		wantAntennaCursor  int
		wantEditorMode     antennaEditorMode
	}{
		{
			name:               "down arrow moves antenna selection instead of settings cursor",
			key:                tea.KeyMsg{Type: tea.KeyDown},
			wantSettingsCursor: 0,
			wantAntennaCursor:  1,
			wantEditorMode:     antennaEditorModeList,
		},
		{
			name:               "j moves antenna selection instead of settings cursor",
			key:                tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")},
			wantSettingsCursor: 0,
			wantAntennaCursor:  1,
			wantEditorMode:     antennaEditorModeList,
		},
		{
			name:               "up arrow wraps antenna selection instead of settings cursor",
			key:                tea.KeyMsg{Type: tea.KeyUp},
			wantSettingsCursor: 0,
			wantAntennaCursor:  1,
			wantEditorMode:     antennaEditorModeList,
		},
		{
			name:               "k wraps antenna selection instead of settings cursor",
			key:                tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")},
			wantSettingsCursor: 0,
			wantAntennaCursor:  1,
			wantEditorMode:     antennaEditorModeList,
		},
		{
			name:               "enter opens antenna edit form instead of settings field edit",
			key:                tea.KeyMsg{Type: tea.KeyEnter},
			wantSettingsCursor: 0,
			wantAntennaCursor:  0,
			wantEditorMode:     antennaEditorModeForm,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewSettingsScreen(&config.GatewayConfig{GatewayID: "gateway", Antennas: testEditorAntennas()})
			m.cursor = requireFieldIndex(t, m, "antenna_protocol:dock")
			tt.wantSettingsCursor = m.cursor

			newModel, _ := m.Update(tt.key)
			m = newModel.(SettingsScreenModel)

			require.False(t, m.editing, "settings scalar edit mode must not consume antenna editor keys")
			assert.Equal(t, tt.wantSettingsCursor, m.cursor)
			assert.Equal(t, tt.wantAntennaCursor, m.antennaEditor.cursor)
			assert.Equal(t, tt.wantEditorMode, m.antennaEditor.mode)
		})
	}
}

func testEditorAntennas() []config.AntennaConfig {
	return []config.AntennaConfig{
		{ID: "dock", IP: "192.168.1.10", Port: 8080, Enabled: true, Zone: "inbound", Protocol: config.ProtocolGeneric},
		{ID: "exit", IP: "192.168.1.11", Port: 8081, Enabled: false, Zone: "outbound", Protocol: config.ProtocolZebra},
	}
}

func antennaIDs(antennas []config.AntennaConfig) []string {
	ids := make([]string, 0, len(antennas))
	for _, antenna := range antennas {
		ids = append(ids, antenna.ID)
	}
	return ids
}
