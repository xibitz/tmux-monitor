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

Baseline yang sudah ada: hub localhost-only, SQLite WAL, migrasi 001, bootstrap
admin, Argon2id, cookie sesi, ACL, CSRF/Origin, profil host, audit, UI responsif,
petunjuk allowlist SSH, Makefile dan CI. Commit baseline terakhir sebelum
checkpoint ini: `1d8c1cf` (sebelumnya `5c99762`, fondasi; `a879f8c`, rancangan).

**Pekerjaan aktif: T10 konektor SSH, lalu T11 inventaris.** Saat memory ini dibuat,
SSH/Agent dan terminal belum tersedia. Jangan menandai seluruh M1 selesai hanya
karena profil berhasil disimpan. T12 terminal dan T13 reconnect adalah gate
tersendiri. Semua task P0 yang belum selesai tetap wajib sebelum rilis aman.

## Tasklist dan langkah implementasi

Status lengkap per ID berada di tabel pelacakan implementation plan.
`DONE` = acceptance yang disebutkan sudah dibuktikan; `PARTIAL` = sebagian kode
atau uji tersedia; `IN_PROGRESS` = irisan aktif; `TODO` = belum dikerjakan.
Kotak task hanya dicentang setelah gate task tersebut benar-benar selesai.

1. **Fondasi (T00–T03).** Pertahankan fondasi yang sudah ada. Lengkapi matriks
   Linux/macOS dan perangkat nyata, uji Serve/Grants negatif, serta spike ukuran
   tmux. T00 selesai, T01 terbuka, T02/T03 sebagian. Lihat ADR 0001.
2. **SSH onboarding (T10).** Tetapkan kontrak; tambahkan migrasi credential
   reference dan cache observasi; konfigurasi private key melalui CLI lokal;
   pin host key yang sudah diverifikasi; batasi dial ke alamat tailnet dalam
   allowlist; tambahkan probe dengan timeout, batas output, audit dan UI.
3. **Inventaris (T11).** Buat katalog perintah tmux tetap dan parser format yang
   aman, native IDs, epoch/fingerprint dan metadata revision; ambil snapshot
   konsisten; simpan cache bertimestamp dan tampilkan stale ketika gagal.
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

## Bukti yang sudah tersedia

- Baseline: `make check`, `go test -race ./...`, `make build`, cross-compile
  Linux tanpa CGO dan uji browser login/profil/audit/logout lulus.
- CI baseline: [run 42185850979](https://github.com/xibitz/tmux-monitor/actions/runs/42185850979), success.
- Perbaikan allowlist: [run 42190573684](https://github.com/xibitz/tmux-monitor/actions/runs/42190573684), success.
- `python3 scripts/tmux-spike.py`: tmux 3.8 macOS, desktop biasa + viewer
  read-only/ignore-size mempertahankan ukuran; disconnect viewer tidak membunuh
  session. Jika semua klien memakai ignore-size, asumsi ukuran tidak berlaku.
  Rincian dan gate terbuka ada di `docs/adr/0001-tmux-client-spike.md`.
- Belum ada bukti SSH ke host nyata, uji Linux tmux, Agent, HP Android/iOS,
  atau gate rilis terminal P0. Tidak ada data/key pengguna yang dijadikan fixture.

## Checkpoint aktif untuk agent berikutnya

- Dokumentasi: daftar status sudah ditambahkan ke implementation plan dan
  `docs/PROGRESS.md` sudah diarahkan ke satu sumber status tersebut.
- Berikutnya: implementasikan T10/T11 sesuai langkah 2–3, mulai dari kontrak API,
  migration 002, credential store/CLI dan konektor SSH yang diuji terisolasi.
- Belum ada kode SSH pada checkpoint awal ini. Saat kode berubah, perbarui bagian
  ini dengan file penting, hasil verifikasi dan pekerjaan berikutnya secara tepat.
- Jangan menghidupkan/mematikan proses pengguna hanya untuk smoke test; gunakan
  port dan database uji terpisah. Jangan menjanjikan terminal sebelum T12 lulus.
