package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"time"

	"gopkg.in/yaml.v3"
)

// GatewayConfig holds the complete configuration for the RFID gateway.
type GatewayConfig struct {
	GatewayID    string          `yaml:"gateway_id"`
	CompanyID    string          `yaml:"company_id"`
	CloudURL     string          `yaml:"cloud_url"`
	JWTSecret    string          `yaml:"jwt_secret"`
	Antennas     []AntennaConfig `yaml:"antennas"`
	SyncInterval time.Duration   `yaml:"sync_interval"`
	BatchSize    int             `yaml:"batch_size"`
	MaxRetries   int             `yaml:"max_retries"`
	HealthPort   int             `yaml:"health_port"`
	DataPath     string          `yaml:"data_path"`

	// Permanent listening mode configuration (REQ-A006)
	ListenMode                string        `yaml:"listen_mode"`
	HeartbeatInterval         time.Duration `yaml:"heartbeat_interval"`
	HeartbeatSilenceThreshold time.Duration `yaml:"heartbeat_silence_threshold"`
	AdaptiveDelayRecent       time.Duration `yaml:"adaptive_delay_recent"`
	AdaptiveDelayRecentWindow time.Duration `yaml:"adaptive_delay_recent_window"`
	AdaptiveDelayStale        time.Duration `yaml:"adaptive_delay_stale"`
	AdaptiveDelayStaleWindow  time.Duration `yaml:"adaptive_delay_stale_window"`
	AdaptiveDelayAutoReading  time.Duration `yaml:"adaptive_delay_auto_reading"`
	ReconnectInitialBackoff   time.Duration `yaml:"reconnect_initial_backoff"`
	ReconnectMaxBackoff       time.Duration `yaml:"reconnect_max_backoff"`
	SocketPath                string        `yaml:"socket_path"`

	// Web UI configuration (local verification frontend)
	WebEnabled                bool          `yaml:"web_enabled"`
	WebPort                   int           `yaml:"web_port"`
	WebListenAddr             string        `yaml:"web_listen_addr"`
	VPSAPIURL                 string        `yaml:"vps_api_url"`
	SyncToolsInterval         time.Duration `yaml:"sync_tools_interval"`
	ConfirmationRetryInterval time.Duration `yaml:"confirmation_retry_interval"`

	// Queue cap configuration (REQ-S004)
	// Use pointers to distinguish between "not set" (nil) and "set to 0" (unlimited)
	MaxPendingConfirmations   *int          `yaml:"max_pending_confirmations,omitempty"`   // nil = default 10000, 0 = unlimited
	PendingWarningThreshold   *int          `yaml:"pending_warning_threshold,omitempty"`   // nil = default 1000, 0 = never warn

	// Logging configuration
	LogLevel                  string        `yaml:"log_level"`
}

// AntennaConfig holds configuration for a single antenna.
type AntennaConfig struct {
	ID      string `yaml:"id"`
	IP      string `yaml:"ip"`
	Port    int    `yaml:"port"`
	Enabled bool   `yaml:"enabled"`
	Zone    string `yaml:"zone"` // "entrada", "salida", or "" (empty for auto)
}

// Validate checks the gateway configuration.
// Uses NEGATIVE PROGRAMMING: check what should NOT be, early returns.
func (c *GatewayConfig) Validate() error {
	// NEGATIVE: GatewayID cannot be empty
	if c.GatewayID == "" {
		return errors.New("gateway_id cannot be empty")
	}

	// NEGATIVE: CompanyID cannot be empty
	if c.CompanyID == "" {
		return errors.New("company_id cannot be empty")
	}

	// NEGATIVE: CloudURL cannot be empty
	if c.CloudURL == "" {
		return errors.New("cloud_url cannot be empty")
	}

	// NEGATIVE: CloudURL must be valid WebSocket URL
	u, err := url.Parse(c.CloudURL)
	if err != nil {
		return fmt.Errorf("cloud_url is invalid: %w", err)
	}
	if u.Scheme != "wss" {
		return errors.New("cloud_url must use wss:// scheme (unencrypted ws:// is not allowed)")
	}

	// NEGATIVE: JWTSecret cannot be empty
	if c.JWTSecret == "" {
		return errors.New("jwt_secret cannot be empty")
	}

	// NEGATIVE: ListenMode must be valid (REQ-A006)
	if c.ListenMode != "active" && c.ListenMode != "passive" && c.ListenMode != "auto" {
		return errors.New("listen_mode must be one of: active, passive, auto")
	}

	// HAPPY PATH: All validations passed
	return nil
}

