# Apply Progress: Raw TCP Fragmented Frame Reassembly

## PR Slices

- Slice: PR1 — shared protocol `StreamExtractor`
- Boundary: `amg-rfid-shared-go/protocol` only; no consumer integration in this slice
- Slice: PR2 — integrate extractor in TCP consumers + runtime tests
- Boundary: `internal/antenna` + `internal/rawtcp` integration only; no OpenSpec archive/finalization in this slice
- Chain strategy: stacked-to-main
- Review budget: chained PRs selected to keep each PR under the 400-line review target
- Mode: Strict TDD

## Completed Tasks

- [x] 1.1 Add RED table-driven cases for split frames and byte-by-byte partial retention.
- [x] 1.2 Add RED table-driven cases for coalesced frames and leading noise before SOI.
- [x] 1.3 Add RED cases for invalid-checksum followed by valid frame and deterministic buffer guard behavior.
- [x] 2.1 Add `DefaultStreamExtractorMaxBuffer`, `StreamExtractor`, constructors.
- [x] 2.2 Implement `Append([]byte) [][]byte` with partial retention, SOI resync, and `6 + LENGTH + 1` sizing.
- [x] 2.3 Add `BufferedLen()` and `Reset()`.
- [x] 2.4 Preserve parser contract: extractor emits raw structurally complete frames and does not validate checksum.
- [x] 3.1 Update `AntennaManager.dataReader` to use `protocol.StreamExtractor` per read chunk.
- [x] 3.2 Iterate `extractor.Append(buf[:n])` and call `HandlePacket` for each complete frame.
- [x] 3.3 Remove redundant per-read checksum filtering from `dataReader`; checksum enforcement remains in `HandlePacket`.
- [x] 3.4 Add `TestDataReader_StreamReassemblyScenarios` for split/coalesced/noise/invalid+valid continuation behaviors.
- [x] 4.1 Update `RunAntenna` to reuse `protocol.StreamExtractor` across reads.
- [x] 4.2 Parse each extracted frame and continue on parse errors.
- [x] 4.3 Keep persistence policy in consumer: require `IsDataPacket()` and `ChecksumOK` before `cache.Store`.
- [x] 4.4 Add `TestRunAntenna_StreamReassemblyScenarios` for split/coalesced/noise/invalid+valid continuation behaviors.
- [x] 5.1 Update `tcp-stream-frame-reassembly` spec wording with explicit `6 + LENGTH + 1` boundary and deterministic cap/discard behavior.
- [x] 5.2 Update `generic-rfid-protocol-framing` delta wording with explicit cross-consumer extraction and invalid-checksum continuation expectations.
- [x] 5.3 Add required verify commands in change notes and re-run verification suite.

## TDD Cycle Evidence

| Task | Test File | Layer | Safety Net | RED | GREEN | TRIANGULATE | REFACTOR |
|------|-----------|-------|------------|-----|-------|-------------|----------|
| 1.1 / 2.1-2.3 | `amg-rfid-shared-go/protocol/packet_test.go` | Unit | ✅ `go test -v ./protocol` passed before changes | ✅ Tests referenced missing `NewStreamExtractor` API; build failed as expected | ✅ `go test -v ./protocol -run TestStreamExtractor` passed | ✅ Split and byte-by-byte cases cover distinct chunking paths | ✅ `gofmt`; helpers keep assertions focused |
| 1.2 / 2.2 | `amg-rfid-shared-go/protocol/packet_test.go` | Unit | ✅ Same baseline | ✅ Coalesced/noise tests written before implementation | ✅ `go test -v ./protocol -run TestStreamExtractor` passed | ✅ Coalesced ordered emission and leading-noise discard cover distinct paths | ✅ SOI helpers extracted |
| 1.3 / 2.4 | `amg-rfid-shared-go/protocol/packet_test.go` | Unit | ✅ Same baseline | ✅ Invalid-checksum and guard tests written before implementation | ✅ `go test -v ./protocol -run TestStreamExtractor` passed | ✅ Invalid checksum emission plus cap-tail retention cover policy and guard paths | ✅ Buffer guard isolated in `enforceMaxBuffer` |
| 3.1-3.4 | `internal/antenna/data_reader_test.go` | Integration | ✅ `go test -v ./internal/antenna -run 'TestDataReader|TestHandlePacket'` passed before changes | ✅ `TestDataReader_StreamReassemblyScenarios` added first; failed on split/coalesced/noise/invalid+valid with old per-read parsing | ✅ `go test -v ./internal/antenna -run TestDataReader_StreamReassemblyScenarios` passed after extractor integration | ✅ 4 scenario cases force fragmented, coalesced, resync noise, and invalid-checksum continuation paths | ✅ Shared packet helpers + exit helper reduced duplication |
| 4.1-4.4 | `internal/rawtcp/client_test.go` | Integration | ✅ `go test -v ./internal/rawtcp -run 'TestRunAntenna|TestAntennaClient'` passed before changes | ✅ `TestRunAntenna_StreamReassemblyScenarios` added first; failed for coalesced/noise/invalid+valid with old per-read parsing | ✅ `go test -v ./internal/rawtcp -run TestRunAntenna_StreamReassemblyScenarios` passed after extractor integration | ✅ 4 scenario cases cover split/coalesced/noise/invalid+valid continuation paths | ✅ Packet-builder helper extracted for deterministic vectors |
| 5.1-5.3 | `amg-rfid-shared-go/protocol/packet_test.go` + OpenSpec specs | Unit + Spec | ✅ `go test -v ./protocol` passed before edits | ✅ Added explicit LENGTH-boundary/resync vectors before any runtime/spec changes; no runtime edits required | ✅ `go test -v ./protocol -run TestStreamExtractor_LengthBoundariesAndResync` passed | ✅ Cases cover `LENGTH=0`, `LENGTH>0`, partial declared length retention, and corrupt-stream resync to later valid SOI | ➖ None needed (no production refactor) |

## Test Summary

- Total tests written: 6 top-level tests with 19 scenario cases
- Total tests passing: all touched package tests, root suite, and shared module suite passed
- Layers used: Unit + Integration
- Approval tests: None — new extractor API, no refactoring-only task
- Pure functions created: `firstSOIIndex`, `lastSOIIndex`, `isPacketStart`

## Verification Commands

- ✅ `cd amg-rfid-shared-go && go test -v ./protocol`
- ✅ `cd amg-rfid-shared-go && go test -v ./...`
- ✅ `go test -v ./...` from repository root because root `go.mod` replaces shared module locally
- ✅ `go test -v ./internal/antenna -run 'TestDataReader|TestHandlePacket'`
- ✅ `go test -v ./internal/rawtcp -run 'TestRunAntenna|TestAntennaClient'`
- ✅ `go test -v ./internal/antenna ./internal/rawtcp`

## Deviations

- None — PR1 stayed within the shared protocol boundary and preserved checksum policy outside the extractor.

## Remaining Tasks

- [x] None — all tasks in this change are complete.
