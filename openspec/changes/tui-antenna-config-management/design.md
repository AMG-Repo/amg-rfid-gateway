# Design: TUI Antenna Configuration Management

## Technical Approach

Extend Settings with an embedded antenna CRUD sub-model that owns antenna list/form/delete state, while `SettingsScreenModel` remains the save boundary. Scalar fields continue using the existing `fields/values` flow; antenna edits are staged in memory and merged only through `GetConfig()`. App persistence stays centralized in `App.handleSettingsSave()`: `GetConfig()` → `Validate()` → `SaveToYAML()`. Hot reload is out of scope.

## Architecture Decisions

| Decision | Choice | Alternatives considered | Rationale |
|---|---|---|---|
| Model boundary | Add an unexported `antennaEditorModel` in `internal/tui/screens/settings_screen.go` | Separate package/screen or more flat settings fields | Keeps CRUD local to Settings and avoids app navigation churn while reducing the current flat-field coupling. |
| Staging | Keep `[]config.AntennaConfig` draft inside the antenna editor; `original` remains source for discard | Mutate `m.config.Antennas` immediately | Existing safety contract requires edits not to persist or become accepted state until explicit save/confirm. |
| Validation | Validate form fields before committing a draft row; validate whole config in app before saving | Rely only on final `GatewayConfig.Validate()` | Gives targeted operator errors while preserving existing persistence guardrails. |
| Protocol input | Selector cycling `generic`/`zebra` for list/form protocol | Free-text editing | Matches constrained protocol requirement and prevents invalid normal-flow values. |

## Data Flow

    keys ─→ SettingsScreenModel.Update ─→ antennaEditorModel.Update
                                      │
                                      ├─ staged scalar values
                                      └─ staged antenna draft list

    save key ─→ confirm ─→ settingsSaveMsg ─→ App.handleSettingsSave
       └──────────────────── GetConfig() ─→ Validate() ─→ SaveToYAML(config.yaml)

Discard calls `settings.SetConfig(a.cfg)`, resetting scalar values and antenna drafts from the last persisted app config.

## File Changes

| File | Action | Description |
|------|--------|-------------|
| `internal/tui/screens/settings_screen.go` | Modify | Add `antennaEditorModel`, route keys by mode, render antenna CRUD UI, merge staged antennas in `GetConfig()`, reset in `SetConfig()`. |
| `internal/tui/screens/settings_screen_test.go` | Modify | Add table-driven key-flow tests for add/edit/delete, validation, protocol cycling, save/discard state. |
| `internal/tui/app.go` | Modify | Keep save handling centralized; optionally expose validation/save error back to Settings if current log-only behavior blocks visible errors. |
| `internal/tui/app_test.go` | Modify | Add config.yaml round-trip tests for create/edit/delete and discard. |
| `internal/config/config.go` | Reference | Reuse `AntennaConfig.Validate()`, `GatewayConfig.Validate()`, `SaveToYAML()`. |

## Interfaces / Contracts

`SettingsScreenModel` gains:

```go
antennaEditor antennaEditorModel
saveError string
```

`antennaEditorModel` owns `mode` (`list`, `form`, `deleteConfirm`), `cursor`, `formCursor`, `draft []config.AntennaConfig`, `form antennaForm`, and `err string`.

Keybindings: list `a` add, `e`/enter edit, `d` delete confirm, `j/k` or arrows navigate, `s` Settings save, `esc` exits form/delete or triggers Settings unsaved prompt. Form fields: `id`, `ip`, `port`, `enabled`, `zone`, `protocol`; left/right/space toggle bool and cycle protocol.

## Testing Strategy

| Layer | What to Test | Approach |
|-------|-------------|----------|
| Unit | Antenna editor transitions, form validation, protocol selector | Table-driven tests with `t.Run`, `tea.KeyMsg`, `require/assert`. |
| Screen | Settings dirty state, render/help text, discard reset | Cast `Update` result to `SettingsScreenModel`; use helpers with `t.Helper()`. |
| App integration | Save confirm persists create/edit/delete; invalid config leaves YAML unchanged; discard leaves YAML unchanged | Temp `config.yaml`, `SaveToYAML`, `LoadFromYAML`; follow existing `app_test.go` helpers. |
| E2E | Not required | Existing app-level tests cover persistence contract. |

## Migration / Rollout

No migration required. Legacy antennas without protocol continue defaulting to `generic` through existing config defaults.

## Open Questions

- [ ] Should duplicate antenna IDs be rejected in TUI before final config validation? Current `AntennaConfig.Validate()` does not enforce uniqueness.
