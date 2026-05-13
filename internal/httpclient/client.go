// Package httpclient provides an HTTP client for VPS REST API communication.
package httpclient

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/amg-rfid/amg-rfid-gateway/internal/localstore"
	"github.com/amg-rfid/amg-rfid-gateway/internal/sync"
)

// Ensure type compatibility with sync package
var _ = sync.SyncDataResponse{}

// GatewayConfirmRequest represents the request body for gateway confirm endpoint.
type GatewayConfirmRequest struct {
	CompanyID string  `json:"company_id"`
	UII       string  `json:"uii"`
	Action    string  `json:"action"`
	AntennaID *string `json:"antenna_id,omitempty"`
}

// VPSClient provides HTTP client for VPS REST API.
type VPSClient struct {
	baseURL    string
	httpClient *http.Client
	timeout    time.Duration
	jwtToken   string
}

// BaseURL returns the VPS API base URL.
func (c *VPSClient) BaseURL() string {
	return c.baseURL
}

// NewVPSClient creates a new VPS HTTP client.
// baseURL: The VPS API base URL (e.g., "http://vps.example.com")
// timeout: Request timeout (0 defaults to 30 seconds)
// jwtToken: JWT token for authentication (can be empty for unauthenticated endpoints)
func NewVPSClient(baseURL string, timeout time.Duration, jwtToken string) *VPSClient {
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	// Remove trailing slash from baseURL
	baseURL = strings.TrimSuffix(baseURL, "/")

	return &VPSClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: timeout,
		},
		timeout:  timeout,
		jwtToken: jwtToken,
	}
}

// FetchTools retrieves all tools for a company from the VPS.
// Calls GET /api/v1/tools?company_id={company_id}
func (c *VPSClient) FetchTools(companyID string) ([]localstore.Tool, error) {
	url := fmt.Sprintf("%s/api/v1/tools?company_id=%s", c.baseURL, companyID)

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var tools []localstore.Tool
	if err := json.Unmarshal(body, &tools); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return tools, nil
}

// FetchUsers retrieves all users for a company from the VPS.
// Calls GET /api/v1/users?company_id={company_id}
func (c *VPSClient) FetchUsers(companyID string) ([]localstore.User, error) {
	url := fmt.Sprintf("%s/api/v1/users?company_id=%s", c.baseURL, companyID)

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var users []localstore.User
	if err := json.Unmarshal(body, &users); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return users, nil
}

// SendConfirmation sends a confirmation to the VPS.
// Calls POST /api/v1/tools/confirm (legacy endpoint)
func (c *VPSClient) SendConfirmation(companyID, uii, action string) error {
	return c.SendConfirmationV2(companyID, uii, action, "")
}

// SendConfirmationV2 sends a confirmation to the VPS with optional antenna ID.
// Calls POST /api/v1/gateway/confirm (new gateway-specific endpoint)
// This endpoint requires GatewayAuth (JWT token).
func (c *VPSClient) SendConfirmationV2(companyID, uii, action, antennaID string) error {
	url := fmt.Sprintf("%s/api/v1/gateway/confirm", c.baseURL)

	payload := GatewayConfirmRequest{
		CompanyID: companyID,
		UII:       uii,
		Action:    action,
	}
	if antennaID != "" {
		payload.AntennaID = &antennaID
	}

	jsonBody, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, url, bytes.NewBuffer(jsonBody))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	// Add Authorization header for protected gateway endpoint
	if c.jwtToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.jwtToken)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("unexpected status code: %d, body: %s", resp.StatusCode, string(body))
	}

	return nil
}

// FetchSyncData retrieves flattened tool+tag sync data from the VPS gateway endpoint.
// Calls GET /api/v1/gateway/sync-data?company_id={company_id}
// This endpoint requires GatewayAuth (JWT token).
// Returns *sync.SyncDataResponse to match the sync package VPSClient interface.
func (c *VPSClient) FetchSyncData(companyID string) (*sync.SyncDataResponse, error) {
	url := fmt.Sprintf("%s/api/v1/gateway/sync-data?company_id=%s", c.baseURL, companyID)

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	// Add Authorization header for protected gateway endpoint
	if c.jwtToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.jwtToken)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var syncResp sync.SyncDataResponse
	if err := json.Unmarshal(body, &syncResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if syncResp.Status != "OK" {
		return nil, fmt.Errorf("unexpected response status: %s", syncResp.Status)
	}

	return &syncResp, nil
}

// GatewayConfirmResponse is the response from the gateway confirm endpoint.
type GatewayConfirmResponse struct {
	Status    string `json:"status"`
	UII       string `json:"uii"`
	Action    string `json:"action"`
	Location  string `json:"location"`
	Timestamp string `json:"timestamp"`
}

// SendGatewayConfirmation sends a confirmation to the VPS gateway endpoint with antenna_id.
// Calls POST /api/v1/gateway/confirm
// This endpoint requires GatewayAuth (JWT token).
func (c *VPSClient) SendGatewayConfirmation(companyID, uii, action, antennaID string) error {
	url := fmt.Sprintf("%s/api/v1/gateway/confirm", c.baseURL)

	payload := GatewayConfirmRequest{
		CompanyID: companyID,
		UII:       uii,
		Action:    action,
	}
	if antennaID != "" {
		payload.AntennaID = &antennaID
	}

	jsonBody, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, url, bytes.NewBuffer(jsonBody))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	// Add Authorization header for protected gateway endpoint
	if c.jwtToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.jwtToken)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("unexpected status code: %d, body: %s", resp.StatusCode, string(body))
	}

	// Parse response to verify success
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}

	var confirmResp GatewayConfirmResponse
	if err := json.Unmarshal(body, &confirmResp); err != nil {
		// Non-JSON success response is acceptable
		return nil
	}

	if confirmResp.Status != "OK" {
		return fmt.Errorf("confirmation failed: %s", confirmResp.Status)
	}

	return nil
}
