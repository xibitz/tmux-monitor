# Memory dan serah-terima implementasi

Terakhir diperbarui: 7 November 2026. Baca file ini sebelum melanjutkan pekerjaan.
Dokumen ini menyimpan keadaan pekerjaan; spesifikasi produk tetap di
[PRD.md](PRD.md), urutan task dan acceptance gate di
[IMPLEMENTATION_PLAN.md](IMPLEMENTATION_PLAN.md), kontrak API aktual di
[docs/api-v1.md](docs/api-v1.md).

## Tujuan dan keadaan saat ini

Mobile Tmux Manager adalah aplikasi Go + React untuk mengamati/mengelola tmux
melalui Tailscale, dengan transport SSH atau Agent yang dipilih per profil.
Pengguna meminta development diteruskan dan status ditulis agar agent baru bisa
melanjutkan tanpa mengandalkan riwayat chat. **Memory ini wajib diperbarui setelah
checkpoint, sebelum berhenti, dan sebelum context/token habis.**

Yang sudah tersedia: hub localhost-only, SQLite WAL/migrasi 001–003, bootstrap
admin, Argon2id/cookie sesi, ACL/CSRF/Origin, profil host, audit, UI responsif,
allowlist SSH, Makefile/CI, **key store + CLI SSH configure/revoke, public host
key pinning, probe dan inventaris tmux dengan epoch/revision serta cache stale**,
serta terminal SSH eksperimental (PTY/ticket/WS/xterm), default disabled dan
read-only saat buka. Interactive tersedia melalui izin + aktivasi per viewer.

Baseline sebelum irisan T12: `33466f0` (SSH/inventaris); dokumentasi awal handoff:
`b7b8c1c`. Sebelumnya `1d8c1cf` (UX allowlist), `5c99762` (fondasi), `a879f8c`
(rancangan). Commit dokumentasi sesudah kode tidak mengubah implementasinya.

**T00, T10, T11 DONE. T02/T03/T12/T13/T31/T32 PARTIAL. Task lain TODO.
Berikutnya gate akses/perangkat T03/T12, toolbar T31 dan mutasi T20.**
DONE pada T10/T11 berarti acceptance kode/automated tests yang dicatat telah
lulus, bukan bukti sshd/tailnet/HP pengguna sudah diuji. Agent belum tersedia; M1 dan gate rilis P0 belum selesai. Terminal memerlukan
opt-in operator, bukan rilis P0 yang telah lulus gate.

## Tasklist dan langkah implementasi

Status lengkap per ID berada di tabel pelacakan implementation plan.
`DONE` = acceptance yang disebutkan sudah dibuktikan; `PARTIAL` = sebagian kode
atau uji tersedia; `IN_PROGRESS` = irisan aktif; `TODO` = belum dikerjakan.
Kotak task hanya dicentang setelah gate task tersebut benar-benar selesai.

1. **Fondasi (T00–T03).** Pertahankan fondasi yang sudah ada. Lengkapi matriks
   Linux/macOS dan perangkat nyata, uji Serve/Grants negatif, serta spike ukuran
   tmux. T00 selesai, T01 terbuka, T02/T03 sebagian. Lihat ADR 0001.
2. **SSH onboarding (T10, DONE).** Kontrak/migrasi credential reference, cache,
   CLI key dan public pin, guard alamat, probe bounded, audit dan UI sudah
   diimplementasikan dan diuji. Proses onboarding operator ada di README.md.
3. **Inventaris (T11, DONE).** Katalog perintah tmux tetap dan parser format yang
   aman, native IDs, epoch/fingerprint dan metadata revision; snapshot
   diverifikasi dan cache bertimestamp dengan stale saat gagal sudah tersedia.
   Host yang dapat dihubungi tetapi tidak punya server tmux bukan host mati.
4. **Terminal SSH (T12–T13).** Kebijakan manual tanpa desktop, PTY,
   tiket sekali pakai, WebSocket/framing/sequence, buffer bounded, xterm dan
   read-only server + tmux serta interactive berizin tersedia sebagai irisan
   eksperimental. Interactive memerlukan izin dan aktivasi per viewer. Reconnect reset read-only, tanpa replay.
