# Local panel lanes (design 09) — gateway endpoints first

Branch `feat/local-panel-lanes` (stacked on `feat/kanban-unassigned-tags`). Implement artboard
`design/local-panel/09-lanes-nox-themes.html`; first the gateway endpoints the panel lacks, then the panel.

## Specs

- S1 Per-antenna health for the panel: each configured antenna with id, zone (`entrada`/`salida`/empty),
  state (`reading`/`reconnecting`/`stopped`) and last read time (null before the first read). One entry per
  antenna id even after reconnects (today the provider appends a stale manager on every reconnect).
- S2 Sync queue detail in `/api/status`: oldest pending confirmation time, the configured warning
  threshold, and the time of the last successful send to the VPS.
- S3 "guardamos todas": every confirmation is recorded locally. An online success is stored as synced with
  the send time; an offline one stays pending as today.
- S4 Today's history: confirmed movements of the current day with kanban code, description, quantity
  (`standard_pack`) and unit, action, antenna, time and synced/pending.
- S5 Retention: synced confirmations older than 30 days are deleted; pending ones are never deleted.
- S6 Spanish user-facing messages on confirm (today "Confirmation sent to VPS",
  "Confirmation queued locally (offline mode)", "No local store available").
- S7 Queue full: no confirmation is silently dropped (today the oldest pending is deleted). The panel warns
  from the warning threshold; at the hard cap (`max_pending_confirmations`) a new confirmation is rejected
  with code `queue_full` and nothing stored is deleted. Confirmations stuck after the max retries are counted
  so the panel can show them.
- S9 Stuck confirmations never block the queue: the sync worker sends the oldest pending confirmations that
  are still retryable, so 100+ stuck rows (max retries reached) cannot starve newer ones (L9).
- S10 Tool-tag sync follows the VPS by tag id: a renormalized UII (same id) or a UII moved to another id
  updates the cache instead of aborting the sync (which also blocked the kanban catalog refresh).
- S8 Panel: implement artboard 09 (lanes Entradas/Salidas, movement filter, attention zone, batch confirm,
  Nox dark/light themes, status summary, history) on the endpoints above. Mock-only parts (`.dbar`, legend,
  hash shortcuts) are not ported.

## Tasks

- [x] T1 S1 — antenna health endpoint + provider dedupe — inline — commit: a05a2b1
- [x] T2 S3,S5 — record online confirms as synced (synced_at) + 30-day purge — inline — commit: e08256c
- [x] T3 S2 — queue detail in /api/status (oldest, threshold, last sync) — inline — commit: e08256c dfc2056
- [x] T4 S4 — today's history endpoint — inline — commit: dfc2056
- [x] T5 S6 — Spanish confirm messages — inline — commit: dfc2056
- [x] T6 S7 — queue-full policy: reject queue_full, stuck count — inline — commit: da5ede5
- [x] T8 S9 — sync worker skips stuck rows in the query (no starvation) — inline — commit: 721e7fa
- [ ] T9 S10 — tool-tag upsert by id (renormalized UIIs) — inline — commit:
- [x] T7 S8 — panel 09 frontend — worker (no commit, rules forbid) — commit: 90d166d

## Log

