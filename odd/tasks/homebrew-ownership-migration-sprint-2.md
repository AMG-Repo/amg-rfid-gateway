# Homebrew Ownership Migration — Sprint 2

## Objective and problem

Introduce a separate typed, non-authorizing proposal/refusal boundary for a future whole-tree owner/group/mode migration. Today's `ContentObservation` only inventories package and config; it has neither contained data-object inventory nor authenticated target account identity. A positive production migration proposal would fabricate evidence. Sprint 1's object-scope contract is in `docs/homebrew-deployment-account-access.md`.

## Scope and constraints

- Local Go implementation, tests and small documentation update only. Separate from existing `PlanChange`, `ContentObservation` acquisition and fixture-only `dataTransaction`; do not change their mode-only behavior, callers or contract.
- The production-facing new proposal must **refuse** current evidence, with zero ownership transitions and `ApplyEligible=false`. It must not accept caller-invented `verified` flags or manually assembled inventory as provenance. Synthetic type-shape tests, if any, must remain test-only and cannot expose an apply-eligible constructor.
- Represent only explicit per-object before/intended owner UID, group GID and mode plus identity for future typed model; no claim that current observations can populate or authenticate it. Distinguish missing whole-tree inventory/target account identity from any established fact, and refuse ambiguous/unsupported objects.
- No provisioning, chown/chmod, service-manager operations, privileged invocation, SSH/Raspberry, Brew/systemctl, apply/activation, push/PR/merge. Keep DEVICE-GATE NO-GO, `ApplyEligible=false` and no production apply caller. Preserve three prior uncommitted tracker notes and generated `.codegraph/`.
- User accepted a reject-only production proposal for this sprint; numeric UID/GID and supplementary-group policy remain future decisions.

## Tasks

- [ ] **SPRINT-2 — Add typed reject-only account-change proposal.** Route: delegated bounded worker, because code, tests, docs and tracker are multiple non-trivial files; previous read-only scout mapped 4+ source surfaces. Add a distinct package-private typed model and refusal path that cannot issue a positive whole-tree transition from existing ContentObservation. Strict Go TDD: observe focused RED before implementation, GREEN/REFACTOR, test stale/empty/unsupported evidence and zero transitions; execute focused and full uncached Go tests. Independent security/semantic readback must confirm no caller-created inventory is promoted to proof, no preexisting mode-only behavior changes, and docs explain the refusal boundary. `git diff --check`, LSP where available. Do not check off until outcome and checks observed and a separate explicitly authorized local work-unit commit exists.

## Acceptance and checks

1. Current production observation, even sealed and content-equal, cannot yield an ownership/group/mode transition; no apply eligibility, service transition or activation.
2. Types do not conflate original owner-preserving mode plan with future whole-tree inventory; before and intended owner UID/GID/mode remain explicit and identities/refusal are typed, without inventing authenticated values.
3. Malformed, partial, stale, unsupported or caller-asserted inventory/target identities refuse rather than guess; tests distinguish synthetic model-shape validation from production evidence.
4. Existing planner/transaction public behavior and tests remain unchanged; docs state missing provenance and future gates accurately.
5. Record focused RED/GREEN/REFACTOR, focused/full Go results, independent verifier, whitespace/LSP, native assessment outcome and any skipped/unavailable check. A local commit needs separate user authorization.

## Verification and delivery

TDD strict for Go changes, from the existing project ODD Go workflow; exact runner `go test -count=1 ./internal/deploy/homebrew` then `go test -count=1 ./...`, with focused RED before production edits. One coherent work-unit commit with tests and docs if separately authorized. Existing branch strategy: `stacked-to-main`; estimated 150–300 authored lines, advisory only. No push or PR authority.

## Progress and evidence

- User accepted reject-only production scope. Read-only mapper confirmed `ContentObservation.Inventory` covers package/config only and `ObjectState` has no group GID or contained-tree inventory; existing `PlanChange` and fixture-only `dataTransaction` preserve owner.
- Delegated writer added the separate package-private typed refusal path, tests and doc. Focused RED failed to compile before the new symbols existed; focused GREEN, uncached `go test -count=1 ./internal/deploy/homebrew`, full `go test -count=1 ./...`, `git diff --check`, direct new-file whitespace and edited-file diagnostics passed per writer. First independent verifier **PASS**ed behavior and repeated focused/full uncached suites; it flagged an unused synthetic shape value as weak test evidence. A bounded test-only correction replaced that test with a forged sealed-observation regression exercising `proposeAccountChange`, but the correcting subagent timed out after a bash call, so its command outcomes were not accepted. A read-only incident scout confirmed the new test source and no verification claim; parent found no active Go test process. A fresh independent verifier then observed targeted forged-seal test, focused and full uncached Go suites PASS, `git diff --check` and direct whitespace PASS, matching before/after git status, and confirmed the test calls the production refusal path. No new RED was claimed for the test-only correction. Parent `git diff --check` spot check passed. LSP primary confirmed one edited Go file clean, while the other timed out (unconfirmed); no blocking finding reported. Native working-tree assessment was `unassessable` due to untracked inventory and unknown outcome; no lineage or receipt is claimed. Existing mode-only planner/fixture were unchanged; no device, account, service or privileged operations occurred.

## Next step

Candidate verified independently. Request separate authorization for an exact local commit of the new Go source, test, proposal doc and this tracker only; exclude prior tracker notes and `.codegraph/`. DEVICE-GATE NO-GO and `ApplyEligible=false` persist; Sprint 3 is not automatic.
