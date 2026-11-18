package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func validCredentialConfig() *GatewayConfig {
	return &GatewayConfig{
		GatewayID:             "gw-001",
		CompanyID:             "comp-123",
		CloudURL:              "wss://cloud.example.com/ws",
		GatewayCredentialFile: "/etc/amg-rfid-gateway/gateway.secret",
		ListenMode:            "auto",
		WebAccessMode:         "local",
		WebListenAddr:         "127.0.0.1",
		VPSAPIURL:             "https://vps.example.com",
	}
}

func TestValidate_VPSAPIURL(t *testing.T) {
	tests := []struct {
		url     string
		wantErr string
	}{
		{"", "vps_api_url cannot be empty"},
		{"http://vps.example.com", "vps_api_url must use https:// (http:// is only allowed for loopback)"},
		{"http://10.0.0.5:8080", "vps_api_url must use https:// (http:// is only allowed for loopback)"},
		{"ftp://vps.example.com", "vps_api_url must use https:// (http:// is only allowed for loopback)"},
		{"http://127.0.0.1:8000", ""},
		{"http://localhost:8000", ""},
		{"http://[::1]:8000", ""},
		{"https://vps.example.com", ""},
	}
	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			cfg := validCredentialConfig()
			cfg.VPSAPIURL = tt.url
			err := cfg.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() = %v; want nil", err)
				}
				return
			}
			if err == nil || err.Error() != tt.wantErr {
				t.Fatalf("Validate() = %v; want %q", err, tt.wantErr)
			}
		})
	}
}

func TestValidate_RequiresGatewayCredentialFile(t *testing.T) {
	cfg := validCredentialConfig()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}

	cfg.GatewayCredentialFile = ""
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "gateway_credential_file cannot be empty") {
		t.Fatalf("err = %v; want gateway_credential_file cannot be empty", err)
	}
}

func TestLoadFromYAML_LegacyJWTSecretIsUnknownAndNeedsCredentialFile(t *testing.T) {
	content := `
gateway_id: "gw-legacy"
company_id: "comp-legacy"
cloud_url: "wss://cloud.example.com/ws"
jwt_secret: "legacy-token"
`
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFromYAML(path)
	if err == nil {
		err = cfg.Validate()
	}
	if err == nil || !strings.Contains(err.Error(), "gateway_credential_file cannot be empty") {
		t.Fatalf("err = %v; want gateway_credential_file cannot be empty", err)
	}
	if cfg != nil {
		found := false
		for _, k := range cfg.UnknownRootKeys() {
			if k == "jwt_secret" {
				found = true
			}
		}
		if !found {
			t.Fatalf("UnknownRootKeys() = %v; want jwt_secret reported", cfg.UnknownRootKeys())
		}
	}
}

func TestLoadFromEnv_ReadsGatewayCredentialFile(t *testing.T) {
	const path = "/var/lib/amg-gw/credential"
	t.Setenv("GATEWAY_CREDENTIAL_FILE", path)
	if got := LoadFromEnv().GatewayCredentialFile; got != path {
		t.Fatalf("GatewayCredentialFile = %q; want %q", got, path)
	}
}

func TestLoadFromEnv_ReadsVPSAPIURL(t *testing.T) {
	t.Setenv("VPS_API_URL", "https://vps.example.com")
	if got := LoadFromEnv().VPSAPIURL; got != "https://vps.example.com" {
		t.Fatalf("VPSAPIURL = %q; want https://vps.example.com", got)
	}
}
