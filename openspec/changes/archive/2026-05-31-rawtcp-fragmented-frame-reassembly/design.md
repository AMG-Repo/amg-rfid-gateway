# Design: Raw TCP Fragmented Frame Reassembly

## Technical Approach

Add a small stateful stream extractor in `amg-rfid-shared-go/protocol` and use it from both TCP read loops. The extractor owns TCP byte buffering only: it appends chunks, resyncs to RFID SOI bytes (`0x7C`, `0xCC`), computes frame length from byte 5, emits complete frames, and retains incomplete tails. Packet parsing, checksum validation, routing, and persistence stay in existing consumers to avoid protocol semantic drift.

## Architecture Decisions

| Decision | Choice | Alternatives considered | Rationale |
|---|---|---|---|
| Extractor API | `type StreamExtractor struct` with `Append(chunk []byte) [][]byte`, `BufferedLen() int`, and `Reset()`; constructor uses default max buffer. | Stateless helper; channel/goroutine decoder. | TCP fragmentation requires retained state; a simple synchronous API fits existing read loops and is easy to unit test. |
| Frame ownership | `Append` copies emitted frames before removing bytes from the internal buffer. | Return slices into internal buffer. | Prevents callers from observing mutated buffer storage after later appends. |
| Resync | Discard bytes before first SOI; if a candidate frame is malformed/unparseable, advance one byte and continue scanning. | Drop entire buffer on corruption. | Preserves valid frames after noise/corruption and matches stream parser behavior. |
| Partial frames | Retain buffer when fewer than `protocol.MinPacketSize` bytes exist or declared total length is incomplete. | Return parse errors to consumers. | Partial TCP reads are normal, not errors. |
| Coalesced frames | Loop extraction until no complete frame remains. | Emit one frame per read. | One TCP read can contain multiple RFID frames. |
| Buffer cap | Add `DefaultStreamExtractorMaxBuffer = 64 * 1024`; if exceeded, resync to the last plausible SOI tail, otherwise drop the buffer. | Unbounded buffer; hard error. | Prevents garbage streams from growing memory while keeping a possible partial frame. |
| Checksum policy | Extractor does not validate checksum for persistence decisions; consumers continue using `ValidateChecksum`/`ParsePacket.ChecksumOK`. | Drop invalid checksum in extractor. | Invalid checksum frames may be useful for logging/metrics, but must not create readings. |

## Data Flow

```text
TCP Read chunk ──→ protocol.StreamExtractor.Append
                         │
                         ├── complete frame ──→ antenna.HandlePacket ──→ cache/event bus
                         └── complete frame ──→ rawtcp.ParsePacket ──→ checksum gate ──→ cache
```

## File Changes

| File | Action | Description |
|------|--------|-------------|
| `amg-rfid-shared-go/protocol/packet.go` | Modify | Add `StreamExtractor`, SOI scan helpers, buffer cap constants, and buffer length accessor. |
| `amg-rfid-shared-go/protocol/packet_test.go` | Modify | Add table-driven unit tests for fragmentation, coalescing, noise, corruption resync, partial retention, and cap behavior. |
| `internal/antenna/manager.go` | Modify | Create extractor in `dataReader`; append every read chunk; call `HandlePacket` for each emitted frame. Remove per-read checksum gate because `HandlePacket` already enforces it. |
| `internal/antenna/data_reader_test.go` | Modify | Add regression coverage for split frames, coalesced frames, leading noise, and invalid-checksum non-persistence. |
| `internal/rawtcp/client.go` | Modify | Create extractor in `RunAntenna`; parse each emitted frame; keep checksum and data-packet gates before storing. |
| `internal/rawtcp/client_test.go` | Modify | Add raw TCP regression tests for split/coalesced/noisy streams and invalid-checksum non-persistence. |

## Interfaces / Contracts

```go
const DefaultStreamExtractorMaxBuffer = 64 * 1024

type StreamExtractor struct { /* private buffer/max */ }

func NewStreamExtractor() *StreamExtractor
func NewStreamExtractorWithMaxBuffer(max int) *StreamExtractor
func (e *StreamExtractor) Append(chunk []byte) [][]byte
func (e *StreamExtractor) BufferedLen() int
func (e *StreamExtractor) Reset()
```

`Append` returns only complete raw frames whose length is `6 + int(frame[5]) + 1`. It does not guarantee checksum validity.

## Testing Strategy

| Layer | What to Test | Approach |
|-------|-------------|----------|
| Unit | Extractor framing/resync/cap behavior | Table-driven tests with `t.Run`; use helpers for valid packet vectors. |
| Integration | `dataReader` and `RunAntenna` consume extracted frames correctly | Existing mock `net.Conn`/TCP listener tests with split and coalesced writes. |
| E2E | Not required | Existing TCP unit/integration boundaries cover this regression without hardware. |

## Migration / Rollout

No migration required. Rollout is code-only and preserves existing packet parser and storage contracts.

## Open Questions

- [ ] Confirm whether `64 KiB` is acceptable as the default garbage-stream buffer cap for production antennas.
