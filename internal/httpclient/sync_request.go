package httpclient

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/amg-rfid/amg-rfid-shared-go/models"
)

// SendSyncRequest uploads a batch of raw readings to the VPS.
// Calls POST /api/v1/gateway/sync (GatewayAuth). Any non-2xx status is a
// *StatusError and returns no response, so the caller keeps the readings
// pending or, by the status, splits the batch.
func (c *VPSClient) SendSyncRequest(syncReq models.SyncRequest) (*models.SyncResponse, error) {
	url := fmt.Sprintf("%s/api/v1/gateway/sync", c.baseURL)

	jsonBody, err := json.Marshal(syncReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := c.doAuthed(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%s: %w", http.StatusText(resp.StatusCode), &StatusError{Code: resp.StatusCode, Body: string(body)})
	}

	var syncResp models.SyncResponse
	if err := json.Unmarshal(body, &syncResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return &syncResp, nil
}
