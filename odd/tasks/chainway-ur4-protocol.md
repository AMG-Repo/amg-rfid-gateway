# Chainway UR4 Native TCP Protocol

Add a `chainway` antenna protocol so the gateway reads tags from a Chainway UR4 fixed UHF reader over Ethernet (command work mode), natively in Go, with no Java or vendor library at runtime.
Status: planned only (session 2026-10-27). Implementation starts next session at T0 when the reader is on hand, otherwise T1 (T1/T2 need no hardware).

## Specs

- S1. Decision (from L3/L4): use Ethernet/TCP natively from Go, not the vendor Java API. The UR4 runs in work mode 0 (command mode): the gateway dials the reader, sends start-inventory, and consumes tag notifications.
- S2. Frame format (decoded from `ReaderAPI20240822.jar`, class `com.rscja.deviceapi.i` methods `b()`/`a()`):
  `A5 5A | LEN(2, big-endian, whole frame length incl. header and trailer) | CMD(1) | DATA(n) | CHK(1) | 0D 0A`.
  `CHK` = XOR of every byte from LEN through the end of DATA. LEN valid range 8..2048; anything else resyncs on the next `A5 5A`.
- S3. Stream reassembly: TCP chunks may split or merge frames; the extractor must buffer, resync on garbage and bad checksum, and cap the buffer (mirror `amg-rfid-shared-go/protocol.StreamExtractor` behavior and the `tcp-stream-frame-reassembly` spec).
- S4. Commands:
  - Start inventory: CMD `0x82`, DATA `00 00` -> `A5 5A 00 0A 82 00 00 88 0D 0A`.
  - Stop inventory: CMD `0x8C`, no DATA -> `A5 5A 00 08 8C 84 0D 0A` (literal in the jar). Response CMD `0x8D`, `DATA[0] == 0x01` means OK.
  - Tag notification (unsolicited): CMD `0x83`.
- S5. Tag payload (DATA of `0x83`, parsed like `com.rscja.deviceapi.h.a(byte[], true)`):
  `PC(2) | EPC(N) | [TID(12)] | [USER] | RSSI(2) | ANT(1)`, where `N = (PC[0] >> 3) * 2`.
  RSSI = `-(65535 - uint16be) / 10` dBm (valid when 0 < magnitude < 200, else unknown). ANT = reader port 1..4.
- S6. Lifecycle: on every (re)connect send start-inventory; on shutdown or context cancel send stop-inventory before closing. While inventory runs, the reader accepts only stop-inventory.
- S7. Config: `protocol: "chainway"` is accepted per antenna in `config.yaml`, the TUI selector, and validation. Default port is unconfirmed (8888 according to the community Node lib `ready2tag/chainway-rfid`); it must be confirmed on the physical reader.
- S8. Readings flow through the existing pipeline (cache, stats `RecordReading(epc, rssi)`, event bus) exactly like generic readings. Reader ANT number is preserved in the reading.
- S9. Existing `generic` behavior must not change (frame extraction, handling, commands).

## Tasks

- T0 (S7) — [partially blocked: see L7] Hardware discovery with the real UR4: confirm IP, TCP port, firmware version, and current work mode (vendor demo app or UDP search); capture raw TCP of start/tag/stop with Wireshark or `nc` into `test/fixtures/chainway/`. Route: inline, operator present. Commit: —
- T1 (S2, S3) — Chainway frame codec + stream extractor (encode, checksum, decode, resync, buffer cap) with table tests including split/merged chunks and bad checksum. Route: inline, test-first. Status: done (`internal/chainway/frame.go`). Commit: see L9
- T2 (S4, S5) — Command builders and `0x83` tag payload parser (EPC length from PC, optional TID/USER, RSSI, ANT) tested against the S4 literal frames and T0 captures. Route: inline, test-first. Commit: —
- T3 (S1, S8, S9) — `ChainwayProtocolHandler` and per-protocol frame extractor selection in both read paths (`internal/antenna/manager.go` dataReader and `internal/rawtcp/client.go` RunAntenna); generic regression tests. Route: inline. Commit: —
- T4 (S6) — Per-protocol session hooks: send start on connect/reconnect, stop on shutdown; keep generic heartbeat/ReadUII unchanged. Route: inline. Commit: —
- T5 (S7) — Config: `ProtocolChainway`, validation, TUI protocol option, `configs/config.example.yaml`, README section. Route: inline. Commit: —
- T6 (S1–S8) — End-to-end check against the physical UR4 on the Raspberry Pi: tags reach cache/backend, reconnect resumes inventory, shutdown sends stop. Route: inline + `gentle-ai-verify` only if risk escalates. Commit: —

