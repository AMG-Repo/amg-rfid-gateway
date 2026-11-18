# Kanban integration onto device auth

Issue: AMG-Repo/amg-rfid-gateway#125. Branch: `feat/kanban-integration` (from `origin/main` after #122-#124).
Source branch: `feat/local-panel-lanes` (41 local commits; its own specs live in `odd/tasks/kanban-unassigned-tags.md` and `odd/tasks/local-panel-lanes.md`).

## Specs

- S1 (L1): "No, necesito kanban" — the release v0.8.0 that goes to the Pi must include the kanban work from `feat/local-panel-lanes`, not only the device-auth readings.
- S2 (L2): "Sí, arrancá" — integrate the branch onto the new main (issue, rebase/merge, FetchKanbans and AssignTag through the new auth helper, chained PRs), then release v0.8.0 with everything together. Show the PR split before opening PRs.
- S3: Kanban VPS calls (`FetchKanbans`, `AssignTag`) use the device token through `doAuthed` (token provider, one retry on 401); no `jwtToken`/`jwt_secret` remains.

## Tasks

- T1 S1-S3 — merge `feat/local-panel-lanes` into the integration branch, resolve `internal/web/server_test.go`, adapt kanban HTTP calls and test call sites to the TokenProvider API, full suite green — worker — commit: 534565b (merge), 6a32724, 469d117
- T2 S2 — propose chained PR split to the user — inline — commit: n/a
- T3 S2 — open chained PRs, CI, merge (user decision) — inline — commit: n/a
- T4 S2 — release v0.8.0 (authorized) — inline — commit: n/a

## Log

- L1: "No, necesito kanban" (answer to: main lacks the kanban work; is it OK for the Pi to lose the kanban panel?).
- L2: "Sí, arrancá" (answer to: start kanban integration — issue, rebase onto main, FetchKanbans/AssignTag with the new helper, chained PRs — then release v0.8.0 with everything together).
- L3: Branch map: ~11k changed lines (web static ~3750, web tests ~2145, localstore ~2109, sync ~1212, web go ~742, rest ~1300); one conflict in internal/web/server_test.go.
