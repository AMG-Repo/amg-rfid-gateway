package models

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func sessionReading() Reading {
	first := time.Date(2026, 11, 19, 19, 0, 0, 0, time.UTC)
	return Reading{AntennaID: "almacen", GatewayID: "gw", EPC: "E280", RSSI: -60, Timestamp: first}
}

func TestReading_JSON_OmitsSessionFieldsWhenUnset(t *testing.T) {
	b, err := json.Marshal(sessionReading())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "last_seen") || strings.Contains(string(b), "read_count") {
		t.Errorf("unset session fields serialized: %s", b)
	}
}

func TestReading_JSON_CarriesSessionFields(t *testing.T) {
	r := sessionReading()
	r.LastSeen = r.Timestamp.Add(4500 * time.Millisecond)
	r.ReadCount = 312
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var back map[string]any
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if back["last_seen"] != "2026-11-19T19:00:04.5Z" || back["read_count"] != float64(312) {
		t.Errorf("session fields = last_seen %v, read_count %v; body %s", back["last_seen"], back["read_count"], b)
	}
}

func TestReading_Validation_LastSeenBeforeTimestamp(t *testing.T) {
	r := sessionReading()
	r.LastSeen = r.Timestamp.Add(-time.Second)
	r.ReadCount = 2
	if err := r.Validate(); err == nil || !strings.Contains(err.Error(), "last_seen") {
		t.Errorf("Validate() = %v; want last_seen error", err)
	}
}

func TestReading_Validation_NegativeReadCount(t *testing.T) {
	r := sessionReading()
	r.ReadCount = -1
	if err := r.Validate(); err == nil || !strings.Contains(err.Error(), "read_count") {
		t.Errorf("Validate() = %v; want read_count error", err)
	}
}

func TestReading_Validation_ValidSession(t *testing.T) {
	r := sessionReading()
	r.LastSeen = r.Timestamp
	r.ReadCount = 1
	if err := r.Validate(); err != nil {
		t.Errorf("Validate() = %v; want nil", err)
	}
}
