# Memory dan serah-terima implementasi

Terakhir diperbarui: 10 November 2026. Baca file ini sebelum melanjutkan pekerjaan.
Dokumen ini menyimpan keadaan pekerjaan; spesifikasi produk tetap di
[PRD.md](PRD.md), urutan task dan acceptance gate di
[IMPLEMENTATION_PLAN.md](IMPLEMENTATION_PLAN.md), kontrak API aktual di
[docs/api-v1.md](docs/api-v1.md).

**Instruksi pengguna terbaru: lanjut development tanpa testing.** Pengujian
otomatis/browser/CI ditunda sampai pengguna memberi arahan berbeda. Checkpoint
create/kill dan snapshot pane di bawah hanya source + format/review; build/compile juga tidak
dijalankan pada turn ini. Jangan memakai CI rename lama sebagai bukti kode baru.

## Tujuan dan keadaan saat ini

Mobile Tmux Manager adalah aplikasi Go + React untuk mengamati/mengelola tmux
melalui Tailscale, dengan transport SSH atau Agent yang dipilih per profil.
Pengguna meminta development diteruskan dan status ditulis agar agent baru bisa
melanjutkan tanpa mengandalkan riwayat chat. **Memory ini wajib diperbarui setelah
checkpoint, sebelum berhenti, dan sebelum context/token habis.**

Yang sudah tersedia: hub localhost-only, SQLite WAL/migrasi source001–007, bootstrap
admin, Argon2id/cookie sesi, ACL/CSRF/Origin, profil host, audit, UI responsif,
allowlist SSH, Makefile/CI, **key store + CLI SSH configure/revoke, public host
key pinning, probe dan inventaris tmux dengan epoch/revision serta cache stale**,
serta terminal SSH eksperimental (PTY/ticket/WS/xterm), default disabled dan
read-only saat buka. Interactive tersedia melalui izin + aktivasi per viewer.
Toolbar interactive kini memiliki modifier sekali pakai, kolom kombinasi dan
prefix control-key yang dapat dipilih lokal; keyboard fisik tetap melalui xterm.
Terminal browser terbaru memakai tmux.v2 dengan heartbeat dan lease15detik;
offline/putus tidak mengaktifkan input ulang. Hub tetap menerima legacy tmux.v1.
Rename session SSH/tmux3.8 eksperimental tersedia dengan flag mutasi terpisah,
konfirmasi, guard identitas dan ledger idempotensi persisten. Source create/kill
telah ditambahkan di belakang flag baru default-off; **belum diuji atau dikompilasi**.
Migrasi005 juga belum diuji; database/runtime pengguna belum diupgrade.
Source snapshot pane SSH read-only kini tersedia dengan flag terpisah default-off.
Belum diuji; tidak ada perubahan schema untuk irisan ini.
Source ubah nama window satu tautan dan buat window detached pada session di
luar grup tersedia untuk SSH dengan flag default-off. Statusnya belum diuji.
Source hapus window dan split/hapus pane SSH memakai flag layout terpisah,
default-off. Semua operasi T21 tersedia melalui SSH di source. Paritas Agent
menunggu T22–T24 dan gate pengujian tetap terbuka.
Source T22 menambahkan pairing, revoke, CLI Agent dan inventaris read-only
melalui WSS. Flag Agent default-off dan perlu Serve HTTPS. Schema006 belum diuji.
Source T23 menambahkan PTY Agent read-only dan interaktif eksplisit dengan
flag hub serta opt-in Agent. T23 belum diuji.
Source T24 menambahkan heartbeat, reconnect otomatis, fence proses/generation,
rekonsiliasi inventaris dan status Agent. T24 dan schema007 belum diuji.
Source T30/T33/T34 menambahkan PWA dan shell offline, paket service dan
backup/restore, serta matriks gangguan dan alat ukur kapasitas. Semua gate
penerimaan untuk checkpoint tersebut masih terbuka.

Baseline sebelum irisan T12: `33466f0` (SSH/inventaris); dokumentasi awal handoff:
`b7b8c1c`. Sebelumnya `1d8c1cf` (UX allowlist), `5c99762` (fondasi), `a879f8c`
(rancangan). Commit dokumentasi sesudah kode tidak mengubah implementasinya.

**T00, T10, T11 DONE. T02/T03/T12/T13/T20/T21/T22/T23/T24/T30/T31/T32/T33/T34 PARTIAL. Task lain TODO.
Berikutnya paritas mutasi Agent T20/T21; gate testing T20–T24 dan perangkat tetap terbuka.**
DONE pada T10/T11 berarti acceptance kode/automated tests yang dicatat telah
lulus, bukan bukti sshd/tailnet/HP pengguna sudah diuji. Konektor Agent memiliki source inventaris dan PTY; M1 dan gate rilis P0 belum selesai. Terminal memerlukan
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

## Checkpoint sebelumnya: T12 read-only (7 November)

Bagian ini adalah baseline sebelum interactive. Keadaan terbaru dan langkah
lanjut berada pada checkpoint interactive di bawah.

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

