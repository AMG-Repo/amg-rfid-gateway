package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

const knownOnlyConfig = `gateway_id: gw-001
company_id: comp-001
cloud_url: wss://example.com/ws
jwt_secret: secret
log_level: info
antennas: []
`

func TestLoadFromYAML_ReportsUnknownRootKeys(t *testing.T) {
	path := writeConfig(t, knownOnlyConfig+`metrics:
  enabled: true
  port: 9090
updates:
  auto_update: true
`)

	cfg, err := LoadFromYAML(path)
	if err != nil {
		t.Fatalf("unknown keys must not fail loading: %v", err)
	}
	if got, want := cfg.UnknownRootKeys(), []string{"metrics", "updates"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("UnknownRootKeys() = %v, want %v", got, want)
	}
	// Known fields still load exactly as before.
	if cfg.GatewayID != "gw-001" || cfg.LogLevel != "info" {
		t.Fatalf("known fields changed: gateway_id=%q log_level=%q", cfg.GatewayID, cfg.LogLevel)
	}
}

func TestLoadFromYAML_KnownKeysAreNotReported(t *testing.T) {
	cfg, err := LoadFromYAML(writeConfig(t, knownOnlyConfig))
	if err != nil {
		t.Fatalf("LoadFromYAML: %v", err)
	}
	if got := cfg.UnknownRootKeys(); len(got) != 0 {
		t.Fatalf("UnknownRootKeys() = %v, want none", got)
	}
}

func TestLoadFromYAML_EveryYAMLTagIsKnown(t *testing.T) {
	// Each GatewayConfig field must count as known, so the warning never fires on valid config.
	var b strings.Builder
	typ := reflect.TypeOf(GatewayConfig{})
	for i := 0; i < typ.NumField(); i++ {
		name := strings.Split(typ.Field(i).Tag.Get("yaml"), ",")[0]
		if name == "" || name == "-" {
			continue
		}
		value := "null"
		if name == "health_listen_addr" {
			value = "127.0.0.1" // explicit values must be loopback
		}
		b.WriteString(name + ": " + value + "\n")
	}
	cfg, err := LoadFromYAML(writeConfig(t, b.String()))
	if err != nil {
		t.Fatalf("LoadFromYAML: %v", err)
	}
	if got := cfg.UnknownRootKeys(); len(got) != 0 {
		t.Fatalf("UnknownRootKeys() = %v, want none", got)
	}
}
