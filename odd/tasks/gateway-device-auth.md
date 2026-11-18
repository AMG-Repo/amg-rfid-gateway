# Gateway device credential with automatic token renewal

Issue: AMG-Repo/amg-rfid-gateway#121. Branch: `feat/gateway-device-auth` (from `origin/main`).
Backend side (token endpoint, credential storage, admin CLI, strict GatewayAuth) is owned by the VPS session in AMG-RFID-Tools-Demo.

## Specs

- S1 (L2): "no hay manera de iniciar session con credenciales desde la rasp como un login? hacienod un cambio de token cada cierto tiempo? digo porque el estar haciendoel pase manual desde el vps en produccion no lo veo viable". The gateway logs in with its own device credential (`gateway_id` + secret file, mode 0600) via `POST /api/v1/gateway/auth/token` and nobody hands it a JWT manually.
- S2 (L3): "jornada de 12 horas evitamos que quede expuesto mucho tiempo, que se interrupa en medio de una jornada deberia ser de 8 pero dejemos en 12". Access tokens live 12 h (`expires_in` from the server); the gateway renews at ~80% of lifetime or on a 401, single-flight, with backoff on outage, without interrupting reads. HTTP calls go through one helper that retries once after a forced renewal; the WebSocket gets a fresh token on every connect.
- S3 (L3): "Reemplazo limpio (Recomendado)". `jwt_secret` is removed (no compatibility window) and replaced by `gateway_id` + credential file path; `install.sh`, `configs/config.example.yaml`, README and TUI settings updated.
- S4 (L3): "Esperar los PRs". No bridge token and no manual production token handoff.

Contract confirmed by backend (AMG-Repo/AMG-RFID-VPS#86; 200 also returns expires_at; 400 invalid_request, 401 invalid_client, 429 rate_limited + Retry-After, 503 gateway_auth_unavailable): request `{"gateway_id","secret"}`; 200 `{"access_token","token_type":"Bearer","expires_in":43200}`; 401 generic on bad credential; 429 when rate limited.

## Tasks

- T1 S1,S2 — `internal/auth` TokenSource + fake token endpoint tests — inline — commit: 24ece9e, 62dee8f
- T2 S2 — httpclient single auth helper, 401 retry once — inline — commit: b9af085
- T3 S2 — wsclient token per connect/reconnect — inline — commit: 7cac17a
- T4 S1,S3 — config fields replace `jwt_secret`; main wiring; TUI settings; install.sh; example config; README — inline — commit: e531fcc
- T5 S1-S4 — independent verify (Risk items 2, 3, 4) and PR — gentle-ai-verify — commit: verify mwmmgbwv-3-rvxg: S1,S2,S4 PASS, S3 FAIL (install.sh) → fixed in 72f4c0d, b6577f6, d99db81

## Log

- L1: "dale arranquen con los issues y despues la implementacion" (2026-11-18), after the design proposal for device-credential auth.
- L2: "no hay manera de iniciar session con credenciales desde la rasp como un login? hacienod un cambio de token cada cierto tiempo? digo porque el estar haciendoel pase manual desde el vps en produccion no lo veo viable"
- L3: Decisions (ask_user_question): signing key "Clave separada (Recomendado)" (backend); token lifetime "(custom) jornada de 12 horas evitamos que quede expuesto mucho tiempo, que se interrupa en medio de una jornada deberia ser de 8 pero dejemos en 12"; meanwhile "Esperar los PRs"; config "Reemplazo limpio (Recomendado)".
- L4: Evidence: Pi gateway was an ad-hoc process stopped cleanly 2026-11-06 23:48 UTC; hand-minted token expired 2026-11-13. `feat/local-panel-lanes` (kanban, 41 commits) is not pushed; this PR is based on main and kanban must adopt the auth helper for FetchKanbans/AssignTag when it lands.
- L5: Slice boundary: T1-T4 total ~1250 authored lines in 5 commits, past the ~400-line review slice; delivery shape decided at PR time. Known pre-existing failure: internal/deploy/homebrew tests also fail on origin/main.
- L6: Verify findings fixed: install.sh credential owned by service user and vps_api_url written (72f4c0d); vps_api_url required with https (loopback http allowed); Retry-After capped 15 min, cancelled callers do not back off others, WS 403 invalidates (b6577f6); env-only config reads VPS_API_URL (d99db81). Backend adds /api/v1/ws to gateway token validation (VPS S9); WS token travels in the Authorization header. Branch: 9 commits, ~1580 authored lines, not pushed.
