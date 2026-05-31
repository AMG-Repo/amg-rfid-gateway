package tui

import (
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/amg-rfid/amg-rfid-gateway/internal/config"
	"github.com/amg-rfid/amg-rfid-gateway/internal/tui/screens"
)

func TestApp_SettingsResizeReachesSettingsScreen(t *testing.T) {
	cfg := validTUITestConfig()
	app := &App{settings: screens.NewSettingsScreen(cfg)}

	newModel, _ := app.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	app = newModel.(*App)

	view := app.settings.View()
	assert.NotContains(t, view, "Loading")
}

func TestApp_SettingsProtocolEditRoundTripsAfterSave(t *testing.T) {
	cfg := validTUITestConfig()
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, cfg.SaveToYAML(configPath))

	app := &App{
		currentScreen: ScreenSettings,
		settings:      screens.NewSettingsScreen(cfg),
		cfg:           cfg,
		configPath:    configPath,
	}

	for range 8 {
		newModel, _ := app.Update(tea.KeyMsg{Type: tea.KeyDown})
		app = newModel.(*App)
	}

	newModel, _ := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	app = newModel.(*App)
	for range len("generic") {
		newModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyBackspace})
		app = newModel.(*App)
	}
	newModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("zebra")})
	app = newModel.(*App)
	newModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	app = newModel.(*App)

	loaded, err := config.LoadFromYAML(configPath)
	require.NoError(t, err)
	require.Len(t, loaded.Antennas, 2)
	assert.Equal(t, config.ProtocolZebra, loaded.Antennas[0].Protocol)
	assert.Equal(t, config.ProtocolGeneric, loaded.Antennas[1].Protocol)
	assert.Equal(t, "192.168.1.10", loaded.Antennas[0].IP)
	assert.Equal(t, "entrada", loaded.Antennas[0].Zone)
	assert.Equal(t, "gateway-original", loaded.GatewayID)

	app.settings.SetSize(100, 25)
	assert.Contains(t, app.settings.View(), "zebra")
}

func validTUITestConfig() *config.GatewayConfig {
	maxPending := 10000
	warning := 1000
	return &config.GatewayConfig{
		GatewayID:               "gateway-original",
		CompanyID:               "company-original",
		CloudURL:                "wss://example.com",
		JWTSecret:               "secret",
		SyncInterval:            30,
		BatchSize:               100,
		MaxRetries:              5,
		HealthPort:              8080,
		DataPath:                "/tmp/amg-rfid-gateway-test",
		ListenMode:              "auto",
		WebEnabled:              true,
		WebPort:                 9090,
		WebAccessMode:           "local",
		WebListenAddr:           "127.0.0.1",
		WebAuthToken:            "token",
		VPSAPIURL:               "https://vps.example.com",
		MaxPendingConfirmations: &maxPending,
		PendingWarningThreshold: &warning,
		LogLevel:                "info",
		Antennas: []config.AntennaConfig{
			{ID: "dock", IP: "192.168.1.10", Port: 8080, Enabled: true, Zone: "entrada", Protocol: config.ProtocolGeneric},
			{ID: "exit", IP: "192.168.1.11", Port: 8081, Enabled: false, Zone: "salida", Protocol: config.ProtocolGeneric},
		},
	}
}
