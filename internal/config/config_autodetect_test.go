package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAutoDetectPath_EnvVar(t *testing.T) {
	// Test that GATEWAY_CONFIG env var is respected
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte("gateway_id: test"), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	// Set env var
	os.Setenv("GATEWAY_CONFIG", configPath)
	defer os.Unsetenv("GATEWAY_CONFIG")

	path, err := AutoDetectPath()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if path != configPath {
		t.Errorf("expected path '%s', got '%s'", configPath, path)
	}
}

func TestAutoDetectPath_CurrentDirectory(t *testing.T) {
	// Test that ./config.yaml is found when env var is not set
	tmpDir := t.TempDir()
	originalDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(originalDir)

	// Create config.yaml in current directory
	configPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte("gateway_id: test"), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	path, err := AutoDetectPath()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if path != configPath {
		t.Errorf("expected path '%s', got '%s'", configPath, path)
	}
}

func TestAutoDetectPath_NotFound(t *testing.T) {
	// Test error when no config is found anywhere
	tmpDir := t.TempDir()
	originalDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(originalDir)

	// Ensure no env var is set
	os.Unsetenv("GATEWAY_CONFIG")

	path, err := AutoDetectPath()
	if err == nil {
		t.Error("expected error when config not found, got nil")
	}
	if path != "" {
		t.Errorf("expected empty path when error, got '%s'", path)
	}

	// Error message should mention searched paths
	errStr := err.Error()
	if errStr == "" {
		t.Error("expected non-empty error message")
	}
}

func TestAutoDetectPath_EnvVarNotFound(t *testing.T) {
	// Test error when GATEWAY_CONFIG is set but file doesn't exist
	os.Setenv("GATEWAY_CONFIG", "/nonexistent/path/config.yaml")
	defer os.Unsetenv("GATEWAY_CONFIG")

	path, err := AutoDetectPath()
	if err == nil {
		t.Error("expected error when GATEWAY_CONFIG points to non-existent file")
	}
	if path != "" {
		t.Errorf("expected empty path when error, got '%s'", path)
	}
}

func TestAutoDetectPath_Precedence(t *testing.T) {
	// Test that env var takes precedence over current directory
	tmpDir := t.TempDir()

	// Create two config files
	envConfigPath := filepath.Join(tmpDir, "env-config.yaml")
	cwdConfigPath := filepath.Join(tmpDir, "cwd-config.yaml")
	if err := os.WriteFile(envConfigPath, []byte("gateway_id: env"), 0644); err != nil {
		t.Fatalf("failed to write env config: %v", err)
	}
	if err := os.WriteFile(cwdConfigPath, []byte("gateway_id: cwd"), 0644); err != nil {
		t.Fatalf("failed to write cwd config: %v", err)
	}

	// Change to directory with cwd-config.yaml
	originalDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(originalDir)

	// Rename to config.yaml so it would be found via current directory
	if err := os.Rename(cwdConfigPath, filepath.Join(tmpDir, "config.yaml")); err != nil {
		t.Fatalf("failed to rename: %v", err)
	}

	// Set env var to env-config.yaml
	os.Setenv("GATEWAY_CONFIG", envConfigPath)
	defer os.Unsetenv("GATEWAY_CONFIG")

	path, err := AutoDetectPath()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if path != envConfigPath {
		t.Errorf("expected env var config '%s' to take precedence, got '%s'", envConfigPath, path)
	}
}

func TestAutoDetectPath_SystemPath(t *testing.T) {
	// Test that system path is searched (mock by creating in temp dir and adjusting)
	// This is tricky because we can't actually write to /etc, so we'll test via error message
	tmpDir := t.TempDir()
	originalDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(originalDir)

	// Ensure no env var and no local config
	os.Unsetenv("GATEWAY_CONFIG")

	path, err := AutoDetectPath()
	if err == nil {
		t.Error("expected error when config not found")
	}

	// Error message should mention /etc/amg-rfid-gateway/config.yaml
	errStr := err.Error()
	if !strings.Contains(errStr, "/etc/amg-rfid-gateway/config.yaml") {
		t.Errorf("error message should mention system path, got: %s", errStr)
	}
	if path != "" {
		t.Errorf("expected empty path when error, got '%s'", path)
	}
}

func TestAutoDetectPath_DirectoryNotFile(t *testing.T) {
	// Test that directories are not accepted as config files
	tmpDir := t.TempDir()
	originalDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(originalDir)

	// Create a directory named config.yaml (should be ignored)
	if err := os.Mkdir(filepath.Join(tmpDir, "config.yaml"), 0755); err != nil {
		t.Fatalf("failed to create directory: %v", err)
	}

	// Ensure no env var
	os.Unsetenv("GATEWAY_CONFIG")

	path, err := AutoDetectPath()
	if err == nil {
		t.Error("expected error when only a directory exists at config.yaml path")
	}
	if path != "" {
		t.Errorf("expected empty path when error, got '%s'", path)
	}
}

func TestAutoDetectPath_EmptyEnvVar(t *testing.T) {
	// Test that empty GATEWAY_CONFIG env var is ignored
	tmpDir := t.TempDir()
	originalDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(originalDir)

	// Set empty env var
	os.Setenv("GATEWAY_CONFIG", "")
	defer os.Unsetenv("GATEWAY_CONFIG")

	// Create config.yaml in current directory
	configPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte("gateway_id: test"), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	path, err := AutoDetectPath()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if path != configPath {
		t.Errorf("expected path '%s' (current dir), got '%s'", configPath, path)
	}
}
