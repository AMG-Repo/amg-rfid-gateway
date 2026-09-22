# Local pinned-release content check (OBSERVE-2)

`homebrew-deploy content-check -package ABSOLUTE -config ABSOLUTE -package-device N -package-inode N -version v0.6.5 -arch linux-amd64` (or `linux-arm64`) compares local gateway bytes against a selected embedded release-member digest. Exit 0 means `content_equal`, **not deployment readiness**; exit 2 is a fixed-code refusal. The old `plan` command refuses with `unsupported_action`. Avoid secret-bearing path names in command arguments and shell history.

The evidence selected by version and architecture is the official v0.6.5 GitHub release archive checked against published `checksums.txt`, then the exact `gateway` member independently hashed. The tag resolves to `5df0cbe456afd5856d6aece28a017784d6082913`.

| Architecture | Archive SHA256 | gateway member SHA256 |
| --- | --- | --- |
| linux-amd64 | bac8c7f75fb3f5b2dfc7a235fa0fc86165f83ae3cc6e6a7efe3fa9bc5b4e959b | 025db883cc0bd424c3c523c6425ea96adf64456b9b3e136bf31c3c280528fac5 |
| linux-arm64 | 8874cd97582b582b28fc212014d9c24f043b540a01e2e5a42f87b68c83c8578f | 60a6acd86e65d5ccfbbf42bc27da72196ad65a1e941a8648def65a63c203d8a5 |

The Linux reader walks no-follow handles, checks directory and final-file owner, mode, type, and snapshot identity, limits reads to 128 MiB, and checks opened-file identity and size again afterward. Output contains fixed reason codes, selected release digests, and metadata but no paths, config contents, or raw errors. Snapshot checks do not prove future pathname continuity. Content equality does **not** attest Homebrew receipt, tap history, installation provenance, config semantics, data state, runtime/network safety, or service state. All results have `ApplyEligible: false`; there is no change plan, mutation, apply, rollback, remote access, or activation.

Hermetic synthetic evidence tests exercise positive observations without overriding production digests; a real local source-file read exercises the production reader. They do not attest an installed release binary on a device.
