# Proposal: TUI Operator Safety Improvements

## Intent

Reduce operator error risk in the TUI by requiring explicit save intent, preventing accidental loss on exit, standardizing navigation behavior, and aligning status uptime with backend truth.

## Scope

### In Scope
- Gate Settings persistence behind explicit save intent (`s`) plus confirmation; remove implicit save-on-edit behavior.
- Define unsaved-changes behavior when leaving Settings (prompt and explicit operator decision path).
- Change Status screen uptime rendering to backend/gateway uptime (`SystemStatus.Uptime`) instead of local TUI session age.
- Replace free-text antenna protocol editing with constrained selector/cycle values (`generic`, `zebra`).
- Define and implement a single navigation contract for navigable screens, including `j/k` parity and a clear wrap/clamp rule.
- Add/update tests for all above behaviors.

### Out of Scope
- Zebra protocol reader/parser implementation.
- Full TUI visual redesign.
- Network screen provider refactor (may be proposed later as follow-up).

## Approach

Adopt an event-driven safety model in the TUI: edits only mutate in-memory state, while persistence occurs exclusively from an explicit save event and confirmation flow. In parallel, align input ergonomics (protocol selector + consistent key behavior) and update status uptime source to backend-provided values. Lock the behavior with targeted screen and app-level tests.

## Affected Areas

| Area | Impact | Description |
|------|--------|-------------|
| `internal/tui/app.go` | Modified | Consume explicit save event for persistence; remove save coupling to generic `HasChanges()` edit detection. |
| `internal/tui/screens/settings_screen.go` | Modified | Add explicit unsaved-exit handling, protocol selector/cycle behavior, and navigation contract (`j/k` parity + wrap/clamp policy). |
| `internal/tui/screens/status_screen.go` | Modified | Render uptime from `SystemStatus.Uptime`. |
| `internal/tui/screens/main_screen.go` | Modified | Align navigation behavior with defined contract. |
| `internal/tui/screens/antennas_screen.go` | Modified | Align navigation behavior with defined contract. |
| `internal/tui/app_test.go` | Modified | Verify event-gated persistence and unsaved-flow handling at app integration boundary. |
| `internal/tui/screens/settings_screen_test.go` | Modified | Verify save gating, exit-with-unsaved behavior, selector/cycle protocol editing, and key parity. |
| `internal/tui/screens/status_screen_test.go` | Modified | Verify uptime source is backend status payload. |
| `internal/tui/screens/main_screen_test.go` | Modified | Verify navigation contract behavior. |
| `internal/tui/screens/antennas_screen_test.go` | Modified | Verify navigation contract behavior. |

## Risks

| Risk | Likelihood | Mitigation |
|------|------------|------------|
| Existing assumptions rely on immediate save on edit | Medium | Add app-level regression tests that assert persistence only occurs after explicit save + confirmation. |
| Navigation contract choice (wrap vs clamp) introduces expectation mismatch | Medium | Document one explicit contract in help text and enforce it consistently via tests across navigable screens. |
| Protocol selector discoverability | Low | Keep visible value + key hints and validate key-path tests for deterministic cycling. |

## Rollback Plan

Revert the TUI safety change set as one unit: restore previous settings persistence trigger, restore free-text protocol editing, restore prior per-screen navigation behavior, and restore status uptime rendering from local session time. Validate rollback by re-running prior TUI tests and smoke-checking settings edit/save flow.

## Dependencies

- Existing exploration artifact: `openspec/changes/tui-operator-safety-improvements/exploration.md`
- Existing protocol spec context: `openspec/specs/antenna-protocol-configuration/spec.md`

## Success Criteria

- [ ] Editing fields in Settings no longer persists config until explicit `s` save action and confirmation.
- [ ] Leaving Settings with unsaved changes follows a deterministic prompt/decision flow without silent data loss.
- [ ] Status screen uptime reflects backend `SystemStatus.Uptime` values.
- [ ] Antenna protocol editing is constrained to `generic`/`zebra` (no free-text invalid entries).
- [ ] Navigable screens implement one documented `j/k` + arrow behavior with explicit wrap/clamp policy.
- [ ] Test suite covers each contract above and passes.
