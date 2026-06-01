# Delta for antenna-protocol-configuration

## MODIFIED Requirements

### Requirement: TUI protocol visibility and edit round-trip

The settings TUI MUST display each antenna protocol and MUST allow editing it per antenna through a constrained selector that only cycles supported values (`generic`, `zebra`). Operators MUST NOT enter arbitrary free-text protocol values through normal selector flow. Save/apply flows SHALL persist only intended protocol changes while preserving unrelated fields.

(Previously: TUI protocol editing allowed free-text entry and post-entry validation.)

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

## Verification Notes

| Scenario | Concrete tests / artifacts |
|----------|----------------------------|
| Edit protocol from TUI and persist | `internal/tui/app_test.go::TestApp_SettingsProtocolEditRoundTripsAfterSave` |
| TUI displays defaulted protocol for legacy entries | `internal/tui/screens/settings_screen_test.go` legacy/default protocol cases |
| Selector flow prevents arbitrary values | `internal/tui/screens/settings_screen_test.go` protocol selector cycle and invalid-value cases |
| Operator-facing docs/screenshots | `README.md` Antenna Protocols and TUI sections; `docs/screenshots/settings.svg` selector help text |
