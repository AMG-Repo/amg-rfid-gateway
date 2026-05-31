# Generic RFID Protocol Framing Specification

## Purpose

Define normative framing and UII decoding behavior for Generic RFID Reader Control Protocol interoperability.

## Requirements

### Requirement: Parse canonical protocol frame layout

The parser MUST decode frames as `SOI ADR(2) CID1 CID2/RTN LENGTH INFO CHKSUM`, where `ADR` is two bytes, `LENGTH` is one byte count of `INFO`, and `CHKSUM` is the trailing checksum byte.

#### Scenario: Parse canonical Read UII command frame

- GIVEN the frame `7CFFFF20000066`
- WHEN the parser decodes fields by canonical indexes
- THEN it yields `SOI=0x7C`, `ADR=0xFFFF`, `CID1=0x20`, `CID2=0x00`, `LENGTH=0x00`
- AND `INFO` is empty and `ChecksumOK=true`

#### Scenario: Reject legacy index interpretation

- GIVEN a frame decoded with legacy `[SOI ADR RTN LEN DATA CHKSUM]` assumptions
- WHEN canonical indexes differ from legacy offsets
- THEN canonical field mapping MUST be used for exported parser output

### Requirement: Validate checksum and SOI semantics

The parser MUST compute checksum as two’s complement over all bytes except the checksum byte. It MUST treat command SOI `0x7C` and response SOI `0xCC` as valid protocol starts.

#### Scenario: Validate checksum using protocol vector

- GIVEN `7CFFFF20000066`
- WHEN checksum is recomputed as two’s complement
- THEN computed checksum equals `0x66`
- AND `ChecksumOK=true`

#### Scenario: Mark invalid checksum without crashing

- GIVEN a frame with one checksum byte altered
- WHEN parser validation runs
- THEN `ChecksumOK=false`
- AND parsing returns structured fields when length permits

### Requirement: Decode UII response payload by LENGTH and PC

For response frames, the antenna pipeline MUST read `RTN` at byte index 4, `LENGTH` at index 5, and `INFO` starting at index 6 with exactly `LENGTH` bytes. `INFO` MUST decode as `ANT + PC + EPC + RSSI`; EPC byte count MUST be derived from PC length bits (`words * 2 bytes`).

#### Scenario: Decode provided UII response vector

- GIVEN `CCFFFF200210003000E2003411B802011383258566C983`
- WHEN parsed and decoded using canonical indexes
- THEN `CID1=0x20`, `RTN=0x02`, `LENGTH=0x10`
- AND decoded `EPC=E2003411B802011383258566` with `RSSI=201`

#### Scenario: PC length controls EPC size

- GIVEN `PC=0x3000` in INFO
- WHEN decoder applies PC length bits
- THEN EPC length is 6 words (12 bytes, 24 hex chars)
- AND EPC extraction does not consume RSSI bytes

### Requirement: Preserve valid EPCs and enforce invalid-checksum policy

The decoder MUST preserve any valid EPC value without marker-prefix dependence (including non-`E200`/`E280` values). Parser checksum failure MUST remain explicit (`ChecksumOK=false`), and downstream data reader/manager MUST NOT store readings from invalid-checksum frames.

#### Scenario: Keep non-marker EPC values

- GIVEN a valid frame whose EPC does not start with `E200` or `E280`
- WHEN EPC extraction runs from PC-derived length
- THEN the full EPC is preserved unchanged

#### Scenario: Do not persist invalid-checksum readings

- GIVEN a parsed frame with `ChecksumOK=false`
- WHEN antenna manager/data reader handles the frame
- THEN no tag reading is stored or emitted as valid inventory data

### Requirement: Exclude TCP stream reassembly from this capability

This capability SHALL NOT include TCP stream reassembly or chunk coalescing behavior.

#### Scenario: Fragmented stream remains out of scope

- GIVEN a frame split across multiple TCP reads
- WHEN evaluating this capability
- THEN correctness requirements apply only to complete frame buffers
- AND stream reassembly is handled by separate future capability work