Delivery: chained PRs on `main` (see L10). PR1 = T1 (`feat/chainway-ur4-protocol`); each next task branches from the previous PR branch and targets it, retargeting to `main` as earlier PRs merge.

Risk to watch: T3/T4 touch the shared read path and command lifecycle used by `generic` (Risk item 3); pin generic behavior with tests before changing it.

## Log

- L1 (user, 2026-10-27, voice): "Mira te acabo de adjuntar la imagen ya no llegó el lector quiero que revises en línea si encuentras el csdk para comenzar las pruebas de desarrollo"
- L2 (user): "Okay antes de revisar quiero que verifique si de alguna manera podemos o tiene manera de comunicación por ethernet o tendríamos que desarrollar el soporte directamente nativo desde Java y hacer la comunicación hace C"
- L3 (user): "ok dale, antes de empezar anaícemos que nos conviene mas java o ehternet en base a nuestras necesidades he implementacion actual." -> Decision: Ethernet native in Go (S1).
- L4 (user): "dale avanzamos locura, un detalle hagamos solo la planeacion, tengo poco tiempo para implementar asi que eso lo realizare en la proxima session"
- L5 (evidence): Device label: Chainway UR4, Fixed UHF Reader, DC12V 2A, FCC ID 2AC6AUR4. Vendor SDK manual states network connection needs only `ReaderAPI.jar` (pure Java TCP). SDK extracted by the user to `sdk/` (untracked, proprietary; suggest adding to `.gitignore`). Decompiled with CFR 0.152 to `/tmp/ur4-decomp/out` (ephemeral; re-run `java -jar cfr.jar sdk/demo/Java/API/ReaderAPI20240822.jar --outputdir <dir>` if needed). Key classes: `deviceapi/i.java`, `deviceapi/h.java`, `deviceapi/c/b.java`, `deviceapi/c/f.java`, `SocketManageUR4.java`.
- L6 (evidence): Work modes per Javadoc: 0 command, 1 auto (pushes to `setDestinationIPAndPort`), 2 GPI trigger. Mode 1/2 would require the gateway to listen for inbound connections; deferred.
- L7 (user, 2026-10-27): "ok registra tengo lector, tengo antena pero no tengo ni sma a tipo N" -> Hardware on hand: UR4 reader and antenna; missing SMA-to-N-type cable/adapter (the UR4 has SMA female ports and the antenna has an N-type connector). Unblocked without the cable: IP/port/firmware/work-mode discovery and start/stop command captures. Blocked until the cable arrives: real 0x83 tag captures and T6. Do not run inventory (RF on) with no antenna connected, to avoid damaging the PA; if needed, set minimum power or use a 50-ohm SMA terminator.
- L8 (evidence, photo from user): the antenna cable ends in an N-type female (external thread, center socket). Part to buy: SMA male (standard, not RP-SMA) to N male, 50 ohm, low-loss and short (LMR-200/400).
- L9 (user, 2026-10-28): "continuamos con la implementacione de t1 para la implementacion de ur4 chainway al gateway" -> T1 done on branch `feat/chainway-ur4-protocol`: package `internal/chainway` (Encode/Decode/Checksum/StreamExtractor); invalid LEN, trailer or checksum resyncs one byte forward to the next `A5 5A`; trailing lone `A5` kept across chunks; buffer cap 64 KiB. `go test -race` and `go vet` green.
- L10 (user, 2026-10-28): "ok realiza commit, vamos a hacer las entregas como pr encadenados sobre main" -> Delivery strategy: chained PRs on main, one PR per task slice.
