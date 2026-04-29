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
)

// VPSClient provides HTTP client for VPS REST API.
type VPSClient struct {
	baseURL    string
	httpClient *http.Client
	timeout    time.Duration
}

// BaseURL returns the VPS API base URL.
func (c *VPSClient) BaseURL() string {
	return c.baseURL
}

// NewVPSClient creates a new VPS HTTP client.
// baseURL: The VPS API base URL (e.g., "http://vps.example.com")
// timeout: Request timeout (0 defaults to 30 seconds)
func NewVPSClient(baseURL string, timeout time.Duration) *VPSClient {
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
		timeout: timeout,
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
// Calls POST /api/v1/tools/confirm
func (c *VPSClient) SendConfirmation(companyID, uii, action string) error {
	url := fmt.Sprintf("%s/api/v1/tools/confirm", c.baseURL)

	payload := map[string]string{
		"company_id": companyID,
		"uii":        uii,
		"action":     action,
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
