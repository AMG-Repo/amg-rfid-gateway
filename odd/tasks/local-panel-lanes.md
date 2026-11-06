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
- S11 Requeue stuck confirmations: `POST /api/confirmations/requeue` (write auth like /api/confirm) resets the
  retry count of every pending confirmation that reached the max retries so the sync worker sends them again;
  answers `{requeued: N}`. The panel's "N confirmaciones sin enviar" attention item gets a "Reintentar" button
  that calls it and refreshes status.
- S12 Retry backoff: a failed send waits longer before its next attempt (per confirmation, growing from the
  retry interval, doubling, capped at 30 min) instead of 10 attempts 30 s apart, so a VPS outage of minutes
  does not leave confirmations stuck. Stuck still means max retries reached.
- S13 The gateway learns a movement first: as soon as a confirmation is accepted (sent online or queued
  offline) the cached tag location changes — entrada to "Almacén General", salida to the kanban's first zone
  (empty when none) — so the next read of the same tag suggests the opposite movement, also offline. A sync
  must not roll it back while that confirmation is still pending.
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
- [x] T9 S10 — tool-tag upsert by id (renormalized UIIs) — inline — commit: efdd26c
- [x] T10 S11 — requeue endpoint + panel Reintentar — worker — commit: 4a8561b
- [x] T11 S12 — per-confirmation retry backoff — worker — commit: 457002a
- [x] T12 S13 — local location follows confirmed movements — inline — commit: 96e6d4e
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
- L15 (evidence): T9 independent verify: core S10 cases PASS but sign-off FAIL — duplicate UIIs in one batch silently dropped a tag; renormalized UIIs left orphan kanban links pinning stale catalog codes (EX3W0AR-A on the Pi). Fixed test-first: UpsertToolTags rejects duplicate UIIs with ErrDuplicateTagUII writing nothing; ApplyKanbanLinks drops links whose UII is missing from the VPS snapshot. Residual (not on the sync path): legacy UpsertToolsFromSync still upserts ON CONFLICT(uii).
- L16 (evidence): re-verify of 3a8a460 FAIL — snapshot-based orphan cleanup deleted legitimate links (empty-SKU items filtered out; a just-accepted assignment wiped by an older in-flight snapshot) and skipped empty snapshots. Approach changed: ApplyKanbanLinks never deletes links of UIIs it does not carry; UpsertToolTags drops only the link of a tag's OLD UII when the VPS renormalizes it (same id, new UII). RED/GREEN on TestUpsertToolTags_RenormalizedUIIDropsOldLinkAndStaleCatalogCode; just-assigned and catalog-keep tests green.
- L17 (user): "dale implementa" — implement requeue of stuck confirmations and retry backoff. Context: cloud 500 was movements.id empty PK (cloud fix deploying); 5 confirmations stuck on the Pi after 10 retries in ~5 min. Do not requeue on the Pi until the cloud confirms its deploy and the "Almacén General" side effect.
- L18 (evidence): T10/T11 by worker (no commits, rules forbid). T10 RED: `TestHandleRequeue_*` answered 404 (route missing); GREEN after `LocalStore.RequeueStuckConfirmations` + `POST /api/confirmations/requeue` behind `requireWriteAuth` (200 `{requeued:N}`, only unsynced rows with retry_count >= MaxRetries reset, non-stuck/synced untouched, 405 other methods, 401 in LAN mode without token). Panel: "Reintentar" button (`btn-warn`, `data-confirm`, no innerHTML) on the stuck item, token flow via `ensureAuth`/`onUnauthorized`, then `loadStatus()`. T11 RED: worker resent a just-failed row on the next tick (2 sends, want 1) and the `next_attempt_at` column/`ScheduleConfirmationRetry` did not exist. GREEN: migration adds `next_attempt_at` (NULL = due), `ScheduleConfirmationRetry` sets now + interval*2^(retry_count-1) capped at 30 min after `IncrementRetryCount` (optional `ConfirmationRetryScheduler` interface keeps mocks untouched), `GetRetryableConfirmations` filters not-due rows in SQL (150 waiting rows do not fill the batch), requeue clears the backoff; stuck still means retry_count >= MaxRetries. Known limit: the worker ticks every retryInterval, so a due row may wait one extra tick. Live VPS/hardware unverified; not requeued on the Pi (L17).
- L19 (evidence): verify of 668a0b8 FAIL only on swap + empty-SKU (link of the skipped UII lost). Fix: applyKanbanLinks applies links from the whole VPS snapshot (items skipped as tool tags included). Verifier matrix imported as internal/sync/toolssync_tag_identity_test.go: RED on the empty-SKU swap, GREEN after; renormalization, moves and 18 swaps green. T10/T11 committed 4a8561b/457002a.
- L20 (evidence): final verify of b1fff46: S10 PASS; S11/S12 FAIL on one race — ScheduleConfirmationRetry read retry_count then wrote next_attempt_at unconditionally, overwriting a concurrent requeue (row left 30 min out). Fix: schedule only if retry_count is unchanged and non-zero. Verifier race tests imported (store and worker level): RED without the fix, GREEN with it, stable over -count=20. Residual outside the sync path: legacy UpsertToolsFromSync still upserts ON CONFLICT(uii).
- L21 (evidence): independent verify of 1a8a6d2: S11 PASS, S12 PASS. All interleavings of increment/read/write with requeue keep a requeued row due; normal backoff never lost. Race tests fail on the parent, pass on 1a8a6d2, stable over -count=20. Boundary (pre-existing, not introduced): increment and scheduling are separate statements.
- L22 (user, e2e 2026-11-06): read EX150BLA → salida (correct), confirmed (synced 23:20:43, VPS location now "E"), read again → salida again. Cause: the cached tool_tags.location is only refreshed by the next sync; SuggestAction reads it. User: "de hecho el primero en enterarse debe ser el gateway, puede pasar que una tag entre y salga en offline si no se entera el gateway y lo registra tenemos un problema".
- L23 (evidence): T12 RED (no-op ApplyMovementLocation fails the store, sync and web tests) / GREEN; go test ./... green except pre-existing homebrew.
- L24 (evidence): independent verify of 96e6d4e: S13 FAIL — ReapplyPendingMovements took the latest PENDING row, so an older pending entrada rolled back a newer sent salida (online or partial flush); accepted uppercase SALIDA online was neither recorded (CHECK) nor applied. Fix: reapply only when the tag's latest movement overall is still pending; handleConfirm normalizes the action after validation. RED/GREEN on new tests; verifier's 5 reproductions imported (internal/sync/toolssync_movement_order_test.go, internal/web/confirm_movement_order_test.go) all green.
- L25 (evidence): independent verify of e798e76: S13 FAIL on retention — the 30-day purge could delete a tag's newer sent movement while an older stuck one stayed pending, so the next sync rolled the tag back. Fix: purge keeps a synced row while an older pending row of the same UII exists. RED (own test + verifier's store and worker reproductions, imported as internal/localstore/movement_retention_test.go and internal/sync/toolssync_movement_retention_test.go) / GREEN; one verifier expectation (purged=1) encoded the old purge and was set to 0. Fix not independently re-verified.
- L26 (user, e2e): after confirming, the panel needed a page reload to show the next movement of the same card (the done card only rearms after 30 s without reads — REARM_GAP_MS). User: stop after T12; this bug and the rest of the e2e go to the next session.
- L27 (evidence): independent verify of 811d973: S5 PASS, S13 PASS (64-case matrix; protected rows purged once the older pending one syncs; changed test expectation judged legitimate). Caveat: while a pending row stays stuck forever, later synced rows of that same tag are kept (bounded by that tag's movements).
