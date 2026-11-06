# Implementation Plan — Mobile Tmux Manager

**Versi:** 1.1 · **Tanggal:** 6 November 2026
**Rujukan kebutuhan:** [PRD.md](PRD.md)  
**Strategi:** irisan vertikal yang dapat diuji pada SSH dahulu, lalu paritas Agent, manajemen lengkap, control mode, dan notifikasi. P0 tidak dianggap siap dipakai pada host penting sebelum gate keamanan dan keselamatan lulus.

## Pelacakan pekerjaan aktual

Diperbarui 6 November 2026. Baca [MEMORY.md](MEMORY.md) untuk langkah implementasi,
aturan, hasil verifikasi, dan titik lanjut agent. Tabel ini mencatat status aktual;
kata **Selesai** pada deskripsi task di bawah berarti *kriteria penerimaan*, bukan
pernyataan bahwa task sudah dikerjakan. Checkbox hanya dicentang jika gate lulus.

| Task | Status | Sudah tersedia / yang masih harus dikerjakan |
|---|---|---|
| T00 | DONE | Struktur Go/React, build, CI, konfigurasi contoh, migrasi 001 naik/turun, health. CI Linux dan uji lokal lulus. |
| T01 | TODO | Matriks host Linux/Agent dan HP Android/iOS nyata belum disiapkan. |
| T02 | PARTIAL | Spike tmux 3.8 macOS dan ADR 0001 tersedia; matriks versi, minimum versi, output burst dan ukuran tanpa desktop belum tuntas. |
| T03 | PARTIAL | Login/ACL/CSRF/Origin/audit dan localhost-only diuji; live Serve/Grants negative tests belum. |
| T10 | PARTIAL | Kode onboarding key lokal, pin host key, pembatas jaringan, probe/API/UI selesai dan uji lokal lulus; verifikasi CI Linux checkpoint ini sedang disiapkan. |
| T11 | PARTIAL | Parser, native IDs/epoch/revision, snapshot SSH dan cache stale selesai; integrasi SSH + tmux terisolasi macOS lulus, CI Linux menunggu. |
| T12 | TODO | PTY SSH, WebSocket, xterm.js, read-only/interactive, bounded buffers. |
| T13 | TODO | Reconnect, status SSH, no replay dan error taxonomy terminal. |
| T20 | TODO | Mutasi session dengan epoch/revision, idempotency, konfirmasi/audit. |
| T21 | TODO | Mutasi window/pane, snapshot pane dan suite dua transport. |
| T22 | TODO | Enrollment/revoke Agent, credential, handshake/generation dan RPC. |
| T23 | TODO | PTY Agent dan paritas mode terminal. |
| T24 | TODO | Heartbeat, reconnect/jitter, capability refresh dan rekonsiliasi. |
| T30 | TODO | Manifest/PWA/service worker asset-only, standalone dan uji perangkat. |
| T31 | TODO | Toolbar keyboard/paste/hardware keyboard dan aksesibilitas; baru ada UI responsif dasar. |
| T32 | PARTIAL | Audit dasar dan redaction tests tersedia; filter/retensi/security review/fuzz belum. |
| T33 | TODO | Packaging service, backup/restore, rollback runbook dan rotasi/revoke. |
| T34 | TODO | Failure matrix dua transport dan kapasitas; gate rilis P0 belum lulus. |
| T40 | TODO | Parser control mode state machine, fixture dan fuzz. |
| T41 | TODO | Event bridge dua transport, sequence/resync dan polling fallback. |
| T42 | TODO | Live pane independen dan pembuktian isolasi klien/ukuran. |
| T50 | TODO | Skema notifikasi, mapping/confidence/dedupe dan inbox. |
| T51 | TODO | Hook lokal Codex/Claude berversi dan autentikasi ke Agent. |
| T52 | TODO | Notifikasi in-app dan Web Push opt-in tanpa isi sensitif. |
| T60 | TODO | Tab/favorit/pencarian dan tuning berbasis benchmark. |

### Checklist irisan T10–T11

- [x] Registri profil dan allowlist admin; opsi alamat dan petunjuk pada UI.
- [x] Kontrak onboarding credential lokal dan metadata/probe API.
- [x] Migrasi 002 credential reference dan cache observasi yang dapat di-rollback.
- [x] Penyimpanan private key + public pin aman; CLI configure/revoke, tanpa secret API (ADR 0002).
- [x] SSH auth, host key pinning, batas jaringan tailnet, timeout dan output.
- [x] Katalog perintah tmux dan parser inventaris native IDs/epoch/revision.
- [x] Probe/inventaris terotorisasi, audit, status dan cache stale yang jujur.
- [x] UI konfigurasi/probe dan pohon session/window/pane; terminal tetap gated.
- [x] Uji key salah, credential hilang/revoke, input/command injection, timeout,
  ACL/CSRF, output malformed/berlebih, cache stale dan tmux restart.
