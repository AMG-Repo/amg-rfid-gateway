# Proposal: Raw TCP Fragmented Frame Reassembly

## Intent

Fix packet-per-read assumptions in TCP consumers by introducing stream-safe RFID frame extraction so fragmented, coalesced, and noisy TCP chunks are decoded reliably without changing RFID protocol semantics.

## Scope (In/Out)

### In Scope
- Add a shared stream frame extractor/decoder flow in `amg-rfid-shared-go/protocol` for `SOI ADR(2) CID1 CID2/RTN LENGTH INFO CHKSUM` frames.
- Integrate extractor into both consumers: `internal/antenna/manager.go` (`dataReader`) and `internal/rawtcp/client.go` (`RunAntenna`).
- Retain partial frames across reads, emit multiple frames from one chunk, and resync by discarding noise before SOI.
- Enforce invalid-checksum policy at consumer level: parse/extract allowed, persistence of invalid-checksum readings forbidden.
- Add bounded-buffer growth guard and resync/drop policy for garbage streams.
- Add tests for extractor behavior and both consumer integrations.

### Out of Scope
- RFID packet field/semantic changes from prior protocol framing work.
- LAN/web feature behavior changes.
- Serial hardware configuration changes.
- Broad rawtcp architecture refactors beyond decoder integration.

## Capabilities (New/Modified)

- New Capabilities: `tcp-stream-frame-reassembly`
- Modified Capabilities: `generic-rfid-protocol-framing`

## Approach

Introduce a protocol-level stream extractor that appends incoming bytes to an internal buffer, scans for valid SOI (`0x7C`/`0xCC`), computes expected frame size from LENGTH, and emits only complete frames. Keep trailing incomplete bytes for the next chunk. On preamble noise or malformed growth conditions, resync to next SOI and cap buffered bytes with deterministic drop/reset rules. Consumers switch from per-read checksum gating to per-extracted-frame handling, preserving existing `HandlePacket`/UII decode semantics while preventing persistence for `ChecksumOK=false` frames.

## Affected Areas

| Area | Impact | Description |
|------|--------|-------------|
| `amg-rfid-shared-go/protocol/packet.go` | Modified | Add reusable stream extraction/reassembly primitives and guard/resync behavior. |
| `internal/antenna/manager.go` | Modified | Replace read-chunk-as-frame logic in `dataReader` with extractor-driven frame iteration. |
| `internal/rawtcp/client.go` | Modified | Update `RunAntenna` read loop to consume extracted frames, including checksum policy enforcement. |
| `internal/antenna/*_test.go` | Modified | Add fragmented/coalesced/noise-path tests in antenna consumer pipeline. |
| `internal/rawtcp/client_test.go` | Modified | Add rawtcp integration tests for partial/multi/noise and invalid-checksum non-persistence. |

## Risks

| Risk | Likelihood | Mitigation |
|------|------------|------------|
| Resync logic drops valid bytes near corruption boundaries | Medium | Add deterministic SOI scan rules and targeted regression vectors. |
| Buffer cap too small under legitimate bursts | Medium | Make cap configurable/constant-backed and validate with burst tests. |
| Behavior drift between two consumers | Medium | Use shared extractor API and mirror scenario tests in both paths. |

## Rollback Plan

Revert this change set and restore prior read-loop behavior in both consumers; keep parser-level code untouched from previous framing capability baseline.

## Dependencies

- Issue context: `#21`.
- Existing protocol framing baseline in `openspec/specs/generic-rfid-protocol-framing/spec.md`.

## Success Criteria

- [ ] Frames split across reads are reconstructed and decoded correctly in both consumers.
- [ ] Multiple coalesced frames in one read are all extracted and processed.
- [ ] Noise before SOI is discarded with successful resync and bounded memory.
- [ ] Invalid-checksum frames are parsed but never persisted as valid inventory data.
- [ ] New tests cover extractor and both consumer integrations and pass.