Kode `7379720` sudah dipush melalui SSH. CI: https://github.com/xibitz/tmux-monitor/actions/runs/42315991987
Run7379720 gagal pada startup PTY runner Ubuntu; diagnosis113610a menunjukkan
metadata path TTY tanpa pemisah LF sebelum escape tmux. Koreksi berikutnya
menggunakan frame NUL + fixed ready ack sebelum tmux dijalankan. Frame ini
dikonsumsi connector; bukan input browser dan bukan output renderer. Tidak
mengandalkan newline/flush terminal. Koreksi ada di `949f10b` dan **CI Ubuntu sukses**:
https://github.com/xibitz/tmux-monitor/actions/runs/42317484080
Suite -race, tmux 3.8 SSH+PTY, input/ACL/no-replay, spike ukuran, build web/hub,
static Linux/amd64 tanpa CGO dan artifact lulus. Catatan hasil akhir sempat
tertahan oleh kegagalan review otomatis akibat batas penggunaan; bukan
penolakan keamanan atas perubahan. Pengembangan berikutnya melanjutkan T31.
Uji tambahan Linux lokal: container baru dari image resmi yang sudah tersedia,
tmux 3.8 dibangun dari tarball resmi/checksum yang sama; fixture SSH/PTY lulus
(CGO=0 tanpa race). Tidak ada data/service/container pengguna yang dipakai.
Fixture browser sudah dihentikan dan tab/viewport pengujian dibersihkan.
Runtime dan data pengguna tidak direstart/diubah.

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
   dan setup policy sama. Prefix katalog menangkap `tty` melalui command substitution (bukan output
   tty langsung), mencetak metadata berakhir NUL dan menunggu fixed `ready` ack
   sebelum exec tmux. Connector mengonsumsi frame (maksimal128byte); metadata
   tidak dikirim ke renderer. Hal ini menghindari output yang di-flush saat
   startup tmux; readonly tidak pernah meneruskan input browser, hanya ack
   inisialisasi yang dikonsumsi shell read sebelum tmux berjalan. Klien pada
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
   dan audit failure -race lulus. CI source `949f10b` lulus seperti tautan di atas.
8. Browser fixture production-CSP telah menguji read-only → interactive, keyboard
   Unicode, Enter/Ctrl+C, paste sebelum confirm tidak muncul pada output dan
   setelah confirm kedua baris muncul, downgrade readonly menghasilkan textarea
   readonly dan output/input lama hilang. Viewport393px: page393, dialog359,
   ketiga style nonce, tanpa console error/warn. Screenshot final dan CI dicatat
   setelah checkpoint. Screenshot final:
   `/Users/yasho/.codex/visualizations/2026/11/06/01a1afc8-c2a7-70c3-9eba-3da9aa5e9063/terminal-interactive-mobile.jpg`.
   Ini output echo simulasi UI; protokol SSH/PTY nyata diuji
   terpisah pada test socket. Bukan bukti HP fisik atau host nyata.

## Checkpoint T31 toolbar (7 November)

Melanjutkan baseline `b8357a1`. Status **T31 tetap PARTIAL** karena perangkat
Android/iOS nyata, IME dan screen-reader belum dikualifikasi. Langkah implementasi:
1. Kontrak perilaku ditulis di docs/api-v1.md sebelum kode; tidak ada endpoint,
   migrasi atau perubahan izin. Guard gateway interactive tetap otoritatif.
2. `apps/web/src/terminalKeys.ts` mengodekan Ctrl/Alt/Shift untuk karakter tunggal
   dan tombol khusus. Unicode tanpa modifier tetap UTF-8; Ctrl/Shift toolbar
   dibatasi ASCII yang didukung. Input tidak valid ditolak tanpa byte. Panah,
   Home/End mengikuti DECCKM xterm; modifier cursor mengikuti CSI bitset legacy.
   Ctrl+C tetap ETX; prefix mengirim persis satu control byte dan tidak menyertakan
   perintah berikutnya. Tidak mengklaim dukungan Kitty/CSI-u.
3. `TerminalToolbar.tsx` menyediakan status/aria-pressed, reset modifier, kolom
   kombinasi (Enter setelah IME composition selesai), tombol khusus dan selector
   prefix. Modifier hanya berlaku pada aksi toolbar/kolom kombinasi dan reset
   setelah kirim berhasil. onData/onBinary/IME/paste xterm tidak ditransformasi.
   Tidak menyimpan input/output/clipboard di log atau browser persistence.
4. `TerminalViewer.tsx` memeriksa control alive/paused/interactive dan target scope
   sebelum aksi toolbar; cursor mode dibaca saat kirim. Reconnect/mode/target
   replacement membuang draft/modifier. Prefix default Ctrl+B, pilihan lokal
   di memory tampilan target, bertahan saat mode berganti, reset saat tampilan
   ditutup/host/session/epoch berubah. Tidak mengubah opsi tmux host. Binding
   hasil mode-ticket memakai scope control asal, bukan props yang mungkin berubah.
5. `apps/web/tests/terminalKeys.test.mjs` memakai Node test runner/strip-types,
   tanpa dependency tambahan. Lima kelompok uji mencakup C0/NUL/DEL, Alt/Unicode,
   Shift ASCII, invalid/surrogate/multi-character, DECCKM + semua bit modifier,
   tombol tetap dan prefix satu byte. `make test` kini menjalankan matriks ini
   setelah Go -race; CI otomatis ikut menjalankan. README berisi cara pakai.
6. `make check test build` lokal lulus (tmp GOPATH/GOCACHE seperti lingkungan di
   atas): format/vet/TypeScript, Go -race termasuk SSH+PTY/tmux nyata terisolasi,
   lima kelompok Node tests, Vite dan hub build. Browser fixture production-CSP
   pada 393x852 lulus: readonly tanpa toolbar; aktivasi; Ctrl+c menghasilkan ^C,
   Alt+x/Shift+a/prefix Ctrl+A sesuai output echo fixture; ketikan biasa tetap
   utuh walau modifier toolbar aktif. Multi-character ditolak, reset mode/draft
   dan prefix setelah close/reopen benar. Lebar halaman393, semua kontrol44px,
   tiga style memiliki atribut nonce, console tanpa warning/error. Fixture
   memiliki penanda control byte khusus test; bukan rekaman input produksi.
   Screenshot:
   `/Users/yasho/.codex/visualizations/2026/11/06/01a1afc8-c2a7-70c3-9eba-3da9aa5e9063/terminal-toolbar-mobile.jpg`.
   Tab/viewport dan fixture sudah dibersihkan. Runtime/.env/data/key/host pengguna
   tidak diubah. Bukti HP fisik/IME/tailnet nyata tetap belum ada.
7. Source **`db29894`** sudah dipush melalui SSH. **CI Ubuntu sukses**:
   https://github.com/xibitz/tmux-monitor/actions/runs/42354512699
   make check/test (termasuk Node toolbar), SSH+PTY/tmux 3.8, sizing spike,
   web/hub build, static Linux/amd64 tanpa CGO dan upload artifact lulus.
   Commit dokumentasi sesudahnya hanya merekam bukti/checkpoint ini.

