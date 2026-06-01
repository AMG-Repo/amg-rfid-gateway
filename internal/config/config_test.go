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

func TestGatewayConfig_Validate_EmptyGatewayID(t *testing.T) {
	cfg := &GatewayConfig{
		GatewayID: "", // EMPTY - should fail
		CompanyID: "comp-123",
		CloudURL:  "wss://cloud.example.com",
	}

	err := cfg.Validate()
	if err == nil {
		t.Error("expected error for empty gateway_id, got nil")
	}
}

func TestGatewayConfig_Validate_EmptyCompanyID(t *testing.T) {
	cfg := &GatewayConfig{
		GatewayID: "gw-001",
		CompanyID: "", // EMPTY - should fail
		CloudURL:  "wss://cloud.example.com",
	}

	err := cfg.Validate()
	if err == nil {
		t.Error("expected error for empty company_id, got nil")
	}
}

func TestGatewayConfig_Validate_EmptyCloudURL(t *testing.T) {
	cfg := &GatewayConfig{
		GatewayID: "gw-001",
		CompanyID: "comp-123",
		CloudURL:  "", // EMPTY - should fail
	}

	err := cfg.Validate()
	if err == nil {
		t.Error("expected error for empty cloud_url, got nil")
	}
}

func TestGatewayConfig_Validate_InvalidCloudURL(t *testing.T) {
	cfg := &GatewayConfig{
		GatewayID: "gw-001",
		CompanyID: "comp-123",
		CloudURL:  "not-a-valid-url", // INVALID
	}

	err := cfg.Validate()
	if err == nil {
		t.Error("expected error for invalid cloud_url, got nil")
	}
}

func TestGatewayConfig_Validate_ValidConfig(t *testing.T) {
	cfg := &GatewayConfig{
		GatewayID: "gw-001",
		CompanyID: "comp-123",
		CloudURL:  "wss://cloud.example.com/ws",
		JWTSecret: "secret-key",
		Antennas: []AntennaConfig{
			{ID: "ant-1", IP: "192.168.1.100", Port: 6000, Enabled: true},
		},
	}

	// Apply defaults to set ListenMode and other new fields
	cfg.ApplyDefaults()

	err := cfg.Validate()
	if err != nil {
		t.Errorf("expected valid config, got error: %v", err)
	}
}

func TestAntennaConfig_Validate_EmptyID(t *testing.T) {
	ant := AntennaConfig{
		ID:      "", // EMPTY - should fail
		IP:      "192.168.1.100",
		Port:    6000,
		Enabled: true,
	}

	err := ant.Validate()
	if err == nil {
		t.Error("expected error for empty antenna id, got nil")
	}
}

func TestAntennaConfig_Validate_EmptyIP(t *testing.T) {
	ant := AntennaConfig{
		ID:      "ant-1",
		IP:      "", // EMPTY - should fail
		Port:    6000,
		Enabled: true,
	}

	err := ant.Validate()
	if err == nil {
		t.Error("expected error for empty antenna ip, got nil")
	}
}

func TestAntennaConfig_Validate_InvalidPort(t *testing.T) {
	ant := AntennaConfig{
		ID:      "ant-1",
		IP:      "192.168.1.100",
		Port:    0, // INVALID - should fail
		Enabled: true,
	}

	err := ant.Validate()
	if err == nil {
		t.Error("expected error for invalid port, got nil")
	}
}

func TestLoadFromYAML_ValidFile(t *testing.T) {
	content := `
gateway_id: "gw-001"
company_id: "comp-123"
cloud_url: "wss://cloud.example.com/ws"
jwt_secret: "test-secret-key"
antennas:
  - id: "ant-1"
    ip: "192.168.1.100"
    port: 6000
    enabled: true
  - id: "ant-2"
    ip: "192.168.1.101"
    port: 6000
    enabled: false
`
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	cfg, err := LoadFromYAML(configPath)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if cfg.GatewayID != "gw-001" {
		t.Errorf("expected gateway_id 'gw-001', got '%s'", cfg.GatewayID)
	}
	if cfg.CompanyID != "comp-123" {
		t.Errorf("expected company_id 'comp-123', got '%s'", cfg.CompanyID)
	}
	if len(cfg.Antennas) != 2 {
		t.Errorf("expected 2 antennas, got %d", len(cfg.Antennas))
	}
}

