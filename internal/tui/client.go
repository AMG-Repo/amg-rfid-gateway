// Package tui provides the Bubbletea-based terminal UI for gateway configuration.
package tui

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/amg-rfid/amg-rfid-gateway/internal/antenna"
	"github.com/amg-rfid/amg-rfid-gateway/internal/config"
	"github.com/amg-rfid/amg-rfid-shared-go/models"
)

const defaultTimeout = 3 * time.Second

// BridgeClient provides a Unix socket client for TUI-to-gateway communication.
type BridgeClient struct {
	socketPath string
	timeout    time.Duration
}

// NewBridgeClient creates a new bridge client.
func NewBridgeClient(socketPath string) *BridgeClient {
	if socketPath == "" {
		socketPath = defaultSocketPath
	}

	return &BridgeClient{
		socketPath: socketPath,
		timeout:    defaultTimeout,
	}
}

// GetStatus retrieves the gateway health status from the bridge server.
func (c *BridgeClient) GetStatus() (models.GatewayHealthStatus, error) {
	var status models.GatewayHealthStatus

	req := BridgeRequest{
		Method: "GET",
		Path:   "/status",
	}

	resp, err := c.sendRequest(req)
	if err != nil {
		return status, err
	}

	if !resp.Success {
		return status, errors.New(resp.Error)
	}

	// Convert response data to GatewayHealthStatus
	dataBytes, err := json.Marshal(resp.Data)
	if err != nil {
		return status, fmt.Errorf("failed to marshal response data: %w", err)
	}

	if err := json.Unmarshal(dataBytes, &status); err != nil {
		return status, fmt.Errorf("failed to unmarshal status: %w", err)
	}

	return status, nil
}

// GetAntennas retrieves the antenna statuses from the bridge server.
func (c *BridgeClient) GetAntennas() ([]antenna.AntennaStatus, error) {
	req := BridgeRequest{
		Method: "GET",
		Path:   "/antennas",
	}

	resp, err := c.sendRequest(req)
	if err != nil {
		return nil, err
	}

	if !resp.Success {
		return nil, errors.New(resp.Error)
	}

	// Convert response data to []AntennaStatus
	dataBytes, err := json.Marshal(resp.Data)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal response data: %w", err)
	}

	var antennas []antenna.AntennaStatus
	if err := json.Unmarshal(dataBytes, &antennas); err != nil {
		return nil, fmt.Errorf("failed to unmarshal antennas: %w", err)
	}

	return antennas, nil
}

// GetConfig retrieves the gateway configuration from the bridge server.
func (c *BridgeClient) GetConfig() (*config.GatewayConfig, error) {
	req := BridgeRequest{
		Method: "GET",
		Path:   "/config",
	}

	resp, err := c.sendRequest(req)
	if err != nil {
		return nil, err
	}

	if !resp.Success {
		return nil, errors.New(resp.Error)
	}

	// Convert response data to GatewayConfig
	dataBytes, err := json.Marshal(resp.Data)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal response data: %w", err)
	}

	var cfg config.GatewayConfig
	if err := json.Unmarshal(dataBytes, &cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	return &cfg, nil
}

// UpdateConfig sends a configuration update to the bridge server.
func (c *BridgeClient) UpdateConfig(cfg *config.GatewayConfig) error {
	// Marshal config to JSON
	body, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	req := BridgeRequest{
		Method: "POST",
		Path:   "/config",
		Body:   body,
	}

	resp, err := c.sendRequest(req)
	if err != nil {
		return err
	}

	if !resp.Success {
		return errors.New(resp.Error)
	}

	return nil
}

// ReloadConfig triggers a configuration reload on the bridge server.
func (c *BridgeClient) ReloadConfig() error {
	req := BridgeRequest{
		Method: "POST",
		Path:   "/config/reload",
	}

	resp, err := c.sendRequest(req)
	if err != nil {
		return err
	}

	if !resp.Success {
		return errors.New(resp.Error)
	}

	return nil
}

// sendRequest sends a request to the bridge server and returns the response.
func (c *BridgeClient) sendRequest(req BridgeRequest) (BridgeResponse, error) {
	var resp BridgeResponse

	// Connect to server
	conn, err := net.Dial("unix", c.socketPath)
	if err != nil {
		return resp, fmt.Errorf("failed to connect to bridge: %w", err)
	}
	defer conn.Close()

	// Set deadline
	conn.SetDeadline(time.Now().Add(c.timeout))

	// Send request
	encoder := json.NewEncoder(conn)
	if err := encoder.Encode(req); err != nil {
		return resp, fmt.Errorf("failed to send request: %w", err)
	}

	// Read response
	decoder := json.NewDecoder(conn)
	if err := decoder.Decode(&resp); err != nil {
		return resp, fmt.Errorf("failed to read response: %w", err)
	}

	return resp, nil
}

// IsAvailable checks if the bridge server is available.
func (c *BridgeClient) IsAvailable() bool {
	conn, err := net.Dial("unix", c.socketPath)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}