## Checkpoint T13 connection liveness (7 November)

Baseline `0bfd504`. **T13 tetap PARTIAL**: koneksi sehat/macet dan restart instance
hub diuji terisolasi; Wi-Fi/seluler, sleep perangkat dan Serve/Grants nyata belum.
Langkah implementasi dan aturan untuk kelanjutan:
1. Kontrak ditulis di docs/api-v1.md sebelum kode. Browser memakai `tmux.v2` +
   ticket sekali pakai; hub tetap menerima v1 tanpa JSON heartbeat. Header binary
   tetap version1/sequence terpisah seperti sebelumnya. Web baru perlu hub baru;
   rebuild/restart backend bersama asset baru, jangan mengubah aturan Origin/ACL.
2. `internal/httpapi/terminal.go` mengirim native ping tiap5detik untuk v2,
   timeout5detik, menutup viewer dengan heartbeat_timeout jika pong hilang.
   Setelah pong, channel bounded1 mengantre heartbeat JSON ke writer utama;
   viewerId/generation terikat, tanpa PTY content/input/sequence increment.
   Tidak membuat endpoint baru, credential, migrasi, retry atau resize. Interval
   server pada server.go internal saja, bukan knob produksi untuk melemahkan cek.
3. `apps/web/src/terminalLiveness.ts`: lease15detik, heartbeat valid dapat
   memperpanjang hanya lease yang belum kedaluwarsa. Queued messages sesudah
   sleep tidak dapat revive. Clock rollback fail-closed. TerminalReplacement
   mengikat respons mode ke satu attempt15detik dan permanen invalid setelah
   offline/close; request retry tidak memperpanjang attempt lama.
4. `TerminalViewer.tsx` memeriksa lease sebelum input/mode, saat pesan diterima,
   timer1detik dan visibility return. Offline/pagehide membatalkan input dan
   pending replacement, termasuk gap mode_changed sebelum respons REST tiba.
   Kegagalan pertama dipertahankan terhadap onclose/onerror/pesan berikutnya;
   expected mode_changed boleh diganti kegagalan offline/timeout. Double mode
   request dicegah saat paused. Explicit user attempt baru memakai fence baru.
   Draft/paste/konfirmasi dibuang; online tidak reconnect/activate otomatis.
5. `terminal_liveness_test.go` membuktikan v1 kompatibel, v2 sehat walau PTY diam,
   heartbeat tidak merusak sequence output maupun input atau masuk SSH stdin,
   readonly tetap menolak input, missing pong detach+audit, ticket terpakai
   ditolak, fresh reopen readonly dengan viewer baru. Restart New(Server) memakai
   database/login/host persisten menolak pending ticket lama dan tidak membawa
   mode/input. Transport terminal di suite ini memakai pipe test; suite SSH/PTY
   nyata terisolasi tetap dijalankan terpisah, bukan klaim restart OS/host nyata.
6. `make check test build` final lulus: format/vet/TypeScript, Go -race termasuk
   SSH+PTY/tmux nyata, 13 kelompok Node tests (8 baru liveness/replacement), web
   dan hub build. Node clock tests menguji sleep/rollback, deadline sebelum timer
   callback, stopped lease dan respons aktivasi setelah offline→online.
7. Browser fixture production-CSP 393x852: idle interactive >15detik tetap sehat,
   input setelah idle diterima; navigasi keluar/kembali tidak mengaktifkan ulang.
   Fixture `TMUX_MONITOR_BROWSER_STALLED_HEARTBEAT=1` (test-only) menghentikan
   heartbeat sementara PTY readonly tetap mengalir tiap detik: browser timeout,
   stdin readonly, toolbar/draft hilang, manual reopen readonly dengan renderer
   bersih tanpa replay. Aktivasi/downgrade dan timeout diulang pada build final.
   Console tanpa warning/error, page393, tiga style memiliki atribut nonce.
   Screenshot:
   `/Users/yasho/.codex/visualizations/2026/11/06/01a1afc8-c2a7-70c3-9eba-3da9aa5e9063/terminal-connection-timeout-mobile.jpg`.
   Ini simulasi koneksi/echo UI, bukan uji fisik offline/sleep/bfcache atau tailnet.
8. Source **`dcffe28`** sudah dipush melalui SSH. **CI Ubuntu sukses**:
   https://github.com/xibitz/tmux-monitor/actions/runs/42356232678
   check/test (termasuk liveness/restart/13 Node groups), SSH+PTY/tmux3.8,
   sizing spike, web/hub build, static Linux/amd64 dan artifact semuanya lulus.
   Commit dokumen sesudahnya hanya mencatat hasil ini. .env/data/host/key dan
   proses pengguna tidak diubah/restart. Fixture/tab/viewport sudah dibersihkan.

## Checkpoint T20 rename (7 November)

Baseline `3681702`; T20 **PARTIAL**, hanya rename SSH/tmux3.8. Create/kill dan
paritas Agent belum tersedia. Kontrak docs/api-v1.md ditulis sebelum kode dan
keputusan/failure semantics tercatat di docs/adr/0003-session-rename.md.

Langkah implementasi:
1. `internal/tmux/inventory.go`: renameRevision terpisah dari tree revision,
   hash(epoch,id,name,created). Perubahan pane/process tidak menggagalkan rename.
   `rename.go`: new names ASCII `[A-Za-z0-9_][A-Za-z0-9_-]{0,63}` (tanpa titik),
   wajib native session ID/epoch/revision/confirmed. Quoting shell/format terpisah.
   Katalog if-shell -F non-yielding memeriksa pid/start/socket/version/id/created/
   name sebelum rename; tidak ada shell job atau raw-command API. Qualified3.8
   saja, terminal minimum3.7 tidak berubah. Hash tidak mendeteksi ABA nama identik.
2. `internal/connector/ssh/rename.go`: bounded shared observation/mutation gate,
   pin/address/user, fresh SSH snapshot, validasi expected epoch/renameRevision,
   callback fresh ACL/login/key sebelum dispatch. Single write attempt. Marker
   conflict/success dan duplicate error disanitasi; lost/invalid reply uncertain.
   In-flight remote operation tidak bisa dibatalkan secara retroaktif oleh revoke.
