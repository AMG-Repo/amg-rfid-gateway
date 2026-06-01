## Exploration: tui operator safety improvements

### Current State
- Settings persistence is triggered in `App.Update` whenever `settings.HasChanges()` is true (`internal/tui/app.go`), independent of explicit save confirmation (`settingsSaveMsg` is emitted by settings screen but never consumed by app).
- Status uptime display uses `time.Since(m.startTime)` from TUI screen init (`internal/tui/screens/status_screen.go`), not backend uptime from polled status payload (`SystemStatus.Uptime` set in `internal/tui/app.go`).
- Antenna protocol fields in Settings are editable free-text fields validated post-entry (`validateAntennaProtocol`), so operators can type invalid/intermediate values before commit (`internal/tui/screens/settings_screen.go`).
- Navigation behavior is inconsistent:
  - `settings_screen`: wraps on up/down and supports tab/shift+tab; help says `j/k` but update handles only arrows/tab.
  - `main_screen` and `antennas_screen`: support `j/k` and arrows, but clamp at boundaries (no wrap).
  - `network_screen` and `status_screen`: no list navigation.

### Affected Areas
- `internal/tui/app.go` — settings save orchestration currently coupled to `HasChanges()` instead of explicit confirmation event.
- `internal/tui/screens/settings_screen.go` — save intent, protocol field UX, and keyboard handling (`j/k` parity) live here.
- `internal/tui/screens/status_screen.go` — uptime source/rendering currently tied to local session start time.
- `internal/tui/screens/main_screen.go` — clamped menu navigation behavior.
- `internal/tui/screens/antennas_screen.go` — clamped list navigation behavior.
- `internal/tui/screens/settings_screen_test.go` — currently tests free-text protocol editing and wrapping with arrow keys only.
- `internal/tui/screens/status_screen_test.go` — currently validates uptime behavior around `startTime` growth.
- `internal/tui/screens/main_screen_test.go`, `internal/tui/screens/antennas_screen_test.go` — currently assert clamped navigation.
- `internal/tui/app_test.go` — currently verifies protocol edit round-trip via free-text edit flow.

### Approaches
1. **Event-gated save + constrained selectors + unified wrap navigation** — save only on explicit `settingsSaveMsg`, convert constrained fields (especially antenna protocol) to cycle/select behavior, and standardize list navigation semantics across screens.
   - Pros: Strong operator-safety guarantees; removes accidental persistence path; reduces invalid input risk; predictable keyboard behavior.
   - Cons: Requires coordinated model + test updates across app and multiple screens.
   - Effort: Medium

2. **Minimal patching of current flow** — keep existing edit model, add additional guards around autosave and selectively patch uptime + key inconsistencies.
   - Pros: Lower initial churn.
   - Cons: Leaves fragile semantics (mixed input metaphors, latent autosave coupling) and weaker UX consistency.
   - Effort: Low-Medium

### Recommendation
Use **Approach 1**. Safety requirements are explicit (no persistence before confirmation), and constrained protocol selection + consistent navigation are UX-level contracts best solved holistically, not with incremental guardrails.

### Risks
- Refactoring save trigger from `HasChanges()` to explicit save event may break current tests and any assumptions relying on immediate persistence.
- Changing navigation semantics (wrap vs clamp) can be opinionated; if not defined per-screen, behavior regressions may reappear.
- Constrained selector UX for protocol must remain discoverable (clear help text and deterministic key bindings).

### Ready for Proposal
Yes — proceed with proposal scoped to: explicit save-event persistence, backend uptime rendering source, constrained antenna protocol selector, and a single documented navigation contract (including `j/k` parity).
