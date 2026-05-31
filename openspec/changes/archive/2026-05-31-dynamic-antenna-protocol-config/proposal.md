## Why

The gateway currently hardcodes Generic RFID assumptions in config, TUI, and runtime packet handling, which blocks safe introduction of additional antenna protocols. We need a controlled path to configure protocol per antenna from the TUI while preserving behavior for existing deployments.

## What Changes

1. **Config schema and compatibility**
   - Add per-antenna `protocol` field in `AntennaConfig`.
   - Define default protocol as `generic` when the field is missing.
   - Validate protocol values against an allowlist (initially `generic`, `zebra`).

2. **TUI settings support**
   - Extend settings UI/state so antenna entries are editable for protocol selection.
   - Ensure save/apply flow round-trips protocol values without mutating unrelated fields.

3. **Protocol strategy abstraction in runtime**
   - Introduce a protocol handler/strategy boundary used by antenna/rawtcp consumers.
   - Keep transport/extraction pipeline shared; move protocol-specific parse/routing logic behind handlers.
   - Keep `generic` as fully supported baseline behavior.

4. **Spec and tests alignment**
   - Update/extend specs to represent multi-protocol configurability.
   - Rewrite brittle tests that encode old hardcoded-generic internals; delete/recreate where needed to assert new protocol-aware contracts.

## Out of Scope (for this change)

- Claiming full Zebra protocol compatibility unless framing/API details are known and implemented.
- Replacing shared TCP transport lifecycle.

## Risk Mitigation

- **Backward compatibility**: Missing `protocol` defaults to `generic`; existing YAML must continue booting unchanged.
- **Test churn control**: Approved strategy is to rewrite or recreate brittle tests instead of preserving tests tied to obsolete hardcoded behavior.
- **Incremental runtime refactor**: First isolate generic logic behind strategy interfaces, then add optional zebra handler stubs/contracts.
- **Scope honesty**: If Zebra details are incomplete, deliver configurability/extensibility only and document remaining implementation gaps.

## Rollback Plan

If regressions occur:
1. Keep config defaulting logic in place and force runtime to use `generic` handler only.
2. Disable TUI protocol edit controls (or lock to `generic`) while preserving persisted field compatibility.
3. Revert protocol dispatch path to generic-only execution behind a feature guard or minimal branch rollback.

## Success Criteria

- Existing configs without `protocol` load and run exactly as before.
- New configs can set per-antenna protocol via TUI and persist/reload correctly.
- Runtime path selects handler by configured protocol without breaking generic processing.
- Specs/tests reflect protocol-aware behavior; brittle generic-assumption tests are replaced with stable contract tests.
- Deliverable text does not claim full Zebra support unless parser/framing implementation is actually provided.
