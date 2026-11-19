# Reading sessions (tag dedupe)

The UR4 reports the same tag tens of times per second; every raw read is queued and uploaded, so the queue grows ~20x faster than the sync drains it.
Group reads of the same tag into one session record with a counter. Branch `feat/reading-sessions` from origin/main (6b29963). Cross-repo: the VPS stores the new fields.

## Specs

- S1 User: "ok en las lecturas para evitar la duplicacion una vez que se le el tags en lugar de volver a generar una etiquet de entrada generamos un contador icremental que dices?" — reads of the same tag (same EPC and antenna) accumulate into one session with an incrementing read counter instead of one queued reading per read.
- S2 User: "detalle aqui estamos mandando de una la info desde mi punto de vista generamos la pila de tags y una vez por ejemplo deje de detectar 5 segundo subo la info de una que dicesx?" — a session closes after 5 s without reading the tag; only then is it queued for upload.
- S3 A session that stays open 60 s is closed and queued anyway, and a new one starts (a tag left on the antenna must not stay invisible to the cloud).
- S4 User chose "Con contador: cambio en gateway y VPS (recomendado)": each session is sent with first_seen (the reading `timestamp`), `last_seen`, `read_count` and the maximum RSSI; the VPS adds those columns to gateway_readings (additive migration).
- S5 The local panel, the movement detection and the read stats keep receiving every raw read (event bus and stats are unchanged); only the cloud queue is deduplicated.
- S6 Open sessions are queued on shutdown so a clean stop loses no data.

## Tasks

- T1 [S4] shared model + local queue: `Reading.LastSeen`, `Reading.ReadCount`; pending_readings columns last_seen, read_count (additive migration) — inline, TDD — 877e6c4
- T2 [S1-S3,S5,S6] session aggregator implementing antenna.Cache, wrapping the SQLite cache; config `reading_session_idle` (5s) and `reading_session_max` (60s) — inline, TDD — 877e6c4
- T3 [S5] wiring in cmd/gateway/main.go; README/config docs — inline — 877e6c4
- T4 [S4] VPS contract agreed and deployed (other session); release v0.8.3; Pi upgrade; verify sessions in prod

## Log

- L1 (2026-11-19) User: "ok vamos con la deduplicacion de tags". Context: after v0.8.2, UR4 at 25 dBm read ~65 reads/s from 11 tags vs ~3.3/s drained.
- L2 User answers to the design questions: S1 and S2 quoted verbatim above; then chose "Con contador: cambio en gateway y VPS (recomendado)" (S4). S3 (60 s max) proposed by el Gentleman in the same question and accepted with that choice.
- L3 Design: both protocol handlers store through `antenna.Cache` (internal/antenna/manager.go:31); the aggregator implements it and wraps the SQLite cache, so handlers, event bus and stats stay unchanged.
- L4 VPS agreed (msg 62c928c7, user OK there "Sí, issue + rama + PR"): optional per-reading "last_seen" (RFC3339/Nano) and "read_count" (integer); missing or null -> last_seen = timestamp, read_count = 1. Rejections in errors[]: "reading[i]: last_seen is before timestamp", "reading[i]: read_count must be at least 1", "reading[i]: read_count out of range" (> 2147483647), malformed -> "reading[i]: invalid reading". timestamp = first_seen keeps the natural key; rssi = session max; duplicate key -> ON CONFLICT DO NOTHING counted as duplicate (no update). Migration 012: last_seen NOT NULL backfilled = read_at, read_count NOT NULL DEFAULT 1, CHECKs. Do not release until their deploy.
- L5 T1 GREEN (not committed): Reading.LastSeen `json:"last_seen,omitzero"`, Reading.ReadCount `json:"read_count,omitempty"` (0 = plain reading, omitted, VPS reads 1); Validate rejects last_seen before timestamp and negative read_count; pending_readings gets last_seen DATETIME NULL and read_count INTEGER NOT NULL DEFAULT 0 via migrateReadingSessionColumns; insert/select carry them.
- L6 T1-T3 committed as 877e6c4 (815 authored lines, one behavior; most are tests). New package internal/readingsession (Aggregator implements antenna.Cache; Run ticks every 1 s; ShutdownQueue = FlushAll + cache Flush, because cache Close only writes the batch in progress, not readings still buffered in its channel). Config reading_session_idle/max with defaults 5s/60s; Validate rejects negatives and max < idle. pending_readings is only read inside internal/cache, so panel/movements are unaffected (S5). Checks: go build, go vet, go test ./... green except the pre-existing internal/deploy/homebrew "transaction refused" failures; go test -race on readingsession, cmd/gateway, config, cache green. Slice boundary past ~400 lines accepted; choose PR shape when publishing.
