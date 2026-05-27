package config

import (
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
	if cfg.SocketPath != "/tmp/amg-rfid-gateway.sock" {
		t.Errorf("expected SocketPath default '/tmp/amg-rfid-gateway.sock', got %q", cfg.SocketPath)
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
