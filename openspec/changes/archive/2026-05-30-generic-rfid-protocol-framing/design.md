# Design: Generic RFID Protocol Framing

## Technical Approach

Reconcile the current uncommitted framing migration into a clean, explicit parser contract for Generic RFID Reader Control Protocol frames: `SOI ADR(2) CID1 CID2/RTN LENGTH INFO CHKSUM`.

The parser in `amg-rfid-shared-go/protocol` becomes the single source of truth for byte indexes, checksum validity, and canonical packet fields (`SOI`, `address`, `CID1`, `CID2OrRTN`, `Length`, `Info`, `RawHex`, `ChecksumOK`).

`ParseAntennaData` stays intentionally narrow: it parses only `INFO = ANT + PC + EPC + RSSI` and does not interpret framing/header bytes.

`internal/antenna` routes by RTN (byte index 4) and gates UII handling on `CID1=0x20` + `RTN=0x02`.

Both command start (`0x7C`) and response start (`0xCC`) are valid at parser level. TCP stream reassembly remains out of scope.

## Architecture Decisions

### Decision: Explicit frame semantics in ParsedPacket

**Choice**: Keep semantic fields in `ParsedPacket` (`SOI`, `Address [2]byte`, `CID1`, `CID2OrRTN`, `Length`, `Info`, `RawHex`, `ChecksumOK`) and derive legacy aliases from them.
**Alternatives considered**: Keep only legacy `CommandCode/DataLength/Data` or map RTN directly into `CommandCode`.
**Rationale**: Prevents byte-role ambiguity, makes tests reflect protocol docs, and localizes compatibility debt.

### Decision: Compatibility alias retained short-term

**Choice**: Keep `CommandCode`, `DataLength`, and `Data` as compatibility aliases to `CID1`, `Length`, and `Info`, with migration notes.
**Alternatives considered**: Remove aliases immediately.
**Rationale**: Existing call sites and tests still reference legacy names; immediate removal expands risk. Alias behavior must be documented as potentially misleading for callers that expect RTN semantics.

### Decision: AntennaManager routes on RTN + CID1 gate

**Choice**: Route handlers on RTN (`data[4]`) and process UII data only when `CID1==0x20 && RTN==0x02`.
**Alternatives considered**: Route by CID1 or mixed heuristics.
**Rationale**: Matches protocol framing while preventing accidental UII parsing for non-UII response families.

## Data Flow

`net.Conn.Read` chunk -> `AntennaManager.dataReader` -> checksum validation (warn-only on mismatch) -> `HandlePacket` RTN routing -> `handleUIIData` slice `INFO` -> `protocol.ParseAntennaData` -> `models.Reading` -> cache/event bus.

`SendReadUII` and read loop command emission construct `7C FF FF 20 00 00 66` via shared checksum logic.

## File Changes

| File | Action | Description |
|------|--------|-------------|
| `amg-rfid-shared-go/protocol/packet.go` | Modify | Finalize framing indexes, accept `0x7C/0xCC`, expose checksum status, and normalize semantic fields plus compatibility aliases. |
| `amg-rfid-shared-go/protocol/packet_test.go` | Modify | Replace ad-hoc tests with table-driven vector tests for command/response framing, truncation, start-byte validation, and checksum outcomes. |
| `amg-rfid-shared-go/protocol/antenna_data_test.go` | Modify | Keep focus on INFO parsing only; verify PC-derived EPC length and RSSI behavior with real vectors. |
| `internal/antenna/manager.go` | Modify | Route by RTN at byte 4; parse INFO from byte 6; gate UII on `CID1=0x20` + `RTN=0x02`; preserve current lifecycle behavior. |
| `internal/antenna/handle_packet_test.go` | Modify | Table-driven RTN routing/state-transition tests (ACK/UII/tag/error/heartbeat/unknown) using protocol-shaped frames. |
| `internal/antenna/data_reader_test.go` | Modify | Use realistic frames to verify read-loop behavior and checksum exposure without adding stream reassembly. |
| `internal/rawtcp/client.go` | Modify | Keep explicit Read UII command framing aligned with protocol vector. |
| `internal/rawtcp/client_test.go` | Modify | Assert parser integration using CID1/RTN semantics and response start vectors. |

## Interfaces / Contracts

```go
type ParsedPacket struct {
    SOI        byte
    Address    [2]byte // ADR1, ADR2
    CID1       byte
    CID2OrRTN  byte
    Length     byte
    Info       []byte
    RawHex     string
    ChecksumOK bool

    // Compatibility aliases (temporary)
    CommandCode byte // alias of CID1
    DataLength  byte // alias of Length
    Data        []byte // alias of Info
}
```

Contract notes:
- `ParsePacket` performs framing, declared-length checks, and checksum evaluation.
- `ParseAntennaData` receives INFO only; it must not parse SOI/ADR/CID/LENGTH.
- `HandlePacket` is RTN-driven; UII decode path requires `CID1=0x20 && RTN=0x02`.

## Testing Strategy

| Layer | What to Test | Approach |
|-------|-------------|----------|
| Unit (`protocol`) | Header indexes, length math, checksum status, SOI acceptance, UII extraction conditions | Table-driven `t.Run(tt.name, ...)` with real vectors (`7CFFFF20000066`, `CCFFFF200210003000E2003411B802011383258566C983`) and negative cases |
| Unit (`protocol` INFO) | ANT/PC/EPC/RSSI parsing, PC-derived EPC size, `003000` stripping | Table-driven INFO-only vectors and truncation cases |
| Unit (`internal/antenna`) | RTN routing + manager state transitions (`readingCount`, `lastPacketTime`, `errorCount`) | Table-driven handler tests with protocol-shaped frames |
| Integration-lite (`rawtcp`) | Client/parser contract and frame semantics | Existing client tests updated to CID1/RTN-aware assertions |

## Migration / Rollout

No runtime migration required. This is a protocol correctness cleanup over current uncommitted changes.

Rollout guidance:
1. Land parser contract + vector tests first.
2. Reconcile manager/client call sites to semantic fields.
3. Keep aliases for one cycle; then remove once no call site relies on `CommandCode` as RTN.

## Open Questions

- [ ] Should `Address` be added now to `ParsedPacket` (preferred for full semantic completeness) or deferred to a follow-up to minimize churn?
- [ ] Do we want a deprecation marker/date for `CommandCode` alias removal in the next protocol cleanup change?
