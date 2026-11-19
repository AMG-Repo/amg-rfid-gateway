package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func sessionTestConfig() *GatewayConfig {
	cfg := &GatewayConfig{
		GatewayID:             "gw-001",
		CompanyID:             "comp-123",
		CloudURL:              "wss://cloud.example.com/ws",
		GatewayCredentialFile: "/etc/amg-rfid-gateway/gateway.secret",
		VPSAPIURL:             "https://vps.example.com",
		Antennas: []AntennaConfig{
			{ID: "ant-1", IP: "192.168.1.100", Port: 6000, Enabled: true},
		},
	}
	cfg.ApplyDefaults()
	return cfg
}

func TestApplyDefaults_ReadingSessionWindows(t *testing.T) {
	cfg := sessionTestConfig()
	if cfg.ReadingSessionIdle != 5*time.Second || cfg.ReadingSessionMax != 60*time.Second {
		t.Errorf("defaults = idle %v max %v; want 5s and 60s", cfg.ReadingSessionIdle, cfg.ReadingSessionMax)
	}
}

func TestLoadFromYAML_ReadingSessionWindows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	content := "reading_session_idle: 3s\nreading_session_max: 2m\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadFromYAML(path)
	if err != nil {
		t.Fatalf("LoadFromYAML: %v", err)
	}
	if cfg.ReadingSessionIdle != 3*time.Second || cfg.ReadingSessionMax != 2*time.Minute {
		t.Errorf("loaded idle %v max %v; want 3s and 2m", cfg.ReadingSessionIdle, cfg.ReadingSessionMax)
	}
}

func TestValidate_ReadingSessionWindows(t *testing.T) {
	tests := []struct {
		name      string
		idle, max time.Duration
		wantErr   string
	}{
		{"defaults are valid", 5 * time.Second, 60 * time.Second, ""},
		{"equal windows are valid", 10 * time.Second, 10 * time.Second, ""},
		{"negative idle", -time.Second, 60 * time.Second, "reading_session_idle must be positive"},
		{"negative max", 5 * time.Second, -time.Second, "reading_session_max must be positive"},
		{"max shorter than idle", 30 * time.Second, 10 * time.Second, "reading_session_max must be at least reading_session_idle"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := sessionTestConfig()
			cfg.ReadingSessionIdle, cfg.ReadingSessionMax = tc.idle, tc.max
			err := cfg.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Errorf("Validate() = %v; want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("Validate() = %v; want %q", err, tc.wantErr)
			}
		})
	}
}
