package antenna

import (
	"errors"
	"fmt"
	"log"

	"github.com/amg-rfid/amg-rfid-gateway/internal/config"
	"github.com/amg-rfid/amg-rfid-gateway/internal/events"
	"github.com/amg-rfid/amg-rfid-shared-go/protocol"
)

// ErrUnsupportedProtocol is returned when runtime dispatch reaches a known but unsupported protocol.
var ErrUnsupportedProtocol = errors.New("unsupported antenna protocol")

// ProtocolHandler handles one complete antenna frame for a configured protocol.
type ProtocolHandler interface {
	Protocol() config.AntennaProtocol
	HandleFrame(ctx PacketContext, frame []byte) error
}

// ReadingStatsUpdater updates runtime counters after handler outcomes.
type ReadingStatsUpdater interface {
	RecordReading(epc string, rssi int)
	RecordError()
}

// PacketContext carries shared dependencies needed by protocol handlers.
type PacketContext struct {
	AntennaID string
	GatewayID string
	Cache     Cache
	EventBus  *events.EventBus
	Stats     ReadingStatsUpdater
}

// ProtocolHandlerFor returns the handler selected for protocol.
func ProtocolHandlerFor(protocol config.AntennaProtocol) ProtocolHandler {
	if protocol == "" || protocol == config.ProtocolGeneric {
		return GenericProtocolHandler{}
	}

	return UnsupportedProtocolHandler{protocol: protocol}
}

// GenericProtocolHandler preserves the existing Generic RFID packet behavior.
type GenericProtocolHandler struct{}

// Protocol identifies this handler.
func (GenericProtocolHandler) Protocol() config.AntennaProtocol {
	return config.ProtocolGeneric
}

// HandleFrame parses and handles a complete Generic RFID frame.
func (GenericProtocolHandler) HandleFrame(ctx PacketContext, frame []byte) error {
	if len(frame) < 7 {
		return fmt.Errorf("packet too short: %d bytes (min 7)", len(frame))
	}

	if frame[0] != 0x7c && frame[0] != 0xcc {
		return fmt.Errorf("invalid start byte: 0x%02x", frame[0])
	}

	if frame[1] != 0xff || frame[2] != 0xff {
		return fmt.Errorf("invalid padding bytes")
	}

	if !protocol.ValidateChecksum(frame) {
		log.Printf("[WARN] Invalid checksum from antenna %s, skipping packet: %X", ctx.AntennaID, frame)
		return nil
	}

	cid1 := frame[3]
	rtnCode := frame[4]

	switch rtnCode {
	case protocol.RTNACK:
		log.Printf("[DEBUG] ACK received from antenna %s", ctx.AntennaID)
		return nil
	case protocol.RTNUIIRead:
		if cid1 != protocol.CID1ReadTypeCUII {
			return handleUnknownGenericRTN(ctx, frame, rtnCode)
		}
		return handleGenericUIIData(ctx, frame)
	case protocol.RTNTagData:
		log.Printf("[INFO] Tag Data received from antenna %s (not a UII scan)", ctx.AntennaID)
		return nil
	case protocol.RTNError:
		log.Printf("[ERROR] Antenna %s error: %X", ctx.AntennaID, frame)
		if ctx.Stats != nil {
			ctx.Stats.RecordError()
		}
		return nil
	case protocol.RTNHeartbeat:
		log.Printf("[DEBUG] Heartbeat response received from antenna %s", ctx.AntennaID)
		return nil
	default:
		return handleUnknownGenericRTN(ctx, frame, rtnCode)
	}
}

// UnsupportedProtocolHandler rejects frames without parsing them as Generic RFID.
type UnsupportedProtocolHandler struct {
	protocol config.AntennaProtocol
}

// Protocol identifies this handler.
func (h UnsupportedProtocolHandler) Protocol() config.AntennaProtocol {
	return h.protocol
}

// HandleFrame returns an explicit unsupported-protocol error and does not parse/store frame data.
func (h UnsupportedProtocolHandler) HandleFrame(ctx PacketContext, frame []byte) error {
	return fmt.Errorf("%w: %s", ErrUnsupportedProtocol, h.protocol)
}

func handleGenericUIIData(ctx PacketContext, frame []byte) error {
	if ctx.Cache == nil {
		return errors.New("cache is required for generic UII data")
	}

	dataLen := int(frame[5])
	if len(frame) < 6+dataLen+1 {
		return fmt.Errorf("UII data packet truncated: expected %d bytes, have %d", 6+dataLen+1, len(frame))
	}

	dataPortion := frame[6 : 6+dataLen]
	parsed, err := protocol.ParseAntennaData(dataPortion)
	if err != nil {
		return fmt.Errorf("failed to parse UII data: %w", err)
	}

	reading := parsed.ToReading(ctx.AntennaID, ctx.GatewayID)
	if err := ctx.Cache.Store(reading); err != nil {
		return fmt.Errorf("failed to store reading: %w", err)
	}

	if ctx.EventBus != nil {
		ctx.EventBus.Publish(events.TagDetected{
			EPC:       reading.EPC,
			RSSI:      reading.RSSI,
			AntennaID: ctx.AntennaID,
			Timestamp: reading.Timestamp,
		})
	}

	if ctx.Stats != nil {
		ctx.Stats.RecordReading(reading.EPC, reading.RSSI)
	}

	log.Printf("[INFO] UII detected from antenna %s: EPC=%s RSSI=%d", ctx.AntennaID, reading.EPC, reading.RSSI)
	return nil
}

func handleUnknownGenericRTN(ctx PacketContext, frame []byte, rtnCode byte) error {
	log.Printf("[WARN] Unknown RTN code 0x%02x from antenna %s: %X", rtnCode, ctx.AntennaID, frame)
	return nil
}
