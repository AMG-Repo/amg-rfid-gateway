package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGatewayConfig_ListenModeValidation(t *testing.T) {
	// Valid listen modes: "active", "passive", "auto"
	validModes := []string{"active", "passive", "auto"}
	for _, mode := range validModes {
		cfg := &GatewayConfig{
			GatewayID:         "gw-001",
			CompanyID:         "comp-001",
			CloudURL:          "wss://example.com/ws",
			JWTSecret:         "secret",
			ListenMode:        mode,
			WebAccessMode:     "local",
			WebListenAddr:     "127.0.0.1",
			HeartbeatInterval: 3 * time.Second,
		}
		if err := cfg.Validate(); err != nil {
			t.Errorf("expected valid listen mode %q, got error: %v", mode, err)
		}
	}

	// Invalid listen mode
	cfg := &GatewayConfig{
		GatewayID:         "gw-001",
		CompanyID:         "comp-001",
		CloudURL:          "wss://example.com/ws",
		JWTSecret:         "secret",
		ListenMode:        "fast",
		WebAccessMode:     "local",
		WebListenAddr:     "127.0.0.1",
		HeartbeatInterval: 3 * time.Second,
	}
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for invalid listen mode 'fast'")
	} else if err.Error() != "listen_mode must be one of: active, passive, auto" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestGatewayConfig_ApplyDefaults(t *testing.T) {
	cfg := &GatewayConfig{}
	cfg.ApplyDefaults()

	// Test ListenMode default
	if cfg.ListenMode != "auto" {
		t.Errorf("expected ListenMode default 'auto', got %q", cfg.ListenMode)
	}

	// Test HeartbeatInterval default
	if cfg.HeartbeatInterval != 3*time.Second {
		t.Errorf("expected HeartbeatInterval default 3s, got %v", cfg.HeartbeatInterval)
	}

	// Test HeartbeatSilenceThreshold default
	if cfg.HeartbeatSilenceThreshold != 5*time.Second {
		t.Errorf("expected HeartbeatSilenceThreshold default 5s, got %v", cfg.HeartbeatSilenceThreshold)
	}

	// Test AdaptiveDelayRecent default
	if cfg.AdaptiveDelayRecent != 3*time.Second {
		t.Errorf("expected AdaptiveDelayRecent default 3s, got %v", cfg.AdaptiveDelayRecent)
	}

	// Test AdaptiveDelayRecentWindow default
	if cfg.AdaptiveDelayRecentWindow != 2*time.Second {
		t.Errorf("expected AdaptiveDelayRecentWindow default 2s, got %v", cfg.AdaptiveDelayRecentWindow)
	}

	// Test AdaptiveDelayStale default
	if cfg.AdaptiveDelayStale != 1*time.Second {
		t.Errorf("expected AdaptiveDelayStale default 1s, got %v", cfg.AdaptiveDelayStale)
	}

	// Test AdaptiveDelayStaleWindow default
	if cfg.AdaptiveDelayStaleWindow != 10*time.Second {
		t.Errorf("expected AdaptiveDelayStaleWindow default 10s, got %v", cfg.AdaptiveDelayStaleWindow)
	}

	// Test AdaptiveDelayAutoReading default
	if cfg.AdaptiveDelayAutoReading != 5*time.Second {
		t.Errorf("expected AdaptiveDelayAutoReading default 5s, got %v", cfg.AdaptiveDelayAutoReading)
	}

	// Test ReconnectInitialBackoff default
	if cfg.ReconnectInitialBackoff != 1*time.Second {
		t.Errorf("expected ReconnectInitialBackoff default 1s, got %v", cfg.ReconnectInitialBackoff)
	}

	// Test ReconnectMaxBackoff default
	if cfg.ReconnectMaxBackoff != 30*time.Second {
		t.Errorf("expected ReconnectMaxBackoff default 30s, got %v", cfg.ReconnectMaxBackoff)
	}

	// Test SocketPath default
	const historicalBridgeSocketPath = "/tmp/amg-gateway.sock"
	if DefaultSocketPath != historicalBridgeSocketPath {
		t.Errorf("DefaultSocketPath = %q, want historical bridge default %q", DefaultSocketPath, historicalBridgeSocketPath)
	}
	if cfg.SocketPath != DefaultSocketPath {
		t.Errorf("SocketPath default = %q, want %q", cfg.SocketPath, DefaultSocketPath)
	}
}

