## Exploration: dynamic antenna/protocol configuration from TUI

### Current State
- Antenna config model only supports network/enable/zone fields (`id`, `ip`, `port`, `enabled`, `zone`) and has no protocol/driver selector (`internal/config/config.go`).
- TUI Settings screen shows antennas as read-only summary rows; only gateway/web/log/queue fields are editable (`internal/tui/screens/settings_screen.go`).
- Runtime packet handling is tightly coupled to Generic protocol semantics:
  - `AntennaManager.HandlePacket` enforces generic framing/routing (`CID1=0x20`, `RTN` at byte 4) and parses UII data via `protocol.ParseAntennaData` (`internal/antenna/manager.go`).
  - `rawtcp.RunAntenna` parses with shared generic parser and stores only generic data packets (`internal/rawtcp/client.go`).
- Shared protocol package and OpenSpec currently define one canonical generic RFID framing capability (`amg-rfid-shared-go/protocol/packet.go`, `openspec/specs/generic-rfid-protocol-framing/spec.md`).

### Affected Areas
- `internal/config/config.go` — add protocol field(s) to `AntennaConfig`, defaults, and validation constraints.
- `internal/config/config_test.go`, `internal/config/config_save_test.go` — extend load/save/validate tests for protocol selection and backward compatibility.
- `internal/tui/screens/settings_screen.go` — currently cannot edit per-antenna config; requires UX/model changes for antenna protocol selection/edit.
- `internal/tui/screens/settings_screen_test.go` — update tests to assert editable antenna/protocol behavior.
- `internal/tui/app.go` — persists settings; must ensure new antenna protocol edits round-trip safely.
- `internal/antenna/manager.go` — decouple `HandlePacket` from hardcoded generic RTN routing/parsing.
- `internal/antenna/handle_packet_test.go`, `internal/antenna/data_reader_test.go` — refactor tests to protocol-aware routing contracts.
- `internal/rawtcp/client.go`, `internal/rawtcp/client_test.go` — choose parser path by configured protocol, not only generic.
- `amg-rfid-shared-go/protocol/*` (or new package) — keep generic parser isolated and introduce pluggable protocol handlers.
- `openspec/specs/generic-rfid-protocol-framing/spec.md` (+ likely new protocol capability spec) — define multi-protocol behavior and invariants.

### Approaches
1. **Single pipeline + protocol strategy interface** — Keep current TCP/extractor pipeline, inject per-antenna protocol handler for parse/route/store decisions.
   - Pros: Reuses stable stream extraction and connection lifecycle; minimal blast radius in reconnect/read-loop logic.
   - Cons: Requires careful interface boundaries (generic assumptions currently spread across manager/rawtcp/tests).
   - Effort: Medium

2. **Separate per-protocol runtime paths** — Branch early (e.g., `if protocol == generic/zebra`) and run distinct handlers/loops.
   - Pros: Fast to bootstrap one new protocol with fewer immediate abstractions.
   - Cons: Duplicates read/store logic, higher long-term maintenance risk, test matrix grows quickly.
   - Effort: Medium-High

### Recommendation
Use **Approach 1 (protocol strategy interface)**. The code already has a shared TCP frame extractor that is transport-level (good reusable boundary). Introduce protocol selection in `AntennaConfig` (e.g., `protocol: generic|zebra`) and route each frame through a protocol handler interface in antenna/rawtcp consumers. This keeps transport concerns stable and localizes protocol-specific parsing/routing rules.

### Risks
- Existing tests strongly encode generic constants (`CID1=0x20`, `RTN=0x02`); refactor can cause broad test churn.
- TUI settings currently lacks editable antenna rows; UX/state changes are non-trivial and could regress save semantics.
- Backward compatibility: existing YAML files without protocol must default deterministically (likely `generic`) to avoid boot failures.
- Unknown Zebra frame semantics may require a new framing/parser capability beyond current generic spec.

### Ready for Proposal
Yes — proceed with proposal scoped to: (1) config schema + migration default, (2) TUI antenna protocol editing UX, (3) protocol strategy abstraction in consumers, (4) tests/spec updates for generic + zebra path.
