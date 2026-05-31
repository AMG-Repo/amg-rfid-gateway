# Tasks: Raw TCP Fragmented Frame Reassembly

## Review Workload Forecast

| Field | Value |
|-------|-------|
| Estimated changed lines | 450-650 |
| 400-line budget risk | High |
| Chained PRs recommended | Yes |
| Suggested split | PR 1 → PR 2 → PR 3 |
| Delivery strategy | ask-always |
| Chain strategy | stacked-to-main |

Decision needed before apply: No (resolved: stacked-to-main chained PRs)
Chained PRs recommended: Yes
Chain strategy: stacked-to-main
400-line budget risk: High

### Suggested Work Units

| Unit | Goal | Likely PR | Notes |
|------|------|-----------|-------|
| 1 | Shared extractor RED + implementation in protocol package | PR 1 | Keep changes in `amg-rfid-shared-go` and add stream-extractor tests. |
| 2 | Integrate extractor into antenna + rawtcp consumers | PR 2 | Keep each consumer loop change together with its focused regression tests. |
| 3 | OpenSpec expectation sync + verification commands | PR 3 | Update spec text and verification notes once behavior is locked. |

## Phase 1: Extraction Tests (Red first)

- [x] 1.1 Add RED table-driven cases in `amg-rfid-shared-go/protocol/packet_test.go` for split frames across reads and **byte-by-byte partial retention** (emit after final byte).
- [x] 1.2 Add RED table-driven cases for **coalesced frames** and **leading noise before SOI** so extractor discards noise and emits ordered complete frames.
- [x] 1.3 Add RED cases for **invalid-checksum then valid frame** in same stream and for **buffer overflow guard** beyond `64 * 1024` to verify bounded/ deterministic resync behavior.

## Phase 2: Shared `protocol.StreamExtractor` Implementation

- [x] 2.1 Add `DefaultStreamExtractorMaxBuffer`, `StreamExtractor`, `NewStreamExtractor()`, and `NewStreamExtractorWithMaxBuffer(max int)` to `amg-rfid-shared-go/protocol/packet.go`.
- [x] 2.2 Implement `Append([]byte) [][]byte` with trailing partial retention, SOI scan/resync rules, and frame-size checks from `LENGTH` (`6 + LENGTH + 1`).
- [x] 2.3 Add `BufferedLen() int` and `Reset()` and validate they expose bounded buffering behavior under malformed or noisy streams.
- [x] 2.4 Ensure existing frame parser contract is preserved: extractor emits raw frames only; checksum validation remains a consumer responsibility.

## Phase 3: Integration in `internal/antenna/manager.go`

- [x] 3.1 Update `AntennaManager.dataReader` to create/use a `protocol.StreamExtractor` and append each `conn.Read` chunk before handling packets.
- [x] 3.2 Replace per-read packet parsing with iterating over `extractor.Append(buf[:n])` and pass each complete frame to `HandlePacket`.
- [x] 3.3 Remove redundant per-read checksum filtering in the read loop; keep checksum enforcement at `HandlePacket` so persistence policy stays centralized.
- [x] 3.4 Add/extend tests in `internal/antenna/data_reader_test.go` for **split**, **coalesced**, **noise-only+resync**, and **invalid-checksum frame not stored** paths.

## Phase 4: Integration in `internal/rawtcp/client.go`

- [x] 4.1 Create and reuse a `protocol.StreamExtractor` in `RunAntenna` and feed every read chunk into `Append`.
- [x] 4.2 Parse each extracted frame via `ParsePacket`; continue the loop on parse errors and malformed partials.
- [x] 4.3 Enforce downstream policy: process only `packet.IsDataPacket()` and require `packet.ChecksumOK` before `cache.Store`, preventing invalid checksums from persisting.
- [x] 4.4 Add regression tests in `internal/rawtcp/client_test.go` for fragmented, coalesced, and noisy reads, plus **invalid-checksum not persisted** while valid frames after it still parse.

## Phase 5: Verification and OpenSpec expectation updates

- [x] 5.1 Update `openspec/changes/rawtcp-fragmented-frame-reassembly/specs/tcp-stream-frame-reassembly/spec.md` with explicit deterministic buffer cap + discard behavior wording if not already precise.
- [x] 5.2 Update `openspec/changes/rawtcp-fragmented-frame-reassembly/specs/generic-rfid-protocol-framing/spec.md` with explicit cross-consumer extraction and persistence expectations for invalid checksum/valid continuation.
- [x] 5.3 Add verify commands in the change notes: `cd amg-rfid-shared-go && go test ./...` and `go test ./...` from repo root.