func TestLoadFromYAML_AntennaProtocolDefaults(t *testing.T) {
	tests := []struct {
		name             string
		antennaYAML      string
		expectedProtocol AntennaProtocol
	}{
		{
			name: "missing protocol defaults to generic",
			antennaYAML: `  - id: "ant-1"
    ip: "192.168.1.100"
    port: 6000
    enabled: true`,
			expectedProtocol: ProtocolGeneric,
		},
		{
			name: "empty protocol defaults to generic",
			antennaYAML: `  - id: "ant-1"
    ip: "192.168.1.100"
    port: 6000
    enabled: true
    protocol: ""`,
			expectedProtocol: ProtocolGeneric,
		},
		{
			name: "explicit zebra remains zebra",
			antennaYAML: `  - id: "ant-1"
    ip: "192.168.1.100"
    port: 6000
    enabled: true
    protocol: "zebra"`,
			expectedProtocol: ProtocolZebra,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content := `
gateway_id: "gw-001"
company_id: "comp-123"
cloud_url: "wss://cloud.example.com/ws"
jwt_secret: "test-secret-key"
antennas:
` + tt.antennaYAML + "\n"
			tmpDir := t.TempDir()
			configPath := filepath.Join(tmpDir, "config.yaml")
			if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
				t.Fatalf("failed to write test config: %v", err)
			}

			cfg, err := LoadFromYAML(configPath)
			if err != nil {
				t.Fatalf("expected no error, got: %v", err)
			}
			if len(cfg.Antennas) != 1 {
				t.Fatalf("expected 1 antenna, got %d", len(cfg.Antennas))
			}
			if cfg.Antennas[0].Protocol != tt.expectedProtocol {
				t.Fatalf("expected protocol %q, got %q", tt.expectedProtocol, cfg.Antennas[0].Protocol)
			}
		})
	}
}

func TestGatewayConfig_Validate_AntennaProtocol(t *testing.T) {
	tests := []struct {
		name        string
		protocol    AntennaProtocol
		expectError bool
	}{
		{name: "generic protocol is supported", protocol: ProtocolGeneric},
		{name: "zebra protocol is supported", protocol: ProtocolZebra},
		{name: "unsupported protocol fails validation", protocol: AntennaProtocol("alien"), expectError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &GatewayConfig{
				GatewayID:     "gw-001",
				CompanyID:     "comp-123",
				CloudURL:      "wss://cloud.example.com/ws",
				JWTSecret:     "secret-key",
				ListenMode:    "auto",
				WebAccessMode: "local",
				WebListenAddr: "127.0.0.1",
				Antennas: []AntennaConfig{
					{ID: "ant-1", IP: "192.168.1.100", Port: 6000, Enabled: true, Protocol: tt.protocol},
				},
			}

			err := cfg.Validate()
			if tt.expectError {
				if err == nil {
					t.Fatal("expected validation error, got nil")
				}
				if !strings.Contains(err.Error(), "unsupported antenna protocol") {
					t.Fatalf("expected unsupported protocol error, got %q", err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("expected no validation error, got: %v", err)
			}
		})
	}
}

func TestGatewayConfig_Validate_DuplicateAntennaIDs(t *testing.T) {
	tests := []struct {
		name     string
		antennas []AntennaConfig
	}{
		{
			name: "adjacent duplicate antenna ids are rejected",
			antennas: []AntennaConfig{
				{ID: "dock-reader", IP: "192.168.1.100", Port: 6000, Enabled: true},
				{ID: "dock-reader", IP: "192.168.1.101", Port: 6001, Enabled: true},
			},
		},
		{
			name: "non-adjacent duplicate antenna ids are rejected",
			antennas: []AntennaConfig{
				{ID: "dock-reader", IP: "192.168.1.100", Port: 6000, Enabled: true},
				{ID: "exit-reader", IP: "192.168.1.101", Port: 6001, Enabled: true},
				{ID: "dock-reader", IP: "192.168.1.102", Port: 6002, Enabled: false},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validGatewayConfigForTest()
			cfg.Antennas = tt.antennas

			err := cfg.Validate()

			require.Error(t, err)
			assert.Contains(t, err.Error(), "duplicate antenna id")
			assert.Contains(t, err.Error(), "dock-reader")
		})
	}
}

func TestGatewayConfig_Validate_DuplicateAntennaIDsErrorIsDeterministic(t *testing.T) {
	cfg := validGatewayConfigForTest()
	cfg.Antennas = []AntennaConfig{
		{ID: "first-duplicate", IP: "192.168.1.100", Port: 6000, Enabled: true},
		{ID: "second-duplicate", IP: "192.168.1.101", Port: 6001, Enabled: true},
		{ID: "first-duplicate", IP: "192.168.1.102", Port: 6002, Enabled: false},
		{ID: "second-duplicate", IP: "192.168.1.103", Port: 6003, Enabled: false},
	}

	err := cfg.Validate()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "first-duplicate")
	assert.NotContains(t, err.Error(), "second-duplicate")
}