3. Migrasi004 `tmux_operations`: actor+hashed key, hashed body/target/credential,
   operation ID, status/outcome metadata. Reserve pending + audit intent + hapus
   cache dalam transaksi sebelum SSH. Completion + audit atomik; kegagalan
   completion tetap pending dan melarang dispatch ulang. Key/body berbeda409,
   pending/crash/restart409 uncertain, hasil done direplay setelah fresh akses.
   Tidak ada expiry/prune otomatis; global10000 catatan lalu429. Jangan menghapus
   ledger untuk memaksa retry. Downgrade4→3 menghapus perlindungan ledger; CLI
   perlu allow-data-loss dan operator harus backup/rekonsiliasi dahulu.
4. `internal/httpapi/session_rename.go`: GET host session-options, PATCH native
   session, Origin/CSRF/login + tmux:read dan tmux:write. Flag defaultfalse
   TMUX_MONITOR_MUTATIONS_ENABLED; `.env` pengguna tidak disentuh. Audit hanya
   actor/host/epoch/id/result, bukan nama/raw key/body. API tidak mengembalikan
   snapshot buatan; refresh inventaris sesudah hasil. Credentials rotate/revoke
   tidak bisa memakai hasil operasi lama untuk request baru.
5. `SessionRename.tsx`, main.tsx/api.ts/styles: tombol Ubah nama sesudah snapshot
   fresh; field + konfirmasi dampak semua klien; input/double submit guarded,
   per-intent random key dan tanpa retry. Sesudah error wajib refresh inventaris;
   draft dibuang. Snapshot baru remount intent; close tidak replay/undo. UI
   menonaktifkan mutasi pada versi tmux selain3.8. README/env.example berisi opt-in.
6. Lokal `make check` + `make test` + `make build` lulus; 13 Node groups, semua
   Go -race termasuk real SSH/PTY. Katalog tmux dan SSH rename menguji nama/path
   dengan format syntax/koma/brace/quote/backslash, duplicate, external rename
   tepat sebelum dispatch, revoke callback, epoch restart dan IDreuse. Suite API
   menguji auth/ACL/CSRF/Origin, invalid payload, same-key replay/concurrency,
   restart/pending, audit unavailable, completion fail, revoke dan quota10000.
   Migrasi4 rollback/reapply dan pending reservation startup lulus. Fixture dua
   session sempat memakai urutan nama sebagai target; sudah diperbaiki ke ID
   eksplisit pada test, seluruh suite ulang lulus. Tidak ada data pengguna diuji.
7. Browser fixture `TMUX_MONITOR_BROWSER_MUTATIONS=1`, production-CSP 393x852:
   save disabled sebelum checkbox, duplicate_name menutup draft/no retry,
   refresh sebelum intent baru, successful rename memperbarui snapshot; page393,
   console tanpa warning/error. SSH rename di fixture UI disimulasikan; protokol
   SSH/tmux nyata diuji terpisah di socket/key sementara. Screenshot hasil:
   `/Users/yasho/.codex/visualizations/2026/11/06/01a1afc8-c2a7-70c3-9eba-3da9aa5e9063/session-rename-mobile.jpg`.
   Fixture/tab/viewport sudah dibersihkan. .env/runtime/data/host pengguna tidak
   diubah atau direstart. Bukti perangkat/host tailnet nyata tetap belum tersedia.
8. Source **`99c0791`** sudah dipush melalui SSH. **CI Ubuntu sukses**:
   https://github.com/xibitz/tmux-monitor/actions/runs/42357657388
   make check/test/build, SSH+PTY dan rename tmux3.8 nyata terisolasi, migrasi4,
   idempotensi/ACL/restart/audit/quota, 13 Node groups, sizing spike, static
   Linux/amd64 tanpa CGO dan artifact semuanya lulus. Commit dokumen sesudahnya
   hanya merekam bukti ini. T20 tetap PARTIAL; lanjut create/kill dengan kontrak
   dan guard khususnya, bukan menganggap rename sudah memenuhi semua manajemen.

## Checkpoint T20 create/kill — BELUM DIUJI (7 November)

Baseline `c98b67f`. User meminta development tanpa testing. T20 tetap PARTIAL,
tidak ada klaim gate selesai. Tidak menjalankan go test/go build/go vet/tsc,
make check/test/build, npm test/check/build, browser fixture atau CI. Hanya membaca
source/dokumentasi, meninjau diff, gofmt dan Prettier write. Tidak menulis test
baru pada checkpoint ini. Commit memakai `[skip ci]`; workflow CI tidak diubah.

Langkah implementasi:
1. Kontrak ditulis sebelum kode di docs/api-v1.md; keputusan di
   docs/adr/0004-session-lifecycle.md. Flag baru
   `TMUX_MONITOR_SESSION_LIFECYCLE_ENABLED=false` harus aktif bersama MUTATIONS.
   Rename tetap memakai jalur dispatch lama. Config/.env.example diperbarui;
   .env pengguna tidak dibaca/diubah dan tidak ada backend direstart.
2. `internal/tmux/session_lifecycle.go` + inventory.go: createRevision cr_ hash
   epoch dan sorted(sessionid/name/created) tanpa window/process. Create hanya
   pada server tmux3.8 yang sudah aktif, native global -N mencegah bootstrap.
   Guard S: memeriksa jumlah dan identitas/nama setiap session; detached new-session
   default shell dengan ukuran awal120x40, tanpa command/env/cwd dari request.
3. Kill memakai full session.revision r_, bukan renameRevision rn_. Wajib
   confirmed dan confirmation tepat native ID ($N). Guard server+session dan
   W:/P: membership/name/index/active/dimensi/current-command lalu kill-session
   targetID saja, tidak ada kill-server. Linked windows mengikuti aturan tmux.
   Guard conjunction balanced untuk menghindari nesting dalam. Batas64 item per
   koleksi/serialized command64KiB; konstruksi guard juga dibatasi. Loop/nama
   khusus/empty-server/last-session behavior semuanya BELUM dieksekusi.
