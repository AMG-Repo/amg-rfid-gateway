# Homebrew Static Account Access Policy

## Objective and problem

Specify a non-authorizing access and migration contract for the user-selected static, dedicated non-root system-service identity. The historical Raspberry data directory was `pi:pi` (UID/GID 1000) at `0775`, while the existing planner and fixture transaction only change its mode to `0700` without changing owner. A different service UID would lose access. Preserve the completed Homebrew hardening tracker in `odd/tasks/homebrew-deployment-hardening.md` and its DEVICE-GATE NO-GO decision.

## Scope and constraints

- Local English documentation only: `docs/homebrew-deployment-account-access.md` and this tracker. No Go changes, unit creation, account provisioning, chown, chmod, adapter, production caller, live verification, or Raspberry action.
- Homebrew systemd **system** service only, with a statically provisioned dedicated non-root account; no `DynamicUser` and no reuse of the legacy `/opt` service account by assumption.
- Describe proposed vs historically observed vs current-unknown facts separately. Do not invent the account name/UID/GID, installed formula receipt, actual unit/log paths, or current device state.
- Treat data ownership migration and inverse as new *future* authority and recovery design, not a capability of today's `0775 → 0700` fixture engine. Keep `ApplyEligible=false`, DEVICE-GATE NO-GO, and no start/enable/activation authority.
- No SSH, sudo, Brew/systemctl, apply, activation, push, PR, merge, or remote publication. Local work-unit commit requires a separate explicit user authorization.

## Tasks

- [ ] **ACCOUNT-ACCESS-1 — Document the static-account access and migration contract.** Route: delegated bounded documentation writer; multi-file documentation and 4+ relevant source surfaces mapped by read-only scout. Specify an access matrix for config, data, runtime/socket, unit/overrides, service logs and deployment journal/lock (owner, writer, reader, lifecycle, evidence status). Identify conflicting existing owner checks; define preflight, identity-bound ownership/mode transition, conditional inverse, interruption evidence and fail-closed outcomes at contract level, not executable steps. Distinguish service journal from deployment recovery journal. Review against current docs/source and official systemd semantics. Check source-grounded readback and `git diff --check`; do not mark complete before verified outcome and a separately authorized local work-unit commit.

## Acceptance and checks

1. Every proposed owner and access mode is labeled as future policy, not installed fact; static account exact IDs and provisioning stay unresolved.
2. `0700` data owned by a deployer is explicitly incompatible with a distinct service UID; no silent `chown`, permission widening, or unsupported transaction claim.
3. Service cannot write its unit, effective overrides, executable, config, or deployment recovery journal; client socket access and log destinations remain explicit decisions.
4. The doc defines refusal when identity, ownership, receipt, unit, runtime or rollback evidence is stale, absent, or ambiguous; no action is authorized.
5. Documentation readback, whitespace check and any unavailable independent check are recorded truthfully.

## Verification and delivery

Documentation-only work: source-grounded semantic readback and `git diff --check`; no Go TDD or Go test required because source is unchanged. Existing Homebrew feature chose `stacked-to-main` for reviewable slices; this documentation slice is locally bounded, not a PR authorization. Forecast: approximately 100-180 authored doc/tracker lines. No commit until explicit user authorization.

## Progress and evidence

- User chose a statically provisioned dedicated non-root account over `DynamicUser` for a future system unit; this is a product policy choice, not live account evidence.
- Read-only mapper confirmed `change_plan.go` and fixture-only `transaction.go` preserve owner and require invoker-owned selected data. The tracker records only historical `pi:pi` UID/GID 1000, data mode `0775`; current device state is unknown. Config, runtime/socket, unit/overrides, logs and recovery journal lack a unified access policy. No tests, edits or device commands occurred in mapping.
- Delegated documentation writer added `docs/homebrew-deployment-account-access.md`, with a proposed access matrix and future identity-bound migration/inverse contract. Writer source readback, tracked `git diff --check` and direct untracked-doc whitespace check passed. Native working-tree assessment was `unassessable` due to untracked files; outcome unknown, no lineage or receipt claimed. Its returned plan required an independent verifier, which **PASS**ed semantic/security readback and observed `git diff --check` and direct whitespace checks on both authored untracked files. Parent spot check `git diff --check` passed. No tests were run because this is docs only; no device checks or privileged operations occurred. The task remains open until a separately authorized local work-unit commit, with `.codegraph/` excluded.

## Next step

Candidate contract verified locally; request separate authorization for the exact documentation-plus-tracker local commit. DEVICE-GATE stays NO-GO and `ApplyEligible=false`; no service or device action follows automatically.