func validGatewayConfigForTest() *GatewayConfig {
	return &GatewayConfig{
		GatewayID:     "gw-001",
		CompanyID:     "comp-123",
		CloudURL:      "wss://cloud.example.com/ws",
		JWTSecret:     "secret-key",
		ListenMode:    "auto",
		WebAccessMode: "local",
		WebListenAddr: "127.0.0.1",
		Antennas: []AntennaConfig{
			{ID: "ant-1", IP: "192.168.1.100", Port: 6000, Enabled: true},
		},
	}
}

func TestLoadFromYAML_FileNotFound(t *testing.T) {
	_, err := LoadFromYAML("/nonexistent/path/config.yaml")
	if err == nil {
		t.Error("expected error for missing file, got nil")
	}
}

func TestLoadFromYAML_LegacyLANConfigRequiresMigration(t *testing.T) {
	content := `
gateway_id: "gw-legacy"
company_id: "comp-legacy"
cloud_url: "wss://cloud.example.com/ws"
jwt_secret: "legacy-secret"
web_listen_addr: "0.0.0.0"
`

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	_, err := LoadFromYAML(configPath)
	if err == nil {
		t.Fatal("expected migration error for legacy LAN config, got nil")
	}

	if !strings.Contains(err.Error(), "web_access_mode is missing") {
		t.Fatalf("expected migration message, got %q", err.Error())
	}
}

func TestLoadFromEnv(t *testing.T) {
	// Set environment variables
	os.Setenv("GATEWAY_ID", "gw-env-001")
	os.Setenv("COMPANY_ID", "comp-env-123")
	os.Setenv("CLOUD_URL", "wss://env.example.com")
	os.Setenv("JWT_SECRET", "env-secret")
	defer func() {
		os.Unsetenv("GATEWAY_ID")
		os.Unsetenv("COMPANY_ID")
		os.Unsetenv("CLOUD_URL")
		os.Unsetenv("JWT_SECRET")
	}()

	cfg := LoadFromEnv()

	if cfg.GatewayID != "gw-env-001" {
		t.Errorf("expected gateway_id 'gw-env-001', got '%s'", cfg.GatewayID)
	}
	if cfg.CompanyID != "comp-env-123" {
		t.Errorf("expected company_id 'comp-env-123', got '%s'", cfg.CompanyID)
	}
}

func TestApplyDefaults(t *testing.T) {
	cfg := &GatewayConfig{
		GatewayID: "gw-001",
		CompanyID: "comp-123",
		CloudURL:  "wss://cloud.example.com",
	}

	cfg.ApplyDefaults()

	// Check defaults were applied
	if cfg.SyncInterval != 30*time.Second {
		t.Errorf("expected default sync interval 30s, got %v", cfg.SyncInterval)
	}
	if cfg.BatchSize != 100 {
		t.Errorf("expected default batch size 100, got %d", cfg.BatchSize)
	}
	if cfg.MaxRetries != 5 {
		t.Errorf("expected default max retries 5, got %d", cfg.MaxRetries)
	}
	if cfg.HealthPort != 8080 {
		t.Errorf("expected default health port 8080, got %d", cfg.HealthPort)
	}
	if cfg.WebListenAddr != "127.0.0.1" {
		t.Errorf("expected default web listen addr 127.0.0.1, got %s", cfg.WebListenAddr)
	}
	if cfg.WebAccessMode != "local" {
		t.Errorf("expected default web access mode local, got %s", cfg.WebAccessMode)
	}
}

