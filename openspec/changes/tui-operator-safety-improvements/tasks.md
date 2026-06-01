# Tasks: TUI Operator Safety Improvements

## Review Workload Forecast

| Field | Value |
|-------|-------|
| Estimated changed lines | 520–700 |
| 400-line budget risk | High |
| Chained PRs recommended | Yes |
| Suggested split | PR 1 → PR 2 → PR 3 |
| Delivery strategy | ask-on-risk |
| Chain strategy | stacked-to-main |

Decision needed before apply: Yes
Chained PRs recommended: Yes
Chain strategy: stacked-to-main
400-line budget risk: High

Suggested work-unit PR split: PR 1 (app/settings save orchestration), PR 2 (settings UX + shared navigation contract), PR 3 (status uptime + docs/screenshots/spec validation)

### Suggested Work Units

| Unit | Goal | Likely PR | Notes |
|------|------|-----------|-------|
| 1 | Event-gated settings persistence + unsaved-exit behavior | PR 1 | Add tests first, keep validation intact |
| 2 | Protocol selector and shared navigation (`j/k` + wrap) | PR 2 | Reuse helper across navigable screens |
| 3 | Backend uptime rendering + docs/spec verification | PR 3 | Include screenshot and spec-mapping audit |

## Phase 1: Foundation / TDD Gate for Settings Persistence

- [x] 1.1 **RED**: add table-driven tests in `internal/tui/app_test.go` asserting edits do not auto-persist until `settingsSaveMsg`, and invalid states are not saved.
- [x] 1.2 **GREEN**: modify `internal/tui/app.go` so `ScreenSettings` branch only saves on explicit `settingsSaveMsg` and validates with `cfg.Validate()` first.
- [x] 1.3 **RED**: add table-driven unsaved-exit tests in `internal/tui/app_test.go` for save/discard/stay outcomes when leaving settings with dirty state.
- [x] 1.4 **GREEN**: handle `settingsBackMsg`, `settingsDiscardChangesMsg`, and `settingsStayMsg` in `internal/tui/app.go` to navigate deterministically and cancel exit safely.

## Phase 2: Core Settings Screen Safety UX

- [x] 2.1 **RED**: add `internal/tui/screens/settings_screen_test.go` cases that `s` opens confirm only on `hasChanges`, `settingsSaveMsg` is not emitted without explicit confirm, and protocol cursor editing is not free-text.
- [x] 2.2 **GREEN**: extend `internal/tui/screens/settings_screen.go` with a prompt mode (`settingsPromptMode`) and messages for save/unsaved-decision flow (`settingsSaveMsg`, `settingsDiscardChangesMsg`, `settingsStayMsg`).
- [x] 2.3 **RED**: add protocol selector tests in `internal/tui/screens/settings_screen_test.go` for `left/right/space/enter` cycles and legacy/default handling (`generic` fallback, reject `foo`).
- [x] 2.4 **GREEN**: implement constrained protocol selector in `internal/tui/screens/settings_screen.go` (no arbitrary text path for normal input) while keeping `validateAntennaProtocol` as hardening.
- [x] 2.5 **REFACTOR**: update `settings` save prompt/help text and edit mode behavior in `internal/tui/screens/settings_screen.go`/`settings_screen_test.go` for clarity of `stay/discard/save` choices.

## Phase 3: Navigation Contract (Arrows + j/k + Wrap)

- [x] 3.1 **RED**: add table-driven `internal/tui/screens/navigation_test.go` for `wrap` helper on boundaries and no-op when list is empty.
- [x] 3.2 **GREEN**: create `internal/tui/screens/navigation.go` with `moveCursor(current, length, delta int) int`.
- [x] 3.3 **GREEN**: update `internal/tui/screens/main_screen.go` to use shared cursor helper for `↑/↓` and `k/j` (wrap behavior).
- [x] 3.4 **GREEN**: update `internal/tui/screens/antennas_screen.go` to use shared cursor helper and clamp-safe wrap semantics for empty antenna lists.
- [x] 3.5 **GREEN**: update `internal/tui/screens/settings_screen.go` normal navigation to use shared helper with `k/j` parity.
- [x] 3.6 **REFACTOR**: update `internal/tui/screens/main_screen_test.go`, `internal/tui/screens/antennas_screen_test.go`, and settings navigation tests to assert wrap behavior.

## Phase 4: Status Uptime Source of Truth

- [x] 4.1 **RED**: add `internal/tui/screens/status_screen_test.go` case that `View()` uses fixed `SystemStatus.Uptime` and does not depend on elapsed local time.
- [x] 4.2 **GREEN**: update `internal/tui/screens/status_screen.go` to render `m.status.Uptime` via `formatDuration` and remove local `startTime` reliance.
- [x] 4.3 **REFACTOR**: update any status expectations in `internal/tui/app_test.go` and `internal/tui/screens/status_screen_test.go` to verify backend `Uptime` mapping remains stable.

## Phase 5: Documentation, Spec Alignment, and Verification

- [x] 5.1 **GREEN**: update `README.md` sections for Settings and TUI navigation to mention explicit save (`s` + confirm), unsaved exit options, and antenna protocol selector values.
- [x] 5.2 **GREEN**: refresh `docs/screenshots/settings.svg` and add/update any related screenshots to reflect confirmation + selector + wrap behavior text.
- [x] 5.3 **REFACTOR**: add/adjust spec verification notes in `openspec/changes/tui-operator-safety-improvements/specs/tui-operator-safety/spec.md` and `openspec/changes/tui-operator-safety-improvements/specs/antenna-protocol-configuration/spec.md` by mapping each requirement scenario to concrete tests.
- [x] 5.4 **GREEN**: run `go test -v ./...` and capture coverage of `internal/tui/...` test outcomes as completion gate.
