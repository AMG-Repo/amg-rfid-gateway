# Antenna Protocol Configuration Specification

## Purpose

Define per-antenna protocol configuration behavior, validation, and TUI editability with safe backward compatibility.

## Requirements

### Requirement: Default protocol for backward-compatible config loading

The system MUST treat missing `protocol` in antenna configuration as `generic` during load and runtime resolution.

#### Scenario: Existing YAML without protocol remains valid

- GIVEN an existing antenna entry without a `protocol` field
- WHEN configuration is loaded
- THEN the effective antenna protocol is `generic`
- AND startup behavior remains equivalent to prior generic-only deployments

#### Scenario: Explicit protocol remains unchanged

- GIVEN an antenna entry with `protocol: zebra`
- WHEN configuration is loaded and saved
- THEN the protocol remains `zebra`

### Requirement: Protocol allowlist validation

The system MUST validate antenna protocol values against configurable values (`generic`, `zebra`) and MUST reject unrecognized values with a safe, explicit configuration error before runtime starts.

#### Scenario: Accept supported protocol values

- GIVEN antenna protocols set to `generic` or `zebra`
- WHEN validation executes
- THEN validation succeeds

#### Scenario: Reject unsupported protocol values

- GIVEN an antenna protocol value not in the supported set
- WHEN validation executes
- THEN configuration is rejected with an explicit unsupported protocol value error
- AND runtime processing for that antenna is not started

### Requirement: Known-but-unimplemented protocol runtime boundary

The system MUST treat `zebra` as a recognized configuration value but MUST route runtime frames to an explicit unsupported handler until Zebra framing is implemented. The runtime MUST NOT silently fall back to Generic parsing for `zebra`.

#### Scenario: Zebra configuration reaches explicit unsupported handler

- GIVEN an antenna configured with `protocol: zebra`
- WHEN a frame reaches runtime dispatch
- THEN dispatch returns or logs an explicit unsupported-protocol runtime error for that antenna boundary
- AND no tag data is emitted or stored by the Generic handler

### Requirement: TUI protocol visibility and edit round-trip

The settings TUI MUST display each antenna protocol and MUST allow editing it per antenna through a constrained selector that only cycles supported values (`generic`, `zebra`). Operators MUST NOT enter arbitrary free-text protocol values through normal selector flow. Save/apply flows SHALL persist only intended protocol changes while preserving unrelated fields.

#### Scenario: Edit protocol from TUI and persist

- GIVEN an antenna shown in settings with protocol `generic`
- WHEN the operator changes it to `zebra` and saves
- THEN persisted configuration for that antenna is `zebra`
- AND no unrelated antenna or gateway fields are mutated

#### Scenario: TUI displays defaulted protocol for legacy entries

- GIVEN a legacy antenna config without protocol
- WHEN settings are rendered
- THEN protocol is shown as `generic`

#### Scenario: Selector flow prevents arbitrary values

- GIVEN the operator is editing antenna protocol in Settings
- WHEN the operator cycles protocol values using supported selector controls
- THEN only `generic` or `zebra` can be selected
- AND arbitrary values such as `foo` cannot be produced by normal selector navigation