4. `internal/connector/ssh/session_lifecycle.go`: gate observation/mutation yang
   sama, connect pin/address/user, fresh snapshot/empty fallback, version3.8,
   callback revalidasi login/ACL/key tepat sebelum satu dispatch. Marker create
   mengembalikan native ID; lost/ambiguous reply operation_uncertain tanpa retry.
   Peninjauan sumber tmux3.8 man/arguments/format dilakukan, bukan bukti uji.
5. `internal/httpapi/session_lifecycle.go`: POST sessions butuh read+write, DELETE
   native session butuh read+kill, keduanya Origin/CSRF dan Idempotency-Key32hex.
   New lifecycle interface/Server wiring; GET session-options menambah izin
   canCreate/canKill/lifecycleEnabled. Executor baru terpisah dari rename: reserve
   pending+audit intent+invalidatecache, action-bound request hash, callback fresh
   credentials, completion+audit atomik, quota10000/no prune, pending tidak replay.
6. Migrasi005 result_target menambah metadata native ID pada ledger agar hasil
   create bisa direplay tanpa resolve nama atau mengeksekusi lagi. LatestVersion5.
   Replay status200/201 memerlukan result target valid; target hilang/tidakvalid
   tetap uncertain. Downgrade menghilangkan metadata hasil, bukan pemulihan retry.
   Audit tidak menyimpan nama/raw key/body. Schema4/rename reservation dipertahankan.
7. `SessionLifecycle.tsx` + main.tsx/api.ts: Buat session dan Hentikan session,
   konfirmasi dampak, typed nativeID untuk kill, no duplicate submit/no retry,
   clear draft dan wajib refresh setelah failure. Options flag off menyembunyikan
   UI; stale/tmux selain3.8/no server memblok create. DTO inventory createRevision
   ditambahkan. Tidak ada LocalStorage, output/input recording atau screenshot
   baru karena browser verification memang tidak dilakukan.
8. README, kontrak API, ADR0004 dan plan menjelaskan bahwa source create/kill dan
   migrasi005 belum diuji. CI sukses99c0791 hanya bukti baseline rename sebelumnya.
   Tidak ada operasi ke host/tmux/SSH pengguna, test fixture, perubahan .data,
   keys, Tailscale atau runtime. Source **`b9c8277`** sudah dipush melalui SSH,
   commit `feat: add gated session create and kill [skip ci]`. CI sengaja dilewati
   lewat marker tersebut; tidak ada hasil test/build/CI baru yang diklaim.
   Commit dokumen sesudahnya hanya mencatat checkpoint tanpa menjalankan testing.

Pekerjaan yang belum diverifikasi (jangan jalankan selama instruksi no-testing
masih berlaku): compile/TypeScript; regression rename/terminal; migrasi4/5 dan
create replay nativeID; concurrent/cross-action/pending/restart/auditfailure/revoke;
create empty existing server/no bootstrap/duplicate/session-set race; kill tree
race/typedID/lastsession/linkedwindow; nested format literals/limits; mobile UI,
focus/close/stale snapshot dan no-retry. Tabel acceptance pending ada di plan.

## Checkpoint T21 snapshot pane — BELUM DIUJI (10 November)

Baseline `6364837`. Instruksi user tetap lanjut tanpa testing. Source saja;
tidak menjalankan test, compiler/build, browser, atau CI. Formatting dan review
diff dilakukan tanpa menyentuh `.env`, `.data`, host, atau proses pengguna.

1. Kontrak ditulis dulu di docs/api-v1.md. POST read-only
   `/hosts/{id}/panes/{pid}/snapshot` memakai Origin/CSRF/login/`tmux:read`,
   native ID `%N`, `epoch` dan `pane.revision` dari inventaris. Tolak query atau
   field ekstra. Flag `TMUX_MONITOR_PANE_SNAPSHOT_ENABLED` defaultfalse dan
   independen dari terminal/mutasi. Options terminal menambah paneSnapshotEnabled.
2. `internal/tmux/pane_snapshot.go`: parse snapshot fresh, cocokkan epoch/revision,
   guard fingerprint server + window/pane ID, active/dimensi/current-command,
   lalu `capture-pane -p -S -100` di antrean yang sama. Tidak attach, select,
   resize, atau membuat paste buffer. Marker membedakan hasil kosong dan conflict.
   Guard64KiB, raw256KiB termasuk marker, hasil tail64KiB pada batas rune. Ganti UTF-8 rusak,
   buang control/bidi overrides, `truncated` hanya untuk clipping byte.
3. `internal/connector/ssh/pane_snapshot.go`: koneksi SSH terverifikasi yang sama
   untuk inventaris dan capture, gate 4 global/1 per host, deadline10detik,
   tmux3.8. Revalidasi izin/key sebelum capture. No stale fallback atau retry.
   Overflow raw ditolak tanpa isi, bukan mengembalikan data parsial.
4. `internal/httpapi/pane_snapshot.go`: fresh authorization juga sebelum hasil,
   audit intent/outcome metadata saja dan fail closed bila audit gagal. Response
   no-store; tidak memasukkan isi pane ke cache inventaris, SQLite atau log.
   Tidak ada migrasi baru. Schema005 dari baseline T20 masih belum diuji.
5. `PaneSnapshotViewer.tsx` + main.tsx/styles.css: buka dari pane inventaris
   fresh, capture hanya setelah klik, render plain text, waktu dan tanda potong.
   Clear saat capture baru/close/pagehide; respons terlambat diabaikan. Tidak ada
   polling, streaming, clipboard otomatis, atau browser persistence.
6. T21 tetap PARTIAL. Kualifikasi yang ditunda: build/TypeScript, route/ACL/CSRF,
   marker/kasus output limit/UTF-8, fresh guard pane-linked/epoch/resize/revoke,
   tmux actual tanpa ubah pane/paste buffer, privasi log/cache, UI mobile dan
   lifecycle request. Jangan jalankan selama instruksi tanpa testing berlaku.
   Window/pane mutations dan Agent parity belum. Source `02677dc` sudah dipush
   melalui SSH dengan `[skip ci]`; tidak ada hasil uji/CI baru yang diklaim.

