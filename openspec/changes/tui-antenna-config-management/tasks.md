# Tasks: TUI Antenna Configuration Management

## Review Workload Forecast

| Field | Value |
|-------|-------|
| Estimated changed lines | 620-860 |
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
| 1 | Guard uniqueness + config validation behavior | PR 1 (foundation) | `internal/config` only, zero UI coupling |
| 2 | Implement Settings antenna editor model and rendering contract | PR 2 (UI/core) | Add CRUD modes + form validation + staged drafts |
| 3 | Save/error integration and persistence round-trip coverage | PR 3 (integration/verification) | App message flow + YAML no-write-on-fail scenarios |

## Phase 1: Foundation / Validation Contracts

- [x] 1.1 RED: add table-driven cases in `internal/config/config_test.go` for duplicate `AntennaConfig.ID` rejection, including adjacent and non-adjacent duplicates.
- [x] 1.2 RED: add table-driven case in `internal/config/config_save_test.go` for save of duplicate antenna IDs and expected failure path from `SaveToYAML`/`LoadFromYAML` round-trip.
- [x] 1.3 GREEN: update `internal/config/config.go` `GatewayConfig.Validate()` to fail fast on duplicate antenna IDs with actionable error text.
- [x] 1.4 GREEN: add helper tests in `internal/config/config_test.go` that verify the duplicate-id error includes the conflicting ID and keeps unrelated validation checks deterministic.

## Phase 2: Core Settings Implementation

- [x] 2.1 RED: add table-driven unit tests in `internal/tui/screens/settings_antenna_editor_test.go` for `antennaEditorModel` modes (`list`, `form`, `deleteConfirm`) and state transitions.
- [x] 2.2 GREEN: implement `antennaEditorModel` in `internal/tui/screens/settings_screen.go` with staged `draft []config.AntennaConfig`, cursor/form state, and mode transitions for add/edit/delete keys.
- [x] 2.3 GREEN: keep `SettingsScreenModel` as save boundary in `internal/tui/screens/settings_screen.go`, wiring `GetConfig()`/`SetConfig()` to use editor drafts instead of mutating `m.config.Antennas` inline.
- [x] 2.4 GREEN: add per-antenna form validation in `internal/tui/screens/settings_screen.go` (`id`, `ip`, `port`, `zone`, `enabled`, `protocol`) with in-UI validation messages while editing form rows.
- [x] 2.5 GREEN: add protocol and ID uniqueness enforcement on form commit in `internal/tui/screens/settings_screen.go` so duplicate IDs never become staged in-memory.

## Phase 3: App Wiring, Error Surfacing, and Operator UX

- [ ] 3.1 RED: add table-driven `internal/tui/app_test.go` cases for SaveIntent with CRUD drafts: invalid edit, valid create/save, valid edit/save, valid delete/save, and discard behavior while antennas are dirty.
- [ ] 3.2 GREEN: add message bridge in `internal/tui/app.go` (`settingsSaveErrorMsg`) so `handleSettingsSave` sets `settings save` error state instead of only logging.
- [ ] 3.3 GREEN: update `internal/tui/screens/settings_screen.go` `View()` to show actionable save/validation error text in-screen and clear it on successful save/discard/reset.
- [ ] 3.4 GREEN: ensure `handleSettingsSave()` in `internal/tui/app.go` keeps operator in Settings on `Validate()`/`SaveToYAML()` failure and does not write `config.yaml`.
- [ ] 3.5 REFACTOR: make unsaved-exit prompt behavior explicit for antenna drafts (save/discard/stay) and verify cursor/dirty-state remains visible after failed save.

## Phase 4: Integration / Verification (TDD RED→GREEN→REFACTOR)

- [ ] 4.1 RED: add `teatest.NewTestModel`-based flow tests in `internal/tui/screens/settings_screen_integration_test.go` for add/edit/delete/save/stay on `internal/tui` models.
- [ ] 4.2 GREEN: extend `internal/tui/app_test.go` with `go test`-driven table cases asserting `go test -v ./...` relevant scenarios from specs 2/3 and operator-visible error messages when save is blocked.
- [ ] 4.3 REFACTOR: split settings tests into focused helpers in `internal/tui/screens/settings_screen_test.go` (`t.Helper()` helpers) and keep field index assertions by key for maintainability.
- [ ] 4.4 RED: add table-driven regression cases in `internal/tui/app_test.go` for protocol selector constrained values (`generic`, `zebra`) and legacy default protocol display from `config.yaml`.

## Phase 5: Cleanup and Consistency

- [ ] 5.1 REFACTOR: update `openspec/changes/tui-antenna-config-management/specs/antenna-protocol-configuration/spec.md` and `.../specs/tui-operator-safety/spec.md` with explicit validation/error UX mapping to each scenario.
- [ ] 5.2 REFACTOR: run `go test -v ./...` and document any spec deltas or follow-up test stabilization tasks.