// Validate checks the antenna configuration.
// Uses NEGATIVE PROGRAMMING: check what should NOT be, early returns.
func (a *AntennaConfig) Validate() error {
	// NEGATIVE: ID cannot be empty
	if a.ID == "" {
		return errors.New("antenna id cannot be empty")
	}

	// NEGATIVE: IP cannot be empty
	if a.IP == "" {
		return errors.New("antenna ip cannot be empty")
	}

	// NEGATIVE: Port must be valid
	if a.Port <= 0 || a.Port > 65535 {
		return errors.New("antenna port must be between 1 and 65535")
	}

	// HAPPY PATH: All validations passed
	return nil
}

// Address returns the full address for the antenna (IP:Port).
func (a *AntennaConfig) Address() string {
	return fmt.Sprintf("%s:%d", a.IP, a.Port)
}

// LoadFromYAML loads configuration from a YAML file.
func LoadFromYAML(path string) (*GatewayConfig, error) {
	// NEGATIVE: Check file exists
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg GatewayConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	// Apply defaults after loading
	cfg.ApplyDefaults()

	// HAPPY PATH
	return &cfg, nil
}

// AutoDetectPath searches for the config file in standard locations.
// Search order:
// 1. $GATEWAY_CONFIG environment variable
// 2. ./config.yaml (current directory)
// 3. $(brew --prefix)/etc/amg-rfid-gateway/config.yaml (macOS Homebrew) - skipped on Linux
// 4. /etc/amg-rfid-gateway/config.yaml (system)
// Returns the path and nil error if found, or empty string and error if not found.
func AutoDetectPath() (string, error) {
	var searched []string

	// 1. Check GATEWAY_CONFIG environment variable
	if envPath := os.Getenv("GATEWAY_CONFIG"); envPath != "" {
		searched = append(searched, fmt.Sprintf("$GATEWAY_CONFIG (%s)", envPath))
		if fileExists(envPath) {
			return envPath, nil
		}
	}

	// 2. Check current directory
	cwdPath := "./config.yaml"
	searched = append(searched, cwdPath)
	if fileExists(cwdPath) {
		// Convert to absolute path for consistency
		absPath, err := filepath.Abs(cwdPath)
		if err == nil {
			return absPath, nil
		}
		return cwdPath, nil
	}

	// 3. Check Homebrew location (macOS + Linux)
	brewPrefix := os.Getenv("HOMEBREW_PREFIX")
	if brewPrefix == "" {
		switch runtime.GOOS {
		case "darwin":
			brewPrefix = "/opt/homebrew" // Apple Silicon
			if runtime.GOARCH == "amd64" {
				brewPrefix = "/usr/local" // Intel Macs
			}
		case "linux":
			brewPrefix = "/home/linuxbrew/.linuxbrew" // Homebrew on Linux
		}
	}
	if brewPrefix != "" {
		brewPath := filepath.Join(brewPrefix, "etc", "amg-rfid-gateway", "config.yaml")
		searched = append(searched, brewPath)
		if fileExists(brewPath) {
			return brewPath, nil
		}
	}

	// 4. Check system location
	systemPath := "/etc/amg-rfid-gateway/config.yaml"
	searched = append(searched, systemPath)
	if fileExists(systemPath) {
		return systemPath, nil
	}

	// Not found - return error with list of searched paths
	return "", fmt.Errorf("config file not found. Searched: %v", searched)
}

// fileExists checks if a file exists and is a regular file (not a directory).
func fileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

