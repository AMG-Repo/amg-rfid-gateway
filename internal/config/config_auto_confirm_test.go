package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestApplyDefaults_AutoConfirm(t *testing.T) {
	cfg := sessionTestConfig()
	if cfg.AutoConfirmIdle != time.Minute || cfg.AutoConfirmMinReads != 3 {
		t.Errorf("defaults = idle %v min reads %d; want 1m and 3", cfg.AutoConfirmIdle, cfg.AutoConfirmMinReads)
	}
}

func TestLoadFromYAML_AutoConfirm(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	content := "auto_confirm_idle: 90s\nauto_confirm_min_reads: 5\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadFromYAML(path)
	if err != nil {
		t.Fatalf("LoadFromYAML: %v", err)
	}
	if cfg.AutoConfirmIdle != 90*time.Second || cfg.AutoConfirmMinReads != 5 {
		t.Errorf("loaded idle %v min reads %d; want 90s and 5", cfg.AutoConfirmIdle, cfg.AutoConfirmMinReads)
	}
}

func TestValidate_AutoConfirm(t *testing.T) {
	tests := []struct {
		name     string
		idle     time.Duration
		minReads int
		wantErr  string
	}{
		{"defaults are valid", time.Minute, 3, ""},
		{"single read is valid", 30 * time.Second, 1, ""},
		{"unset idle means the default", 0, 3, ""},
		{"idle below the minimum", 29 * time.Second, 3, "auto_confirm_idle must be at least 30s"},
		{"one second idle", time.Second, 3, "auto_confirm_idle must be at least 30s"},
		{"negative idle", -time.Second, 3, "auto_confirm_idle must be"},
		{"negative min reads", time.Minute, -1, "auto_confirm_min_reads must be positive"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := sessionTestConfig()
			cfg.AutoConfirmIdle, cfg.AutoConfirmMinReads = tc.idle, tc.minReads
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
