# Proposal: Generic RFID Protocol Framing

## Intent

Align Go backend RFID antenna framing with the Generic RFID Reader Control Protocol. Current code and prior non-SDD edits are partially migrated from legacy `[SOI ADR RTN LEN DATA CHKSUM]` assumptions; apply/verify must reconcile that diff with this SDD contract, not restart from scratch.

## Scope

### In Scope
- Parse/validate frames as `SOI ADR(2) CID1 CID2/RTN LENGTH INFO CHKSUM`.
- Emit the Read UII command `7C FF FF 20 00 00 66`.
- Decode UII responses from `INFO = ANT + PC + EPC + RSSI`, using PC length bits for EPC size.
- Lock behavior with table-driven Go tests using protocol vectors.

### Out of Scope
- TCP stream reassembly for fragmented/coalesced reads.
- Heartbeat or broader command coverage beyond compatibility needs.
- Large API redesign beyond shims needed to reconcile current code.

## Capabilities

### New Capabilities
- `generic-rfid-protocol-framing`: Packet framing, checksum, Read UII command emission, and UII response decoding for the Generic RFID Reader Control Protocol.

### Modified Capabilities
- None — no existing OpenSpec specs are present.

## Approach

Use the exploration recommendation: continue the current incremental compatibility patch, correct remaining byte offsets/semantics, and add authoritative vector tests. Treat `CID1=0x20`, response RTN in the CID2/RTN slot, `LENGTH` at index 5, `INFO` at index 6, and checksum as two’s complement over all bytes except checksum.

## Affected Areas

| Area | Impact | Description |
|------|--------|-------------|
| `amg-rfid-shared-go/protocol/packet.go` | Modified | Frame fields, checksum, packet classification. |
| `internal/antenna/manager.go` | Modified | Runtime UII routing and INFO decoding. |
| `internal/rawtcp/client.go` | Modified | Read UII command framing. |
| `*_test.go` protocol/antenna/rawtcp | Modified | Protocol vectors and state-transition coverage. |

## Risks

| Risk | Likelihood | Mitigation |
|------|------------|------------|
| Legacy RTN/CMD assumptions remain | Med | Search call paths and assert CID1/RTN meanings in tests. |
| Compatibility aliases hide misuse | Med | Keep aliases minimal and test public behavior. |
| TCP packet-boundary bug conflated with framing | Low | Document as out of scope for this change. |

## Rollback Plan

Revert the OpenSpec change and the related parser/antenna/rawtcp implementation diff. Restore prior tests if production hardware rejects the new protocol vectors.

## Dependencies

- User-provided Generic RFID Reader Control Protocol facts and vectors.
- Existing uncommitted implementation diff must be reviewed during apply/verify.

## Success Criteria

- [ ] Read UII command encodes exactly as `7CFFFF20000066`.
- [ ] Sample response decodes EPC `E2003411B802011383258566` and RSSI `201`.
- [ ] Tests cover framing indexes, checksum, PC-derived EPC length, and handler behavior.