- L1 (user, 2026-11-06): "vamos a hacer la prueba end to end con el cambio implementado para no tener sorpresas y volver a hacer la prueba end to end" (implement design 09 before e2e).
- L2 (user): "no me urge la prueba end to end vamos con los endpoints y fixtures que nos faltan en el gateway"; "fixtures" = "me referia al endpoint".
- L3 (evidence): explorer maps — panel gaps: antenna health, queue age/limit/last send, history, queue-full code, picker undesigned, no batch endpoint. Online confirms are never stored locally (handlers.go:172-186); synced rows are kept forever; CreateConfirmation drops the oldest pending at the cap (store.go:426-446); antenna provider appends a manager per reconnect (main.go ~399).
- L4 (user): "dale guardamos todas" — accepting: record every confirmation locally, purge synced > 30 days.
- L5 (open): queue full. Engram #9987 / issue #103 principle "registration never blocks, no event lost"; auto-confirm with requires_review was designed but never implemented (separate feature). Proposed: never drop; warn at threshold; reject with code `queue_full` only at the hard cap. Awaiting user confirmation.
- L6 (evidence): T2–T5 by worker (no commits, rules forbid). Independent verify: S3, S4, S5 PASS (migration on HEAD schema preserved rows; purge never touches pending; local-day history in 3 timezones). S2 FAIL (last_sync_at lost after purge) and S6 FAIL (English validation errors, raw DB error). Fixed inline test-first: purge keeps the latest synced row (COALESCE guard for legacy NULL synced_at), Spanish 400 messages, queue failure without DB text. go test ./... green except pre-existing homebrew. Commits e08256c (store), dfc2056 (web). Slice note: branch is stacked on feat/kanban-unassigned-tags; split into chained PRs at delivery.
- L7 (evidence): second independent verify on e08256c/dfc2056: S2–S6 PASS (purge matrix: latest kept, legacy NULL purged, pending preserved, empty table; a05a2b1 schema reopen preserves rows; Spanish 400s write nothing; no DB text leak). Known limit: online local-record failure only logs (VPS already accepted). Live VPS/hardware unverified.
- L8 (user, 2026-11-06): "dale vamos con eso" — accepts L5: never lose, warn from threshold, reject with `queue_full` at the hard cap; also surface confirmations stuck after max retries (example given: VPS-rejected UIIs accumulate forever).
- L9 (evidence): independent verify of da5ede5: S7 PASS. Differential vs parent: parent answered 200 and deleted the oldest queued row; da5ede5 answers 507 queue_full and keeps it. Pre-existing defect found (not introduced): the sync worker reads only the oldest 100 pending rows and skips stuck ones (toolssync.go:388,403; store.go:482-489), so 100+ stuck rows at the front starve newer retryable confirmations.
- L10 (user): "dale arreglalos" — fix the worker starvation defect from L9 before T7.
- L11 (evidence): T8 RED reproduced: 150 stuck confirmations ahead, flush sent nothing; GREEN after GetRetryableConfirmations (retry_count < MaxRetries in SQL). go test ./... green except pre-existing homebrew. Commit 721e7fa.
- L12 (user): "dale arranca" — start T7. Decisions for gaps the artboard leaves open: "Asignar" opens a simple picker (search the cached catalog GET /api/kanbans, assign via POST /api/tags/assign, Spanish error codes); batch confirm = sequential POST /api/confirm per item with failures (incl. 507 queue_full) moved to "Requieren tu atención"; drop the htmx CDN; token via the design's inline form (sessionStorage key amgWebAuthToken kept).
- L13 (evidence): T7 panel ported from artboard 09 into internal/web/static (index.html, style.css, app.js); htmx CDN removed; DOM built with text nodes only. Decisions beyond the artboard: a tag just assigned joins its lane as a pending card (movement from its last suggestion, default entrada); a confirmed tag read again after a 30 s gap becomes a new pending movement (a tag still at the antenna stays done); a batch stops on 401 or `queue_full` and leaves the rest pending; the group row of several unassigned tags opens its drawer ("Ver tags") instead of assigning in bulk; picker needs select-then-"Asignar a <código>" to avoid gloved mis-taps. Not ported: `.dbar`, legend, `dnote`, hash shortcuts, hardcoded data, `#f-sel`. Verified: node --check, TestPanelServesLanesDesign, Playwright Chromium against a stub gateway (32 behavior checks, dark/light at 1440 and 390, no console errors beyond the provoked 401/507), go vet, go test ./internal/web/... Live VPS/hardware unverified.
- L14 (user, e2e on Pi): picker kanbans out of sync with the VPS (106 local, old suffixed codes vs 87 on VPS); assign answered "El kanban no existe en el VPS." Root cause: performSync aborted at UpsertToolTags with "UNIQUE constraint failed: tool_tags.id" after the cloud renormalized UIIs (same id, new UII), so ReplaceKanbanCatalog never ran. RED reproduced the exact error.
