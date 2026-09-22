package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amg-rfid/amg-rfid-gateway/internal/config"
)

func TestHealthServerAddress(t *testing.T) {
	tests := []struct {
		name       string
		listenAddr string
		port       int
		want       string
	}{
		{
			name:       "IPv4 loopback",
			listenAddr: "127.0.0.1",
			port:       8080,
			want:       "127.0.0.1:8080",
		},
		{
			name:       "IPv6 loopback",
			listenAddr: "::1",
			port:       8080,
			want:       "[::1]:8080",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := healthServerAddress(tt.listenAddr, tt.port); got != tt.want {
				t.Fatalf("healthServerAddress(%q, %d) = %q, want %q", tt.listenAddr, tt.port, got, tt.want)
			}
		})
	}
}

func TestCacheDBPathUsesConfiguredDataPath(t *testing.T) {
	tests := []struct {
		name     string
		dataPath string
		expected string
	}{
		{
			name:     "absolute data path",
			dataPath: "/var/lib/amg-rfid-gateway",
			expected: filepath.Join("/var/lib/amg-rfid-gateway", "cache.db"),
		},
		{
			name:     "relative data path",
			dataPath: "./runtime-data",
			expected: filepath.Join("runtime-data", "cache.db"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.GatewayConfig{DataPath: tt.dataPath}

			actual := cacheDBPath(cfg)

			if actual != tt.expected {
				t.Fatalf("cacheDBPath() = %q, expected %q", actual, tt.expected)
			}
		})
	}
}

func TestCacheDBPathUsesDefaultDataPath(t *testing.T) {
	cfg := &config.GatewayConfig{}
	cfg.ApplyDefaults()

	actual := cacheDBPath(cfg)
	expected := filepath.Join("data", "cache.db")

	if actual != expected {
		t.Fatalf("cacheDBPath() = %q, expected %q", actual, expected)
	}
}

func TestLoadConfigReturnsExistingYAMLError(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	content := `
gateway_id: "gw-1"
company_id: "company-1"
cloud_url: "wss://example.com/ws"
jwt_secret: "secret"
web_enabled: true
web_listen_addr: "0.0.0.0"
antennas: []
`
	if err := os.WriteFile(configPath, []byte(content), 0o600); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	_, err := loadConfig(configPath)
	if err == nil {
		t.Fatal("expected loadConfig to return existing YAML error")
	}

	errText := err.Error()
	for _, expected := range []string{
		"failed to load config from",
		"legacy config detected",
		`web_access_mode: "lan"`,
		"web_auth_token",
		"127.0.0.1",
	} {
		if !strings.Contains(errText, expected) {
			t.Fatalf("expected error %q to contain %q", errText, expected)
		}
	}
}
