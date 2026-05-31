# Design: Dynamic Antenna Protocol Configuration

## Technical Approach

Add protocol as an explicit per-antenna configuration concern, then select runtime packet handling through a strategy boundary. Existing Generic RFID behavior remains the only fully supported parser; Zebra is configurable but handled as an explicit unsupported/future protocol boundary until exact framing details are implemented.

## Architecture Decisions

| Decision | Choice | Alternatives considered | Rationale |
|---|---|---|---|
| Protocol type | Define `type AntennaProtocol string` and constants in `internal/config/config.go`: `generic`, `zebra` | Free-form strings at call sites | Centralizes allowlist/defaulting and prevents typo-driven fallback. |
| Backward compatibility | `AntennaConfig.Protocol yaml:"protocol,omitempty"`; `ApplyDefaults()` sets missing/empty antenna protocol to `generic` | Require config migration before startup | Existing YAML must boot unchanged. Saving after load may include protocol depending on YAML tags/zero value; behavior relies on effective default, not manual migration. |
| Runtime dispatch | Create protocol handler boundary in `internal/antenna` and reuse it from `manager.go`; mirror only if `internal/rawtcp.RunAntenna` remains supported | Branch inline in `HandlePacket`/`RunAntenna` | Keeps Generic parsing isolated and ensures non-generic never reaches Generic by accident. |
| Zebra | Register `zebra` as known-but-unsupported handler returning `ErrUnsupportedProtocol` | Pretend Zebra uses Generic frames | Safer and honest: configurability now, compatibility only after protocol details exist. |

## Data Flow

```text
YAML/TUI ──→ config.ApplyDefaults/Validate ──→ AntennaManager
                                             └─→ ProtocolHandlerFor(protocol)
TCP bytes ──→ Generic stream extractor ──→ handler.HandleFrame(frame)
                                      generic ──→ parse/store/event
                                      zebra/unknown ──→ explicit error, no store
```

## File Changes

| File | Action | Description |
|------|--------|-------------|
| `internal/config/config.go` | Modify | Add `AntennaProtocol`, constants, allowlist helper, `Protocol` field, antenna defaults and validation. `GatewayConfig.Validate()` should validate each antenna. |
| `configs/config.example.yaml` | Modify | Show `protocol: generic`; Zebra example uses `protocol: zebra` and notes unsupported/future handler. |
| `internal/tui/screens/settings_screen.go` | Modify | Add editable antenna protocol fields, render defaulted protocol, validate through config allowlist, and update only selected antenna protocol in `GetConfig()`. |
| `internal/tui/app.go` | Modify | Existing save flow remains; ensure settings resize also reaches settings screen if touched. |
| `internal/antenna/protocol_handler.go` | Create | Handler interface, registry/factory, `ErrUnsupportedProtocol`, Generic and Unsupported handlers. |
| `internal/antenna/manager.go` | Modify | Resolve handler from `m.config.Protocol`; `HandlePacket` delegates after shared activity update. Generic handler preserves current ACK/UII/tag/error/heartbeat behavior. |
| `internal/rawtcp/client.go` | Modify | Add protocol to local `AntennaConfig` or replace with `config.AntennaConfig`; dispatch via same handler if this legacy loop remains used. |
| Tests listed in strategy | Modify/rewrite | Replace brittle Generic-internals assertions with behavior contracts. |

## Interfaces / Contracts

```go
type AntennaProtocol string
const (
    ProtocolGeneric AntennaProtocol = "generic"
    ProtocolZebra   AntennaProtocol = "zebra"
)

type ProtocolHandler interface {
    Protocol() config.AntennaProtocol
    HandleFrame(ctx PacketContext, frame []byte) error
}

type PacketContext struct {
    AntennaID string
    GatewayID string
    Cache     Cache
    EventBus  *events.EventBus
    Stats     ReadingStatsUpdater
}
```

`HandlerFor(protocol)` MUST return Generic only for `generic`. `zebra` and unknown values return explicit unsupported errors and MUST NOT parse/store data.

## Testing Strategy

| Layer | What to Test | Approach |
|-------|-------------|----------|
| Unit | Config default/validate/save round-trip | Table-driven tests in `internal/config/*_test.go`; missing protocol becomes `generic`, unsupported rejected. |
| Unit | TUI display/edit/preserve fields | Rewrite settings tests to cover antenna protocol field editing, validation, and unchanged unrelated fields. |
| Unit | Runtime dispatch safety | New handler/manager tests: Generic stores same UII/RSSI behavior; Zebra/unknown returns unsupported and stores nothing. |
| Integration | Startup validation blocks unsupported config | Existing config/main tests with temp YAML. |

Legacy tests tied to old hardcoded Generic internals may be deleted/recreated when replacement tests assert observable behavior.

## Migration / Rollout

No manual migration required. Missing protocol defaults to `generic`; invalid protocols fail during config validation before antenna runtime starts. Rollback can lock TUI choices to `generic` while keeping the field compatible.

## Open Questions

- [ ] Exact Zebra framing/LLRP details remain unknown; full Zebra reading compatibility is intentionally out of scope.
