# Delta for antenna-protocol-configuration

## ADDED Requirements

### Requirement: TUI antenna entry CRUD fields and validation boundaries

The settings TUI MUST allow operators to create, edit, and delete antenna entries containing `id`, `ip`, `port`, `enabled`, `zone`, and `protocol`. The system MUST block save when the antenna set fails configuration validation and SHALL show an error without persisting.

#### Scenario: Create valid antenna entry and save

- GIVEN an operator creates a new valid antenna entry
- WHEN save is confirmed
- THEN the new antenna is persisted to `config.yaml`
- AND existing unrelated settings remain unchanged

#### Scenario: Invalid antenna data blocks persistence

- GIVEN an operator commits an antenna set with invalid values
- WHEN save is confirmed
- THEN persistence is rejected with a validation message
- AND `config.yaml` remains unchanged

#### Scenario: Delete antenna entry and save

- GIVEN an existing antenna entry is marked for deletion
- WHEN save is confirmed
- THEN the antenna entry is removed from persisted configuration

## MODIFIED Requirements

### Requirement: TUI protocol visibility and edit round-trip

The settings TUI MUST display each antenna protocol and MUST allow editing it per antenna through a constrained selector that only cycles supported values (`generic`, `zebra`). Operators MUST NOT enter arbitrary free-text protocol values through normal selector flow. Save/apply flows SHALL persist only intended protocol changes while preserving unrelated fields. Protocol editing MUST work for new and existing entries.
(Previously: protocol editing was specified per displayed antenna but did not explicitly require compatibility with newly created entries.)

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

#### Scenario: New antenna protocol respects constrained selector

- GIVEN an operator is creating a new antenna in Settings
- WHEN the operator sets protocol via selector and saves
- THEN persisted protocol is one of `generic` or `zebra`
