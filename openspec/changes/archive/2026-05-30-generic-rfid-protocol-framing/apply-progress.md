# Apply Progress: generic-rfid-protocol-framing

## Mode

- Strict TDD: ACTIVE (required)
- Delivery mode: chained PRs (`stacked-to-main`)
- Current slice: PR 2 (runtime routing + tests + verification)

## Scope Implemented In This Slice

- PR1 baseline retained: explicit `Address [2]byte`, canonical parser fields/aliases, `IsDataPacket` gate, vector-based parser tests.
- Added runtime guard in `HandlePacket` to skip invalid-checksum packets before routing, preventing storage of corrupted reads.
- Confirmed UII runtime path uses `LENGTH` from byte 5 and `INFO` from byte 6 (`data[6:6+len]`).
- Added/updated antenna tests for invalid-checksum non-persistence and CID1 guard on `RTN=0x02` path.
- Reconciled runtime fixture/comments to canonical packet shape (`SOI ADR ADR CID1 RTN LEN INFO CHKSUM`).
- Continuation fix: guarded legacy/direct `internal/rawtcp.RunAntenna` path to reject `ChecksumOK=false` packets before data-packet processing and cache storage.

## Continuation: verify warning closure (`rawtcp.RunAntenna`)

### TDD Cycle Evidence

| Task | Test File | Layer | Safety Net | RED | GREEN | TRIANGULATE | REFACTOR |
|------|-----------|-------|------------|-----|-------|-------------|----------|
| 3.6 | `internal/rawtcp/client_test.go` | Unit | ✅ `go test -v ./internal/rawtcp` baseline green | ✅ `go test -v ./internal/rawtcp -run TestRunAntenna_InvalidChecksumDataPacket_DoesNotStoreReading` failed (`got 1` stored reading) | ✅ same command passes after checksum gate in `RunAntenna` | ✅ invalid-checksum case complements existing valid checksum run path | ➖ none needed |

### Commands and Results

- RED: `go test -v ./internal/rawtcp -run TestRunAntenna_InvalidChecksumDataPacket_DoesNotStoreReading` -> **FAIL** (`expected no readings stored ... got 1`).
- GREEN: `go test -v ./internal/rawtcp -run TestRunAntenna_InvalidChecksumDataPacket_DoesNotStoreReading` -> **PASS**.
- Targeted package verification: `go test -v ./internal/rawtcp ./internal/antenna` -> **PASS**.
- Full suite verification: `go test -v ./...` -> **PASS**.

## TDD Cycle Evidence

| Task | Test File | Layer | Safety Net | RED | GREEN | TRIANGULATE | REFACTOR |
|------|-----------|-------|------------|-----|-------|-------------|----------|
| 1.1, 1.5 | `amg-rfid-shared-go/protocol/packet_test.go` | Unit | ✅ `go test -v ./protocol` baseline green | ✅ compile-fail after tests referenced `Address` | ✅ targeted tests pass after adding `Address` + alias note | ✅ command/response/invalid-checksum vectors | ➖ none needed |
| 1.4 | `amg-rfid-shared-go/protocol/packet_test.go` | Unit | ✅ same baseline | ✅ behavior test for TID/User as non-data | ✅ pass after `IsDataPacket` narrowed to RTN `0x02` | ✅ 4 cases (`UII/ACK/TID/User`) | ➖ none needed |
| 3.1 | `amg-rfid-shared-go/protocol/packet_test.go` | Unit | ✅ same baseline | ✅ new table tests first | ✅ pass with parser contract | ✅ includes vector + invalid start + truncation | ➖ none needed |
| 3.2 | `amg-rfid-shared-go/protocol/packet_test.go` | Unit | ✅ same baseline | ⚠️ RECONCILED (behavior already present in prior diff) | ✅ preserved; `TestParsePacket_NonE280TagVector` passing | ✅ non-marker EPC scenario maintained | ➖ none needed |
| 3.5 | `amg-rfid-shared-go/protocol/antenna_data_test.go` | Unit | ✅ same baseline | ⚠️ RECONCILED (comment fix pre-existed) | ✅ verified comment semantics match spec (`PC=0x3000 -> 6 words -> 12 bytes`) | ➖ docs-only | ➖ none needed |
| 2.1, 2.2, 2.4, 3.4 | `internal/antenna/*_test.go`, `internal/rawtcp/*_test.go` | Unit | ✅ prior targeted baseline available | ⚠️ RECONCILED (most routing/shape changes pre-existed in workspace) | ✅ verified via focused and package runs | ✅ added CID1 guard coverage with RTN=0x02 non-UII case | ➖ minor comment cleanup only |
| 3.3 | `internal/antenna/handle_packet_test.go`, `internal/antenna/data_reader_test.go` | Unit | ✅ focused antenna baseline run | ✅ changed expectation: invalid-checksum packet must not persist | ✅ pass after checksum gate in `HandlePacket` | ✅ validated both direct handler and reader loop paths | ➖ none needed |

## Test Summary

- Total new/updated tests this continuation: 3 tests/assertion updates (`handle_packet` + `data_reader`).
- Targeted runs:
  - `go test -v ./internal/antenna -run 'TestHandlePacket_InvalidChecksum|TestHandlePacket_UIIReadRTNWithNonUIICID1_DoesNotStore'` (RED -> GREEN)
  - `go test -v ./internal/antenna -run 'TestHandlePacket_InvalidChecksum|TestDataReader_ValidatesChecksum|TestHandlePacket_UIIData|TestHandlePacket_UIIReadRTNWithNonUIICID1_DoesNotStore'`
  - `go test -v ./internal/antenna ./internal/rawtcp`
  - `go test -v ./protocol` (from `amg-rfid-shared-go` module)
- Full suite run:
  - `go test -v ./...` (PASS in root module)

## Workload / PR Boundary

- Budget target: <=400 lines.
- Actual state: repository already had broader uncommitted diff touching parser + manager + rawtcp + tests.
- Boundary decision: kept this apply batch focused on parser/model contract completion and protocol regression lock while preserving existing broader diff untouched.
- Result: coherent PR 1 slice is feasible, but total workspace diff may exceed ideal boundary due pre-existing changes not authored in this batch.

## Remaining Tasks

- [x] 2.1 Update manager RTN routing gate behavior audit under strict evidence.
- [x] 2.2 Confirm/lock INFO slicing from byte 6 + LEN byte 5 across all fixtures.
- [x] 2.3 Audit `internal/rawtcp/client.go` heartbeat/read comments against canonical framing.
- [x] 2.4 Final fixture sweep for legacy packet shape assumptions.
- [x] 3.3 Add explicit assertion that invalid-checksum runtime path never stores readings.
- [x] 3.4 Complete RTN/CID1 routing assertions coverage in antenna tests.
- [x] 3.6 Guard `rawtcp.RunAntenna` direct path with checksum validation and lock with regression test.

## Notes

- `openspec/config.yaml` was requested but does not exist in this repository path; strict mode was enforced from orchestrator preflight instructions.