5. **Mutasi dan Agent (T20–T24).** Implementasikan epoch/revision, idempotency,
   konfirmasi/audit; CRUD tmux; enrollment/revoke, PTY, heartbeat/generation dan
   rekonsiliasi Agent. Jalankan suite kontrak sama pada dua transport.
6. **Mobile dan hardening (T30–T34).** PWA/service worker asset-only, toolbar,
   aksesibilitas, audit/retensi/fuzz, packaging, backup/restore, failure matrix
   dan kapasitas. Baru evaluasi gate P0; uji lokal tidak menggantikan uji HP.
7. **Control mode (T40–T42).** Parser state machine, event/resync, flow control
   dan pembuktian pane independen. Jangan menjanjikan isolasi pane dari PTY biasa.
8. **Notifikasi (T50–T52).** Event terstruktur, mapping/confidence/dedupe,
   adapter opt-in Codex/Claude, inbox dan push tanpa isi terminal.
9. **Penyempurnaan (T60).** Tab/favorit/pencarian dan tuning berbasis benchmark.

## Aturan implementasi

- Baca memory, task aktif, kontrak dan kode terkait; cek `git status` sebelum edit.
  Pertahankan perubahan pengguna. Jangan reset checkout atau menghapus data nyata.
- Kerjakan satu irisan yang bisa dibangun dan diuji. Tulis kontrak sebelum
  endpoint baru, tambahkan migrasi bernomor untuk perubahan SQLite, lalu backend,
  UI dan pengujian yang membuktikan perilaku berisiko.
- Jangan memperluas scope secara diam-diam. Agent adalah program pendamping Go,
  bukan coding agent AI. Transport tidak boleh fallback otomatis.
- Listener hub hanya `127.0.0.1`; akses produksi lewat Serve/Grants, bukan Funnel.
  Login, izin host/aksi, Origin dan CSRF tidak boleh dilewati untuk menghilangkan
  error konfigurasi. Origin adalah URL halaman browser, bukan alamat SSH host.
- Kredensial di secret file `0600`, direktori `0700`, reference saja di DB.
  Jangan kirim private key melalui respons API/log; jangan pakai
  `InsecureIgnoreHostKey` atau menerima host key baru tanpa verifikasi eksplisit.
- Perintah administrasi adalah katalog/enum tetap dengan argumen tervalidasi
  dan quoting teruji. Tidak ada endpoint raw shell atau `kill-server`.
  Gunakan native IDs + epoch + socket/user untuk target tmux.
- Close/disconnect/restart hub/agent/browser hanya melepas viewer, bukan kill.
  Kill objek tmux memerlukan revalidasi, izin, konfirmasi dan audit. Terminal
  read-only harus ditolak di server sekalipun client mengirim frame manual.
- Semua operasi jaringan punya deadline, batas output dan sumber daya. Cache
  inventaris bukan otoritas mutasi. Tidak boleh ada input terminal yang diputar
  ulang otomatis setelah gangguan.
- Jangan simpan terminal input/output, password, token, key, payload hook mentah,
  atau isi `.env` ke log/audit/browser persistence/memory yang masuk Git.
  Jangan salin hostname/IP tailnet pribadi pengguna ke dokumen/contoh publik.
- Uji memakai database, key dan socket tmux sementara milik test. Jangan memakai
  key GitHub pribadi sebagai credential host atau menyentuh tmux pengguna untuk
  smoke test. Jangan mengubah Tailscale/host sungguhan tanpa kebutuhan yang jelas.
- Jalankan `make check`, `make test`, `make build` untuk irisan backend/UI; checks
  tambahan sesuai risiko. Bedakan unit/SSH test server, tmux lokal, hosted CI,
  host nyata dan HP nyata. Jangan klaim bukti yang belum dijalankan.
- Sebelum checkpoint: periksa diff, update task status + sisa pekerjaan spesifik,
  catat hasil/command/commit/CI, commit/push pekerjaan yang diotorisasi. Jika
  berhenti di tengah, tulis file/fungsi aktif dan perintah berikutnya di bawah.
- Jangan mendelegasikan ke subagent tanpa permintaan pengguna atau instruksi
  yang berlaku. Permintaan serah-terima ini tidak meminta delegasi paralel.

## Lingkungan dan cara menjalankan

