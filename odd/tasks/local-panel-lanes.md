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
- S7 Queue full: no confirmation is silently dropped (today the oldest pending is deleted). Pending user
  confirmation (see L5) before implementing.
- S8 Panel: implement artboard 09 (lanes Entradas/Salidas, movement filter, attention zone, batch confirm,
  Nox dark/light themes, status summary, history) on the endpoints above. Mock-only parts (`.dbar`, legend,
  hash shortcuts) are not ported.

## Tasks

- [ ] T1 S1 — antenna health endpoint + provider dedupe — inline — commit:
- [ ] T2 S3,S5 — record online confirms as synced (synced_at) + 30-day purge — inline — commit:
- [ ] T3 S2 — queue detail in /api/status (oldest, threshold, last sync) — inline — commit:
- [ ] T4 S4 — today's history endpoint — inline — commit:
- [ ] T5 S6 — Spanish confirm messages — inline — commit:
- [ ] T6 S7 — queue-full policy (blocked on L5) — inline — commit:
- [ ] T7 S8 — panel 09 frontend — inline — commit:

## Log

- L1 (user, 2026-11-06): "vamos a hacer la prueba end to end con el cambio implementado para no tener sorpresas y volver a hacer la prueba end to end" (implement design 09 before e2e).
- L2 (user): "no me urge la prueba end to end vamos con los endpoints y fixtures que nos faltan en el gateway"; "fixtures" = "me referia al endpoint".
- L3 (evidence): explorer maps — panel gaps: antenna health, queue age/limit/last send, history, queue-full code, picker undesigned, no batch endpoint. Online confirms are never stored locally (handlers.go:172-186); synced rows are kept forever; CreateConfirmation drops the oldest pending at the cap (store.go:426-446); antenna provider appends a manager per reconnect (main.go ~399).
- L4 (user): "dale guardamos todas" — accepting: record every confirmation locally, purge synced > 30 days.
- L5 (open): queue full. Engram #9987 / issue #103 principle "registration never blocks, no event lost"; auto-confirm with requires_review was designed but never implemented (separate feature). Proposed: never drop; warn at threshold; reject with code `queue_full` only at the hard cap. Awaiting user confirmation.