- [ ] Uji SSH + tmux pada host/perangkat matriks yang benar-benar tersedia.
- [ ] Checkpoint: format/vet/TypeScript/race/build/CI; update memory dan status.

## 1. Hasil akhir dan urutan rilis

| Tahap | Prioritas | Hasil yang dapat didemokan | Gate keluar |
|---|---:|---|---|
| M0 — Fondasi dan spike | P0 | Tailnet privat, login, skeleton Go/PWA, percobaan tmux multi-klien | Tidak ada listener publik; perilaku tmux/resize terdokumentasi |
| M1 — SSH vertical slice | P0 | Host SSH, inventaris, read-only dan interactive PTY dari HP | Host key diverifikasi; disconnect tidak membunuh tmux |
| M2 — Manajemen dan Agent | P0 | CRUD session/window/pane; agent enrollment, PTY, heartbeat/reconnect | Paritas kontrak dan mutasi aman pada dua transport |
| M3 — Mobile PWA dan hardening | P0 | Toolbar, instalasi/fullscreen, audit, backup/runbook | Semua acceptance P0 dan uji gangguan lulus |
| M4 — Control-mode bridge | P1 | Event tmux pada SSH/Agent, live pane spesifik, resync | Parser dan multi-klien/flow-control lulus |
| M5 — Codex/Claude notifications | P1 | Event terstruktur, inbox, dedupe; Web Push opt-in | Tidak ada klaim palsu “completed”; tidak ada konten sensitif dalam push |
| M6 — Penyempurnaan | P2 | Tab ganda, favorit, tuning performa | Target kapasitas dan UX terukur |

Estimasi kasar untuk satu engineer berpengalaman: M0 1–2 minggu, M1 2–3, M2 3–5, M3 2–3, M4 2–4, M5 1–3. Ini rentang perencanaan, bukan komitmen jadwal; hasil spike tmux dan uji HP nyata dapat mengubahnya. M0–M3 menghasilkan rilis aman yang berguna; M4–M5 melengkapi cakupan wajib PRD.

## 2. Arsitektur implementasi

```text
apps/web (React/Vite/TypeScript, xterm.js, service worker)
        │ REST v1 + WSS terminal/events
        ▼
cmd/hub (Go HTTP, auth, authorization, audit, SQLite, WS gateway)
        │
        ├─ internal/connector/ssh  ─ SSH+PTY ─▶ sshd ─▶ tmux
        └─ internal/connector/agent ◀ WSS ─ cmd/agent ─ PTY/tmux
                        └─ internal/tmux (format parser, target validation,
                                          admin operations, control mode)
```

**Batas modul:** `identity` (login/session/izin), `registry` (profil/secret ref), `inventory` (snapshot+epoch), `tmux` (operasi terstruktur), `terminal` (viewer, backpressure), `agentproto` (RPC/heartbeat), `events` (control-mode bridge), `notifications`, `audit`, `storage`. UI hanya memakai kontrak publik, tidak membedakan perintah tmux berdasarkan transport.

**Interface Go yang diusulkan:**

```go
type Connector interface {
    Capabilities(ctx context.Context) (Capabilities, error)
    Health(ctx context.Context) (Health, error)
    Snapshot(ctx context.Context, socket SocketProfile) (Inventory, error)
    Execute(ctx context.Context, op TmuxOperation) (OperationResult, error)
    OpenTerminal(ctx context.Context, req TerminalRequest) (TerminalStream, error)
    Subscribe(ctx context.Context, req EventSubscription) (EventStream, error)
}
```

`Subscribe` boleh melaporkan `unsupported` sampai M4. Setiap `TmuxOperation` adalah enum+field bertipe, bukan string command umum. `TerminalStream` memiliki read/write/resize/close; close hanya melepas viewer/client. `Capabilities` menyatakan fitur nyata hasil probe, termasuk flag tmux yang dibutuhkan. Terminal stream tidak menjadi pemilik proses tmux.

### 2.1 Identitas target dan rekonsiliasi

