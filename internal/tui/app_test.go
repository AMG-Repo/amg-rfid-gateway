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
	app = saveSettingsChanges(t, app, true)

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

func TestApp_SettingsExplicitSaveControlsPersistence(t *testing.T) {
	tests := []struct {
		name              string
		edit              func(t *testing.T, app *App) *App
		save              bool
		wantSaveCommand   bool
		wantGatewayID     string
		wantScreenGateway string
	}{
		{
			name: "editing a valid field marks dirty without persisting",
			edit: func(t *testing.T, app *App) *App {
				return editSettingsField(t, app, 0, "gateway-edited")
			},
			wantGatewayID:     "gateway-original",
			wantScreenGateway: "gateway-edited",
		},
		{
			name: "save intent plus confirmation persists edited field",
			edit: func(t *testing.T, app *App) *App {
				return editSettingsField(t, app, 0, "gateway-saved")
			},
			save:              true,
			wantSaveCommand:   true,
			wantGatewayID:     "gateway-saved",
			wantScreenGateway: "gateway-saved",
		},
		{
			name: "invalid edit cannot be committed or persisted",
			edit: func(t *testing.T, app *App) *App {
				return attemptInvalidSettingsFieldEdit(t, app, 2, "http://not-secure.example.com")
			},
			save:              true,
			wantSaveCommand:   false,
			wantGatewayID:     "gateway-original",
			wantScreenGateway: "gateway-original",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validTUITestConfig()
			configPath := filepath.Join(t.TempDir(), "config.yaml")
			require.NoError(t, cfg.SaveToYAML(configPath))

			app := &App{
				currentScreen: ScreenSettings,
				settings:      screens.NewSettingsScreen(cfg),
				cfg:           cfg,
				configPath:    configPath,
			}

			app = tt.edit(t, app)

			if tt.save {
				app = saveSettingsChanges(t, app, tt.wantSaveCommand)
			}

			loaded, err := config.LoadFromYAML(configPath)
			require.NoError(t, err)
			assert.Equal(t, tt.wantGatewayID, loaded.GatewayID)
			assert.Equal(t, tt.wantScreenGateway, app.settings.GetConfig().GatewayID)
		})
	}
}

func TestApp_SettingsUnsavedExitProtection(t *testing.T) {
	tests := []struct {
		name          string
		decisionKey   tea.KeyMsg
		wantScreen    Screen
		wantPersisted string
		wantVisible   string
	}{
		{
			name:          "save confirms changes and leaves settings",
			decisionKey:   tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")},
			wantScreen:    ScreenMainMenu,
			wantPersisted: "gateway-unsaved",
			wantVisible:   "gateway-unsaved",
		},
		{
			name:          "discard leaves settings without saving",
			decisionKey:   tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")},
			wantScreen:    ScreenMainMenu,
			wantPersisted: "gateway-original",
			wantVisible:   "gateway-original",
		},
		{
			name:          "stay cancels navigation and keeps unsaved edit visible",
			decisionKey:   tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")},
			wantScreen:    ScreenSettings,
			wantPersisted: "gateway-original",
			wantVisible:   "gateway-unsaved",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validTUITestConfig()
			configPath := filepath.Join(t.TempDir(), "config.yaml")
			require.NoError(t, cfg.SaveToYAML(configPath))

			app := &App{
				currentScreen: ScreenSettings,
				settings:      screens.NewSettingsScreen(cfg),
				cfg:           cfg,
				configPath:    configPath,
			}

			app = editSettingsField(t, app, 0, "gateway-unsaved")
			newModel, cmd := app.Update(tea.KeyMsg{Type: tea.KeyEsc})
			app = newModel.(*App)
			require.Nil(t, cmd)
			require.Equal(t, ScreenSettings, app.currentScreen)

			newModel, cmd = app.Update(tt.decisionKey)
			app = newModel.(*App)
			if cmd != nil {
				newModel, _ = app.Update(cmd())
				app = newModel.(*App)
			}

			loaded, err := config.LoadFromYAML(configPath)
			require.NoError(t, err)
			assert.Equal(t, tt.wantPersisted, loaded.GatewayID)
			assert.Equal(t, tt.wantScreen, app.currentScreen)
			assert.Equal(t, tt.wantVisible, app.settings.GetConfig().GatewayID)
		})
	}
}

