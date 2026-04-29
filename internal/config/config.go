package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
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
