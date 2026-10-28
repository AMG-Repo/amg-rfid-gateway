package config

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestAntennaConfig_ChainwayProtocolIsSupported(t *testing.T) {
	a := AntennaConfig{ID: "ur4-1", IP: "192.168.1.50", Port: 8888, Enabled: true, Protocol: ProtocolChainway}
	if err := a.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestAntennaConfig_PortLocationsYAML(t *testing.T) {
	var a AntennaConfig
	src := "id: ur4-1\nprotocol: chainway\nport_locations:\n  1: entrada-x\n  2: salida-x\n"
	if err := yaml.Unmarshal([]byte(src), &a); err != nil {
		t.Fatal(err)
	}
	if a.PortLocations[1] != "entrada-x" || a.PortLocations[2] != "salida-x" || len(a.PortLocations) != 2 {
		t.Fatalf("PortLocations = %v", a.PortLocations)
	}

	out, err := yaml.Marshal(AntennaConfig{ID: "gen-1", Protocol: ProtocolGeneric})
	if err != nil {
		t.Fatal(err)
	}
	if got := string(out); contains(got, "port_locations") {
		t.Fatalf("generic antenna must omit port_locations, got:\n%s", got)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
