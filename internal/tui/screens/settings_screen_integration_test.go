package screens

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/amg-rfid/amg-rfid-gateway/internal/config"
)

func TestSettingsScreenIntegration_AddEditDeleteFlowWithTeatest(t *testing.T) {
	m := newProtocolSettingsModel(config.ProtocolGeneric)
	model := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(120, 40))
	t.Cleanup(func() { _ = model.Quit() })

	sendRunes(model, "a")
	sendRunes(model, "ant2")
	sendKey(model, tea.KeyTab)
	sendRunes(model, "10.0.0.2")
	sendKey(model, tea.KeyTab)
	sendRunes(model, "8088")
	sendKey(model, tea.KeyTab)
	sendKey(model, tea.KeyTab)
	sendRunes(model, "zone")
	sendKey(model, tea.KeyTab)
	sendKey(model, tea.KeyRight)
	sendKey(model, tea.KeyUp)
	sendKey(model, tea.KeyEnter)

	sendRunes(model, "e")
	sendKey(model, tea.KeyTab)
	sendKey(model, tea.KeyTab)
	sendKey(model, tea.KeyTab)
	sendKey(model, tea.KeyTab)
	sendRunes(model, "-x")
	sendKey(model, tea.KeyEnter)

	sendRunes(model, "d")
	sendRunes(model, "y")

	current := finalSettingsModel(t, model)
	require.Len(t, current.GetConfig().Antennas, 1)
	assert.Equal(t, "dock", current.GetConfig().Antennas[0].ID)
}

func TestSettingsScreenIntegration_UnsavedExitStayWithTeatest(t *testing.T) {
	m := newProtocolSettingsModel(config.ProtocolGeneric)
	m.cursor = requireFieldIndex(t, m, "gateway_id")
	model := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(120, 40))
	t.Cleanup(func() { _ = model.Quit() })

	sendKey(model, tea.KeyEnter)
	sendRunes(model, "-changed")
	sendKey(model, tea.KeyEnter)

	sendKey(model, tea.KeyEsc)
	sendRunes(model, "n")

	current := finalSettingsModel(t, model)
	assert.True(t, current.HasChanges())
	assert.Equal(t, settingsPromptNone, current.promptMode)
	assert.Equal(t, "test-changed", current.GetConfig().GatewayID)
}

func sendKey(tm *teatest.TestModel, key tea.KeyType) {
	tm.Send(tea.KeyMsg{Type: key})
	time.Sleep(20 * time.Millisecond)
}

func sendRunes(tm *teatest.TestModel, value string) {
	for _, r := range value {
		tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		time.Sleep(20 * time.Millisecond)
	}
}

func finalSettingsModel(t *testing.T, tm *teatest.TestModel) SettingsScreenModel {
	t.Helper()
	require.NoError(t, tm.Quit())
	result := tm.FinalModel(t, teatest.WithFinalTimeout(100*time.Millisecond))
	model, ok := result.(SettingsScreenModel)
	require.Truef(t, ok, "expected SettingsScreenModel, got %T", result)
	return model
}
