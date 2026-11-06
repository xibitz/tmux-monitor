# Memory dan serah-terima implementasi

Terakhir diperbarui: 6 November 2026. Baca file ini sebelum melanjutkan pekerjaan.
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

Yang sudah tersedia: hub localhost-only, SQLite WAL/migrasi 001–002, bootstrap
admin, Argon2id/cookie sesi, ACL/CSRF/Origin, profil host, audit, UI responsif,
allowlist SSH, Makefile/CI, **key store + CLI SSH configure/revoke, public host
key pinning, probe dan inventaris tmux dengan epoch/revision serta cache stale**.

Checkpoint kode terbaru: `33466f0` (SSH/inventaris); dokumentasi awal handoff:
`b7b8c1c`. Sebelumnya `1d8c1cf` (UX allowlist), `5c99762` (fondasi), `a879f8c`
(rancangan). Commit dokumentasi sesudah kode tidak mengubah implementasinya.

**T00, T10, T11 DONE. T02/T03/T32 PARTIAL. Task lain TODO. Berikutnya T12.**
DONE pada T10/T11 berarti acceptance kode/automated tests yang dicatat telah
lulus, bukan bukti sshd/tailnet/HP pengguna sudah diuji. Terminal dan Agent belum
tersedia; M1 dan gate rilis P0 belum selesai. Jangan aktifkan terminal sebelum
kebijakan ukuran dan gate akses dipenuhi.

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
4. **Terminal SSH (T12–T13).** Selesaikan kebijakan ukuran tanpa desktop, baru
   buat PTY, upgrade WebSocket terautentikasi, framing/version/nonce, buffer
   bounded dan xterm.js. Read-only default ditegakkan server + tmux. Interactive
   perlu izin dan aktivasi per viewer. Reconnect reset read-only, tanpa replay.
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
  ignore-size, ukuran dapat berubah. Minimum versi dan kondisi tanpa desktop
  masih perlu diselesaikan pada T02 sebelum T12 diaktifkan.
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
  selalu false karena fitur tersebut belum dibangun/dibuktikan.
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

## Titik lanjut tepat: T12

Catatan onboarding tambahan: `target-host.pub` adalah salinan public host key
yang harus disediakan operator; tidak dibuat oleh `ssh-keygen -lf` atau CLI hub.
Endpoint **Tailscale SSH** dapat menyajikan key lain dari OpenSSH `/etc/ssh`.
Verifikasi terhadap `sshHostKeys` metadata node Tailscale yang terautentikasi dan
cocokkan key koneksi; scan jaringan saja bukan sumber kepercayaan. Dukungan
terakhir hanya memverifikasi/menyimpan public pin pada lokasi operator, belum
menjalankan probe tmux terautentikasi ke host pengguna atau mengganti konfigurasi
hub. Tidak ada hostname/fingerprint pribadi yang dicatat di repositori.

1. Baca task T12/T13, ADR 0001/0002 dan FR-04/06/09. Pastikan git status dan
   konfigurasi runtime aktual; jangan berasumsi proses backend memakai source
   terbaru hanya karena Vite melakukan hot reload.
2. Selesaikan spike ukuran tanpa desktop, beberapa window dengan ukuran berbeda,
   dan cara mengaktifkan/menonaktifkan flag read-only pada client tmux yang tepat.
   Tetapkan capability/minimum versi dan fallback aman; jangan membiarkan viewport
   HP otomatis mengubah ukuran remote. Catat hasil sebelum membuka PTY.
3. Lengkapi negative access Serve/Grants dan matriks host nyata yang tersedia.
   Key dan public host fingerprint harus dipasang operator melalui CLI lokal.
   Tidak ada key/izin yang boleh ditebak atau diambil dari `.ssh` secara otomatis.
4. Tulis kontrak terminal/WS dan nonce satu pakai yang diikat ke sesi login,
   host/socket/epoch/session serta versi protokol. Tinjau middleware `/ws/`
   sekarang: header CSRF REST belum menjadi mekanisme upgrade browser yang siap.
   Jangan melompati cookie/Origin/ACL untuk membuat WebSocket berhasil.
5. Tambahkan PTY SSH, framing/sequence, limit frame/buffer/viewer, xterm.js,
   read-only default pada gateway DAN tmux. Input/resize perlu izin eksplisit;
   reset read-only saat reconnect/berpindah host. Jangan replay input ambigu.
6. Buktikan close, disconnect, timeout dan restart hub hanya detach, bukan kill;
   pencabutan sesi login/izin/key harus menutup viewer yang terkait;
   viewer lambat tidak menahan buffer tak terbatas; OSC52/URL berbahaya tidak
   otomatis aktif. Setelah itu barulah implementasikan interactive toggle.
7. Jalankan contract/integration/browser/failure tests yang relevan dan CI;
   perbarui MEMORY.md + tabel/checkbox. T12/T13 tetap TODO sampai gate terpenuhi.

## Upgrade runtime setelah checkpoint ini

Backend pengguna tidak direstart atau dimigrasi oleh agent pada pekerjaan ini.
Untuk memakai source terbaru: hentikan backend lama, backup data penting, muat
.env di terminal backend, lalu jalankan kembali `make dev` (atau build/binary
baru). Startup menjalankan migrasi 002 secara otomatis dan mempertahankan data
001. CLI SSH harus memakai TMUX_MONITOR_DATA_DIR yang sama. Frontend Vite tetap
pada 5173; backend 8080 untuk konfigurasi dev yang sekarang didokumentasikan.

Tidak ada terminal/Agent yang sudah siap. Tidak ada proses tmux/host pengguna
atau credential nyata yang dipakai selama pengujian. Temp browser fixture sudah
dihentikan; proses pengembangan pengguna dibiarkan berjalan untuk mereka restart.
