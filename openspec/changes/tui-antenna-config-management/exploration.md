## Exploration: TUI antenna config management

### Current State
- Antenna entries already exist in config schema (`internal/config/config.go`): `id`, `ip`, `port`, `enabled`, `zone`, `protocol`, and validation is enforced by `GatewayConfig.Validate()` + `AntennaConfig.Validate()`.
- Settings screen builds editable fields from a fixed base list, then appends **only protocol fields for existing antennas** (`settingsFieldsAndValues` in `internal/tui/screens/settings_screen.go`).
- `GetConfig()` only maps edited scalar fields + per-antenna protocol back into a copy of original config; it does not create/remove/reorder `cfg.Antennas` entries.
- Save path is already wired and safe (`App.handleSettingsSave` → `Validate()` → `SaveToYAML()` in `internal/tui/app.go`), so persistence and validation pipeline exists.
- Tests currently cover protocol edit round-trip and save behavior, but not full antenna CRUD flows (`internal/tui/screens/settings_screen_test.go`, `internal/tui/app_test.go`).

### Affected Areas
- `internal/tui/screens/settings_screen.go` — current model is field-list oriented; needs antenna row management (create/edit/delete) and input flow beyond protocol cycling.
- `internal/tui/screens/settings_screen_test.go` — add table-driven tests for antenna add/edit/delete validation and key flow.
- `internal/tui/app.go` — minimal changes expected; may need message handling only if settings emits new CRUD-specific commands.
- `internal/tui/app_test.go` — integration-level save round-trip tests for CRUD outcomes.
- `internal/config/config.go` — likely no schema changes, but validation constraints will define TUI UX guardrails (unique IDs, valid IP/port expectations if added).
- `internal/config/config_test.go` + `config_save_test.go` — extend if behavior around antenna list normalization/ordering or new invariants is introduced.

### Approaches
1. **Extend current Settings screen in-place** — keep one screen, add antenna CRUD mode inside `SettingsScreenModel`.
   - Pros: Reuses existing save/unsaved-change/validation flow; lowest integration overhead with `App`.
   - Cons: `settings_screen.go` is already large/stateful; adding modal CRUD states increases complexity and regression risk.
   - Effort: Medium

2. **Split antenna management into dedicated sub-screen/component** — Settings keeps global fields, antenna management handled by a dedicated model (embedded or separate screen).
   - Pros: Better separation of concerns, clearer keybindings, easier long-term maintenance and testing.
   - Cons: Requires navigation/state plumbing between models and slightly larger initial refactor.
   - Effort: Medium-High

### Recommendation
Use **Approach 2**: isolate antenna CRUD into a dedicated model/screen and keep Settings as the orchestration/save boundary. This reduces cognitive load in `settings_screen.go`, keeps existing save pipeline intact, and makes TDD for antenna workflows cleaner (add/edit/delete scenarios as focused table tests).

### Risks
- Keybinding collisions and UX ambiguity (navigation/edit/confirm/delete) can create operator errors if interaction states are not explicit.
- Validation feedback for partially entered antennas (e.g., invalid port/IP) may block save unexpectedly unless draft vs committed entry states are modeled.
- Delete behavior is destructive; without clear confirmation and unsaved-exit integration, accidental removals are possible.
- YAML list ordering stability may affect diff readability and operator trust if order changes unexpectedly after edits.

### Ready for Proposal
Yes — proceed to proposal with explicit scope: antenna CRUD UX in TUI, validation/error-handling rules, save/discard semantics, and integration tests for config.yaml round-trip.
