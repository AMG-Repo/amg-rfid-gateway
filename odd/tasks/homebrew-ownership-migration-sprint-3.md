# Homebrew Ownership Migration — Sprint 3

## Objective and problem

Exercise a separate fixture-only whole-tree owner/group/mode transaction with bounded durable recovery. The existing `dataTransaction` changes the mode of one directory while preserving its owner; Sprint 2's production proposal refuses all ownership transitions because provenance is missing. Fixture tests cannot establish device or production authority.

## Scope and constraints

- User selected modeled/injected ownership transitions in local fixtures, **not** actual kernel `chown`, sudo, or a privileged test harness. Simulate per-object UID/GID/mode updates through an isolated adapter, while exercising journal and recovery semantics against local temporary fixture state.
- Keep existing `PlanChange`, `ContentObservation`, `proposeAccountChange`, mode-only `dataTransaction`, and their callers unchanged. Never introduce a production apply caller, public activation path, or positive proposal from current observations.
- No SSH/Raspberry, sudo, Brew/systemctl, actual chown, apply/activation, push, PR or merge. Preserve pre-existing dirty tracker notes and `.codegraph/`. DEVICE-GATE NO-GO and `ApplyEligible=false` remain unchanged.
- Keep a bounded fixture tree (root, regular file, empty directory) with explicit per-object relative path, kind, identity, before/intended UID, GID, mode. Refuse unsupported objects and changed/partial/ambiguous inventory. Recheck object identities and state before each transition and inverse. Durable intent/progress must precede mutations; interruption must not imply success or erase recovery evidence.
- No claim of host kernel ownership, hostile same-UID writer resistance, account provisioning, group policy, or device durability. If safety requires a larger work unit, stop and report rather than weakening checks.

## Tasks

- [ ] **SPRINT-3 — Implement isolated fixture-only ownership transaction and recovery.** Route: delegated bounded writer (multiple non-trivial code/docs files; read-only scout mapped 4+). Strict Go TDD with observed focused RED before implementation, GREEN then REFACTOR. Add isolated modeled fixture engine and tests for root/file/empty-dir, interruption/retry, exact-state inverse and fail-closed replacement/partial/unsupported inventory; document fixture limitation. Verify focused and full uncached Go tests, `git diff --check`, edited-file diagnostics. Independent check when required by risk assessment. Close only after observed checks and separately authorized local work-unit commit identity recorded.

## Acceptance and checks

1. A complete bounded fixture inventory drives only explicit per-object modeled transitions; no production observation, planner or caller can authorize ownership transfer.
2. Recovery is repeatable after interruption at every mutation boundary, with durable intent before mutation, exact pre/post reconciliation, and fail-closed ambiguous state; conditional inverse requires a complete matching record and no intervening change.
3. Symlinks, unsupported kinds, substitutions, partial tree, unknown object state and mismatching UID/GID/mode are refused without guessed progress or permission widening.
4. Original mode-only planner/transaction behavior is unchanged; documentation distinguishes simulated fixture proof from real ownership operations.
5. Record RED/GREEN/REFACTOR, targeted/full Go outcomes, diagnostics, whitespace, independent readback/risk outcome and any skipped or unavailable checks. Commit requires explicit user authorization under current repository safety policy.

## Verification and delivery

TDD strict, inherited from the established project ODD Go workflow and Sprint 2 tracker. Exact runner: `go test -count=1 ./internal/deploy/homebrew` then `go test -count=1 ./...`; targeted tests use `go test -count=1 ./internal/deploy/homebrew -run <focused-pattern>`. One coherent code+tests+docs work unit, Conventional Commit only after separate user authorization. Existing branch `feat/homebrew-deployment-hardening` uses stacked-to-main. Forecast approximately 300–500 authored lines (advisory only); if actual scope is larger, preserve correctness and report review workload before commit. Native candidate boundary is the work-unit commit, not this checkbox.

## Progress and evidence

- Read-only scout mapped existing mode-only fixture journal and recovery in `internal/deploy/homebrew/transaction.go` and tests, plus Sprint 2 reject-only proposal and policy; no tests or writes during mapping.
- User selected simulated/injected fixture ownership, explicitly not privileged real chown. No product account UID/GID or supplementary group assumption is made.
- Delegated writer added `ownership_transaction.go`, tests and two doc edits without changing the old mode-only engine. Focused RED failed to build before new symbols existed, then focused GREEN and full uncached Go suite passed. First independent verifier found two real defects: permission widening and premature acceptance of a later object's post-state. Bounded correction added three RED regressions, then GREEN; focused/full uncached Go tests and whitespace checks passed. Independent re-verifier PASSed both corrections and the existing crash-window tests, with simulated-only limitations. Parent confirmed `gofmt -l` empty, stable SHA-256 before/after its own focused passing spot check, and `git diff --check` clean. Parent LSP checked two Go files: one clean, one timeout (unconfirmed). A verifier noted possible out-of-turn automatic formatter activity; read-only incident mapping could not establish historical bytes, but current hashes remained stable across the parent's check. No device/privileged operations occurred.
- Native assessment was `unassessable` due untracked files and selected high-risk fallback independent verification. Native inspect with selected new files is ready but its working-tree candidate also includes **four unrelated dirty tracker notes**; no START, lineage, receipt or native approval was sought against that mixed target. The Sprint 3 work unit has not been committed; the task remains unchecked. Approximate new Go source/test total is 557 lines before this tracker and docs, above the review heuristic; do not reduce correctness to meet a line count.

## Next step

STOP before commit/review: ask user for separate authorization for an exact-path local work-unit commit, keeping unrelated notes and `.codegraph/` excluded. If authorized, stage only Sprint 3 source/test, its two docs and this tracker; validate staged scope and checks, commit, assess the exact committed range and follow native review routing. DEVICE-GATE remains NO-GO and `ApplyEligible=false`.
