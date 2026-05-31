## Exploration: generic-rfid-protocol-framing

### Current State
The Go gateway already has a protocol parser and antenna packet handlers, but it previously modeled packets as `[SOI, ADR1, ADR2, RTN, LEN, DATA, CHKSUM]` and treated `0x02` directly as a top-level command/response code. The generic RFID protocol provided by the user requires a 6-byte header before payload: `SOI ADR(2) CID1 CID2/RTN LENGTH INFO CHKSUM`. Existing uncommitted work is moving toward this model by splitting CID1 (`0x20`) from RTN (`0x02`), shifting LENGTH to byte index 5, and parsing INFO from byte index 6.

### Affected Areas
- `amg-rfid-shared-go/protocol/packet.go` — core frame parser, checksum validation, packet classification (`CID1`, `RTN`, `LEN`, `INFO`).
- `amg-rfid-shared-go/protocol/packet_test.go` — protocol vectors; now includes `7CFFFF20000066` and `CCFFFF200210003000...` style cases.
- `amg-rfid-shared-go/protocol/antenna_data_test.go` — PC/EPC interpretation comments and expectations.
- `internal/antenna/manager.go` — runtime packet routing and UII extraction offsets in `HandlePacket` / `handleUIIData`.
- `internal/antenna/handle_packet_test.go` — handler packet fixtures updated to include `CID1=0x20`.
- `internal/antenna/data_reader_test.go` — read-loop packet fixtures and ACK frame shape.
- `internal/rawtcp/client.go` — command framing (`SendReadUII`) and integration with shared parser.
- `internal/rawtcp/client_test.go` — parser integration expectations for response framing.

### Approaches
1. **Incremental compatibility patch on current direction** — continue from current uncommitted direction, fix remaining semantic mismatches, and lock behavior with authoritative vectors.
   - Pros: Minimal churn; aligns with current code changes; fastest path to correctness.
   - Cons: Risk of preserving legacy alias confusion (`CommandCode` now mapped to `CID1`); can mask future protocol misunderstandings.
   - Effort: Medium

2. **Protocol-model cleanup pass (explicit command/response types)** — keep frame migration, then remove legacy alias semantics and make packet intent explicit across parser, manager, and tests.
   - Pros: Clearer long-term model; lower future maintenance risk; less ambiguous RTN/CID usage.
   - Cons: Larger review surface; more refactor/test updates now.
   - Effort: Medium/High

### Recommendation
Use **Approach 1 now**, with strict acceptance vectors from the user docs, then schedule a small follow-up cleanup from Approach 2. This change is primarily a correctness fix to protocol framing; preserving momentum while adding guardrail tests is the safest next step.

### Risks
- Directional alignment is good, but there are likely partial/untouched call sites still assuming legacy `RTN-at-byte-3` semantics.
- `CommandCode` backward-compat alias now mirrors `CID1`; any consumer expecting old RTN value may silently misbehave.
- Read-loop currently treats `conn.Read` chunks as whole packets; protocol framing correctness does not solve packet boundary fragmentation/coalescing risk.
- Heartbeat command (`0x01`) behavior is not validated here against full reader-control protocol docs.

### Ready for Proposal
Yes — tell the user we can proceed to `sdd-propose` with scope: finalize frame parsing per `SOI ADR CID1 RTN LEN INFO CHKSUM`, lock checksum/indexing with real vectors, and identify any remaining legacy RTN assumptions before apply.