## Checkpoint T21 ubah nama window (10 November)

Baseline `81089f0`. Status **BELUM DIUJI** sesuai instruksi pengguna. Pekerjaan
meliputi perubahan source, formatting dan review diff. Push memakai `[skip ci]`.
Commit source `bc3f7a3` sudah dipush ke `main` melalui SSH.

1. `PATCH /hosts/{id}/windows/{wid}` memakai Origin, CSRF, login, izin read/write
   dan kunci idempotensi. Body memakai session ID, nama baru, epoch, revision
   window dan konfirmasi. Flag `TMUX_MONITOR_WINDOW_MUTATIONS_ENABLED` default
   nonaktif dan bekerja bersama flag mutasi.
2. `internal/tmux/window_rename.go` memakai inventaris SSH baru dan guard antrean
   tmux 3.8. Target eksplisit memakai session ID dan window ID. Guard memeriksa
   fingerprint, nama, indeks, status aktif, pane, jumlah window dan duplikasi
   nama. Window dengan beberapa tautan ditolak. tmux menonaktifkan penamaan
   otomatis ketika rename berhasil.
3. Jalur SSH memakai ulang dispatch dengan marker hasil. Jalur HTTP memakai
   ulang ledger schema005 dan mengembalikan `windowId`. Audit menyimpan metadata
   operasi. Hasil ambigu memerlukan pemeriksaan inventaris sebelum permintaan baru.
4. `TmuxRename.tsx` dipakai untuk form rename session dan window. Tombol window
   tampil untuk window satu tautan dalam inventaris SSH. Scope ini dipilih untuk
   membatasi dampak lintas session.
5. Kualifikasi tertunda meliputi compile/build, regresi rename session dan
   lifecycle, guard tautan/duplikasi/revision pada tmux aktual, ledger/replay,
   izin, dan UI. T21 tetap PARTIAL. Berikutnya create/kill window, split/kill
   pane dan paritas Agent.

## Checkpoint T21 buat window (10 November)

Baseline `15f125c`, commit source `48b0bdd`. Status **BELUM DIUJI**. Hanya perubahan source,
formatting dan review diff. Commit checkpoint menggunakan `[skip ci]`.

1. `POST /hosts/{id}/sessions/{sid}/windows` dan opsi `canCreateWindow` memakai
   flag mutasi umum dan window. Endpoint menerima nama, epoch, revision tree
   session, konfirmasi, dan kunci idempotensi. Login, Origin, CSRF, izin read
   dan write serta verifikasi ulang binding SSH memakai jalur lifecycle.
2. `internal/tmux/window_create.go` mencocokkan revision session dengan
   inventaris SSH baru. Guard antrean tmux memeriksa fingerprint server,
   identitas session, status di luar grup dan daftar window (ID, nama, indeks).
   Nama duplikat ditolak dan batasnya kurang dari 64 window sebelum create.
   tmux 3.8 menyinkronkan window antar-session dalam satu grup, sehingga grup
   ditolak oleh guard. Katalog menjalankan `new-window -d`, memakai proses
   default host, dan meminta tmux mengalokasikan indeks. Hook host dapat berjalan.
3. Parser marker mengembalikan native `windowId` pada respons 201. Ledger
   schema005 menyimpan hasil untuk replay, dengan audit dan invalidasi cache
   melalui jalur yang sudah ada. Tidak ada migrasi atau perubahan format inventaris.
4. `TmuxLifecycle.tsx` berbagi form dengan operasi session. Tombol **Buat window**
   ada pada session di inventaris SSH. UI menjelaskan dampak pada daftar window
   bersama dan meminta pemeriksaan inventaris setelah hasil tidak pasti.
5. Kualifikasi ditunda. Periksa compile/build, target session dan alokasi
   indeks, duplikasi serta perubahan nama/indeks/grup sebelum dispatch, window
   tetap detached, hook dan hasil ambigu, marker 201 dan replay, izin/revoke
   serta UI. T21 tetap PARTIAL. Irisan berikutnya kill window, lalu split/kill
   pane dan paritas Agent.

## Checkpoint T21 layout SSH (10 November)

Baseline `48b0bdd`. Status **BELUM DIUJI**. Checkpoint ini menutup
implementasi source SSH untuk T21. Hanya formatting dan review diff.
Commit/push memakai `[skip ci]`.

1. `DELETE /hosts/{id}/windows/{wid}`, `POST /hosts/{id}/panes/{pid}/split`
   dan `DELETE /hosts/{id}/panes/{pid}` memakai
   `TMUX_MONITOR_LAYOUT_MUTATIONS_ENABLED` bersama flag mutasi umum.
   Opsi per host menampilkan kemampuan sesuai flag dan izin. Read+kill untuk
   hapus, read+write untuk split. Jalur login/Origin/CSRF, ledger idempotensi,
   audit, cache invalidation dan revalidasi binding SSH memakai lifecycle.
2. `internal/tmux/layout_mutations.go` memvalidasi native IDs, epoch, revision
   window, dan keanggotaan pane. Guard antrean memakai identitas server/session,
   state window/pane, jumlah yang relevan, satu tautan dan session di luar grup.
   Hapus window terakhir dan pane terakhir ditolak agar tidak menutup objek
   induk. Split/hapus pane menolak window yang sedang zoom. Split menerima enum
   berdampingan/bertumpuk dan memakai `split-window -d` tanpa argumen shell.
3. Marker hasil memakai `windowId` untuk hapus window dan `paneId` untuk
   split/hapus pane. API mengembalikan 201 untuk split dan 200 untuk hapus.
   Tidak ada perubahan schema atau format inventaris.
4. Form layout memakai alur yang sama dengan create/kill session. Hapus
   menampilkan nama/ID dan jumlah pane terdampak serta meminta ID yang persis.
   Split meminta arah dan menjelaskan dampak pada layout bersama. Setelah
   percobaan, pengguna harus memeriksa inventaris sebelum permintaan baru.