// SaveToYAML saves the configuration to a YAML file atomically.
// 1. Creates .bak backup of existing file
// 2. Marshals config to YAML
// 3. Writes to temp file in same directory
// 4. Atomic rename temp -> target path
func (c *GatewayConfig) SaveToYAML(path string) error {
	// NEGATIVE: Validate we have a valid path
	if path == "" {
		return errors.New("path cannot be empty")
	}

	// Get the directory for the config file
	dir := filepath.Dir(path)
	if dir == "" {
		dir = "."
	}

	// Ensure directory exists
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// 1. Create backup of existing file if it exists
	if fileExists(path) {
		backupPath := path + ".bak"
		if err := copyFile(path, backupPath); err != nil {
			return fmt.Errorf("failed to create backup: %w", err)
		}
	}

	// 2. Marshal config to YAML
	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	// 3. Write to temp file in same directory
	tempFile, err := os.CreateTemp(dir, "config-*.yaml.tmp")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tempPath := tempFile.Name()

	// Ensure temp file is cleaned up on error
	defer func() {
		if err != nil {
			os.Remove(tempPath)
		}
	}()

	// Write YAML data to temp file
	if _, err := tempFile.Write(data); err != nil {
		tempFile.Close()
		return fmt.Errorf("failed to write temp file: %w", err)
	}

	// Sync to disk for durability
	if err := tempFile.Sync(); err != nil {
		tempFile.Close()
		return fmt.Errorf("failed to sync temp file: %w", err)
	}

	if err := tempFile.Close(); err != nil {
		return fmt.Errorf("failed to close temp file: %w", err)
	}

	// 4. Atomic rename temp -> target path
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("failed to rename temp file to target: %w", err)
	}

	// HAPPY PATH
	return nil
}

// copyFile copies a file from src to dst, preserving permissions.
func copyFile(src, dst string) error {
	// Open source file
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	// Get source file info for permissions
	info, err := sourceFile.Stat()
	if err != nil {
		return err
	}

	// Create destination file
	destFile, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode())
	if err != nil {
		return err
	}
	defer destFile.Close()

	// Copy content
	if _, err := destFile.ReadFrom(sourceFile); err != nil {
		return err
	}

	return destFile.Sync()
}

// LoadFromEnv loads configuration from environment variables.
func LoadFromEnv() *GatewayConfig {
	cfg := &GatewayConfig{}

	// Load required fields from env
	cfg.GatewayID = os.Getenv("GATEWAY_ID")
	cfg.CompanyID = os.Getenv("COMPANY_ID")
	cfg.CloudURL = os.Getenv("CLOUD_URL")
	cfg.JWTSecret = os.Getenv("JWT_SECRET")

	// Load optional fields with defaults
	if val := os.Getenv("SYNC_INTERVAL"); val != "" {
		if d, err := time.ParseDuration(val); err == nil {
			cfg.SyncInterval = d
		}
	}
	if val := os.Getenv("BATCH_SIZE"); val != "" {
		if n, err := strconv.Atoi(val); err == nil {
			cfg.BatchSize = n
		}
	}
	if val := os.Getenv("MAX_RETRIES"); val != "" {
		if n, err := strconv.Atoi(val); err == nil {
			cfg.MaxRetries = n
		}
	}
	if val := os.Getenv("HEALTH_PORT"); val != "" {
		if n, err := strconv.Atoi(val); err == nil {
			cfg.HealthPort = n
		}
	}

	// Permanent listening mode fields
	if val := os.Getenv("LISTEN_MODE"); val != "" {
		cfg.ListenMode = val
	}
	if val := os.Getenv("HEARTBEAT_INTERVAL"); val != "" {
		if d, err := time.ParseDuration(val); err == nil {
			cfg.HeartbeatInterval = d
		}
	}
	if val := os.Getenv("SOCKET_PATH"); val != "" {
		cfg.SocketPath = val
	}
	if val := os.Getenv("ADAPTIVE_DELAY_RECENT_WINDOW"); val != "" {
		if d, err := time.ParseDuration(val); err == nil {
			cfg.AdaptiveDelayRecentWindow = d
		}
	}
	if val := os.Getenv("ADAPTIVE_DELAY_STALE_WINDOW"); val != "" {
		if d, err := time.ParseDuration(val); err == nil {
			cfg.AdaptiveDelayStaleWindow = d
		}
	}
	if val := os.Getenv("ADAPTIVE_DELAY_RECENT"); val != "" {
		if d, err := time.ParseDuration(val); err == nil {
			cfg.AdaptiveDelayRecent = d
		}
	}
	if val := os.Getenv("ADAPTIVE_DELAY_STALE"); val != "" {
		if d, err := time.ParseDuration(val); err == nil {
			cfg.AdaptiveDelayStale = d
		}
	}
	if val := os.Getenv("ADAPTIVE_DELAY_AUTO_READING"); val != "" {
		if d, err := time.ParseDuration(val); err == nil {
			cfg.AdaptiveDelayAutoReading = d
		}
	}
	if val := os.Getenv("RECONNECT_INITIAL_BACKOFF"); val != "" {
		if d, err := time.ParseDuration(val); err == nil {
			cfg.ReconnectInitialBackoff = d
		}
	}
	if val := os.Getenv("RECONNECT_MAX_BACKOFF"); val != "" {
		if d, err := time.ParseDuration(val); err == nil {
			cfg.ReconnectMaxBackoff = d
		}
	}

	// Apply defaults
	cfg.ApplyDefaults()

	return cfg
}

