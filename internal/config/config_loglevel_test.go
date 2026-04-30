package config

import (
	"os"
	"testing"
)

func TestGatewayConfig_LogLevelField(t *testing.T) {
	// Test that LogLevel field exists and can be set
	cfg := &GatewayConfig{
		GatewayID: "gw-001",
		CompanyID: "comp-123",
		CloudURL:  "wss://cloud.example.com",
		LogLevel:  "debug",
	}

	if cfg.LogLevel != "debug" {
		t.Errorf("expected LogLevel to be 'debug', got '%s'", cfg.LogLevel)
	}
}

func TestGatewayConfig_LogLevelYamlTag(t *testing.T) {
	// Test that LogLevel is loaded from YAML with correct tag
	content := `
gateway_id: "gw-001"
company_id: "comp-123"
cloud_url: "wss://cloud.example.com"
jwt_secret: "test-secret"
log_level: "warn"
`
	tmpDir := t.TempDir()
	configPath := tmpDir + "/config.yaml"
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	cfg, err := LoadFromYAML(configPath)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if cfg.LogLevel != "warn" {
		t.Errorf("expected LogLevel 'warn' from YAML, got '%s'", cfg.LogLevel)
	}
}

func TestGatewayConfig_LogLevel_DefaultEmpty(t *testing.T) {
	// Test that LogLevel defaults to empty string when not specified
	cfg := &GatewayConfig{
		GatewayID: "gw-001",
		CompanyID: "comp-123",
		CloudURL:  "wss://cloud.example.com",
	}

	if cfg.LogLevel != "" {
		t.Errorf("expected LogLevel to default to empty string, got '%s'", cfg.LogLevel)
	}
}

func TestGatewayConfig_LogLevel_ValidValues(t *testing.T) {
	// Test various valid log levels
	validLevels := []string{"debug", "info", "warn", "error"}
	for _, level := range validLevels {
		cfg := &GatewayConfig{
			GatewayID: "gw-001",
			CompanyID: "comp-123",
			CloudURL:  "wss://cloud.example.com",
			LogLevel:  level,
		}
		if cfg.LogLevel != level {
			t.Errorf("expected LogLevel '%s', got '%s'", level, cfg.LogLevel)
		}
	}
}