5. Kualifikasi tertunda meliputi compile/build, target compound tmux 3.8,
   link/group/zoom/last-item races, ACL/revoke/CSRF/Origin, marker dan hook,
   pending/replay, layout desktop, konfirmasi dan UI ponsel. Uji hanya setelah
   pengguna mengizinkan. T21 tetap PARTIAL karena Agent membutuhkan T22–T24
   dan suite kontrak kedua transport belum dijalankan.
6. Untuk paritas, gunakan tipe request T21 pada RPC `tmux.operation` yang
   diizinkan satu per satu. Agent membaca inventaris baru pada socket profil
   dan menjalankan guard lokal. Hub mempertahankan izin per aksi, ledger dan
   audit, mengarahkan operasi sesuai transport profil dan memperlakukan
   putus koneksi setelah dispatch sebagai hasil tidak pasti.

## Checkpoint T22 pairing dan inventaris Agent (10 November)

Baseline `bacea40`, commit source `e89126c`. Status **BELUM DIUJI**. Source, formatting dan review diff
saja. Tidak ada test/build/compile/browser/CI dan commit memakai `[skip ci]`.

1. `006_agent_pairing` menyimpan hash token dan credential. Admin menerbitkan
   token 10 menit untuk profil dan IP Tailnet. Redeem memeriksa akun OS dan
   socket profil, mengonsumsi token dan menerbitkan identitas dalam satu
   transaksi. Satu identitas aktif per profil. Revoke menghapus cache dan
   observasi, menutup koneksi yang cocok. Rotasi memakai revoke dan pairing baru.
2. `internal/httpapi/agent_enrollment.go` membedakan endpoint admin
   (login/Origin/CSRF) dan mesin (Bearer). Mesin harus datang via Serve HTTPS
   dengan header host/protokol/IP yang ditulis proxy loopback. Header Funnel
   ditolak. Sumber harus sama dengan IP yang dipasang admin. Asumsi trusted
   proxy perlu dikualifikasi pada Serve nyata sebelum rilis.
3. `internal/httpapi/agent_connection.go` memakai `tmux-agent.v1`, hello
   versioned, generation persisten yang ditetapkan hub dan satu koneksi
   aktif per profil. Hasil inventaris diperiksa lagi terhadap identitas
   dan generation di transaksi observasi sebelum cache ditulis.
4. `cmd/agent` menyediakan `enroll` dan `run` untuk Linux/macOS sebagai akun
   non-root pemilik tmux. Token dimasukkan lewat terminal; binding disimpan
   sebagai file baru mode 0600. Agent memverifikasi TLS hostname, menolak
   redirect, dan tidak membuka listener. Setup ada di README.
5. Satu RPC `inventory.snapshot` memakai deadline, request ID, generation,
   batas frame/output dan katalog tmux argv read-only. Hub memakai parser
   inventaris yang sama dengan SSH. UI Agent memiliki pairing, revoke dan
   probe; token hanya ditampilkan sementara. Tidak ada fallback SSH.
6. Kualifikasi tertunda meliputi compile, schema006 up/down, replay/expiry,
   race revoke/delete/generation, Serve/TLS aktual, limits, file dan UI.
   T23 tetap PTY Agent. T24 tetap heartbeat, reconnect/backoff, dan
   rekonsiliasi. Paritas operasi T20/T21 melalui Agent belum dikerjakan.

## Checkpoint T23 terminal Agent (10 November)

Baseline `e89126c`, commit source `2facade`. Status **BELUM DIUJI**. Hanya source, formatting dan review
diff. Commit/push memakai `[skip ci]`. Tidak ada perubahan schema.

1. Browser memakai tiket, ACL, audit, dan framing `tmux.v1`/`tmux.v2` yang sama.
   Tiket Agent mengikat identitas dan generation koneksi. Pengaturan memerlukan
   flag Agent, terminal, dan Agent terminal, serta `agent run --terminal`.
   Opsi terminal dan inventaris fresh menentukan tombol viewer di UI.
2. Hub menawarkan stream satu kali melalui koneksi Agent T22. Agent melakukan
   upgrade outbound ke `/ws/v1/agent/terminal` dengan bearer, IP Tailnet
   dan tiket stream singkat. Satu WebSocket per viewer membatasi dampak
   backpressure ke viewer itu. PTY tidak berbagi kanal dengan inventaris.
3. Agent membuka attach client pada PTY lokal Linux/macOS. Katalog argv,
   epoch, ukuran manual dan pemeriksaan tty memakai logika bersama tmux.
   Agent juga memeriksa `exit-unattached` dan `destroy-unattached` bernilai off.
   Mode read-only menolak input di hub, relay, dan tmux. Mode interaktif
   memakai PTY baru, konfirmasi detach, izin `terminal:write`, dan sequence
   input dengan ack setelah write. Tidak ada resize atau replay input.
4. Socket per viewer, revoke, atau pergantian generation melepas attach
   client. Hub meminta konfirmasi detach sebelum menerbitkan tiket mode baru.
   T24 tetap memiliki heartbeat/reconnect dan rekonsiliasi level Agent.
5. Kualifikasi tertunda meliputi compile, PTY poll/deadline Linux/macOS,
   Serve aktual, reuse/generation/revoke, parity frame SSH, ukuran dan
   input/control keys, ack ambigu, backlog, serta UI. Jangan tandai T23
   selesai sebelum suite T12 pada Agent diizinkan dan lulus.

## Checkpoint T24 lifecycle Agent (10 November)

Baseline `2facade`, commit source `3bc95b8`. Status **BELUM DIUJI**. Hanya source, formatting dan review
diff. Commit/push memakai `[skip ci]`.

1. `tmux-agent.v2` menambahkan `instanceId`, `previousGeneration`, dan interval
   heartbeat pada ACK. Agent lama masih diterima lewat v1 dan dipantau dengan
   ping/pong. Upgrade hub dulu sebelum Agent v2. Schema007 menyimpan instance
   terakhir tanpa menambah secret atau menyimpan heartbeat sebagai status live.