// ApplyDefaults sets default values for optional fields.
func (c *GatewayConfig) ApplyDefaults() {
	if c.SyncInterval == 0 {
		c.SyncInterval = 30 * time.Second
	}
	if c.BatchSize == 0 {
		c.BatchSize = 100
	}
	if c.MaxRetries == 0 {
		c.MaxRetries = 5
	}
	if c.HealthPort == 0 {
		c.HealthPort = 8080
	}
	if c.DataPath == "" {
		c.DataPath = "./data"
	}

	// Permanent listening mode defaults (REQ-A006)
	if c.ListenMode == "" {
		c.ListenMode = "auto"
	}
	if c.HeartbeatInterval == 0 {
		c.HeartbeatInterval = 3 * time.Second
	}
	if c.HeartbeatSilenceThreshold == 0 {
		c.HeartbeatSilenceThreshold = 5 * time.Second
	}
	if c.AdaptiveDelayRecent == 0 {
		c.AdaptiveDelayRecent = 3 * time.Second
	}
	if c.AdaptiveDelayRecentWindow == 0 {
		c.AdaptiveDelayRecentWindow = 2 * time.Second
	}
	if c.AdaptiveDelayStale == 0 {
		c.AdaptiveDelayStale = 1 * time.Second
	}
	if c.AdaptiveDelayStaleWindow == 0 {
		c.AdaptiveDelayStaleWindow = 10 * time.Second
	}
	if c.AdaptiveDelayAutoReading == 0 {
		c.AdaptiveDelayAutoReading = 5 * time.Second
	}
	if c.ReconnectInitialBackoff == 0 {
		c.ReconnectInitialBackoff = 1 * time.Second
	}
	if c.ReconnectMaxBackoff == 0 {
		c.ReconnectMaxBackoff = 30 * time.Second
	}
	if c.SocketPath == "" {
		c.SocketPath = "/tmp/amg-rfid-gateway.sock"
	}

	// Web UI defaults
	if c.WebPort == 0 {
		c.WebPort = 9090
	}
	if c.WebListenAddr == "" {
		c.WebListenAddr = "0.0.0.0"
	}
	if c.SyncToolsInterval == 0 {
		c.SyncToolsInterval = 1 * time.Hour
	}
	if c.ConfirmationRetryInterval == 0 {
		c.ConfirmationRetryInterval = 30 * time.Second
	}

	// Queue cap defaults (REQ-S004)
	// Default values: MaxPendingConfirmations = 10000, PendingWarningThreshold = 1000
	// nil means "use default", explicit 0 means "unlimited" or "never warn"
	if c.MaxPendingConfirmations == nil {
		defaultMax := 10000
		c.MaxPendingConfirmations = &defaultMax
	}
	if c.PendingWarningThreshold == nil {
		defaultThreshold := 1000
		c.PendingWarningThreshold = &defaultThreshold
	}
}