func TestApp_SettingsUnsavedExitSaveValidationFailurePreservesEdits(t *testing.T) {
	cfg := validTUITestConfig()
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, cfg.SaveToYAML(configPath))

	app := &App{
		currentScreen: ScreenSettings,
		settings:      screens.NewSettingsScreen(cfg),
		cfg:           cfg,
		configPath:    configPath,
	}

	app = editSettingsField(t, app, 1, "")
	newModel, cmd := app.Update(tea.KeyMsg{Type: tea.KeyEsc})
	app = newModel.(*App)
	require.Nil(t, cmd)
	require.Equal(t, ScreenSettings, app.currentScreen)

	newModel, cmd = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	app = newModel.(*App)
	require.NotNil(t, cmd)

	newModel, _ = app.Update(cmd())
	app = newModel.(*App)

	loaded, err := config.LoadFromYAML(configPath)
	require.NoError(t, err)
	assert.Equal(t, "company-original", loaded.CompanyID)
	assert.Equal(t, ScreenSettings, app.currentScreen)
	assert.Empty(t, app.settings.GetConfig().CompanyID)
}

func TestApp_SettingsEscWithoutUnsavedChangesGoesBack(t *testing.T) {
	cfg := validTUITestConfig()
	app := &App{
		currentScreen: ScreenSettings,
		settings:      screens.NewSettingsScreen(cfg),
		cfg:           cfg,
	}

	newModel, cmd := app.Update(tea.KeyMsg{Type: tea.KeyEsc})
	app = newModel.(*App)
	if cmd != nil {
		newModel, _ = app.Update(cmd())
		app = newModel.(*App)
	}

	assert.Equal(t, ScreenMainMenu, app.currentScreen)
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

func editSettingsField(t *testing.T, app *App, field int, value string) *App {
	t.Helper()
	app.settings = screens.NewSettingsScreen(app.cfg)
	app.settings.SetSize(100, 25)
	for range field {
		newModel, _ := app.Update(tea.KeyMsg{Type: tea.KeyDown})
		app = newModel.(*App)
	}
	newModel, _ := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	app = newModel.(*App)
	for range len(currentSettingsFieldValue(t, app, field)) {
		newModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyBackspace})
		app = newModel.(*App)
	}
	newModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(value)})
	app = newModel.(*App)
	newModel, _ = app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	app = newModel.(*App)
	return app
}

func currentSettingsFieldValue(t *testing.T, app *App, field int) string {
	t.Helper()
	cfg := app.settings.GetConfig()
	switch field {
	case 0:
		return cfg.GatewayID
	case 1:
		return cfg.CompanyID
	case 2:
		return cfg.CloudURL
	case 3:
		return cfg.WebAccessMode
	case 4:
		return cfg.WebAuthToken
	case 5:
		return cfg.LogLevel
	default:
		t.Fatalf("field %d is not supported by test helper", field)
		return ""
	}
}

func attemptInvalidSettingsFieldEdit(t *testing.T, app *App, field int, value string) *App {
	t.Helper()
	return editSettingsField(t, app, field, value)
}

func saveSettingsChanges(t *testing.T, app *App, wantSaveCommand bool) *App {
	t.Helper()
	newModel, _ := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	app = newModel.(*App)
	newModel, cmd := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	app = newModel.(*App)
	if !wantSaveCommand {
		require.Nil(t, cmd)
		return app
	}
	require.NotNil(t, cmd)
	newModel, _ = app.Update(cmd())
	app = newModel.(*App)
	return app
}
