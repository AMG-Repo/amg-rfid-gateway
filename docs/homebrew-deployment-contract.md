# Homebrew gateway deployment safety contract (draft)

**Decision:** A Homebrew deployment has its own explicit profile and transaction. The existing `/opt` installer, updater, and unit are not inputs to this tool. No command in this contract enables or starts a gateway service; activation requires a separate operator authorization and runtime proof.

## Review path

1. Confirm the profile and trust boundary below match the target installation.
2. Review the state transitions and exact objects before implementing an apply operation.
3. Exercise the failure matrix with local fixtures before considering device validation.

This document specifies intended behavior, **not a guarantee of exact-object recovery** or an assertion that the current Raspberry satisfies it. The first implementation milestone provides only read-only inspection and planning. Apply and rollback remain unavailable until each allowed mutation has an independently reviewed inverse, crash-surviving identity evidence, interruption-point behavior, and verified recovery tests.

## Profile and trust boundary

| Input | Contract |
| --- | --- |
| Installation | Explicit Homebrew prefix, expected package version and executable identities; resolve from verified package metadata, never a guessed `PATH` entry. |
| Config and data | Explicit selected config, backup, state, runtime, and data paths from a reviewed profile. Preserve existing secret values and exact original bytes. No config contents in plan, logs, exceptions, or journal. |
| Service | Explicit systemd scope, unit name, unit path, service account, and expected pre-state. Do not assume a Homebrew service is interchangeable with a system unit. |
| Policy | Explicit listener and sandbox expectations, zero enabled antennas, and allowed writable paths. Offline behavior is not inferred from disabled antennas alone. |
| Privilege | Inspect and plan require no elevation. Any future apply/rollback privilege must be operator-invoked with an exact reviewed plan; no password capture, passwordless sudo rule, hidden remote call, or auto-escalation. |

Use fixed, operator-reviewed roots and reject unexpected symlinks, traversal, ownership, permissions, file type, or mount changes. A check followed by an unanchored pathname operation is not an identity proof: operations must remain anchored to verified directory handles and compare object identity at each transition. When a required identity-preserving primitive is unavailable, stop rather than weaken the check. Private mutation parents and journal directories must exclude untrusted writers. **Same-UID hostile concurrency cannot be ruled out by mode bits or a cooperative lock alone**; if an untrusted actor can modify those parents, the target is outside this contract and apply must refuse. This trust premise requires direct verification before a device operation.

## States and transitions

| State | Allowed action | Required result |
| --- | --- | --- |
| `uninspected` | Inspect | Sanitized typed inventory or a bounded refusal; no write. |
| `inspected` | Plan | Deterministic plan bound to profile, original object identities, package version, service pre-state, and permitted transitions; no write. |
| `planned` | Apply remains unavailable until per-operation proofs/tests and all applicable filesystem and service-manager trust boundaries are resolved, **and** the operator separately authorizes it. | Exclusive lock and durable transaction record precede the first mutation; stale inventory invalidates the plan. |
| `applying` | Continue only a proved transition, or recover | A step may be classified only by exact before/after identities and durable evidence; an unknown intermediate state blocks. |
| `prepared` | Verify preparation or rollback | Config/data/unit preparation is complete; service remains stopped and not newly enabled. Preparation does not imply runtime safety. |
| `rolling_back` | Recover rollback | Restore only objects proven to belong to this transaction; never delete an unknown replacement or pre-existing directory. |
| `restored` / `blocked` | Inspect | `restored` requires verified pre-state; `blocked` needs explicit human diagnosis and cannot silently restart or overwrite. |

The journal must record a versioned transaction and plan identifier, sufficient pre- and post-object identity evidence for each concrete operation, intended step, completion evidence, and sync status—never secret-bearing bytes. Which metadata, digests, and protected handles constitute sufficient crash-surviving proof is an open design question for each operation; a digest alone does not establish ownership or identity. Before each mutation, persist and sync the intended transition; after it, persist and sync completion evidence and the containing directory as applicable. A crash between these writes leaves a **pending** step: recovery must inspect and prove either exact pre-state or exact transaction-created post-state, or enter `blocked`. A PID file or advisory lock is not a substitute for durable recovery evidence. Repeated recovery must be idempotent for every supported intermediate state. Define each operation's inverse and interruption points in its implementation review; no generic "undo everything" routine.

## Mutation and activation boundary

Future apply may address only the reviewed config metadata/content, the selected existing Homebrew data directory, one private runtime directory, bounded rollback state, and one exact service-unit surface when each is supported by the plan. Do not remove a pre-existing directory to reverse a mode change; restore its original metadata only after proving identity and ownership of every generated artifact. Never stage or execute the rejected demo remediation v1/v2/v3. No package upgrade, arbitrary cleanup, VPS/reader access, gateway/TUI execution, `systemctl enable`, or `systemctl start` is part of preparation. A stopped unit, absent listeners and a valid sandbox are independent preconditions to a later activation proposal—not effects that preparation may assume.

Unit installation and rollback also require an explicit proof that the unit file and the service-manager state still match the recorded pre- or transaction-created state after interruption. Concurrent unit replacement or service-state changes invalidate that proof and must block recovery without overwrite or automatic start. The service-manager trust boundary and detection method remain unverified; no unit mutation is authorized until resolved.

A separate activation decision would require independent evidence for exact executable/process identity, listener binding, local socket permissions, systemd network confinement, data writability, zero enabled antennas, and absence of reader/VPS traffic. A local fixture cannot establish those runtime properties on the Raspberry.

## Evidence and failure matrix

| Scenario | Required observation |
| --- | --- |
| Inspect/plan on ordinary, missing, stale, or symlinked paths | No mutation or secret scalar disclosure; deterministic refusal for unsupported state. |
| Interrupt immediately before/after each mutation and each journal sync | Recovery recognizes the exact before/after state or blocks without guessing. |
| Retry apply or rollback; process dies holding a lock | No duplicate mutation; released process lock does not erase unfinished transaction. |
| Replace an object or parent after planning | Identity mismatch blocks; no deletion or ownership transfer of a replacement. |
| Existing data directory with generated files or unexpected entries | Preserve the pre-existing directory; remove only proven transaction-created objects, otherwise block. |
| Permissions, disk-full, fsync, rename, or partial systemd failure | Preserve durable evidence and a stopped/non-activated service; report exact step and safe next action. |
| Error messages and diagnostic output | Emit bounded reason codes and non-sensitive metadata, never config contents or raw command stderr containing secrets. |

## Established facts and open proofs

- Prior read-only closure recorded the Raspberry on v0.6.5 with gateway/TUI stopped, zero enabled antennas, original config unchanged, and no demo unit or sockets; that evidence is historical, not a current preflight.
- The repository's `/opt` scripts use their own paths and service identity. The release workflow publishes archives, whereas those scripts request standalone binary assets; they are not a Homebrew transaction implementation.
- The exact live tap formula, filesystem trust boundary (including concurrent same-UID writers), systemd ownership/state behavior, runtime network behavior, and recoverability of each planned mutation require fresh verification. Until per-operation proofs and the service-manager trust boundary are specified and tested, this contract authorizes neither apply, rollback, nor activation.

## Next review gate

Implement inspect and plan against synthetic local fixtures. Review its refusal and redaction behavior before specifying the first mutation primitive. Any target-device operation needs a new explicit scope and operator decision.
