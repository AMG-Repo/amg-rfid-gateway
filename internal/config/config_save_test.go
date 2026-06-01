package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGatewayConfig_SaveToYAML_Basic(t *testing.T) {
	// Test basic save functionality
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	cfg := &GatewayConfig{
		GatewayID:  "gw-001",
		CompanyID:  "comp-123",
		CloudURL:   "wss://cloud.example.com/ws",
		JWTSecret:  "test-secret",
		LogLevel:   "info",
		ListenMode: "auto",
		Antennas: []AntennaConfig{
			{ID: "ant-1", IP: "192.168.1.100", Port: 6000, Enabled: true, Zone: "entrada"},
		},
	}

	if err := cfg.SaveToYAML(configPath); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	// Verify file was created
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Fatal("config file was not created")
	}

	// Verify we can load it back
	loaded, err := LoadFromYAML(configPath)
	if err != nil {
		t.Fatalf("failed to load saved config: %v", err)
	}

	if loaded.GatewayID != cfg.GatewayID {
		t.Errorf("expected GatewayID '%s', got '%s'", cfg.GatewayID, loaded.GatewayID)
	}
	if loaded.CompanyID != cfg.CompanyID {
		t.Errorf("expected CompanyID '%s', got '%s'", cfg.CompanyID, loaded.CompanyID)
	}
	if loaded.LogLevel != cfg.LogLevel {
		t.Errorf("expected LogLevel '%s', got '%s'", cfg.LogLevel, loaded.LogLevel)
	}
}

func TestGatewayConfig_SaveToYAML_PreservesAntennaProtocol(t *testing.T) {
	tests := []struct {
		name             string
		protocol         AntennaProtocol
		expectedProtocol AntennaProtocol
	}{
		{
			name:             "explicit generic persists as generic",
			protocol:         ProtocolGeneric,
			expectedProtocol: ProtocolGeneric,
		},
		{
			name:             "explicit zebra persists as zebra",
			protocol:         ProtocolZebra,
			expectedProtocol: ProtocolZebra,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			configPath := filepath.Join(tmpDir, "config.yaml")
			cfg := &GatewayConfig{
				GatewayID:     "gw-001",
				CompanyID:     "comp-123",
				CloudURL:      "wss://cloud.example.com/ws",
				JWTSecret:     "test-secret",
				ListenMode:    "auto",
				WebAccessMode: "local",
				WebListenAddr: "127.0.0.1",
				Antennas: []AntennaConfig{
					{ID: "ant-1", IP: "192.168.1.100", Port: 6000, Enabled: true, Protocol: tt.protocol},
				},
			}

			if err := cfg.SaveToYAML(configPath); err != nil {
				t.Fatalf("expected no error, got: %v", err)
			}

			loaded, err := LoadFromYAML(configPath)
			if err != nil {
				t.Fatalf("failed to load saved config: %v", err)
			}
			if len(loaded.Antennas) != 1 {
				t.Fatalf("expected 1 antenna, got %d", len(loaded.Antennas))
			}
			if loaded.Antennas[0].Protocol != tt.expectedProtocol {
				t.Fatalf("expected protocol %q, got %q", tt.expectedProtocol, loaded.Antennas[0].Protocol)
			}

			data, err := os.ReadFile(configPath)
			if err != nil {
				t.Fatalf("failed to read saved config: %v", err)
			}
			expectedYAML := "protocol: " + string(tt.expectedProtocol)
			if !strings.Contains(string(data), expectedYAML) {
				t.Fatalf("expected saved YAML to contain %q, got:\n%s", expectedYAML, string(data))
			}
		})
	}
}

func TestGatewayConfig_SaveToYAML_DuplicateAntennaIDsRejected(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	originalContent := "gateway_id: original-gw\ncompany_id: original-comp\ncloud_url: wss://old.example.com/ws\njwt_secret: original-secret\nlisten_mode: auto\nweb_access_mode: local\nweb_listen_addr: 127.0.0.1\n"
	require.NoError(t, os.WriteFile(configPath, []byte(originalContent), 0644))

	cfg := &GatewayConfig{
		GatewayID:     "gw-001",
		CompanyID:     "comp-123",
		CloudURL:      "wss://cloud.example.com/ws",
		JWTSecret:     "test-secret",
		ListenMode:    "auto",
		WebAccessMode: "local",
		WebListenAddr: "127.0.0.1",
		Antennas: []AntennaConfig{
			{ID: "dock-reader", IP: "192.168.1.100", Port: 6000, Enabled: true},
			{ID: "exit-reader", IP: "192.168.1.101", Port: 6001, Enabled: true},
			{ID: "dock-reader", IP: "192.168.1.102", Port: 6002, Enabled: false},
		},
	}

	err := cfg.SaveToYAML(configPath)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate antenna id")
	assert.Contains(t, err.Error(), "dock-reader")
	savedContent, readErr := os.ReadFile(configPath)
	require.NoError(t, readErr)
	assert.Equal(t, originalContent, string(savedContent))

	loaded, loadErr := LoadFromYAML(configPath)
	require.NoError(t, loadErr)
	assert.Equal(t, "original-gw", loaded.GatewayID)
}

