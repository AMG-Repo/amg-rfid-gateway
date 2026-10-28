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

func TestAntennaConfig_PortLocationsValidation(t *testing.T) {
	base := func(protocol AntennaProtocol, locations map[int]string) AntennaConfig {
		return AntennaConfig{ID: "ur4-1", IP: "192.168.99.202", Port: 8888, Enabled: true, Protocol: protocol, PortLocations: locations}
	}

	tests := []struct {
		name      string
		antenna   AntennaConfig
		wantError string
	}{
		{name: "port 1 is the lower bound", antenna: base(ProtocolChainway, map[int]string{1: "entrada-x"})},
		{name: "port 4 is the upper bound", antenna: base(ProtocolChainway, map[int]string{4: "salida-x"})},
		{name: "port 0 is rejected", antenna: base(ProtocolChainway, map[int]string{0: "entrada-x"}), wantError: "port_locations port 0"},
		{name: "port 5 is rejected", antenna: base(ProtocolChainway, map[int]string{5: "entrada-x"}), wantError: "port_locations port 5"},
		{name: "empty location is rejected", antenna: base(ProtocolChainway, map[int]string{1: ""}), wantError: "port_locations port 1 location cannot be empty"},
		{name: "blank location is rejected", antenna: base(ProtocolChainway, map[int]string{2: "   "}), wantError: "port_locations port 2 location cannot be empty"},
		{name: "generic with port_locations is rejected", antenna: base(ProtocolGeneric, map[int]string{1: "entrada-x"}), wantError: "port_locations is only supported with protocol chainway"},
		{name: "legacy empty protocol with port_locations is rejected", antenna: base("", map[int]string{1: "entrada-x"}), wantError: "port_locations is only supported with protocol chainway"},
		{name: "zebra with port_locations is rejected", antenna: base(ProtocolZebra, map[int]string{1: "entrada-x"}), wantError: "port_locations is only supported with protocol chainway"},
		{name: "generic without port_locations stays valid", antenna: base(ProtocolGeneric, nil)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.antenna.Validate()
			if tt.wantError == "" {
				if err != nil {
					t.Fatalf("Validate: %v", err)
				}
				return
			}
			if err == nil || !contains(err.Error(), tt.wantError) {
				t.Fatalf("Validate error = %v, want it to contain %q", err, tt.wantError)
			}
		})
	}
}
