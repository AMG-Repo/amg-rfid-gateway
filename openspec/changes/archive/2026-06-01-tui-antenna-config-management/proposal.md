# Proposal: TUI Antenna Configuration Management

## Intent

Enable operators to create, edit, and delete antenna entries directly in the TUI, persisted to `config.yaml`, so routine topology changes do not require manual YAML editing.

## Scope

### In Scope
- Add TUI CRUD flow for antenna entries (`id`, `ip`, `port`, `enabled`, `zone`, `protocol`) with clear key-driven interactions.
- Keep Settings as save boundary: edits remain in-memory until explicit save/confirm flow persists through existing `Validate()` + `SaveToYAML()` pipeline.
- Add validation/error feedback for invalid draft entries and prevent saving invalid antenna sets.
- Add/update table-driven tests (`t.Run`) for screen-level key flows and app-level persistence round-trip.

### Out of Scope
- Hot runtime reload of antenna changes without restart.
- Protocol runtime implementation changes beyond existing `generic`/`zebra` selection support.
- Large TUI visual redesign outside antenna management UX.

## Capabilities

- `tui-operator-safety`: preserve explicit save confirmation and unsaved-exit protections while introducing antenna CRUD.
- `antenna-protocol-configuration`: preserve constrained protocol selection and round-trip persistence for each antenna.

## Approach

Implement a dedicated antenna management sub-model/screen used by Settings, instead of extending the current flat field list further. This isolates CRUD state transitions (list, create/edit form, delete confirm), reduces regression risk in `settings_screen.go`, and keeps app persistence integration minimal.

## Affected Areas

| Area | Impact | Description |
|------|--------|-------------|
| `internal/tui/screens/settings_screen.go` | Modified | Integrate antenna CRUD sub-flow and map committed antenna state into Settings config output. |
| `internal/tui/screens/settings_screen_test.go` | Modified | Add table-driven key-flow tests for add/edit/delete/save/discard behavior using `tea.KeyMsg` and `Model` casts. |
| `internal/tui/app.go` | Modified | Minimal wiring changes if new settings messages are emitted for CRUD state/intent handling. |
| `internal/tui/app_test.go` | Modified | Verify save-confirm persists CRUD outcomes to YAML and discard leaves persisted config unchanged. |
| `internal/config/config.go` | Referenced | Reuse existing antenna validation as persistence guardrails. |

## Risks

| Risk | Likelihood | Mitigation |
|------|------------|------------|
| Ambiguous key states cause operator mistakes | Medium | Keep explicit mode indicators/help text and lock behavior with key-flow tests. |
| Invalid draft entries block save unexpectedly | Medium | Distinguish draft/edit errors from committed list and show targeted validation messages before save. |
| Antenna ordering churn in YAML reduces trust | Low | Preserve stable list ordering unless user intentionally reorders in future scope. |

## Rollback Plan

Revert the TUI antenna CRUD changeset as one unit: restore prior Settings field mapping behavior, remove CRUD-specific state/messages/tests, and retain existing protocol-only editing path. Validate rollback by running TUI tests and confirming Settings save behavior matches pre-change flow.

## Dependencies

- `openspec/changes/tui-antenna-config-management/exploration.md`
- `openspec/specs/tui-operator-safety/spec.md`
- `openspec/specs/antenna-protocol-configuration/spec.md`

## Success Criteria

- [ ] Operators can add, edit, and delete antennas in TUI without editing YAML manually.
- [ ] Saving from Settings persists only confirmed changes to `config.yaml` and passes config validation.
- [ ] Discard/stay flows with unsaved antenna edits follow existing operator-safety contract.
- [ ] Protocol selection remains constrained to supported values per antenna.
- [ ] New/updated tests follow project standards (`t.Run`, `tea.KeyMsg` simulation, `teatest.NewTestModel`, `require/assert`, `t.Helper`) and pass.
