# Tasks: Dynamic Antenna Protocol Configuration

## Review Workload Forecast

| Field | Value |
|-------|-------|
| Estimated changed lines | 450-700 |
| 400-line budget risk | High |
| Chained PRs recommended | Yes |
| Suggested split | PR 1 → PR 2 → PR 3 |
| Delivery strategy | ask-on-risk |
| Chain strategy | pending |

Decision needed before apply: Yes
Chained PRs recommended: Yes
Chain strategy: pending
400-line budget risk: High

### Suggested Work Units

| Unit | Goal | Likely PR | Notes |
|------|------|-----------|-------|
| 1 | Add protocol schema/defaults and save/load validation behavior | PR 1 | Foundation slice; includes config + unit tests only. |
| 2 | Introduce runtime protocol handler boundary and integrate manager/rawtcp + TUI edits | PR 2 | Builds on PR 1; includes routing and edit UX wiring. |
| 3 | Replace brittle protocol tests and update spec/contracts | PR 3 | Behavior-focused tests + spec updates + verification. |

## Phase 1: Foundation and Contract Surface (RED → GREEN)

- [x] 1.1 RED: Add table-driven cases in `internal/config/config_test.go` and `internal/config/config_save_test.go` for (a) missing protocol defaults to `generic`, (b) explicit `zebra` persists, (c) unsupported protocol fails validation.
- [x] 1.2 GREEN: Add `AntennaProtocol` constants and allowlist in `internal/config/config.go`, including `Protocol` field on `AntennaConfig`.
- [x] 1.3 GREEN: Implement defaulting and compatibility behavior in `AntennaConfig.ApplyDefaults()` so missing/empty protocol resolves to `ProtocolGeneric`.
- [x] 1.4 GREEN: Update `GatewayConfig.Validate()` in `internal/config/config.go` to validate protocol for every antenna during startup.
- [x] 1.5 GREEN: Update `configs/config.example.yaml` with `protocol: generic` and an example `protocol: zebra` entry plus unsupported-protocol warning comments.

## Phase 2: Protocol Strategy + Runtime Dispatch (RED → GREEN → REFACTOR)

- [x] 2.1 RED: Add `internal/antenna/protocol_handler_test.go` table-driven tests for `HandlerFor()` behavior, generic delegation, and explicit unsupported-protocol errors.
- [x] 2.2 GREEN: Add `internal/antenna/protocol_handler.go` with `ProtocolHandler`, `PacketContext`, `ProtocolHandlerFor()`, `ErrUnsupportedProtocol`, `GenericProtocolHandler`, and `UnsupportedProtocolHandler`.
- [x] 2.3 RED: Add/extend `internal/antenna/handle_packet_test.go` for scenario “generic frame uses generic behavior” and “zebra does not parse as generic”.
- [x] 2.4 GREEN: Refactor `internal/antenna/manager.go` to delegate frame handling via `ProtocolHandlerFor(m.config.Protocol)` after shared activity updates.
- [x] 2.5 RED: Add/adjust `internal/rawtcp/client_test.go` for protocol-aware dispatch and “unsupported emits no tag storage” behavior.
- [x] 2.6 GREEN: Update `internal/rawtcp/client.go` to resolve protocol from config and call the selected handler; avoid shared parser assumptions outside handler contract.

## Phase 3: TUI Protocol Editability (RED → GREEN)

- [x] 3.1 RED: Update `internal/tui/screens/settings_screen_test.go` to assert antenna protocol rendering, default display as `generic`, and per-antenna edit flow.
- [x] 3.2 GREEN: Extend `internal/tui/screens/settings_screen.go` model/state to support protocol field editing and allowlist validation.
- [x] 3.3 GREEN: Update `GetConfig()` in `internal/tui/screens/settings_screen.go` to persist only intended protocol changes while preserving unrelated gateway/antenna fields.
- [x] 3.4 GREEN: Update `internal/tui/app.go` save/restore wiring so settings apply/resize keeps protocol values round-tripped after edit.

## Phase 4: Verification, Churn Reduction, and Spec Alignment (REFACTOR)

- [x] 4.1 REFACTOR: Rewrite brittle assumptions in `internal/antenna/handle_packet_test.go` and `internal/antenna/data_reader_test.go` to protocol contract checks (not hardcoded generic internals).
- [x] 4.2 REFACTOR: Update or replace `internal/rawtcp/client_test.go` cases that assert old Generic-only behavior with behavior-focused protocol routing checks.
- [x] 4.3 REFACTOR: Add/extend integration scenario test for mixed antennas (generic + unsupported) in runtime or integration package; confirm failure isolation.
- [x] 4.4 REFACTOR: Update `openspec/changes/dynamic-antenna-protocol-config/specs/antenna-protocol-configuration/spec.md` and `.../specs/generic-rfid-protocol-framing/spec.md` to reflect handler boundary and explicit errors.
- [x] 4.5 Verification: Run `go test -v ./internal/config ./internal/antenna ./internal/rawtcp ./internal/tui/screens ./internal/tui` then `go test -v ./...` and align any spec wording with observed outcomes.