Setiap inventaris memuat `hostId`, `socketId`, `epoch`, `observedAt`, `sessionId`, `windowId`, dan `paneId`. `epoch` berasal dari fingerprint server tmux (`socket_path`, `pid`, `start_time`, versi) yang dibaca melalui format tmux, ditambah generation lokal adapter. Bila fingerprint tidak dapat diverifikasi, mutasi destruktif ditolak sampai snapshot baru berhasil. ID tmux native dipakai karena tidak ambigu **di dalam satu server tmux**; nama/index hanya untuk tampilan. [Manual tmux](https://man7.org/linux/man-pages/man1/tmux.1.html) mendokumentasikan `pid`, `start_time`, `socket_path`, serta native IDs; [tmux control-mode wiki](https://github.com/tmux/tmux/wiki/Control-Mode) menganjurkan ID daripada nama/index.

Mutasi memiliki syarat `If-Match: "<epoch>:<object-revision>"` atau field `expectedEpoch` dan diverifikasi langsung pada host. `object-revision` adalah fingerprint metadata target hasil snapshot terbaru, bukan versi global database. Jika target hilang/berubah, kembalikan `404`/`409`; jangan menjalankan operasi terhadap ID yang baru muncul setelah tmux restart. `create` dan `kill` memakai idempotency record berumur terbatas; retry dengan kunci yang sama mengembalikan hasil awal, dan hasil ambigu memicu `reconcile_required`.

### 2.2 Koneksi SSH

- Alamat yang didaftarkan harus berada dalam allowlist admin untuk nama/IP tailnet; server tidak menerima URL/host bebas dari request viewer. Hub melakukan koneksi SSH ke alamat profil yang sudah disetujui melalui tailnet.
- SSH key per profil atau secret ref yang dilindungi; hub menyimpan private key dalam file di luar DB dengan permission `0600` atau secret manager setara. UI tidak menerima key setelah pendaftaran.
- Pin host key lewat `known_hosts`/fingerprint terverifikasi pada onboarding. Perubahan key menghentikan koneksi sampai admin menyetujui rotasi secara eksplisit; tidak ada `InsecureIgnoreHostKey`. Implementasi Go memakai [ssh/knownhosts](https://pkg.go.dev/golang.org/x/crypto/ssh/knownhosts).
- Pool koneksi SSH dibatasi per host. Command admin memakai channel singkat; terminal PTY memiliki channel tersendiri dan attach `tmux` ke session. Command remote SSH melewati shell remote, sehingga hanya template perintah tetap dan argumen yang divalidasi/di-quote oleh satu serializer yang diuji. Tidak ada endpoint raw shell untuk admin.
- Probe awal memeriksa user OS, socket yang diizinkan, versi tmux, host key, dan kemampuan PTY; timeout per tahap. Bila tmux belum berjalan, host tetap `ONLINE` dengan inventaris kosong, bukan `UNREACHABLE`.

### 2.3 Koneksi Agent

- Agent adalah satu binary Go per user OS dan socket tmux, dijalankan dengan `systemd --user` atau `launchd`; tidak menjalankan root. Agent membuka WSS outbound ke hub melalui URL Serve pada tailnet. Tailscale Grants hanya mengizinkan node agent mencapai hub yang diperlukan.
- Enrollment: admin membuat token sekali pakai berumur pendek; agent pertama kali mendaftar memakai token itu dan identitas node/profil yang diharapkan; hub mengeluarkan credential unik yang disimpan file `0600`. Token enrollment tidak masuk log. Credential dapat dirotasi/dicabut; server menyimpan verifier/hash, bukan token mentah. TLS dan verifikasi hostname tetap wajib.
- Hello memuat protocol version, agent ID, generation, OS, tmux version, capabilities, dan socket. Heartbeat misalnya 15 detik, `UNREACHABLE` setelah 3 heartbeat hilang; nilai konfigurabel. Reconnect exponential backoff+jitter, autentikasi ulang, snapshot penuh, lalu subscription baru. Generation baru menggantikan koneksi lama secara atomik.
- RPC agent hanya menerima operasi tmux yang di-allowlist dan ID yang valid. PTY lokal dibuat sebagai user agent dan `tmux attach-session` dijalankan di dalamnya; bukan shell umum. Agent tidak pernah mengeksekusi payload command arbitrer dari hub. Host Agent yang offline tidak memicu fallback SSH.

### 2.4 Terminal dan control mode

M1/M2 memakai PTY attach untuk **session view**. Read-only menggunakan dua lapis: gateway menolak `input` dan client tmux dibuka dengan read-only/ignore-size bila versi mendukung. Interactive memakai permission `terminal:write`, aktivasi per viewer, dan `ignore-size` default. Ukuran HP tidak mengubah ukuran tmux bersama secara otomatis. `resize` hanya mengubah viewport lokal sampai capability dan kebijakan host mengizinkan perubahan ukuran remote eksplisit.

M4 menambah control client per subscription yang dibatasi jumlahnya, parser state machine untuk blok command dan notifikasi asynchronous, escaping `%output`, partial frames, dan flow control (`pause-after`, resume, snapshot ulang). Satu event ring per host/socket memiliki sequence; gap memicu `resync_required`. SSH menjalankan control client melalui channel SSH, Agent menjalankannya lokal. Jika versi/fitur tidak cocok, polling inventaris dan PTY tetap berfungsi. `capture-pane` menyediakan snapshot terhingga dan diberi label historis; tidak dipakai sebagai loop streaming. Saat membuka pane, renderer mengambil snapshot awal lalu mengikuti output control mode; aplikasi full-screen dapat membutuhkan redraw dan snapshot historis tidak dijanjikan identik dengan layar sebelumnya. [Control-mode wiki](https://github.com/tmux/tmux/wiki/Control-Mode) adalah spesifikasi perilaku acuan.

**Catatan multi-klien:** `attach-session` tidak menarget pane secara independen. M4 harus membuktikan pemilihan window/pane per viewer sebelum UI mengaktifkan navigasi independen. Jika capability tmux tidak cukup, viewer pane bersifat pengamatan dan perubahan pilihan/layout diberi peringatan bahwa desktop dapat ikut berubah.

## 3. Kontrak API v1

Semua respons memakai `application/json`; waktu UTC RFC 3339; ID publik opaque. Semua request dilacak dengan `X-Request-ID`. Cookie sesi aplikasi tidak digunakan sebagai kredensial agent. Error konsisten:

```json
{"error":{"code":"conflict","message":"Target tmux berubah; muat ulang daftar.","requestId":"...","details":{"reason":"epoch_mismatch"}}}
```

Kode umum: `401 unauthenticated`, `403 forbidden`, `404 not_found`, `409 conflict`, `422 invalid_request`, `423 host_unreachable`, `429 rate_limited`, `503 transport_unavailable`. Mutasi berbasis cookie memerlukan token CSRF; `Origin` dicek untuk REST sensitif dan upgrade WS. Rate limit diterapkan pada login, pairing, input, dan mutasi.

| Method/path | Izin | Input/hasil inti |
|---|---|---|
| `POST /api/v1/auth/login` | publik di tailnet | credential → cookie sesi, CSRF token; tanpa detail pembeda akun |
| `POST /api/v1/auth/logout` | login | revoke sesi |
| `GET /api/v1/me` | login | identitas dan izin ringkas |
| `GET /api/v1/hosts` | `host:read` | profil, transport, status, `observedAt` |
| `POST /api/v1/hosts` | `host:admin` | tipe SSH/Agent, label, socket, alamat tailnet; credential lewat alur terpisah |
| `PATCH /api/v1/hosts/{hostId}` | `host:admin` | label, policy, konfigurasi aman |
| `DELETE /api/v1/hosts/{hostId}` | `host:admin` | hapus profil/kredensial, **bukan** tmux host |
| `POST /api/v1/hosts/{hostId}/probe` | `host:admin` | health/capability; tanpa secret |
| `POST /api/v1/agents/enrollments` | `agent:admin` | token sekali pakai dengan expiry |
| `POST /api/v1/agents/{agentId}/revoke` | `agent:admin` | cabut credential, putus koneksi |
| `GET /api/v1/hosts/{hostId}/inventory` | `tmux:read` | epoch, freshness, sessions/windows/panes |
| `POST /api/v1/hosts/{hostId}/sessions` | `tmux:write` | `name`, idempotency key → session |
| `PATCH /api/v1/hosts/{hostId}/sessions/{sid}` | `tmux:write` | `name`, expected epoch/revision |
| `DELETE /api/v1/hosts/{hostId}/sessions/{sid}` | `tmux:kill` | expected epoch/revision, confirmed intent |
| `POST /api/v1/hosts/{hostId}/sessions/{sid}/windows` | `tmux:write` | nama opsional → window |
| `PATCH/DELETE /api/v1/hosts/{hostId}/windows/{wid}` | `tmux:write`/`tmux:kill` | rename/kill + precondition |
| `POST /api/v1/hosts/{hostId}/windows/{wid}/panes` | `tmux:write` | split direction, target pane, precondition |
| `DELETE /api/v1/hosts/{hostId}/panes/{pid}` | `tmux:kill` | kill pane + precondition |
| `GET /api/v1/hosts/{hostId}/panes/{pid}/snapshot` | `tmux:read` | bounded capture text + truncated flag |
| `POST /api/v1/hosts/{hostId}/panes/{pid}/select` | `tmux:write` | P1; viewer-scoped jika capability ada |
| `POST /api/v1/hosts/{hostId}/panes/{pid}/resize` | `tmux:write` | P1; eksplisit, policy/capability gate |
| `GET /api/v1/audit` | `audit:read` | filter actor/host/action/waktu; metadata saja |
| `GET /api/v1/notifications` | login | event terstruktur dan confidence |
| `POST /api/v1/push-subscriptions` | login | P1 opt-in, subscription browser |

Contoh `GET inventory`:

```json
{
  "hostId":"h_01","socketId":"default","transport":"ssh",
  "state":"ONLINE","observedAt":"2026-11-06T05:00:00Z",
  "stale":false,"epoch":"e_4a2...",
  "capabilities":{"ptyAttach":true,"controlMode":false,"independentPane":false},
  "sessions":[{"id":"$1","name":"codex","revision":"r_...","windows":[{"id":"@2","name":"editor","panes":[{"id":"%3","active":true}]}]}]
}
```

### WebSocket browser

`GET /ws/v1/terminal` dan `GET /ws/v1/events` hanya melalui HTTPS Serve. Upgrade memeriksa sesi, Origin, CSRF/nonce satu pakai, izin host, jumlah koneksi, dan `Sec-WebSocket-Protocol` versi yang disepakati. `terminal.open` meminta `hostId`, `sessionId`, `epoch`, `mode=read_only`, dan `cols/rows` viewport. Server membalas `terminal.opened` dengan `viewerId`, `mode`, `capabilities`, dan `sequence`.

Frame kontrol JSON: `terminal.open`, `terminal.set_mode`, `terminal.resize_request`, `terminal.detach`, `ack`, `ping`, `error`, `resync_required`. Frame data binary berisi header versi+`viewerId`+sequence+direction, lalu byte PTY. Input binary hanya diterima setelah mode interactive disetujui server; tidak ada replay otomatis. Server mengirim `terminal.closed` dengan alasan ketika host/target hilang. Event UI memakai JSON kecil (`inventory.changed`, `host.state`, `notification.created`); klien mengambil snapshot lagi setelah gap.

**Backpressure:** target awal buffer maksimum 1 MiB per viewer dan frame maksimum 64 KiB; batas disetel dari benchmark. Jika viewer tertinggal, server hentikan stream viewer dengan `resync_required`, bukan menahan RAM tanpa batas. Batas ini tidak menjamin output yang terlewat bisa dipulihkan penuh dari tmux history.

### Protokol agent

`GET /ws/v1/agent` mempunyai autentikasi agent terpisah, versi protokol, TLS hostname verification, dan identitas yang dicocokkan dengan profil. Envelope kontrol berisi `type`, `requestId`, `agentId`, `generation`, `deadline`, `payload`. Pesan: `hello`, `heartbeat`, `capabilities`, `inventory.snapshot`, `tmux.operation`, `terminal.open/input/output/close`, `event.subscribe/data/resync`, `notification.emit`, `error`. RPC memiliki deadline dan idempotency key. Output terminal tidak disimpan dalam queue offline agent; reconnect memulai stream baru.

## 4. Model data SQLite dan kepemilikan data

| Tabel | Kolom penting | Catatan |
|---|---|---|
| `users` | `id`, `username`, `password_hash`, `disabled_at`, `created_at` | Bootstrap admin satu kali; password hash memory-hard, parameter dicatat |
| `app_sessions` | `id_hash`, `user_id`, `expires_at`, `revoked_at`, `last_seen_at` | Token acak hanya di cookie, hash di DB |
| `hosts` | `id`, `label`, `transport`, `tailnet_address`, `os_user`, `socket_name`, `environment`, `enabled` | Satu profil = satu user/socket |
| `host_access` | `user_id`, `host_id`, `role`, `allowed_actions` | Default deny |
| `ssh_credentials` | `id`, `host_id`, `secret_ref`, `host_key_fingerprint`, `rotated_at` | Key tidak berada di SQLite |
| `agent_identities` | `id`, `host_id`, `credential_hash`, `generation`, `last_heartbeat`, `revoked_at` | Unik per agent |
| `enrollment_tokens` | `id`, `host_id`, `token_hash`, `expires_at`, `used_at` | Sekali pakai |
| `host_capabilities` | `host_id`, `tmux_version`, `flags_json`, `observed_at` | Bisa invalidasi saat reconnect |
| `inventory_cache` | `host_id`, `socket_id`, `epoch`, `snapshot_json`, `observed_at` | Hanya untuk tampilan stale; bukan otoritas mutasi |
| `idempotency_records` | `actor_id`, `key_hash`, `request_hash`, `result`, `expires_at` | Mencegah retry ganda |
| `notification_events` | `id`, `source`, `kind`, `confidence`, `host_id`, `target_ref`, `occurred_at`, `dedupe_key` | Tidak menyimpan prompt/output |
| `push_subscriptions` | `id`, `user_id`, `endpoint`, `keys_encrypted`, `created_at` | Opt-in, dapat dicabut |
| `audit_events` | `id`, `actor`, `action`, `host_id`, `target`, `result`, `request_id`, `at`, `metadata_json` | Redaksi ketat dan retensi |
| `user_preferences` | `user_id`, `keyboard_json`, `theme`, `font_size` | Tidak berisi secret |

SQLite memakai WAL, migration bernomor, foreign keys, backup konsisten dan restore test. Secret file berada di direktori hanya dapat dibaca proses hub/agent; backup secret terpisah dan terenkripsi. `inventory_cache`/notifikasi boleh dibersihkan; tmux host tetap sumber sebenarnya. Browser service worker hanya cache asset statis; API menggunakan `Cache-Control: no-store`.

## 5. Keamanan dan keselamatan sebagai pekerjaan lintas fase

1. **Jaringan:** verifikasi listener hub `127.0.0.1`, status Serve privat, port firewall, tidak ada Funnel. Grants mengizinkan HP→hub HTTPS, agent→hub HTTPS, hub→SSH host port SSH yang terdaftar. Uji negatif dari node tidak diizinkan. [Tailscale Serve](https://tailscale.com/docs/features/tailscale-serve), [Grants](https://tailscale.com/docs/features/access-control/grants).
2. **Login dan izin:** bootstrap admin lokal, password hash yang kuat, cookie Secure/HttpOnly/SameSite, session expiry/revoke, CSRF token dan Origin check; akses host/aksi `read`, `write`, `kill`, `admin`, `audit` dicek per request dan per upgrade WS. Login dicatat tanpa password. Pertimbangkan passkey setelah rilis jika memang dibutuhkan.
3. **Secret dan transport:** SSH key/agent token unik, rotasi/revoke, host key pinning, TLS verification, tidak dicatat. Hook lokal mengirim event ke Agent lewat Unix socket milik user dengan permission terbatas; Agent memvalidasi sumber dan ukuran payload sebelum meneruskan. Jangan percaya identitas dari header HTTP yang dapat dipalsukan; jika kelak memakai header Serve, listener localhost dan sanitasi proxy harus diuji.
4. **Batas command:** admin operation enum; validasi native ID dan nama; nama awal dibatasi ke `[A-Za-z0-9._-]{1,64}`; pilihan socket dari profil, bukan request; tidak ada `kill-server` atau raw command. Input terminal interactive tetap merupakan shell penuh dan harus diotorisasi sebagai kemampuan berisiko tinggi.
5. **Safety:** close browser/gateway = detach viewer; kill memerlukan konfirmasi UI + izin khusus + revalidasi epoch/target; read-only ditegakkan gateway; resize remote eksplisit; tidak melakukan fallback transport otomatis. Status unreachable tidak menyatakan session mati.
6. **Audit/privasi dan renderer:** structured metadata dengan redaction test. Jangan menyimpan terminal output/input, clipboard, command history, token pairing, atau pesan hook mentah. Nonaktifkan OSC 52, batasi handler tautan terminal, gunakan CSP ketat, dan jangan membuka URL eksternal tanpa tindakan pengguna. Retensi awal 90 hari dan ekspor manual; batas retensi dapat diubah admin.
7. **Operasi:** file permission, startup ordering sesudah Tailscale, health endpoint lokal, disk-space alert untuk SQLite, backup/restore, upgrade/migration rollback. Server restart tidak boleh menjadi pemicu perintah tmux destruktif.

## 6. Task breakdown dan dependensi

Setiap task menghasilkan kode, dokumentasi singkat, dan bukti gate yang relevan. Urutan di bawah adalah dependency order; UI dan backend dalam milestone yang sama dapat dikerjakan paralel bila kontrak sudah disepakati.

### M0 — Fondasi (P0)

- [x] **T00** Tetapkan repo, lint/format, CI, konfigurasi contoh, migration runner, health endpoint lokal, build reproducible Go+PWA. **Selesai:** build bersih dan migration upgrade/rollback dasar lulus.
- [ ] **T01** Buat lingkungan uji: hub, satu SSH host Linux, satu host Agent Linux/macOS, tmux versi target, HP Android/iOS nyata. Catat versi tmux, OS, browser. **Selesai:** matriks dapat diulang.
- [ ] **T02** Spike tmux multi-klien: `attach-session` read-only/ignore-size, output burst, ukuran desktop saat HP attach, per-window/pane selection, server epoch, control mode basic. **Selesai:** ADR perilaku dan minimum versi yang akan didukung; kegagalan memblokir fitur yang bergantung padanya.
- [ ] **T03** Pasang Serve/Grants contoh, hub localhost-only, login+cookie/CSRF/Origin, user/permission model, audit skeleton. **Selesai:** negative access tests lulus sebelum terminal dibuka.

### M1 — SSH vertical slice (P0; setelah T02–T03)

- [ ] **T10** Registri SSH, credential ref, onboarding host key dengan verifikasi out-of-band, probe capability/health. **Selesai:** host salah key ditolak dan tidak ada key di respons/log.
- [ ] **T11** Parser inventaris tmux dengan `-F` format terstruktur dan native IDs; epoch `pid/start_time/socket_path`; cache stale. **Selesai:** inventaris akurat pada empty server, rename, kill eksternal, restart tmux.
- [ ] **T12** PTY SSH session attach, WS gateway, xterm.js viewer read-only, interactive toggle, input Unicode/control keys, bounded buffer. **Selesai:** HP dapat membuka sesi tanpa mengubah ukuran desktop; close viewer tidak membunuh proses.
- [ ] **T13** Reconnect terminal dan status host SSH; no replay input; error taxonomy. **Selesai:** pindah jaringan dan hub restart menampilkan state yang benar.

### M2 — Manajemen dan Agent (P0; setelah T11–T12)

- [ ] **T20** Operasi session create/rename/kill dengan precondition, idempotency, confirm dan audit. **Selesai:** perubahan eksternal menghasilkan conflict, bukan salah target.
- [ ] **T21** Operasi window create/rename/kill dan pane split/kill; snapshot pane bounded; label dampak shared layout. **Selesai:** dua transport menjalankan contract suite yang sama.
- [ ] **T22** Agent enrollment/revoke, credential unik, WS generation/version handshake, allowlisted RPC. **Selesai:** token reuse/agent revoked ditolak.
- [ ] **T23** PTY Agent, terminal framing yang sama, read-only/interactive parity. **Selesai:** suite T12 lulus pada Agent.
- [ ] **T24** Heartbeat, timeout, reconnect+jitter, capability refresh, inventory reconciliation. **Selesai:** sleep/wake dan koneksi ganda tidak menciptakan status ganda.

### M3 — Mobile dan rilis aman (P0; setelah M2)

- [ ] **T30** PWA manifest, install, shell cache statis, standalone/landscape, safe-area, offline/reconnect screen. **Selesai:** Android/iOS uji membuka PWA terpasang; API/output tidak di-cache.
- [ ] **T31** Keyboard toolbar, modifier state, copy/paste, hardware keyboard, touch accessibility, indikator host/mode. **Selesai:** matriks tombol dan screen-reader UI non-terminal lulus.
- [ ] **T32** Audit viewer/admin, filter, retensi, redaction; security review dan fuzz input protokol. **Selesai:** sampel log tidak memuat secret/output/input terminal.
- [ ] **T33** Packaging hub systemd, agent systemd/launchd, backup/restore SQLite+secret, rollback migration, runbook rotasi/revoke dan pemulihan. **Selesai:** clean install dan restore pada mesin uji lulus.
- [ ] **T34** End-to-end failure matrix dan kapasitas awal. **Selesai:** seluruh gate P0 di §8 lulus; batas/target disetel menurut pengukuran.

### M4 — Event bridge dan pane independen (P1; setelah T02, T24, T34)

- [ ] **T40** Parser control mode sebagai state machine dengan fixture dan fuzz untuk interleaving, escaping, `%error`, output parsial. **Selesai:** tidak ada desinkronisasi pada fixture/generasi acak.
- [ ] **T41** Adapter event SSH dan Agent, sequence, resume/resync, polling fallback. **Selesai:** perubahan eksternal muncul di UI dan gap memicu snapshot ulang.
- [ ] **T42** Live pane spesifik, selection per viewer dan kebijakan resize berdasar capability; uji dua klien. **Selesai:** desktop tidak berubah saat HP hanya menjelajah, atau fitur dinonaktifkan dengan alasan yang jelas.

### M5 — Notifikasi coding agent (P1; setelah T24, T41)

- [ ] **T50** Skema event internal, mapping host/session/pane, dedupe, confidence, inbox. **Selesai:** event tak terpetakan tampil sebagai unknown, bukan atribusi palsu.
- [ ] **T51** Pengirim lokal opt-in untuk Codex `notify` dan Claude Code hooks; parser/adapter berversi, autentikasi ke Agent, penghapusan teks sensitif. **Selesai:** completion/attention/error pada versi CLI uji menghasilkan event yang tepat. [OpenAI Docs: configuration reference](https://learn.chatgpt.com/docs/config-file/config-reference), [Claude Code hooks](https://code.claude.com/docs/en/hooks).
- [ ] **T52** Notifikasi in-app; Web Push opt-in dengan payload umum dan feature detection. **Selesai:** izin ditolak/unsupported tetap memberi inbox; payload tidak memuat terminal text. [WebKit Web Push](https://webkit.org/blog/13878/).

### M6 — Penyempurnaan (P2)

- [ ] **T60** Tab terminal ganda dengan batas per pengguna, favorites/filter/search, benchmark RAM/CPU/bandwidth, tuning buffer dan reconnect. **Selesai:** kapasitas terukur dan tidak ada degradasi safety.

## 7. Rencana pengujian dan gate penerimaan

| Lapisan | Kasus penting | Bukti |
|---|---|---|
| Unit | Parser tmux format, validasi ID/nama, serializer SSH, epoch/fingerprint, ACL, CSRF, idempotency, redaction | Test deterministic + fuzz untuk parser/protokol |
| Contract | Satu suite `Connector` untuk SSH dan Agent; error mapping, capability negotiation | Hasil identik untuk operasi yang didukung |
| Integrasi tmux | Empty server; session/window/pane berubah dari desktop; kill saat viewer ada; socket/epoch berubah | Snapshot dan conflict sesuai ekspektasi |
| Terminal | ANSI/Unicode, `Esc`, `Ctrl+C`, arrow, Tab, paste multiline, high output, viewer lambat, OSC 52/tautan berbahaya | Input tepat; buffer bounded; output gap terlihat; clipboard/URL tidak aktif tanpa izin |
| Multi-klien | Desktop+HP read-only, interactive, close viewer, ukuran desktop, pilihan window/pane | Tidak ada perubahan pasif pada desktop |
| Failure | Wi-Fi↔seluler, laptop sleep, agent mati/restart, hub restart, tmux restart, timeout setelah input/kill | No replay/kill otomatis; state dan audit benar |
| Security | Node di luar Grants, tanpa login, CSRF/Origin WS, host key berubah, token pairing reuse/revoke, unauthorized host, path/arg injection | Semua ditolak dan tidak bocor secret |
| Mobile | PWA Android/iOS terpasang, portrait/landscape, keyboard virtual/Bluetooth, offline, refresh, fullscreen fallback | Checklist perangkat nyata dan tangkapan layar |
| Control mode | Interleaved response/event, escape, `%pause`/resume, reconnect gap, tmux versi berbeda | Parser tidak macet; resync andal |
| Notifikasi | Codex/Claude event nyata, duplicate, source missing, push denied/offline | Label/confidence tepat dan payload aman |
| Operasional | Clean install, backup/restore, upgrade/downgrade sesuai migration policy, credential rotation | Runbook dieksekusi pada mesin uji |

**Gate P0:** seluruh FR-01–10 serta NFR-01–09 pada PRD diterima; tidak ada temuan keamanan kritis/tinggi yang terbuka; uji disconnect pada kedua transport membuktikan tmux tetap hidup; audit redaksi lulus. **Gate P1:** FR-11–13 dan notifikasi lulus pada matriks versi/perangkat; fallback yang diizinkan terdokumentasi. Kriteria performa diukur pada profil 10 host/50 session dengan output burst; bila tidak tercapai, rilis ditahan atau target diubah secara eksplisit dalam PRD berdasarkan hasil uji, bukan diasumsikan.

## 8. Deployment, observability, dan rollback

1. **Persiapan:** buat akun service hub non-root, direktori data/secret dengan permission ketat, SQLite WAL, Tailscale pada hub dan host. Pastikan SSH host dapat dicapai hanya melalui tailnet. Jadikan port aplikasi hanya localhost dan jalankan Serve privat. Verifikasi `tailscale serve status`, kebijakan Grants, firewall, dan akses negatif.
2. **Bootstrap:** jalankan migration, buat admin lokal melalui CLI yang tidak mengeluarkan password ke log, login dari HP tailnet, daftarkan satu host uji SSH, baru host penting. Agent dipasang per user OS dan dienroll dengan token sementara.
3. **Observability:** health lokal, metrik jumlah viewer, buffer bytes, koneksi SSH/Agent, heartbeat age, error per adapter, audit action counts. Log memakai request ID; terminal bytes, credentials, dan hook payload mentah tidak dicatat.
4. **Backup:** backup SQLite konsisten dan secret store secara terpisah, terenkripsi; uji restore berkala. Backup hanya memulihkan konfigurasi aplikasi, bukan proses tmux setelah reboot.
5. **Upgrade:** rolling restart hub tunggal berarti viewer putus sesaat; tmux tetap hidup. Skema DB migration harus memiliki backup sebelum upgrade. Jika versi baru gagal, hentikan hub, pulihkan binary+DB backup yang cocok, dan biarkan tmux host tetap berjalan. Agent version skew dibatasi oleh negosiasi protokol; upgrade agent satu host per waktu.
6. **Insiden:** cabut credential agent/SSH yang terdampak, putus app sessions, review audit, verifikasi Grants dan host key, lalu rotasi. Tidak ada prosedur otomatis yang mengirim `kill-session` ketika insiden koneksi terjadi.

## 9. Risiko keputusan teknis yang harus ditutup oleh spike

| Keputusan | Eksperimen/hasil yang dibutuhkan | Fallback bila gagal |
|---|---|---|
| Minimum tmux version dan flag read-only/ignore-size | Uji Linux/macOS dengan versi yang akan didukung | Disable fitur via capability; naikkan minimum versi |
| Pane live independen dan per-client selection | Dua klien desktop+HP, berbagai layout/fullscreen | Snapshot pane + live session view; jelaskan dampak shared |
| Epoch cukup kuat menghadapi tmux restart/ID reuse | Kill/recreate cepat saat koneksi putus | Tolak mutasi ketika fingerprint tidak pasti; refresh dan konfirmasi ulang |
| `pause-after` dan output burst control mode | Stress output dan viewer lambat | Putus viewer + snapshot/resync |
| Integrasi Codex/Claude stabil | Event dari versi CLI yang dipakai dan mapping ke pane | Tampilkan unknown/in-app saja, jangan klaim completed |
| Web Push pada perangkat target | PWA terpasang, izin, jaringan aktif; uji tanpa isi sensitif | Inbox in-app sebagai baseline |

Tidak ada ketergantungan pada Web Push, control mode, atau pemberitahuan coding agent untuk keamanan dasar terminal. Ketiganya tetap deliverable wajib pada fase P1 setelah risiko teknisnya ditutup.
