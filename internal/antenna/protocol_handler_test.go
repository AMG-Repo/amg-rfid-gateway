package antenna

import (
	"errors"
	"testing"

	"github.com/amg-rfid/amg-rfid-gateway/internal/config"
)

func TestProtocolHandlerFor(t *testing.T) {
	tests := []struct {
		name           string
		protocol       config.AntennaProtocol
		wantProtocol   config.AntennaProtocol
		wantErrOnFrame bool
	}{
		{
			name:         "generic returns generic handler",
			protocol:     config.ProtocolGeneric,
			wantProtocol: config.ProtocolGeneric,
		},
		{
			name:           "zebra returns unsupported handler",
			protocol:       config.ProtocolZebra,
			wantProtocol:   config.ProtocolZebra,
			wantErrOnFrame: true,
		},
		{
			name:           "unknown returns unsupported handler",
			protocol:       config.AntennaProtocol("alien"),
			wantProtocol:   config.AntennaProtocol("alien"),
			wantErrOnFrame: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := ProtocolHandlerFor(tt.protocol)
			if handler.Protocol() != tt.wantProtocol {
				t.Fatalf("expected protocol %q, got %q", tt.wantProtocol, handler.Protocol())
			}

			err := handler.HandleFrame(PacketContext{}, validUIITestPacket())
			if tt.wantErrOnFrame {
				if !errors.Is(err, ErrUnsupportedProtocol) {
					t.Fatalf("expected ErrUnsupportedProtocol, got %v", err)
				}
				return
			}

			if err == nil {
				t.Fatal("expected generic handler to delegate parsing and fail without cache")
			}
			if errors.Is(err, ErrUnsupportedProtocol) {
				t.Fatalf("generic handler must not return unsupported error: %v", err)
			}
		})
	}
}

func validUIITestPacket() []byte {
	packet := []byte{
		0x7c, 0xff, 0xff, 0x20, 0x02, 0x10,
		0x00,
		0x30, 0x00,
		0xE2, 0x00, 0x34, 0x11, 0xB8, 0x02, 0x01, 0x13, 0x83, 0x25, 0x85, 0x66,
		0xC9,
	}
	return append(packet, calculateTestChecksum(packet))
}
