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

- [x] **CONTRACT-1 — Specify the Homebrew deployment safety contract.** Route: delegated exploration already mapped 4+ surfaces; bounded documentation task may be inline if one file. Record exact allowed states, targets, preconditions, mutation and rollback identities, recovery invariants, privilege separation, secret-safe evidence, and explicit non-activation. Define test scenarios and distinguish known source facts from Raspberry-specific assumptions. Check: review the contract against prior rejected findings and existing Homebrew packaging evidence; `git diff --check`. Commit a reviewable documentation work unit.
- [ ] **INSPECT-1 — Implement hermetic, non-authorizing metadata inspection.** Route: delegated worker (multi-file code/tests/doc). Explicit local paths and expected metadata only; reject unsafe ancestors including sticky world-writable parents, malformed identity, symlinks, unsupported platform, and secret disclosure. Output is a snapshot, not verified Homebrew provenance or a mutation plan. Strict TDD RED/GREEN/REFACTOR plus CLI/adversarial fixture tests and focused/full Go checks. Commit code/tests/docs as one work unit.
- [ ] **PLAN-2 — Bind independently verified Homebrew provenance and deterministic plan.** Route: delegated read-only mapping first for actual release/tap metadata, then bounded worker. Establish a verifiable source of expected installed package/version identity without executing brew or trusting caller-supplied inode alone; define typed inventory/pre-state and a deterministic, non-authorizing change plan. Refuse when provenance or trust cannot be established; no mutation, remote access, elevation, or service calls. Strict TDD and independent security verification before commit.
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
- Contract `docs/homebrew-deployment-contract.md`: first independent challenge BLOCKED an overclaim, follow-up identified one contradictory `planned` row; both were corrected to make per-operation proofs/tests and filesystem/systemd trust gates prerequisites, not current guarantees. The original-worktree prior activation tracker was read in the follow-up; no remaining blocking overclaim was found beyond the corrected row.
- CONTRACT-1 documentation work-unit commit `0c46ae5bc21b98ec1c71cac2247927e25bca1390` contains only the contract and initial tracker (120 added lines); staged and committed-range `git diff --check` passed. Untracked `.codegraph/` is a generated index cache and was not staged. Native committed-range assessment returned `unassessable` because untracked files require an explicit declaration; no native outcome was invented. Independent read-only verifier PASS inspected both exact committed files and confirmed no recovery guarantee; documentation task complete. Later tracker progress is uncommitted and must be included in a subsequent work unit, not attributed to that commit.

- Original PLAN-1 writer returned `partial`; independent verifier BLOCKED it despite passing uncached focused/full Go suites and `git diff --check`: caller-supplied inode is not package provenance, `inspection_only` is not a plan, root-owned sticky world-writable ancestors were allowed, second stat type assertion could panic, and CLI/adversarial tests were absent. No code commit or device activity.
- Replanned within the confirmed Homebrew-only scope: narrow the current work unit to INSPECT-1 (honest read-only metadata snapshot), then add PLAN-2 for independent provenance and deterministic planning. Neither task authorizes mutation or activation; no acceptance criteria were removed.

## Next step

Correct and independently verify INSPECT-1 before its work-unit commit. Map package provenance separately for PLAN-2 rather than claiming a caller-supplied inode is authenticated. Remote deployment and publication remain separate decisions.
