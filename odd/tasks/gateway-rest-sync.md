# Gateway REST sync

Raw readings never reached prod: the gateway sends a bare SyncRequest over WS, the backend has no WS handler for it (rfid_readings = 0 rows ever).
User chose option A: authenticated REST. Branch `feat/gateway-rest-sync` from origin/main (9b438c8). VPS side owned by the AMG-RFID-Tools-Demo session.

## Specs

- S1 "A) REST autenticado (recomendado)": "El VPS expone POST /api/v1/gateway/sync bajo GatewayAuth, reusando el SyncHandler legacy; el gateway postea ahí con doAuthed y el WS queda solo para eventos del servidor."
- S2 Contract sent to VPS: body = bare SyncRequest {gateway_id, company_id, readings[], timestamp}; response 200 = {success, accepted, duplicates, errors[], next_sync_in, server_time}; 401 → renew and retry once (doAuthed); non-2xx → sync failure, readings stay pending.
- S3 Resends must not loop: readings the server stored (`accepted`) or already had (`duplicates`) are marked synced. Per-reading rejections come only in errors[] as "reading[<index>]: <reason>" (0-based request index); mark by index, never "first N"; rejected readings are terminal (never accepted on resend), so they must not be resent forever. Whole-request errors: 400 invalid JSON / 0 readings / >500; 403 ids != claims; 413 > 1 MiB; 401/429; 500 nothing stored, safe to resend.
- S4 Release v0.8.2 and Pi rollout only after the VPS endpoint is deployed; verify rows land in prod rfid_readings.

## Tasks

- T1 [S3] sync engine depends on a narrow sender interface; mark accepted+duplicates — inline, TDD
- T2 [S1,S2] httpclient `VPSClient.SendSyncRequest` POST /api/v1/gateway/sync via doAuthed — inline, TDD
- T3 [S1] main wires engine to the VPS client, stops dialing WS; docs — inline
- T4 [S4] issue + PR, release v0.8.2, Pi upgrade (operator sudo), verify prod readings — after VPS deploy

## Log

- L1 (2026-11-19) User: "Retoma el relevo de la session pasada". Relay step 1 found WS stable after VPS 256 KB fix (deployed 23:30:22 UTC, PR #89 merged), but backend logs "Unknown message type" type="" every ~30 s and drops every frame.
- L2 User chose option A (authenticated REST) over B (WS handler), C (envelope + new type), D (stop uploading raw readings).
- L4 VPS (msg 9046061f): reusing ProcessSync fails (rfid_readings FK to empty rfid_readers, no unique constraint, timestamp/rssi not stored). VPS proposes new table gateway_readings with idempotent insert; asked to hold the gateway merge. Wire Reading has no `id` (shared models.Reading); local pending_readings.id is a per-cache.db autoincrement, not a stable global key. Gateway proposes natural key (company_id, gateway_id, antenna_id, epc, read_at). Q3 (purge or keep 111556 cached readings) is the user's.
- L5 VPS agreed (msg 15d34993): UNIQUE(company_id, gateway_id, antenna_id, epc, read_at), read_at µs precision, index-based errors[], status codes as in S3. VPS implementing on branch feat/gateway-sync-rest.
- L6 User asked "es un rescan de la misma tag?" Evidence (~/ur4-t6/data/cache.db, source of the service copy): 111556 unsynced, 16 EPCs, 1 antenna, 2026-11-05 18:38 to 2026-11-06 17:38 CST, ~15k reads per tag. Stored timestamps use Go time.String() with monotonic suffix ("... -0600 CST m=+103.84"); must check they parse back non-zero.
- L3 Engine bug found: marks only ids[:accepted]; idempotent resend returns accepted=0, duplicates=N → infinite resend. Folded into T1 (S3).