func TestAntennaConfig_Address(t *testing.T) {
	ant := AntennaConfig{
		IP:   "192.168.1.100",
		Port: 6000,
	}

	addr := ant.Address()
	expected := "192.168.1.100:6000"
	if addr != expected {
		t.Errorf("expected address '%s', got '%s'", expected, addr)
	}
}

// Test queue cap config defaults
func TestApplyDefaults_QueueCapFields(t *testing.T) {
	cfg := &GatewayConfig{
		GatewayID: "gw-001",
		CompanyID: "comp-123",
		CloudURL:  "wss://cloud.example.com",
	}

	cfg.ApplyDefaults()

	// Check queue cap defaults
	if cfg.MaxPendingConfirmations == nil {
		t.Error("expected MaxPendingConfirmations to be set, got nil")
	} else if *cfg.MaxPendingConfirmations != 10000 {
		t.Errorf("expected default MaxPendingConfirmations 10000, got %d", *cfg.MaxPendingConfirmations)
	}
	if cfg.PendingWarningThreshold == nil {
		t.Error("expected PendingWarningThreshold to be set, got nil")
	} else if *cfg.PendingWarningThreshold != 1000 {
		t.Errorf("expected default PendingWarningThreshold 1000, got %d", *cfg.PendingWarningThreshold)
	}
}

// Test queue cap config zero values (unlimited cap)
func TestApplyDefaults_QueueCapZeroMeansUnlimited(t *testing.T) {
	zeroMax := 0
	zeroThreshold := 0
	cfg := &GatewayConfig{
		GatewayID:               "gw-001",
		CompanyID:               "comp-123",
		CloudURL:                "wss://cloud.example.com",
		MaxPendingConfirmations: &zeroMax,       // Explicitly set to 0 (unlimited)
		PendingWarningThreshold: &zeroThreshold, // Explicitly set to 0
	}

	cfg.ApplyDefaults()

	// Zero values should remain 0 (not overwritten by defaults)
	if cfg.MaxPendingConfirmations == nil {
		t.Error("expected MaxPendingConfirmations to remain set, got nil")
	} else if *cfg.MaxPendingConfirmations != 0 {
		t.Errorf("expected MaxPendingConfirmations to remain 0 (unlimited), got %d", *cfg.MaxPendingConfirmations)
	}
	// Note: PendingWarningThreshold with 0 would mean never warn, which is valid
	if cfg.PendingWarningThreshold == nil {
		t.Error("expected PendingWarningThreshold to remain set, got nil")
	} else if *cfg.PendingWarningThreshold != 0 {
		t.Errorf("expected PendingWarningThreshold to remain 0, got %d", *cfg.PendingWarningThreshold)
	}
}

// Test queue cap config custom values
func TestApplyDefaults_QueueCapCustomValues(t *testing.T) {
	customMax := 5000
	customThreshold := 4000
	cfg := &GatewayConfig{
		GatewayID:               "gw-001",
		CompanyID:               "comp-123",
		CloudURL:                "wss://cloud.example.com",
		MaxPendingConfirmations: &customMax,       // Custom cap
		PendingWarningThreshold: &customThreshold, // Custom threshold
	}

	cfg.ApplyDefaults()

	// Custom values should not be overwritten
	if cfg.MaxPendingConfirmations == nil {
		t.Error("expected MaxPendingConfirmations to remain set, got nil")
	} else if *cfg.MaxPendingConfirmations != 5000 {
		t.Errorf("expected MaxPendingConfirmations to remain 5000, got %d", *cfg.MaxPendingConfirmations)
	}
	if cfg.PendingWarningThreshold == nil {
		t.Error("expected PendingWarningThreshold to remain set, got nil")
	} else if *cfg.PendingWarningThreshold != 4000 {
		t.Errorf("expected PendingWarningThreshold to remain 4000, got %d", *cfg.PendingWarningThreshold)
	}
}
