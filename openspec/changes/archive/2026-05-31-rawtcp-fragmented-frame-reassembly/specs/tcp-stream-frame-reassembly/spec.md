# TCP Stream Frame Reassembly Specification

## Purpose

Define shared extraction of RFID frames from arbitrary TCP byte streams without changing protocol packet semantics.

## Requirements

### Requirement: Extract complete frames from arbitrary chunks

The shared extractor MUST accept arbitrary byte chunks and emit zero or more complete frames in stream order.

#### Scenario: Reassemble split frame across reads

- GIVEN one valid frame split across two TCP chunks
- WHEN chunks are processed sequentially
- THEN zero frames are emitted after chunk one
- AND one complete frame is emitted after chunk two

#### Scenario: Emit coalesced frames in order

- GIVEN one chunk containing two complete back-to-back frames
- WHEN the extractor processes the chunk
- THEN two frames are emitted
- AND emission order matches input order

### Requirement: Enforce canonical frame boundaries

Frame boundaries MUST follow `SOI ADR(2) CID1 CID2/RTN LENGTH INFO CHKSUM`. Minimum complete frame size MUST be 7 bytes, and total size MUST be `6 + LENGTH + 1` (equivalently `7 + LENGTH`).

#### Scenario: Reject incomplete minimum frame

- GIVEN buffered bytes fewer than 7
- WHEN extraction runs
- THEN no frame is emitted

#### Scenario: Size derived from LENGTH

- GIVEN a candidate frame header with `LENGTH=n`
- WHEN extraction computes expected size
- THEN expected size is `6 + n + 1`
- AND `LENGTH=0` yields a 7-byte complete frame

### Requirement: Retain partial bytes and resynchronize on noise

The extractor MUST retain trailing partial frame bytes until complete. It MUST discard bytes before the next valid SOI and SHALL resume extraction from that SOI.

#### Scenario: Byte-by-byte partial retention

- GIVEN a valid frame delivered one byte per chunk
- WHEN each chunk is appended
- THEN no frame is emitted until final byte
- AND exactly one frame is emitted at completion

#### Scenario: Discard noise before SOI

- GIVEN noise bytes followed by a valid SOI and frame
- WHEN extraction scans buffered data
- THEN pre-SOI noise is discarded
- AND the valid frame is emitted once complete

### Requirement: Guard buffer growth on garbage streams

The extractor MUST apply a deterministic buffer-growth guard so garbage or never-complete streams do not cause unbounded memory growth. When the cap is exceeded, implementation MUST deterministically retain only a plausible SOI tail within cap or clear buffered bytes if no plausible SOI tail remains.

#### Scenario: Overflow guard activates on long garbage stream

- GIVEN continuous non-frame input exceeding the configured cap
- WHEN buffered bytes cross the cap
- THEN the extractor drops or resets buffered data deterministically
- AND memory usage remains bounded

### Requirement: Preserve checksum visibility and consumer policy

The extractor MAY emit structurally complete frames regardless of checksum validity. Runtime consumers MUST NOT persist readings from invalid-checksum frames.

#### Scenario: Invalid checksum followed by valid frame

- GIVEN an invalid-checksum complete frame followed by a valid complete frame
- WHEN both frames pass through extractor and parser
- THEN both frames are emitted for downstream handling
- AND only the valid frame reading is persisted

### Requirement: Apply extractor in both TCP consumers

`internal/antenna/dataReader` SHALL extract frames before `HandlePacket`, and `internal/rawtcp/RunAntenna` SHALL extract frames before `ParsePacket` and storage.

#### Scenario: Antenna dataReader uses shared extractor gate

- GIVEN fragmented and coalesced TCP chunks in antenna path
- WHEN `dataReader` processes input
- THEN only complete extracted frames are passed to `HandlePacket`

#### Scenario: RawTCP RunAntenna uses shared extractor gate

- GIVEN fragmented and noisy TCP chunks in rawtcp path
- WHEN `RunAntenna` processes input
- THEN only complete extracted frames are passed to `ParsePacket`
- AND invalid-checksum readings are not persisted
