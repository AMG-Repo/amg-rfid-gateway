# RSSI as signed dBm (issue #134)

Branch `feat/rssi-signed-dbm` from origin/main a861e63. Move reading `rssi` from a positive number (Chainway dBm magnitude, generic raw byte) to signed integer dBm like LLRP PeakRSSI, so a reading session keeps its strongest read. Coordinated with the VPS session (AMG-RFID-VPS).

## Specs

- S1. Meaning (user chose option 2 "Todo como dBm negativo, el estándar de LLRP (-55, -82; más alto = más fuerte)"; "dBm entero (Recomendado)"; "0, como hoy (Recomendado)"): `rssi` on every reading, event, cache row and the VPS sync wire is an integer dBm, larger = stronger, `0` = unknown. The shared contract (`models.Reading.Validate`, REQ-A008) accepts `rssi <= 0` and rejects `rssi > 0` (a passive tag never returns more than 0 dBm). The field stays required and always serialized.
- S2. Chainway: `rssi = round(dBm)` keeping the sign (-65.9 dBm -> -66); unknown RSSI (parser `RSSIValid == false`) -> 0.
- S3. Generic (user: "dBm con signo: 0xC9 = -55 (Recomendado)"): the RSSI byte is read as a signed 8-bit dBm value (`0xC9` -> -55). A byte that decodes to a positive dBm (0x01-0x7F) is impossible for a passive tag and becomes 0 (unknown); `0x00` -> 0. Documented as an assumption until checked with a real generic reader.
- S4. A reading session keeps its strongest known read: the largest non-zero `rssi`; 0 only when every read was unknown (an unknown read never replaces a known one, and a known read replaces an initial unknown).
- S5. Local queue on upgrade (user: "Convertirlas al actualizar (Recomendado)"): queued `pending_readings` rows written before this change are converted once to signed dBm (`rssi = -rssi WHERE rssi > 0`), so nothing is lost or sent with the old scale. Assumption: queued rows come from Chainway (the only deployed gateway is the Pi's UR4); idempotent because new values are always <= 0.
- S6. Display: the local panel shows the signed value (`-66 dBm`), the TUI `(-66)`; SSE `tag_detected.rssi` carries the signed value.
- S7. Docs: README (session max RSSI = strongest, rssi meaning per protocol, SSE field), the generic framing spec (0xC9 -> -55), the shared contract comment, and the Chainway notes describe signed dBm.
- S8. Rollout (VPS proposal msg 0584efe0, adopted): gateway release first (VPS already accepts negative rssi: only required + int32, column INTEGER without CHECK; nothing on the VPS consumes rssi); then VPS one-off backfill `UPDATE gateway_readings SET rssi = -rssi WHERE rssi > 0` with backup, rehearsal, row-count guard and the user's OK; optionally the VPS then pins `rssi <= 0`.

## Tasks

- T1 (S1, S2, S3, S7 contract/generic/Chainway) Shared contract accepts signed dBm and rejects > 0; generic byte -> signed dBm; Chainway keeps the sign; every test pinning positive rssi (201, 93, positives through Validate) moved to the new scale; generic framing spec and contract comment. One unit because Validate rejecting > 0 breaks Chainway and every positive pin at once. Route: gentle-ai-worker (context backstop), test-first. Risk: item 3 (contract). Commit: pending.
- T2 (S4, S6, S7) Session keeps the strongest known read (0 = unknown never wins), panel/TUI display, README. Route: worker after T1, test-first. Commit: pending.
- T3 (S5) Cache migration converts queued positive rows once. Route: worker after T1 (parallel with T2, disjoint files), test-first. Risk: item 1 (rewrites stored data). Commit: pending.
- T4 (S1-S7) Independent verify (items 1, 3), PR closing #134, release v0.8.4 (includes #133 fix), Pi upgrade, VPS backfill with the user's OK. Route: gentle-ai-verify, then delivery with the user. Commit: pending.

## Log

- L1 (user, 2026-11-19): "ok realiza una investigacion que es RSSI en rfid y en cada protocolo" -> "que problemas nos puede traer no tener los negativos?" -> "dale vamos con 2". Answers: "dBm con signo: 0xC9 = -55 (Recomendado)"; "dBm entero (Recomendado)"; "0, como hoy (Recomendado)"; "Convertirlas al actualizar (Recomendado)".
- L2 (research): RSSI = power of the tag backscatter measured by the reader, dBm, negative in practice (-45 near, -80 weak), not a distance. LLRP 2.0 PeakRSSI = signed 8-bit whole dBm. Chainway UR4: 2 bytes, dBm = -(65535 - raw)/10, valid when 0 < magnitude < 200. Generic `0xCC` frame: 1 byte (`0xC9` in the spec vector); manual not obtained, signed int8 is the assumption.
- L3 (VPS msg 0584efe0): /gateway/sync validates rssi only as required + int32 (gateway_readings_handler.go:256-269); gateway_readings.rssi INTEGER NOT NULL, no CHECK (011_gateway_readings.sql:16); no VPS consumer of rssi; /gateway/sync does not use models.Reading.Validate. Rollout as S8. VPS holds all changes until v0.8.4 is on the Pi.
- L5 (parent, 2026-11-19): tasks regrouped: Chainway sign (S2) moved into T1 because Validate rejecting > 0 would make the Chainway handler and cache reject every reading if they landed apart; context backstop reached, so T1-T3 go to workers.
- L4 (explore mwo4rjaa-3-zqm7): producers only generic `ToReading` (packet.go:352) and Chainway handler (protocol_handler.go:170-174); Validate callers sync_request.go:42, cache/sqlite.go:712, aggregator.go:82; aggregator plain max at aggregator.go:106-107 (0 not special-cased, would win with negatives); cache pending_readings.rssi INTEGER NOT NULL, no migration; panel app.js:749 shows "N dBm"; TUI prints the int. Tests pinning 201/93/positives listed in the explore result (shared models/protocol, antenna, cache, wsclient, e2e, readingsession).
