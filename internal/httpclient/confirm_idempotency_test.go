package httpclient

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func captureConfirm(t *testing.T) (*VPSClient, *map[string]any) {
	t.Helper()
	sent := map[string]any{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&sent)
		w.Write([]byte(`{"status":"OK"}`))
	}))
	t.Cleanup(srv.Close)
	return NewVPSClient(srv.URL, 2*time.Second, StaticToken("tok")), &sent
}

func TestSendIdempotentConfirmation_SendsIDAndOccurredAt(t *testing.T) {
	c, sent := captureConfirm(t)
	occurredAt := time.Date(2026, 11, 26, 18, 30, 1, 123456789, time.FixedZone("CST", -6*3600))

	if err := c.SendIdempotentConfirmation("co", "EPC-1", "entrada", "ant-1", "3f2b8c1e-9a4d-4e6f-8b1a-2c3d4e5f6a7b", occurredAt); err != nil {
		t.Fatalf("send failed: %v", err)
	}
	if (*sent)["confirmation_id"] != "3f2b8c1e-9a4d-4e6f-8b1a-2c3d4e5f6a7b" {
		t.Errorf("confirmation_id = %v", (*sent)["confirmation_id"])
	}
	if want := "2026-11-27T00:30:01.123456789Z"; (*sent)["occurred_at"] != want {
		t.Errorf("occurred_at = %v, want %s (RFC3339Nano UTC)", (*sent)["occurred_at"], want)
	}
}

func TestSendIdempotentConfirmation_OmitsEmptyFields(t *testing.T) {
	c, sent := captureConfirm(t)
	if err := c.SendIdempotentConfirmation("co", "EPC-1", "entrada", "ant-1", "", time.Time{}); err != nil {
		t.Fatalf("send failed: %v", err)
	}
	for _, k := range []string{"confirmation_id", "occurred_at"} {
		if _, ok := (*sent)[k]; ok {
			t.Errorf("%s sent for an empty value: %v", k, *sent)
		}
	}
}

// The legacy sender keeps its payload: no idempotency fields.
func TestSendConfirmationV2_SendsNoIdempotencyFields(t *testing.T) {
	c, sent := captureConfirm(t)
	if err := c.SendConfirmationV2("co", "EPC-1", "entrada", "ant-1"); err != nil {
		t.Fatalf("send failed: %v", err)
	}
	if _, ok := (*sent)["confirmation_id"]; ok {
		t.Errorf("legacy send carries confirmation_id: %v", *sent)
	}
}