func TestLoadFromYAMLHealthListenAddr(t *testing.T) {
	tests := []struct {
		name           string
		content        string
		want           string
		wantHealthPort int
		wantErr        string
	}{
		{
			name: "omitted address defaults to loopback",
			content: `health_port: 18080
`,
			want:           "127.0.0.1",
			wantHealthPort: 18080,
		},
		{
			name: "explicit IPv4 loopback override is accepted",
			content: `health_listen_addr: "127.0.0.2"
`,
			want: "127.0.0.2",
		},
		{
			name: "explicit IPv6 loopback is accepted",
			content: `health_listen_addr: "::1"
`,
			want: "::1",
		},
		{
			name: "explicit empty address is rejected",
			content: `health_listen_addr: ""
`,
			wantErr: "health_listen_addr",
		},
		{
			name: "IPv4 wildcard address is rejected",
			content: `health_listen_addr: "0.0.0.0"
`,
			wantErr: "health_listen_addr",
		},
		{
			name: "non-loopback address is rejected",
			content: `health_listen_addr: "192.0.2.10"
`,
			wantErr: "health_listen_addr",
		},
		{
			name: "IPv6 wildcard address is rejected",
			content: `health_listen_addr: "::"
`,
			wantErr: "health_listen_addr",
		},
		{
			name: "malformed address is rejected",
			content: `health_listen_addr: "not-an-ip"
`,
			wantErr: "health_listen_addr",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte(tt.content), 0o600); err != nil {
				t.Fatalf("write config: %v", err)
			}

			cfg, err := LoadFromYAML(path)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("LoadFromYAML() error = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("LoadFromYAML() error = %v", err)
			}
			if cfg.HealthListenAddr != tt.want {
				t.Fatalf("HealthListenAddr = %q, want %q", cfg.HealthListenAddr, tt.want)
			}
			if tt.wantHealthPort != 0 && cfg.HealthPort != tt.wantHealthPort {
				t.Fatalf("HealthPort = %d, want %d", cfg.HealthPort, tt.wantHealthPort)
			}
		})
	}
}

func TestGatewayConfig_ExplicitValuesNotOverridden(t *testing.T) {
	cfg := &GatewayConfig{
		ListenMode:                "active",
		HeartbeatInterval:         5 * time.Second,
		HeartbeatSilenceThreshold: 10 * time.Second,
		AdaptiveDelayRecent:       4 * time.Second,
		AdaptiveDelayRecentWindow: 3 * time.Second,
		AdaptiveDelayStale:        2 * time.Second,
		AdaptiveDelayStaleWindow:  15 * time.Second,
		AdaptiveDelayAutoReading:  8 * time.Second,
		ReconnectInitialBackoff:   2 * time.Second,
		ReconnectMaxBackoff:       60 * time.Second,
		SocketPath:                "/custom/socket.sock",
	}
	cfg.ApplyDefaults()

	if cfg.ListenMode != "active" {
		t.Errorf("expected ListenMode 'active', got %q", cfg.ListenMode)
	}
	if cfg.HeartbeatInterval != 5*time.Second {
		t.Errorf("expected HeartbeatInterval 5s, got %v", cfg.HeartbeatInterval)
	}
	if cfg.SocketPath != "/custom/socket.sock" {
		t.Errorf("expected SocketPath '/custom/socket.sock', got %q", cfg.SocketPath)
	}
}

func TestGatewayConfig_WebAccessModeValidation(t *testing.T) {
	tests := []struct {
		name    string
		cfg     GatewayConfig
		wantErr string
	}{
		{
			name: "local mode with loopback is valid",
			cfg: GatewayConfig{
				GatewayID:     "gw-001",
				CompanyID:     "comp-001",
				CloudURL:      "wss://example.com/ws",
				JWTSecret:     "secret",
				ListenMode:    "auto",
				WebAccessMode: "local",
				WebListenAddr: "127.0.0.1",
				WebAuthToken:  "",
			},
		},
		{
			name: "lan mode requires token",
			cfg: GatewayConfig{
				GatewayID:     "gw-001",
				CompanyID:     "comp-001",
				CloudURL:      "wss://example.com/ws",
				JWTSecret:     "secret",
				ListenMode:    "auto",
				WebAccessMode: "lan",
				WebListenAddr: "0.0.0.0",
			},
			wantErr: "web_auth_token",
		},
		{
			name: "lan mode with token is valid",
			cfg: GatewayConfig{
				GatewayID:     "gw-001",
				CompanyID:     "comp-001",
				CloudURL:      "wss://example.com/ws",
				JWTSecret:     "secret",
				ListenMode:    "auto",
				WebAccessMode: "lan",
				WebListenAddr: "0.0.0.0",
				WebAuthToken:  "change-me-token",
			},
		},
		{
			name: "local mode rejects non-loopback address",
			cfg: GatewayConfig{
				GatewayID:     "gw-001",
				CompanyID:     "comp-001",
				CloudURL:      "wss://example.com/ws",
				JWTSecret:     "secret",
				ListenMode:    "auto",
				WebAccessMode: "local",
				WebListenAddr: "0.0.0.0",
			},
			wantErr: "web_access_mode=local",
		},
		{
			name: "invalid mode rejected",
			cfg: GatewayConfig{
				GatewayID:     "gw-001",
				CompanyID:     "comp-001",
				CloudURL:      "wss://example.com/ws",
				JWTSecret:     "secret",
				ListenMode:    "auto",
				WebAccessMode: "internet",
				WebListenAddr: "127.0.0.1",
			},
			wantErr: "web_access_mode",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.wantErr == "" && err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %q", tt.wantErr, err.Error())
				}
			}
		})
	}
}
