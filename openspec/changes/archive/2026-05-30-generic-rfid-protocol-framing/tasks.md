# Tasks: Generic RFID Protocol Framing

## Review Workload Forecast

| Field | Value |
|-------|-------|
| Estimated changed lines | 420-550 |
| 400-line budget risk | High |
| Chained PRs recommended | Yes |
| Suggested split | PR 1 → PR 2 |
| Delivery strategy | ask-always |
| Chain strategy | stacked-to-main |

Decision needed before apply: Yes
Chained PRs recommended: Yes
Chain strategy: stacked-to-main
400-line budget risk: High

### Suggested Work Units

| Unit | Goal | Likely PR | Notes |
|------|------|-----------|-------|
| 1 | Lock parser model contract, canonical indexes, and legacy alias mapping behavior. | PR 1 | Includes parser core + protocol checksum/data tests. |
| 2 | Reconcile antenna manager routing/INFO slicing and call-site tests for new header layout. | PR 2 | Includes manager + rawtcp + data-reader behavioral adjustments + full verification run. |

## Phase 1: Parser/model contract

- [x] 1.1 Update `amg-rfid-shared-go/protocol/packet.go` to enforce explicit `SOI`, `Address [2]byte`, `CID1`, `CID2OrRTN`, `Length`, `Info`, `RawHex`, and `ChecksumOK` in `ParsedPacket`.
- [x] 1.2 In `ParsePacket`, shift header parsing to byte indexes `3..5`, parse `Length` from byte `5`, extract `Info` from `6:6+Length`, and keep parsing even when checksum is wrong.
- [x] 1.3 Enforce command/response SOI semantics (`0x7C` and `0xCC`) and two’s-complement checksum calculation for `PacketStartCommand` and `PacketStartResponse` frames.
- [x] 1.4 Tighten `IsDataPacket` and `TagUID` extraction to require `CID1=0x20` + `RTN=0x02`, then parse `Info` via `ParseAntennaData`.
- [x] 1.5 Keep `CommandCode`, `DataLength`, and `Data` as temporary compatibility aliases with explicit doc notes warning they are aliases of `CID1/Length/Info`.

## Phase 2: Runtime routing and legacy call-site alignment

- [x] 2.1 Update `internal/antenna/manager.go` `HandlePacket` to read RTN from index `4`, gate UII handling on `CID1==0x20` and `RTN==0x02`, and route other RTNs per existing handlers.
- [x] 2.2 Fix `handleUIIData` to slice payload from index `6` for `INFO` and retain `Length` from index `5`.
- [x] 2.3 Audit `internal/rawtcp/client.go` command emission usage so `SendReadUII`/heartbeat framing remains explicit and comment-aligned with canonical packet shape.
- [x] 2.4 Update all in-repo call sites building packet fixtures (`internal/antenna/handle_packet_test.go`, `internal/antenna/data_reader_test.go`, `internal/rawtcp/client_test.go`, `internal/rawtcp/client_commands_test.go`) from old `[SOI PAD PAD RTN LEN ...]` to `[SOI ADR ADR CID1 RTN LEN INFO ...]`.

## Phase 3: Tests and regression lock

- [x] 3.1 Replace/extend protocol tests with table-driven vectors in `amg-rfid-shared-go/protocol/packet_test.go` for: read command `7CFFFF20000066`, response `CCFFFF200210003000E2003411B802011383258566C983`, invalid checksum variant, wrong start byte, and truncation.
- [x] 3.2 Add/adjust tests for non-marker EPC preservation (`ABCDEF0123456789`) in `ParseAntennaData` and `ParsePacket` without prefix filtering assumptions.
- [x] 3.3 Add invalid-checksum behavior checks in manager path: `ParsePacket` returns `ChecksumOK=false` but antenna reader does not persist/tag-store when checksum validation fails.
- [x] 3.4 Replace old framing assertions in `internal/antenna/handle_packet_test.go` and `internal/antenna/data_reader_test.go` with RTN byte 4 + INFO-at-6 expectations and CID1/RTN routing checks.
- [x] 3.5 Fix misleading PC comments in `amg-rfid-shared-go/protocol/antenna_data_test.go` and related tests to state PC length-as-words mapping to EPC bytes (not tag-type descriptions).
- [x] 3.6 Add regression coverage for `internal/rawtcp.RunAntenna` direct path so invalid-checksum data packets are not stored.

## Phase 4: Verification

- [x] 4.1 Run targeted checks for changed packages: `go test ./amg-rfid-shared-go/protocol ./internal/antenna ./internal/rawtcp` and capture failing scenarios around legacy-packet expectations.
- [x] 4.2 Run full repository verification: `go test ./...` and record outcomes for unresolved framing or stream-boundary assumptions.
