# Delta for Generic RFID Protocol Framing

## ADDED Requirements

### Requirement: Delegate TCP chunk reassembly to shared extractor

TCP-facing consumers SHALL use the shared protocol stream extractor before packet parsing/handling so parser and decode logic receive complete frame buffers only. This requirement applies equally to `internal/antenna/dataReader` and `internal/rawtcp/RunAntenna`.

#### Scenario: Fragmented stream handled via extractor

- GIVEN a protocol frame split across multiple TCP reads
- WHEN the consumer processes reads through the shared extractor before parser calls
- THEN only complete frames are passed to parser/handler
- AND fragmented/coalesced chunk boundaries do not change decode semantics

#### Scenario: Both TCP consumers apply the same extraction gate

- GIVEN `internal/antenna/dataReader` and `internal/rawtcp/RunAntenna` receive arbitrary chunks
- WHEN each path invokes shared extraction before `HandlePacket` or `ParsePacket`
- THEN both paths follow identical frame-boundary behavior
- AND parser/storage behavior stays consistent across both consumers

## MODIFIED Requirements

### Requirement: Preserve valid EPCs and enforce invalid-checksum policy

The decoder MUST preserve any valid EPC value without marker-prefix dependence (including non-`E200`/`E280` values). Parser checksum failure MUST remain explicit (`ChecksumOK=false`), and downstream data reader/manager MUST NOT store readings from invalid-checksum frames even when the frame was structurally complete and emitted by stream extraction. After any invalid/corrupt sequence, consumers MUST continue processing subsequent valid extracted frames in the same stream.
(Previously: Invalid-checksum non-persistence was required, but extractor-emitted invalid complete frames were not explicitly called out.)

#### Scenario: Keep non-marker EPC values

- GIVEN a valid frame whose EPC does not start with `E200` or `E280`
- WHEN EPC extraction runs from PC-derived length
- THEN the full EPC is preserved unchanged

#### Scenario: Do not persist invalid-checksum readings

- GIVEN a parsed frame with `ChecksumOK=false`
- WHEN antenna manager/data reader handles the frame
- THEN no tag reading is stored or emitted as valid inventory data
