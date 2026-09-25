# Homebrew Ownership Migration — Sprint 1

## Objective

Specify a local, non-authorizing object-scope and evidence contract for eventual migration of the existing Homebrew data tree to a statically provisioned dedicated non-root service account. The present mode-only planner and fixture transaction preserve the original owner; applying `0700` to data still owned by `pi` would deny a distinct service UID access.

## Scope and constraints

- Refine `docs/homebrew-deployment-account-access.md` only; this tracker records intent and evidence. Keep the completed account-access and deployment-hardening trackers untouched; preserve their pending uncommitted notes and generated `.codegraph/`.
- Explicitly distinguish selected root, contained object classes, identities, ownership/group/modes, ancestors and mounts; refuse unknown, mixed-owner, ACL-bearing, link/substitution or otherwise unsupported trees unless a future separately reviewed policy proves them safe. Do not claim the current code can enumerate, transfer, inverse or recover those objects.
- Specify minimum fresh, identity-bound before/after evidence and recovery refusal for a future design, without instructions to provision accounts, inspect the Raspberry, chown, chmod, change unit, invoke sudo/Brew/systemctl, apply or activate.
- Preserve `ApplyEligible=false` and DEVICE-GATE NO-GO; the historical `pi:pi` UID/GID 1000 and `0775` observation is not a current precondition. No push, PR, merge, or publication.
- The static account name/UID/GID, installed unit, receipt, exact data object inventory, ACLs and access are unknown. Conservative refusal is a design proposal, not evidence of real device state.

## Tasks

- [ ] **SPRINT-1 — Define object scope and proof for a future ownership migration.** Route: delegated bounded documentation writer, based on prior read-only 4+ surface map and exact planner/fixture contract. Extend the existing access document with a short reviewable decision matrix: accepted candidate object class vs rejected/unknown class, required exact snapshot and proof, partial-progress and inverse eligibility. Fail closed on mixed ownership/ACL, hard links, symlinks, nested mounts and untrusted same-UID writers unless a later task defines independent validation; never imply the current engine can perform the migration. Verify source-grounded semantic/security readback, tracked `git diff --check` and direct whitespace on new tracker. Documentation-only; Go TDD not applicable. Local work-unit commit requires a separate explicit user authorization; no checkbox completion until proof and commit.

## Acceptance and checks

1. The contract names the root and contained-object boundary and distinguishes fresh evidence from stale historical data without inventing account IDs or installed paths.
2. Mixed owners/groups, unsupported object types, ACL ambiguity, links, mounts, traversal races and any unproved inverse block a future proposal; no blanket recursive ownership operation or silent permission widening.
3. Future durable evidence binds all selected object identities and recorded owner/group/mode before mutation and conditions a rollback on matching exact post-state, while leaving mechanics for later sprints.
4. It clearly states that sprint 2 may only propose, sprint 3 may only exercise local fixtures, and device observation/privileged mutation/activation require separate authorization.
5. Record each check and any failed, unavailable or skipped result; exclude unrelated modified trackers and `.codegraph/` from any authorized commit.

## Delivery and progress

- Existing feature delivery strategy is `stacked-to-main`; this sprint forecasts about 80–160 authored doc/tracker lines as one reviewable unit. No push/PR implied.
- User explicitly chose local documentation-only Sprint 1. Earlier read-only mapper confirmed the current planner and fixture engine only support owner-preserving `0775 → 0700`. No Go changes, device operations, tests or commits have occurred in this sprint yet.
- Delegated writer extended `docs/homebrew-deployment-account-access.md` with an object-scope refusal matrix, complete-tree per-object evidence, partial-progress conditions and a whole-tree conditional inverse. Source-grounded readback and `git diff --check` passed. Native working-tree assessment was `unassessable` due to untracked files, with unknown outcome; no lineage or receipt is claimed. The required independent verifier **PASS**ed the exact candidate against the planner, fixture engine and trust/transaction docs; `git diff --check` and direct untracked-tracker whitespace checks passed. Parent `git diff --check` spot check passed. No Go tests (docs-only), device checks or privileged operations ran. Candidate awaits separate local commit authorization; existing completed trackers and `.codegraph/` remain excluded.

## Next step

Request separate authorization for a local commit containing only `docs/homebrew-deployment-account-access.md` and this sprint tracker. DEVICE-GATE remains NO-GO and `ApplyEligible=false`; no Sprint 2 or device action follows automatically.