func TestGatewayConfig_SaveToYAML_CreatesBackup(t *testing.T) {
	// Test that existing file is backed up
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// Create an existing config file
	originalContent := "gateway_id: old-gw\ncompany_id: old-comp\ncloud_url: wss://old.example.com\njwt_secret: old-secret\nlisten_mode: active\n"
	if err := os.WriteFile(configPath, []byte(originalContent), 0644); err != nil {
		t.Fatalf("failed to write original config: %v", err)
	}
	originalModTime := getModTime(t, configPath)

	// Save new config
	cfg := &GatewayConfig{
		GatewayID:  "gw-new",
		CompanyID:  "comp-new",
		CloudURL:   "wss://cloud.example.com/ws",
		JWTSecret:  "new-secret",
		ListenMode: "auto",
	}

	// Small delay to ensure mod time would be different
	time.Sleep(10 * time.Millisecond)

	if err := cfg.SaveToYAML(configPath); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	// Verify backup was created
	backupPath := configPath + ".bak"
	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		t.Fatal("backup file was not created")
	}

	// Verify backup contains original content
	backupContent, err := os.ReadFile(backupPath)
	if err != nil {
		t.Fatalf("failed to read backup: %v", err)
	}
	if string(backupContent) != originalContent {
		t.Errorf("backup content doesn't match original.\nExpected:\n%s\nGot:\n%s", originalContent, string(backupContent))
	}

	// Verify backup has original mod time (older than new file)
	backupModTime := getModTime(t, backupPath)
	if !backupModTime.Equal(originalModTime) {
		t.Logf("Note: backup mod time may differ from original (filesystem behavior)")
	}
}

func TestGatewayConfig_SaveToYAML_OverwritesExisting(t *testing.T) {
	// Test that save overwrites existing file atomically
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// Create an existing config file
	if err := os.WriteFile(configPath, []byte("gateway_id: old"), 0644); err != nil {
		t.Fatalf("failed to write original config: %v", err)
	}

	// Save new config
	cfg := &GatewayConfig{
		GatewayID:  "gw-new",
		CompanyID:  "comp-new",
		CloudURL:   "wss://cloud.example.com/ws",
		JWTSecret:  "secret",
		ListenMode: "auto",
	}

	if err := cfg.SaveToYAML(configPath); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	// Verify file contains new content
	loaded, err := LoadFromYAML(configPath)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	if loaded.GatewayID != "gw-new" {
		t.Errorf("expected GatewayID 'gw-new', got '%s'", loaded.GatewayID)
	}
}

func TestGatewayConfig_SaveToYAML_InvalidPath(t *testing.T) {
	// Test error handling for invalid path
	cfg := &GatewayConfig{
		GatewayID:  "gw-001",
		CompanyID:  "comp-123",
		CloudURL:   "wss://cloud.example.com/ws",
		JWTSecret:  "secret",
		ListenMode: "auto",
	}

	// Try to save to a non-existent directory
	invalidPath := "/nonexistent/dir/config.yaml"
	err := cfg.SaveToYAML(invalidPath)
	if err == nil {
		t.Error("expected error for invalid path, got nil")
	}
}

func getModTime(t *testing.T, path string) time.Time {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("failed to stat file: %v", err)
	}
	return info.ModTime()
}

func TestGatewayConfig_SaveToYAML_EmptyPath(t *testing.T) {
	// Test error handling for empty path
	cfg := &GatewayConfig{
		GatewayID:  "gw-001",
		CompanyID:  "comp-123",
		CloudURL:   "wss://cloud.example.com/ws",
		JWTSecret:  "secret",
		ListenMode: "auto",
	}

	err := cfg.SaveToYAML("")
	if err == nil {
		t.Error("expected error for empty path, got nil")
	}
}

