# TUI Operator Safety Specification

## Purpose

Define operator-safe TUI behavior for explicit settings persistence, unsaved-change exits, uptime sourcing, and consistent navigation.

## Requirements

### Requirement: Settings persistence requires explicit save confirmation

The system MUST NOT persist Settings changes solely because fields were edited. The system MUST persist Settings only after explicit save intent and explicit confirmation.

#### Scenario: Editing fields does not persist automatically

- GIVEN an operator edits one or more Settings fields
- WHEN no explicit save intent is triggered
- THEN persisted configuration remains unchanged

#### Scenario: Save intent plus confirmation persists changes

- GIVEN an operator has unsaved Settings edits
- WHEN the operator triggers save intent and confirms
- THEN the system persists the edited Settings
- AND only the intended edited values are changed

### Requirement: Unsaved Settings exit protection

When leaving Settings with unsaved edits, the system MUST present an explicit decision prompt with options to confirm save, discard changes, or stay in Settings.

#### Scenario: Confirm save while leaving Settings

- GIVEN unsaved Settings edits exist
- WHEN the operator chooses save and confirms
- THEN changes are persisted
- AND navigation proceeds away from Settings

#### Scenario: Discard while leaving Settings

- GIVEN unsaved Settings edits exist
- WHEN the operator chooses discard
- THEN unsaved edits are not persisted
- AND navigation proceeds away from Settings

#### Scenario: Stay in Settings

- GIVEN unsaved Settings edits exist
- WHEN the operator chooses stay
- THEN navigation away is canceled
- AND the current editable state remains visible in Settings

### Requirement: Status uptime uses backend truth

The Status screen MUST display uptime from polled backend/gateway status payload (`SystemStatus.Uptime`) and MUST NOT derive uptime from TUI session age.

#### Scenario: Backend uptime is rendered

- GIVEN backend status includes uptime `1h23m`
- WHEN the Status screen renders
- THEN the displayed uptime is `1h23m`

### Requirement: Consistent navigable-screen key contract

On navigable screens, the system MUST support Arrow Up/Down and `k/j` with equivalent behavior. Navigable lists MUST use wrapping navigation. Help text MUST match actual supported keys.

#### Scenario: Down navigation wraps to first item

- GIVEN focus is on the last item of a navigable list
- WHEN the operator presses Down Arrow or `j`
- THEN focus moves to the first item

#### Scenario: Up navigation wraps to last item

- GIVEN focus is on the first item of a navigable list
- WHEN the operator presses Up Arrow or `k`
- THEN focus moves to the last item

## Verification Notes

| Requirement | Concrete tests / artifacts |
|-------------|----------------------------|
| Settings persistence requires explicit save confirmation | `internal/tui/app_test.go::TestApp_SettingsExplicitSaveControlsPersistence`; `internal/tui/screens/settings_screen_test.go` save-confirm cases |
| Unsaved Settings exit protection | `internal/tui/app_test.go::TestApp_SettingsUnsavedExitProtection`; `internal/tui/screens/settings_screen_test.go` unsaved-exit prompt cases |
| Status uptime uses backend truth | `internal/tui/screens/status_screen_test.go::TestStatusScreen_ViewUsesBackendUptime` |
| Consistent navigable-screen key contract | `internal/tui/screens/navigation_test.go`; `main_screen_test.go`, `antennas_screen_test.go`, and settings navigation cases |
| Help/docs alignment | `README.md` TUI sections and `docs/screenshots/settings.svg` describe save confirmation, selector keys, wrap navigation, and backend uptime |
