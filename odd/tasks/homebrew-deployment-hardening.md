# Homebrew Deployment Hardening

## Objective

Build a same-repository, Homebrew-only deployment component that can inspect and plan a bounded gateway installation change, recover from interrupted local operations, and keep activation separate and operator-controlled. The Raspberry Pi remains on verified v0.6.5, stopped, until a separately authorized device operation.

## Problem and why

The existing `/opt` install/update scripts and unit do not describe the Homebrew installation. Three one-off demo remediation candidates failed independent security review and must not be executed. A maintained, testable transaction contract is needed before attempting deployment hardening.

## Scope and constraints

- Homebrew profile only; leave `/opt` entry points, unit, and behavior unchanged. Do not infer paths or service manager from ambient state.
- Design explicit state, path/identity, privilege, secret-redaction, locking, crash-recovery, and rollback contracts before mutating code.
- Preserve the Raspberry v0.6.5 stopped state. No SSH, sudo, brew mutation, gateway/TUI execution, service activation, or staging of rejected remediation v1/v2/v3 in this feature's local implementation tasks.
- No release, remote deployment, push, PR, or merge is authorized by this plan.
- Isolated branch/worktree: `feat/homebrew-deployment-hardening` at `/home/jesus/Work/amg-rfid-gateway-homebrew-deployment-hardening`, branched from `origin/main` at `5df0cbe456afd5856d6aece28a017784d6082913`. Preserve the original dirty worktree.
- Treat formula-level assertions not independently verified against the tap as assumptions. Reject inconsistent inventory rather than guessing.

## Tasks

- [ ] **CONTRACT-1 — Specify the Homebrew deployment safety contract.** Route: delegated exploration already mapped 4+ surfaces; bounded documentation task may be inline if one file. Record exact allowed states, targets, preconditions, mutation and rollback identities, recovery invariants, privilege separation, secret-safe evidence, and explicit non-activation. Define test scenarios and distinguish known source facts from Raspberry-specific assumptions. Check: review the contract against prior rejected findings and existing Homebrew packaging evidence; `git diff --check`. Commit a reviewable documentation work unit.
- [ ] **PLAN-1 — Implement hermetic inspect and plan.** Route: delegated worker (multi-file code and tests). Implement explicit Homebrew profile input, sanitized read-only inventory, deterministic change plan and precondition failures; no remote access, elevation, service calls, or mutation. Strict TDD RED/GREEN/REFACTOR and focused/full applicable Go tests. Commit code, tests, and usage docs as one work unit.
- [ ] **TRANSACTION-1 — Implement bounded local transaction and recovery.** Route: delegated worker (multi-file code/tests). Require durable journal, exclusive lock, exact object identity, crash/fault injection coverage, idempotent recovery, and fail-closed ambiguity. Never apply to the Raspberry during this task. Independent security review and applicable tests before committing.
- [ ] **SERVICE-1 — Integrate Homebrew service boundaries without activation.** Route: delegated worker (multi-file code/tests). Make unit/service transitions explicit, keep file preparation distinct from start/enable, and prove isolation assumptions with fixtures or report them unresolved. Local-only verification and independent review; no device mutation. Commit one coherent work unit.
- [ ] **DEVICE-GATE — Prepare a read-only device validation and operator gate.** Route: delegated read-only verification. Compare the finished local tool contract with the actual stopped installation without secrets; no remote mutation. Any application, privileged invocation, or activation is a separate explicit authorization and is not a checkbox auto-continuation.

## Acceptance criteria and checks

1. Homebrew and `/opt` contracts remain separate; existing `/opt` entry points are not changed.
2. Inspect and plan disclose no config scalars or secrets, make no modifications, and reject unknown paths, symlinks, or changed identities.
3. Every mutation has a specified inverse, durable progress evidence, bounded privilege, and explicit fail-closed behavior for interrupted or ambiguous recovery.
4. Preparing files neither enables nor starts a service; any activation requires a later operator decision and evidence of local-only listeners/network confinement.
5. Fault-injection tests cover interruptions before and after each step, retries, identity replacement, wrong permissions, partial service errors, and sanitized diagnostics.
6. Required checks are recorded with observed outcomes, including failures, skips, and unavailable verification.

## Verification and delivery

- Resolved TDD: strict for Go changes, sourced from this project's previously recorded Go-change workflow; focused RED before production edits, GREEN, REFACTOR, then uncached `go test -count=1 ./...` when applicable. Documentation-only task uses diff/readback instead.
- Delivery strategy: `ask-on-risk`; initial forecast exceeds approximately 400 authored changed lines across multiple work units. User chose `stacked-to-main`: keep independent reviewable slices targeted to updated main; no push/PR is implicit.
- One Conventional Commit per completed work unit; record each commit identity and assessed review outcome here. Native review candidates are commits or PR slices, not task checkboxes.

## Progress and evidence

- Scope confirmed by user: same repository, Homebrew-only first stage; `/opt` out of scope.
- Read-only mapper identified two distinct packaging/service contracts and mismatch between legacy standalone-binary script URLs and release archives. This does not independently verify tap history.
- Initial isolated worktree created from the v0.6.5 merge commit; no Raspberry or source changes.
- User selected independent main-targeted review slices (`stacked-to-main`); this does not authorize publishing PRs.
- Draft contract: `docs/homebrew-deployment-contract.md`; initial readback and no-index whitespace check passed. Independent assumption challenge BLOCKED an overclaim of exact-object recovery and unverified systemd trust boundaries; corrected the text to label them unproved design requirements and to prohibit apply/rollback pending per-operation proof. Prior rejected-remediation tracker exists only in the original worktree and was supplied to the follow-up reviewer. CONTRACT-1 remains in progress; follow-up review, final checks, and commit pending.

## Next step

Verify and commit the first Homebrew-only safety contract; then start a separate inspect/plan implementation slice. Remote deployment and publication remain separate decisions.