2. Agent v2 mengirim heartbeat bersequence dan versi tmux. Hub mengakui
   sequence. Default 15 detik, konfigurabel 5–60, lease tiga interval.
   Reconnect mengautentikasi ulang dengan backoff eksponensial dan jitter.
   Credential/IP yang ditolak atau instance usang menghentikan retry.
3. Proses yang sama mengganti socket sebelumnya dengan generation baru.
   Proses kedua tidak mengambil alih koneksi v2 yang sehat. Fence schema007
   mengizinkan proses yang sama pulih dari ACK hilang dan menolak generation
   lama setelah proses berbeda memperoleh profil.
4. Hub mengambil snapshot penuh setelah handshake, saat versi berubah, dan
   secara berkala. Cache lama hanya tersedia sebagai stale. Simpan hasil
   hanya jika identitas/generation masih aktif. UI menampilkan heartbeat,
   status `RECONNECTING`, dan stale saat inventaris berasal dari generation
   berbeda. Refresh daftar host di UI tidak melakukan probe SSH.
5. Tiket terminal Agent memerlukan inventaris fresh dari generation saat ini.
   Koneksi putus, revoke atau pergantian generation tidak memulihkan viewer
   atau mengulang input. Buka ulang tetap read-only secara eksplisit.
6. Kualifikasi tertunda meliputi compile, migrasi007, heartbeat lease,
   sleep/wake, koneksi ganda/ACK hilang, stale cache, revoke, PTY dan UI.
   Mutasi Agent untuk T20/T21 masih perlu implementasi.

## Checkpoint T30, T33 dan T34 (10 November)

Baseline `3bc95b8`. Status **BELUM DIUJI**. Hanya perubahan source, formatting
dan review diff. Commit/push memakai `[skip ci]`.

1. **T30.** Manifest standalone dan ikon Android/iOS di `apps/web/public`.
   Service worker hanya diregistrasi di build produksi HTTPS. Navigasi root
   memakai jaringan dengan fallback HTML offline statis. Cache dibatasi ke
   shell offline publik dan JS/CSS build immutable. HTML aplikasi dengan nonce,
   API, WebSocket dan data terminal tidak masuk Cache Storage. Dashboard
   melepas detail/viewer saat offline dan mengambil data kembali setelah online.
   Modal viewer mendukung fullscreen browser dengan fallback ukuran viewport.
2. **T33.** `make package` membungkus binary dan template hub systemd,
   Agent systemd user/launchd serta runbook. Instalasi dan pemulihan ada di
   `docs/operations.md`. `scripts/hub-backup.py` mengambil SQLite snapshot
   ketika hub dan writer lain berhenti, mengenkripsi DB dan secret SSH terpisah
   dengan age, dan mengikat pasangan via SHA-256 DB. Restore memeriksa
   integritas/ref dan hanya membuat direktori baru dengan key mode `0600`.
   Runbook memuat tindakan terhadap akses yang mungkin kembali dari backup.
3. **T34.** `docs/release-qualification.md` mendefinisikan failure matrix
   SSH/Agent, setup 10 host/50 session, kapasitas burst dan pencatatan bukti.
   `scripts/measure-capacity.py` mengukur GET daftar host/inventaris dan
   mengeluarkan P95 HTTP agregat dan per transport. Login hanya untuk membuat
   sesi lab sendiri. Ukuran browser, burst dan gangguan tetap diukur di lab.
4. Kualifikasi tertunda meliputi Android/iOS, cache/CSP dan fallback fullscreen,
   build dan paket service, clean install, backup/restore dari SQLite dengan WAL,
   rotasi/revoke, serta semua skenario kapasitas dan gate P0. Jangan jalankan
   pengujian sampai instruksi pengguna berubah. Deployment dan data pengguna
   tidak disentuh.

## Titik lanjut tepat

1. Baca hasil CI/commit checkpoint interactive terbaru. Cek git status dan
   perubahan pengguna. `.env` dan runtime pengguna tidak diubah/restart.
2. Lengkapi negative access Serve/Grants dan device matrix T01/T03. Minimum
   tmux3.7 tetap wajib; default manual pada tmux3.6a bisa crash. Jangan merendahkan
   gate minimum untuk membuat terminal bekerja di host lama; lihat ADR0001.
3. T31: uji keyboard fisik/IME Android+iOS, clipboard sensitif dan aksesibilitas
   screen-reader/fokus pada perangkat nyata. Modifier/prefix dan matriks byte
   sudah tersedia; bukan klaim T31 DONE. Input membagi session dengan desktop.
4. T13: uji real network switch/sleep/restart proses hub dan error taxonomy final;
   mode activation/reopen harus tetap readonly dan tanpa replay. Input yang
   sudah in-flight pada waktu revoke/disconnect mungkin sudah diproses remote;
   jangan menjanjikan undo/zero input secara retroaktif.
5. T20: source create/kill sekarang tersedia tetapi belum diuji. Tetap ikuti
   instruksi no-testing pengguna. Kualifikasi checklist di atas hanya bila
   pengguna mengizinkan testing kembali; jangan menandai selesai dari review
   source. Seluruh operasi T21 SSH sudah ditulis. Untuk paritas T21, gunakan kembali kontrak dan katalog
   operasi untuk transport Agent. Tidak ada raw-shell/
   kill-server API; ledger pending harus tetap retained setelah restart.
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
baru). Source startup kini menuju migrasi007 (BELUM DIUJI); jangan menyamakan
dengan baseline binary/CI schema004. CLI SSH harus memakai TMUX_MONITOR_DATA_DIR
yang sama. Frontend Vite tetap
pada 5173; backend 8080 untuk konfigurasi dev yang sekarang didokumentasikan.

Terminal SSH eksperimental tersedia dengan opt-in dan aktivasi input terpisah;
Pairing, inventaris dan PTY Agent tersedia di source. Tidak ada proses tmux/host pengguna
atau credential nyata yang dipakai selama pengujian. Temp browser fixture sudah
dihentikan; proses pengembangan pengguna dibiarkan berjalan untuk mereka restart.
