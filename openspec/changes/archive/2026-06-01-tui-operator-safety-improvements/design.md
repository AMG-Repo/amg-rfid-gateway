# Design: TUI Operator Safety Improvements

## Technical Approach

Make Settings edits purely in-memory until an explicit screen event asks the app to persist. Keep Bubble Tea message boundaries: `internal/tui/screens/settings_screen.go` owns field state, prompts, and selector UX; `internal/tui/app.go` owns config validation and `SaveToYAML`. Status rendering will use the already-polled `screens.SystemStatus.Uptime`. Navigable lists will share one wrap helper so `↑/↓` and `k/j` cannot drift again.

## Architecture Decisions

| Decision | Choice | Alternatives considered | Rationale |
|---|---|---|---|
| Save trigger | Persist only when `settingsSaveMsg` reaches `App.Update`; stop saving from `HasChanges()` | Keep autosave with extra guards | The spec requires explicit save intent + confirmation; `HasChanges()` is state, not intent. |
| Unsaved exit | Add Settings prompt state for pending navigation with `save`, `discard`, `stay` outcomes | Let global ESC always leave | Operators need a deterministic no-loss path before leaving Settings. |
| Protocol input | Treat antenna protocol fields as selector fields cycling `generic`/`zebra` on `←/→`, space, or Enter | Keep free-text + validation | Constrained controls prevent normal invalid intermediate values while retaining validation defense-in-depth. |
| Navigation | Create a package-local helper for wrapping cursor movement | Copy per-screen switch logic | One helper enforces identical arrow + `j/k` behavior across Main, Settings, and Antennas. |
| Uptime label | Render backend/gateway uptime from `SystemStatus.Uptime`; do not keep TUI session uptime in Status unless renamed separately | Continue `time.Since(startTime)` | The bridge already maps backend uptime into `SystemStatus`; Status must show backend truth. |

## Data Flow

```text
Settings key edit ──→ SettingsScreenModel.values + hasChanges
      │
      ├─ s ─→ save confirm ─ y ─→ settingsSaveMsg ─→ App validates ─→ SaveToYAML
      │
      └─ esc/back ─→ unsaved prompt ─ save/discard/stay ─→ save or navigation/cancel

Bridge health uptime ─→ App.pollData SystemStatus.Uptime ─→ StatusScreen.SetStatus ─→ View
```

## File Changes

| File | Action | Description |
|---|---|---|
| `internal/tui/app.go` | Modify | Handle `settingsSaveMsg`; remove `HasChanges()` autosave; handle Settings back/discard navigation. |
| `internal/tui/screens/settings_screen.go` | Modify | Add unsaved-exit prompt state/messages; add selector behavior for `antenna_protocol:*`; add `j/k`; update help. |
| `internal/tui/screens/navigation.go` | Create | Small helper such as `wrapCursor(current, length, delta int) int` used by navigable screens. |
| `internal/tui/screens/main_screen.go` | Modify | Use shared wrap helper for up/down and `k/j`. |
| `internal/tui/screens/antennas_screen.go` | Modify | Use shared wrap helper; no-op safely when list is empty. |
| `internal/tui/screens/status_screen.go` | Modify | Render `formatDuration(m.status.Uptime)` and remove/ignore local `startTime`. |
| `internal/tui/*_test.go`, `internal/tui/screens/*_test.go` | Modify | Add RED/GREEN tests below. |

## Interfaces / Contracts

Screen package messages remain unexported because `app.go` already imports `screens` and receives concrete message values from commands.

```go
type settingsSaveMsg struct{}
type settingsBackMsg struct{}
type settingsDiscardChangesMsg struct{}
type settingsStayMsg struct{}

type settingsPromptMode int // none, saveConfirm, unsavedExit
```

Contract: `HasChanges()` only reports dirty state. It MUST NOT trigger persistence by itself. `SetConfig(savedCfg)` resets `original`, `values`, prompts, editing state, and dirty flag after successful save or discard reload.

Protocol selector contract: for `antenna_protocol:*`, Enter/space/right cycles forward, left cycles backward; values are always lower-case supported protocols. `validateAntennaProtocol` stays for tests and corrupted/manual state.

## Testing Strategy

| Layer | RED targets | GREEN approach |
|---|---|---|
| App | Editing Settings does not modify YAML until `settingsSaveMsg`; save+confirm persists; unsaved save/discard/stay navigates correctly. | Table-driven `internal/tui/app_test.go` using temp YAML and direct `tea.KeyMsg`/message simulation. |
| Screen | Settings emits save only after confirm; unsaved exit prompt branches; protocol selector cannot produce `foo`; `j/k` wraps. | Table-driven tests in `settings_screen_test.go` with `require` for message types and config preservation. |
| Screen | Status renders provided `SystemStatus.Uptime`, not elapsed local time. | Set uptime to fixed duration and assert view contains expected formatted value. |
| Screen | Main and Antennas wrap at boundaries for arrows and `j/k`. | Update existing navigation tests from clamp expectations to wrap expectations. |

## Migration / Rollout

No data migration required. Runtime rollout is TUI-only: existing YAML stays valid, and config validation remains the final guard before save.

## Open Questions

None.