Go 1.27.3 tersedia lewat Homebrew; Node 24, npm, tmux 3.8 dan Tailscale tersedia
di mesin macOS pengembangan. CI Linux memakai Go 1.27.3 dan Node 24. SQLite dan
secret berada di `.data/` (diabaikan Git); jangan hapus untuk mengulang bootstrap.
Ada `.env` lokal berisi konfigurasi pengguna; jangan commit atau menyalin isinya.

Hub **belum membaca `.env` otomatis**. Untuk dev lokal, gunakan origin browser
`http://127.0.0.1:5173`, listen `127.0.0.1:8080`, dev `true`. Di terminal backend:

```sh
set -a
source .env
set +a
make dev
```

Terminal frontend: `npm run dev --prefix apps/web`. Buka URL 5173 di atas.
`make dev` sekarang memaksa Origin 5173 dan DEV=true; Vite mem-proxy ke 8080.
Hot reload hanya frontend; backend Go harus di-restart setelah perubahan.
HTTPS Tailscale + DEV=true belum didukung konfigurasi sekarang. Untuk produksi,
pakai binary, origin HTTPS dan DEV=false; jangan melemahkan Origin check.

## Bukti verifikasi dan batasnya

- Kode `33466f0`: [CI Linux sukses 42206312482](https://github.com/xibitz/tmux-monitor/actions/runs/42206312482).
  Workflow memasang tmux, menjalankan format/vet/TypeScript, `go test -race ./...`,
  build web/backend, cross-compile Linux tanpa CGO dan unggah artifact binary.
- Lokal macOS: `make check test build` lulus; SSH protocol server dengan key
  sementara + tmux nyata pada socket terisolasi lulus. Kasus: no server, server
  tanpa session, rename, split, close window, restart/ID reuse, pin mismatch,
  server multi-key, RSA SHA2, tailnet-IP guard/fallback, timeout/cancellation,
  output limit, ACL/CSRF, stale cache, revoke-in-flight, corrupt/cross-host cache,
  audit rollback dan migrasi 002 yang mempertahankan data 001.
- Browser fixture lulus: login, metadata SSH, probe fresh lalu timeout/stale,
  pohon session/window/pane dan timestamp snapshot tetap. Pada viewport 393 px,
  document/dialog tidak overflow horizontal; console tidak melaporkan error/warn.
  Hasil SSH di fixture UI disimulasikan; HTTP/login/CSRF/cache nyata. Protokol
  SSH dan tmux nyata diuji terpisah. Fixture tidak ikut binary production.
- `scripts/tmux-spike.py` dan ADR 0001 membuktikan ukuran tetap untuk desktop
  normal + viewer read-only/ignore-size di tmux 3.8 macOS. Jika semua klien
  ignore-size, ukuran dapat berubah. Irisan T12 sekarang menolak otomatis sizing, mensyaratkan tmux >=3.7
  dan manual global/semua window; spike tambahan tanpa desktop lulus lokal.
  Matriks versi luas dan perangkat nyata tetap terbuka.
- Belum ada uji ke host/key SSH pengguna, sshd host tailnet nyata, Agent,
  HP Android/iOS, live Serve/Grants negative access atau gate terminal P0.
  CIDR guard bukan pembuktian routing/keanggotaan Tailscale; lihat ADR 0002.

## Peta kode dan keputusan yang penting

- `internal/connector/ssh/credentials.go`: private key owner-only, public pin dan
  expected fingerprint, file reference, rotasi/revoke dan audit lokal. Key tanpa
  passphrase; password auth, ssh-agent, host certificate dan import known_hosts
  belum tersedia. RSA hanya SHA2, algoritme pin dipilih eksplisit.
- `internal/connector/ssh/connector.go`: resolve sekali/validasi seluruh IP lalu
  dial langsung, maksimal empat request global/satu per host, total 10 detik,
  3 detik per TCP attempt, stdout 1 MiB/stderr 16 KiB per command, tutup koneksi.
- `internal/tmux/inventory.go`: katalog command read-only, quoting, q: metadata,
  delimiter fingerprint printable, parser native IDs, epoch dan revision.
- `internal/httpapi/ssh.go`: metadata admin-only, probe + audit, GET inventaris
  dengan tmux:read, cache stale yang tetap memakai timestamp lama. Hasil dari
  credential yang sudah direvoke/dirotasi tidak boleh mengisi cache kembali.
- `internal/storage/migrations/002_ssh_observation.*.sql`: credential reference,
  observasi dan inventory_cache. Runner migrasi mendukung nama bernomor umum.
- `cmd/hub/main.go`: CLI `ssh-configure` / `ssh-revoke`. Detail host menampilkan
  template command; `README.md` menjelaskan onboarding dan upgrade.
- `apps/web/src/main.tsx`: detail metadata/probe, state observasi terakhir,
  pohon inventaris dan label stale. Capabilities PTY/control/independentPane
  tetap false pada inventaris umum: kelayakan PTY read-only diperiksa fresh saat
  membuka viewer, bukan dijanjikan dari cache.
- `internal/httpapi/browser_fixture_test.go`: fixture browser opt-in, hanya test.
  Jalankan sesudah `make build`:

  ```sh
  TMUX_MONITOR_BROWSER_FIXTURE=1 go test ./internal/httpapi -run '^TestBrowserFixture$' -count=1 -timeout=6m -v
  ```

  URL localhost:18081, akun/kata sandi fixture ada di server_test.go; bukan akun
  pengguna. Probe pertama fresh, berikutnya timeout/stale. Maksimum lima menit;
  hentikan setelah selesai. Jangan mengganti fixture dengan host/key pengguna.

Temuan yang telah diperbaiki dan dilindungi regresi: embedding bytes.Buffer
memungkinkan io.Copy melewati limit (sekarang composition); tmux tanpa session
menormalisasi tab dan list target gagal (fingerprint printable + recheck kosong);
negosiasi multi-key harus dipersempit ke public pin; cache salah-host/korup dan
hasil in-flight dari credential lama tidak boleh ditampilkan sebagai fresh.

## Checkpoint T12 read-only (7 November)

Kode: `32af116` sudah dipush ke origin/main melalui SSH. CI Linux:
https://github.com/xibitz/tmux-monitor/actions/runs/42305690493
Run awal ini gagal pada fixture `new-window` setelah global window-size manual:
tmux bawaan Ubuntu 3.6a crash, cocok upstream tmux/tmux#4849. Tidak mengenai host
pengguna. Minimum viewer/spike dinaikkan dari 3.4 ke 3.7, regression test menolak
3.4/3.6/3.6a/3.6b. CI kini build rilis resmi 3.8 dengan checksum SHA256 tetap,
verifikasi selected binary sebelum suite. Hasil run perbaikan dicatat di bawah.
Percobaan reproduksi Docker dibatalkan karena unduhan image tidak maju; log
berhasil dibaca lewat sesi Chrome GitHub yang sudah terautentikasi. Tidak ada
container/data aplikasi pengguna yang dipakai. Jangan klaim test Docker lulus.
Cross-build Linux/amd64 CGO_ENABLED=0 lokal lulus.

Koreksi kompatibilitas: `6cdca59` sudah dipush melalui SSH; make check/test/build
dan spike lokal diulang dan lulus. CI perbaikan:
https://github.com/xibitz/tmux-monitor/actions/runs/42307000042
**CI Linux sukses**, termasuk make check/test/build, spike multi-klien tmux 3.8,
static Linux/amd64 build tanpa CGO dan artifact. Ini bukti kode `6cdca59`;
commit dokumentasi sesudahnya tidak mengubah implementasi.
Postman CLI lokal tidak mendukung `status`; verifikasi memakai kontrak lokal
dan suite Go/SSH/WS/browser, bukan hasil Postman yang diasumsikan.

Kontrak berada di docs/api-v1.md. Backend baru memakai schema 3 (kolom target
pada audit), dependensi coder/websocket 1.8.15, xterm 6.0.0 dan creack/pty 1.1.24
(PTY hanya untuk test server). **T12/T13 PARTIAL**, interactive, mobile keyboard,
real network switching dan gate Serve/Grants/HP belum dibuktikan.

- `internal/tmux/terminal.go`: fixed catalog policy/attach, native session IDs,
  fresh epoch, minimum tmux 3.7, global dan semua window manual; grid remote
  maksimal 500×200 + 5 status rows. Tidak mengubah opsi remote. Active window,
  ukuran/policy berubah => tutup viewer. Operator change dapat terjadi sebelum
  periodic check, jadi jangan menjanjikan invariant atomik terhadap aksi eksternal.
- `internal/connector/ssh/terminal.go`: reuse pin/address/user connection helper,
  setup timeout 10 detik, SSH PTY read-only/ignore-size, tidak punya Write/Resize,
  clear setup deadline hanya setelah verifikasi. Check policy bounded 2 detik,
  Close hanya menutup koneksi SSH. Fixture PTY nyata memakai socket/key sementara.
- `internal/httpapi/terminal.go`: POST ticket CSRF+Origin+tmux:read, one-use TTL30s,
  disimpan hashed dan bound login token/user/host/socket/keyref/epoch/session.
  Browser WS cookie+exact Origin+subprotocol tmux.v1 dan ticket; tanpa token URL.
  64 pending global/8 login; 8 viewers global/4 user/4 host; lifetime30menit.
  Binary v1 output header30bytes, generation1 + sequence + viewer16bytes;
  chunk32KiB, queue1MiB, frame incoming64KiB, write deadline2detik. Input, mode
  interactive dan resize ditolak; hanya terminal.detach diterima. No replay.
  Cek izin/kredensial dan SSH policy tiap2detik, masing-masing timeout2detik.
  Shutdown eksplisit menutup hijacked sockets sebelum database ditutup.
- `apps/web/src/TerminalViewer.tsx`: lazy xterm, read-only context/status,
  fixed remote grid dengan scroll di ponsel, buka ulang manual/newticket/reset,
  backlog renderer1MiB; disableStdin, handler OSC52 menolak clipboard, link
  activation noop, window operations disabled. Tidak ada localStorage/cache.
- CSP produksi tetap tanpa unsafe-inline. Nonce random per response disisipkan
  pada meta index.html; documentOverride memberi nonce pada style xterm.
  xterm6.0 memiliki precedence bug CoreBrowserService yang mengabaikan override
  untuk Viewport style. Wrapper synchronous open() memberi nonce pada style
  creation itu, lalu segera mengembalikan document.createElement; override tetap
  dipakai renderer. Browser fixture menunjukkan ketiga style bernonce. Vite dev
  tetap memakai unsafe-inline tanpa nonce agar style injection HMR berjalan.
- `.env.example` default terminal false. README memuat panduan opt-in dan dampak
  window-size manual terhadap resize desktop. `.env`/data/proses pengguna tidak
  diubah atau direstart. Tidak ada koneksi ke host nyata pengguna dalam test.

Bukti checkpoint lokal:
- `make check test build` lulus (race suite termasuk real SSH+PTY/tmux, ticket
  Origin/CSRF/reuse/expiry/login binding, input/mode/resize rejection, framing,
  revoke login/ACL/key/host, detach, queue overload, kapasitas/shutdown, migrasi3).
- `python3 scripts/tmux-spike.py` lulus tmux 3.8 macOS: manual tanpa desktop,
  dua window beda ukuran, PTY resize/window switch/detach mempertahankan ukuran;
  read-only memblokir new-window. Workflow Linux juga menjalankan spike ini.
- Browser production build + CSP melalui test-only fixture, viewport393×852:
  login/probe/open/output, putus SSH simulasi, buka ulang read-only, link noop;
  pageWidth393, dialog359, scrolling hanya di remote grid, tiga style nonce,
  console tanpa error/warn. Screenshot fixture (bukan host nyata):
  `/Users/yasho/.codex/visualizations/2026/11/06/01a1afc8-c2a7-70c3-9eba-3da9aa5e9063/terminal-readonly-mobile.jpg`.
  Jalankan fixture sesudah build dengan `TMUX_MONITOR_BROWSER_FIXTURE=1
  TMUX_MONITOR_BROWSER_TERMINAL=1 go test ./internal/httpapi -run
  '^TestBrowserFixture$' -count=1 -timeout=6m -v`. Localhost18081, akun test-only
  server_test.go. SSH/output disimulasikan, WS/login/CSRF/SQLite/CSP nyata; viewer
  fixture ditutup setelah 10 heartbeat agar manual reopen bisa diuji.

## Checkpoint T12 interactive (7 November)

Implementasi melanjutkan baseline `fb4d840`. Status tetap **T12/T13/T31 PARTIAL**:
fungsi kode dan fixture telah tersedia, sedangkan gate perangkat/Serve-Grants
nyata, keyboard/modifier lengkap dan network switching belum selesai.

Langkah yang sudah dikerjakan:
1. Kontrak diperluas **sebelum kode** di docs/api-v1.md. Mode menggunakan REST
   `POST /terminal/viewers/{id}/mode-tickets` (Origin+CSRF+owner login), bukan
   mutasi flag in-band. Izin `tmux:read` dan `terminal:write` diverifikasi untuk
   interactive. Default `/terminal/tickets` menolak field mode dan tetap readonly.
2. `internal/httpapi/terminal_mode.go`: satu transisi per viewer, audit intent
   sebelum detach, cancel/wait cleanup viewer lama, revalidasi akses dan tiket
   baru15detik/sekali pakai. Login token + host/socket/keyref/epoch/session + mode
   terikat ke tiket. Viewer ID baru/generation1 menjadi namespace input baru;
   tidak perlu menaikkan generation pada socket yang sama. Mode failure tidak
   replay input; buka ulang generik kembali readonly. Mode API tidak membuat
   kredensial persisten atau menyentuh flag client desktop.
3. `internal/connector/ssh/terminal.go`: `OpenInteractiveTerminal` mengembalikan
   `InputTerminal.WriteInput(ctx,bytes)`, sedangkan `OpenTerminal` tidak punya
   writer. Katalog interactive attach hanya `ignore-size`; sizing/version/pin
   dan setup policy sama. Prefix katalog `tty` memberi identitas PTY yang
   dikonsumsi connector (maksimal128byte), bukan dikirim ke renderer. Klien pada
   tty itu harus tetap berada di native session terpilih; fresh list-clients
   dicek saat attach dan periodic Check. Explicit switch-client ke session lain
   terdeteksi walaupun session asal masih hidup. Write maksimal16KiB, deadline2detik, short/error write
   menutup koneksi tanpa retry. Tidak ada metode resize viewport.
4. PTY memakai raw modes (ECHO/ICANON/ISIG/IEXTEN/CR mapping/flow control off),
   supaya Ctrl+C melewati outer tty ke pane. Fixture awal menangkap Ctrl+C yang
   mengenai tmux client sebelum siap; fixture kini memvalidasi request modes dan
   menyiapkan raw slave sebelum child dijalankan. Real SSH+PTY/tmux test lulus
   tiga kali: Unicode/Tab/Enter masuk byte-exact ke cat test, attach/detach
   mempertahankan kedua ukuran, Ctrl+C memutus cat yang dituju, window lain dan
   session tetap ada. Semua pada socket/key/data sementara, bukan host pengguna.
5. Gateway membaca input hanya pada mode interactive: header30bytes arah1,
   generation1, viewer raw16bytes, sequence terpisah dari output mulai1. Maksimal
   payload16KiB, antrean8frame (128KiB), bucket64KiB/s burst32KiB. Cek sesi/read/
   write ACL/keyref **setiap frame** sebelum SSH write; policy/epoch juga tetap
   dipoll. Input mismatch/gap/replay/overflow/error => close, tidak retry. Audit
   hanya metadata mode/target/alasan; hasil open interactive ditandai terpisah.
6. UI `TerminalViewer.tsx`: konfirmasi Aktivasi, mode interaktif + tombol kembali
   readonly, input Unicode/onBinary, tombol awal Esc/Tab/Ctrl+C/panah/Enter.
   Mode ticket hanya dipakai sekali dan terikat scope host/session/epoch pada
   state; ordinary reopen/scope change menggunakan readonly ticket. Byte lama
   tidak diantrekan untuk reconnect. Input dipause saat transisi. Clipboard
   multiline <=16KiB ditinjau/dikonfirmasi, pending paste dibuang saat close/mode
   change. Fokus dipindah ke pembatalan paste; setelah confirm kembali ke terminal.
   Nonce CSP, OSC52 block, link noop dan window operation block tetap berlaku.
   Header konteks/status/mode menempel saat modal digulir pada layar kecil.
7. Suite Go menambah bukti: owner login (termasuk login lain user sama), CSRF/
   Origin, concurrent transition, permission grant + per-frame revoke, frame
   viewer/direction/generation/sequence, replay, failure-no-retry, flood bounded,
   audit failure tidak detach/activate. `make check test build` lokal lulus;
   suite final diulang sesudah binding TTY dan seluruh check/test/build lulus.
   Test tambahan flood
   dan audit failure -race lulus. CI source terbaru masih perlu dicatat di bawah.
8. Browser fixture production-CSP telah menguji read-only → interactive, keyboard
   Unicode, Enter/Ctrl+C, paste sebelum confirm tidak muncul pada output dan
   setelah confirm kedua baris muncul, downgrade readonly menghasilkan textarea
   readonly dan output/input lama hilang. Viewport393px: page393, dialog359,
   ketiga style nonce, tanpa console error/warn. Screenshot final dan CI dicatat
   setelah checkpoint. Screenshot final:
   `/Users/yasho/.codex/visualizations/2026/11/06/01a1afc8-c2a7-70c3-9eba-3da9aa5e9063/terminal-interactive-mobile.jpg`.
   Ini output echo simulasi UI; protokol SSH/PTY nyata diuji
   terpisah pada test socket. Bukan bukti HP fisik atau host nyata.

## Titik lanjut tepat

1. Baca hasil CI/commit checkpoint interactive terbaru. Cek git status dan
   perubahan pengguna. `.env` dan runtime pengguna tidak diubah/restart.
2. Lengkapi negative access Serve/Grants dan device matrix T01/T03. Minimum
   tmux3.7 tetap wajib; default manual pada tmux3.6a bisa crash. Jangan merendahkan
   gate minimum untuk membuat terminal bekerja di host lama; lihat ADR0001.
3. T31: modifier Ctrl/Alt/Shift, prefix tmux, keyboard fisik/IME Android+iOS,
   clipboard sensitif dan aksesibilitas perangkat nyata. Toolbar saat ini baru
   subset awal, bukan klaim T31 DONE. Input membagi session dengan desktop.
4. T13: uji real network switch/sleep/hub restart dan error taxonomy final;
   mode activation/reopen harus tetap readonly dan tanpa replay. Input yang
   sudah in-flight pada waktu revoke/disconnect mungkin sudah diproses remote;
   jangan menjanjikan undo/zero input secara retroaktif.
5. T20: kontrak epoch/revision/idempotency untuk mutasi tmux, revalidasi target,
   konfirmasi kill dan audit sebelum menjalankan operasi. Tidak ada raw shell
   administratif/kill-server API. T21+ setelah kontrak T20 selesai.
6. Ikuti aturan implementasi di atas; perbarui memory/tasklist/CI dan push SSH.

Catatan onboarding: target-host.pub harus disediakan operator; bukan dibuat oleh
ssh-keygen -lf. Endpoint Tailscale SSH bisa menyajikan key lain dari OpenSSH
/etc/ssh. Verifikasi dari metadata `sshHostKeys` node terautentikasi dan cocokkan
key koneksi. Scan jaringan saja bukan sumber kepercayaan. Tidak ada public pin,
hostname atau fingerprint pribadi pengguna yang dicatat dalam repositori.

## Upgrade runtime setelah checkpoint ini

Backend pengguna tidak direstart atau dimigrasi oleh agent pada pekerjaan ini.
Untuk memakai source terbaru: hentikan backend lama, backup data penting, muat
.env di terminal backend, lalu jalankan kembali `make dev` (atau build/binary
baru). Startup menjalankan migrasi 003 secara otomatis dan mempertahankan data
001/002. CLI SSH harus memakai TMUX_MONITOR_DATA_DIR yang sama. Frontend Vite tetap
pada 5173; backend 8080 untuk konfigurasi dev yang sekarang didokumentasikan.

Terminal SSH eksperimental tersedia dengan opt-in dan aktivasi input terpisah;
Agent belum tersedia. Tidak ada proses tmux/host pengguna
atau credential nyata yang dipakai selama pengujian. Temp browser fixture sudah
dihentikan; proses pengembangan pengguna dibiarkan berjalan untuk mereka restart.