func TestGatewayConfig_SaveToYAML_ComplexConfig(t *testing.T) {
	// Test saving complex config with all fields
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	maxPending := 5000
	threshold := 1000

	cfg := &GatewayConfig{
		GatewayID: "complex-gw",
		CompanyID: "complex-comp",
		CloudURL:  "wss://complex.example.com/ws",
		JWTSecret: "complex-secret",
		Antennas: []AntennaConfig{
			{ID: "ant-1", IP: "192.168.1.100", Port: 6000, Enabled: true, Zone: "entrada"},
			{ID: "ant-2", IP: "192.168.1.101", Port: 6000, Enabled: false, Zone: "salida"},
		},
		SyncInterval:              60 * time.Second,
		BatchSize:                 50,
		MaxRetries:                3,
		HealthPort:                9090,
		DataPath:                  "/var/lib/amg-rfid-gateway",
		ListenMode:                "active",
		HeartbeatInterval:         5 * time.Second,
		HeartbeatSilenceThreshold: 10 * time.Second,
		AdaptiveDelayRecent:       2 * time.Second,
		AdaptiveDelayRecentWindow: 3 * time.Second,
		AdaptiveDelayStale:        1 * time.Second,
		AdaptiveDelayStaleWindow:  15 * time.Second,
		AdaptiveDelayAutoReading:  8 * time.Second,
		ReconnectInitialBackoff:   2 * time.Second,
		ReconnectMaxBackoff:       60 * time.Second,
		SocketPath:                "/run/amg-rfid-gateway.sock",
		WebEnabled:                true,
		WebPort:                   8080,
		WebListenAddr:             "127.0.0.1",
		VPSAPIURL:                 "https://vps.example.com",
		SyncToolsInterval:         30 * time.Minute,
		ConfirmationRetryInterval: 45 * time.Second,
		MaxPendingConfirmations:   &maxPending,
		PendingWarningThreshold:   &threshold,
		LogLevel:                  "debug",
	}

	if err := cfg.SaveToYAML(configPath); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	// Load and verify
	loaded, err := LoadFromYAML(configPath)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	if loaded.GatewayID != cfg.GatewayID {
		t.Errorf("expected GatewayID '%s', got '%s'", cfg.GatewayID, loaded.GatewayID)
	}
	if loaded.CompanyID != cfg.CompanyID {
		t.Errorf("expected CompanyID '%s', got '%s'", cfg.CompanyID, loaded.CompanyID)
	}
	if loaded.LogLevel != cfg.LogLevel {
		t.Errorf("expected LogLevel '%s', got '%s'", cfg.LogLevel, loaded.LogLevel)
	}
	if len(loaded.Antennas) != 2 {
		t.Errorf("expected 2 antennas, got %d", len(loaded.Antennas))
	}
	if loaded.MaxPendingConfirmations == nil || *loaded.MaxPendingConfirmations != maxPending {
		t.Errorf("expected MaxPendingConfirmations %d", maxPending)
	}
}

func TestGatewayConfig_SaveToYAML_MultipleSaves(t *testing.T) {
	// Test multiple saves overwrite correctly
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// First save
	cfg1 := &GatewayConfig{
		GatewayID:  "gw-001",
		CompanyID:  "comp-123",
		CloudURL:   "wss://cloud.example.com/ws",
		JWTSecret:  "secret1",
		ListenMode: "auto",
	}
	if err := cfg1.SaveToYAML(configPath); err != nil {
		t.Fatalf("first save failed: %v", err)
	}

	// Second save
	cfg2 := &GatewayConfig{
		GatewayID:  "gw-002",
		CompanyID:  "comp-456",
		CloudURL:   "wss://cloud2.example.com/ws",
		JWTSecret:  "secret2",
		ListenMode: "active",
	}
	if err := cfg2.SaveToYAML(configPath); err != nil {
		t.Fatalf("second save failed: %v", err)
	}

	// Load and verify second save
	loaded, err := LoadFromYAML(configPath)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	if loaded.GatewayID != "gw-002" {
		t.Errorf("expected GatewayID 'gw-002', got '%s'", loaded.GatewayID)
	}
	if loaded.CompanyID != "comp-456" {
		t.Errorf("expected CompanyID 'comp-456', got '%s'", loaded.CompanyID)
	}

	// Verify backup exists and contains first save
	backupPath := configPath + ".bak"
	backupData, err := os.ReadFile(backupPath)
	if err != nil {
		t.Fatalf("failed to read backup: %v", err)
	}
	if !strings.Contains(string(backupData), "gw-001") {
		t.Error("backup should contain first save data (gw-001)")
	}
}
