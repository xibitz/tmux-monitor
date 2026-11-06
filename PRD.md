# PRD — Mobile Tmux Manager

**Versi:** 1.0 (rancangan untuk implementasi)  
**Tanggal:** 6 November 2026  
**Status:** Siap dipecah menjadi pekerjaan engineering  
**Produk:** Aplikasi self-hosted, mobile-first, hanya dapat diakses melalui Tailscale

## 1. Ringkasan dan keputusan tetap

Mobile Tmux Manager adalah satu PWA di ponsel untuk melihat dan mengelola tmux pada VPS serta laptop Linux/macOS. PWA berbicara dengan satu central server Go. Setiap profil host memilih **SSH langsung melalui tailnet** atau **Go Agent melalui tailnet**. tmux pada host tetap menjadi pemilik session dan proses; server pusat hanya mengelola dan mengamati. Putusnya browser, server, koneksi, atau agent tidak boleh mengirim perintah kill atau menghentikan proses di tmux.

Keputusan produk:

1. UI/API hanya dipublikasikan secara privat melalui **Tailscale Serve**; tidak memakai Funnel atau listener publik. Koneksi server→host SSH dan agent→server juga melalui tailnet.
2. Pilihan transport disimpan **eksplisit per profil host**. Kegagalan Agent tidak diam-diam beralih ke SSH.
3. **Login aplikasi tetap wajib** di atas pembatasan Tailscale. Akses terminal dianggap setara akses shell pengguna host.
4. **Read-only adalah default** saat membuka terminal; pengguna mengaktifkan interactive secara sadar per viewer. Server menegakkan mode tersebut.
5. SSH PTY attach menjadi jalur terminal awal. Agent menyediakan PTY attach yang setara. **tmux control mode** ditambahkan untuk event dan pengamatan pane yang lebih tepat setelah parser serta perilaku multi-klien lulus pengujian.
6. Satu profil host pada rilis pertama mengikat satu akun OS dan satu socket tmux. Banyak akun/socket dapat direpresentasikan sebagai profil terpisah.

### Asumsi yang dapat dikonfigurasi

Produk awal ditujukan untuk pemilik tunggal atau tim sangat kecil, dengan beberapa host dan beberapa viewer aktif. Server pusat berjalan sebagai satu proses Go dengan SQLite; agent berjalan sebagai user OS pemilik tmux tanpa hak root. Angka kapasitas, timeout, dan dukungan versi tmux adalah **target pengujian**, bukan janji hasil sebelum benchmark.

## 2. Masalah, pengguna, dan hasil yang diinginkan

Saat pengguna meninggalkan laptop, pekerjaan Codex CLI, Claude Code, editor, atau shell masih berjalan di tmux pada beberapa host. Pengguna membutuhkan satu tempat di HP untuk mengetahui host yang dapat dijangkau, membuka sesi yang tepat, membaca output, menjawab prompt, dan mengelola struktur tmux tanpa merusak sesi desktop.

**Pengguna utama:** developer/operator yang mengelola host miliknya sendiri. **Aktor operasional:** admin aplikasi yang mendaftarkan host, memasangkan agent, mengatur akses, dan meninjau audit. Pada instalasi satu orang, keduanya orang yang sama.

Hasil sukses:

- Pengguna dapat menemukan host→session→window→pane dan melihat keadaan koneksi yang akurat.
- Pengguna dapat melanjutkan input dari HP setelah login dan mengembalikan kontrol ke read-only.
- Session di host tetap berjalan ketika HP, hub, atau agent terputus.
- Operasi destruktif selalu diarahkan ke objek tmux yang telah diverifikasi ulang dan memiliki jejak audit.
- Notifikasi Codex/Claude menyebut sumber dan tingkat keyakinan, tanpa mengklaim pekerjaan selesai hanya dari terminal yang diam.

**Di luar cakupan rilis ini:** shell umum di luar tmux; akses internet publik; transfer file/SFTP; rekaman permanen terminal; multi-tenant dengan isolasi organisasi; pemulihan proses setelah reboot host; dukungan Windows native; fallback transport otomatis; sinkronisasi state proses Codex/Claude.

## 3. Alur pengguna utama

1. **Daftar host.** Admin menambahkan profil SSH dengan alamat tailnet, user, socket, dan host key yang diverifikasi; atau membuat kode pemasangan agent sekali pakai. UI menunjukkan transport, status, waktu terakhir terlihat, dan pesan kesalahan yang aman.
2. **Pantau.** Pengguna membuka host, melihat inventaris tmux aktual beserta waktu sinkronisasi. Saat host offline, inventaris terakhir diberi label *stale* dan tindakan mutasi dinonaktifkan.
3. **Lihat terminal.** Pengguna membuka session dalam read-only. Untuk pane tertentu, produk menampilkan snapshot historis terbatas dahulu; streaming pane yang independen hadir setelah control mode siap. Indikator mode, host, session, dan konektivitas selalu tampak.
4. **Interaksi.** Pengguna memilih “Aktifkan input”, melihat konteks host/session, lalu mengetik melalui keyboard HP atau Bluetooth. Detach/menutup PWA hanya menutup viewer.
5. **Kelola tmux.** Pengguna membuat/rename/menghapus session; membuat/rename/menutup window; split/menutup pane; memilih target yang ada. Penghapusan meminta konfirmasi dengan nama target dan jumlah pane/proses yang mungkin terpengaruh.
6. **Pulih dari gangguan.** Saat Wi-Fi↔seluler berganti atau laptop tidur, viewer menunjukkan reconnecting/unreachable. Setelah tersambung, server memverifikasi epoch tmux dan target sebelum melanjutkan. Input yang tidak terkonfirmasi **tidak diputar ulang otomatis**.
7. **Perhatian coding agent.** Hook lokal yang dipasang secara opt-in mengirim event terstruktur. UI menampilkan status dan notifikasi di dalam aplikasi; Web Push opsional jika perangkat mendukung dan pengguna memberi izin.

## 4. Prioritas dan ruang lingkup

**P0:** wajib sebelum rilis yang dapat mengakses terminal nyata. **P1:** wajib untuk cakupan produk penuh, dirilis setelah fondasi P0 stabil. **P2:** penyempurnaan.

| ID | Kebutuhan | Prioritas | Rilis |
|---|---|---:|---|
| FR-01 | Registri host SSH/Agent, capability, status dan uji koneksi | P0 | Fondasi |
| FR-02 | List/create/rename/kill session tmux | P0 | Inti |
| FR-03 | List/create/rename/kill window; list/split/kill pane | P0 | Inti |
| FR-04 | PTY terminal interaktif via SSH | P0 | Inti |
| FR-05 | PTY terminal interaktif via Go Agent | P0 | Paritas |
| FR-06 | Read-only default, interactive eksplisit, detach aman | P0 | Inti |
| FR-07 | Toolbar keyboard mobile, paste, hardware keyboard | P0 | Mobile |
| FR-08 | PWA install, standalone/fullscreen yang didukung browser | P0 | Mobile |
| FR-09 | Tailscale Serve, login, izin per host, audit | P0 | Fondasi |
| FR-10 | Agent heartbeat, reconnect, rekonsiliasi inventaris | P0 | Paritas |
| FR-11 | tmux control-mode event bridge pada SSH dan Agent | P1 | Event |
| FR-12 | Notifikasi Codex/Claude berbasis event terstruktur | P1 | Notifikasi |
| FR-13 | Streaming pane independen, pilihan window/pane per viewer | P1 | Event |
| FR-14 | Tab terminal ganda, favorit, pencarian, Web Push opsional | P2 | Lanjutan |

### Detail perilaku wajib

**FR-01 Registri.** Profil berisi ID stabil, label, transport, alamat tailnet atau agent ID, user OS, socket tmux, grup/label lingkungan, dan kebijakan izin. Kredensial SSH/agent memiliki referensi secret terpisah; API tidak mengembalikan secret. Host yang sama boleh memiliki dua profil untuk dua socket/user. Status `ONLINE`, `DEGRADED`, `RECONNECTING`, `UNREACHABLE` merepresentasikan *observasi transport*, tidak menyatakan session mati.

**FR-02/03 Manajemen tmux.** Daftar menggunakan native tmux IDs (`$`, `@`, `%`) dan nama. Create/rename menerima nama terbatas dan menolak duplikasi dengan pesan jelas. Kill session/window/pane memerlukan target+epoch yang masih cocok, konfirmasi, audit, dan hasil `not_found` atau `conflict` ketika state berubah. Tidak ada API `kill-server`. Operasi layout yang memang mengubah tmux bersama diberi label dampak bersama; UI tidak menyamakan “menutup tab viewer” dengan “kill pane”.

**FR-04/05 Terminal.** Browser menerima ANSI output via WSS dan xterm.js. Satu viewer membuka kanal PTY terpisah; SSH memakai sesi/channel SSH tersendiri, Agent memakai PTY lokal yang disalurkan melalui kanal agent. Server membatasi viewer aktif, ukuran frame, buffer, dan laju input. Reconnect membuat attach baru setelah validasi; tidak mengasumsikan byte yang terputus sudah atau belum diproses.

**FR-06 Mode.** Read-only menolak semua frame input di gateway dan memakai flag read-only tmux ketika attach didukung. Interactive memerlukan izin `terminal:write` dan aktivasi per viewer; berpindah host atau reconnect kembali ke read-only. Toolbar tidak mengirim tombol ketika read-only. Clipboard dan paste dikonfirmasi untuk teks multi-baris pada host berlabel sensitif.

**FR-07/08 Mobile.** Toolbar minimal: `Esc`, `Tab`, `Ctrl`, `Alt`, `Shift`, panah, `Enter`, `Ctrl+C`, dan prefix tmux yang dapat diubah. Tombol memiliki affordance jelas, ukuran sentuh layak, status modifier terlihat, dan tidak menelan input keyboard fisik. Manifest PWA, ikon, HTTPS, mode standalone, orientasi landscape yang nyaman, safe-area, dan opsi layar penuh diterapkan; bila browser tidak mendukung fullscreen, UI tetap dapat dipakai. Asset statis boleh di-cache, tetapi respons API dan output terminal tidak.

**FR-09 Akses.** Hub hanya listen `127.0.0.1`; Serve mempublikasikan HTTPS di tailnet dan Tailscale Grants membatasi sumber. Login membuat session aplikasi dengan cookie Secure/HttpOnly/SameSite, masa berlaku dan logout/revoke. Semua REST dan upgrade WebSocket memeriksa session, izin per host, Origin, serta CSRF pada mutasi berbasis cookie. Audit merekam siapa, kapan, host, target, aksi, hasil, request ID, dan alasan gagal; tidak merekam isi terminal, password, token, atau command input.

**FR-10 Kesehatan.** Agent mengirim heartbeat dan capability/versi. Setelah melewati ambang, hub menandai unreachable; setelah reconnect, agent mengautentikasi ulang, mengirim epoch tmux/inventaris, dan hanya memulihkan subscription yang masih diizinkan. Koneksi ganda untuk agent yang sama ditangani secara deterministik dengan generation ID. SSH memakai probe ringan dan koneksi on-demand. Laptop tidur tidak memicu kill.

**FR-11/13 Control mode.** Bridge memparse respons `%begin/%end/%error`, notifikasi asynchronous, output pane, escaping, dan flow control. Event dinormalisasi menjadi `inventory.changed`, `pane.output`, dan `client.state`; sequence per koneksi serta `resync_required` membantu pemulihan. Control mode digunakan untuk live pane yang spesifik dan navigasi viewer yang tidak mengubah pilihan klien desktop jika capability tmux memadai. Jika tidak memadai, fitur dibatasi dan UI menjelaskan dampak shared tmux.

**FR-12 Notifikasi.** Sumber awal: Codex `notify` yang dikonfigurasi di tingkat user dan Claude Code hooks `Notification`/`Stop`, melalui pengirim lokal yang diautentikasi agent. Skema produk memiliki `attention_required`, `turn_completed`, `error`, `unknown`, `source`, `confidence`, `occurred_at`, dan mapping host/session/pane. Mapping yang tidak pasti masuk inbox tanpa klaim pane spesifik. Dedupe dan rate limit mencegah spam. Pola output terminal boleh memberi indikator *heuristic*, bukan status final. Web Push hanya mengirim judul umum dan tautan tailnet; tidak ada cuplikan terminal atau secret dalam payload. Penerimaan push bergantung platform, instalasi PWA, izin, dan layanan push eksternal.

## 5. Arsitektur produk

```text
HP: PWA React/TypeScript + xterm.js
  │ HTTPS/WSS, cookie aplikasi
  ▼
Tailscale Serve + Grants (tailnet privat)
  │ proxy ke 127.0.0.1
  ▼
Go central server: auth, REST, WS gateway, host registry, audit, SQLite
  ├── SSH connector ── SSH over tailnet ── sshd ── tmux (user host)
  └── Agent connector ◀─ WSS outbound via tailnet ── Go Agent ── tmux
```

Hub adalah modular monolith. Adapter SSH dan Agent menerapkan kontrak kemampuan yang sama untuk inventaris, mutasi tmux, terminal, dan event. tmux host adalah *source of truth* untuk session/window/pane; SQLite menyimpan konfigurasi, akun/izin, audit, dan cache inventaris dengan timestamp/epoch. Terminal output hanya transit dan buffer memori terbatas, bukan disimpan permanen. [Tailscale Serve](https://tailscale.com/docs/features/tailscale-serve) membatasi layanan ke tailnet; [Grants](https://tailscale.com/docs/features/access-control/grants) mengatur siapa yang dapat menjangkaunya.

### Batas penting tmux

PTY `attach-session` melekat pada **session**, bukan membuat terminal independen untuk setiap pane. Pada tahap PTY, navigasi pane/window yang mengubah pilihan aktif dapat memengaruhi klien desktop yang berbagi session. Read-only dapat melihat tanpa mengubah pilihan, dan snapshot `capture-pane` dapat membaca pane tertentu. UX pane live independen baru menjadi syarat P1 setelah control mode dan compatibility test membuktikan perilakunya. `ignore-size`/read-only membantu menjaga ukuran bersama; resize tidak otomatis mengikuti viewport HP. [Manual tmux](https://man7.org/linux/man-pages/man1/tmux.1.html) dan [panduan control mode resmi](https://github.com/tmux/tmux/wiki/Control-Mode) menjadi acuan implementasi.

## 6. Model data dan kontrak ringkas

Entitas persisten: `users`, `app_sessions`, `hosts`, `host_access`, `ssh_credentials_ref`, `agent_identities`, `host_capabilities`, `inventory_cache`, `notification_events`, `notification_subscriptions`, `audit_events`, `user_preferences`. Cache inventaris selalu memuat `observed_at` dan `tmux_epoch`; tidak menjadi dasar tunggal mutasi. `terminal_viewers` dan state WebSocket berada di memori; tidak diperlukan untuk kelangsungan tmux.

Kontrak publik memakai `/api/v1` untuk registri, inventaris, mutasi, login, audit, notifikasi; `/ws/v1/terminal` untuk terminal; `/ws/v1/events` untuk event UI; `/ws/v1/agent` untuk agent. Semua objek tmux diidentifikasi oleh gabungan **host ID + socket profile + tmux epoch + native ID**. API tidak menerima nama bebas sebagai target mutasi. Mutasi memakai `requestId` dan `Idempotency-Key`; kill/create yang diulang setelah timeout harus memberi hasil yang dapat direkonsiliasi, bukan membabi buta dijalankan ulang. Daftar dan format detail ada di [IMPLEMENTATION_PLAN.md](IMPLEMENTATION_PLAN.md).

## 7. Persyaratan nonfungsional dan keselamatan

| ID | Syarat yang dapat diuji |
|---|---|
| NFR-01 | Semua jalur aplikasi memerlukan tailnet dan login; tidak ada listener UI/API atau agent inbound publik. |
| NFR-02 | Tidak ada credential, input terminal, atau output terminal pada log/audit/cache persisten browser. Secret file `0600`, credential unik dan dapat dicabut. |
| NFR-03 | Hub/agent/browser restart atau disconnect tidak mengirim kill dan tidak menghentikan session/proses tmux. |
| NFR-04 | Buffer output dibatasi; viewer lambat diputus dengan `resync_required`, tanpa membebani host tanpa batas. |
| NFR-05 | Status stale terlihat dan mutasi ditolak jika host unreachable atau epoch target berubah. |
| NFR-06 | P95 daftar session host online dan halaman host mengikuti target awal 2 detik pada 10 host/50 session; diukur, lalu dikalibrasi. |
| NFR-07 | Target pemulihan tampilan setelah koneksi kembali ≤10 detik pada jaringan uji; input tidak diputar ulang otomatis. |
| NFR-08 | Semua aksi administratif, perubahan izin, login gagal, pairing/revoke, dan perubahan mode interactive memiliki audit yang dapat dicari. |
| NFR-09 | Kontras, fokus keyboard, label tombol, dan state koneksi/mode dapat diakses lewat pembaca layar; sasaran WCAG 2.2 AA untuk UI non-terminal. |

**Aturan keselamatan operasi:** kill hanya setelah konfirmasi dan revalidasi; menutup viewer hanya detach; perintah yang mengubah struktur tmux ditampilkan sebagai perubahan bersama; tidak ada shell command yang dibangun dari input bebas untuk operasi admin; mode read-only dijaga di server; upgrade/restart tidak boleh mengubah proses milik tmux; backup mencakup konfigurasi dan audit, bukan menjanjikan pemulihan proses host.

## 8. Kriteria penerimaan tingkat produk

1. Dari HP yang **bukan** anggota tailnet, UI/API tidak dapat diakses; dari HP anggota tailnet tetapi belum login, endpoint terlindungi menolak akses. Penolakan termasuk WebSocket.
2. Profil SSH dan Agent masing-masing dapat list/create/rename/kill session, mengelola window/pane, dan membuka terminal interaktif yang lulus matriks input (`Esc`, `Ctrl+C`, arrow, paste, Unicode, resize aman).
3. Read-only tidak mengirim input walau client membuat frame WebSocket manual. Aktivasi interactive dicatat dan reset setelah reconnect.
4. Browser ditutup, jaringan diputus, agent dibunuh, dan hub di-restart ketika proses berjalan dalam tmux: proses dan session tetap hidup. Pengujian dilakukan untuk kedua transport.
5. Mutasi terhadap cache lama atau tmux epoch lama menghasilkan `409 conflict`/`404 not_found`; tidak mengenai objek baru yang kebetulan memakai ID yang sama.
6. Dua klien (desktop dan HP) dapat mengamati session; HP read-only tidak mengubah ukuran atau pilihan aktif desktop. Operasi layout yang eksplisit menjelaskan dampaknya sebelum dijalankan.
7. Agent yang tidur/disconnect tampil `UNREACHABLE`, lalu rekonsiliasi tanpa duplikasi saat kembali. Audit tidak memuat isi terminal.
8. Event control mode pada kedua transport memperbarui inventaris dan viewer pane, menangani output beruntun serta resync setelah gap; fallback polling tetap tersedia ketika capability tidak cocok.
9. Event Codex/Claude terstruktur menghasilkan notifikasi yang tepat dan dedupe; event tanpa sumber tepercaya tidak dilabeli “selesai”. Push, bila diaktifkan, tidak memuat isi sensitif.
10. PWA dapat dipasang dan dipakai dalam mode standalone pada perangkat uji Android dan iOS yang didukung; saat offline hanya shell aplikasi/status tersedia, tidak ada terminal palsu yang terlihat aktif.

## 9. Risiko, ketergantungan, dan keputusan terbuka

| Risiko/ketergantungan | Dampak | Penanganan |
|---|---|---|
| tmux versi/flag berbeda pada Linux/macOS | Fitur client isolation, control mode, resize mungkin berbeda | Capability probe, matriks versi, feature gate; minimum versi ditetapkan setelah spike |
| PTY attach berbagi active window/pane | Navigasi HP dapat mengganggu desktop | Read-only default, snapshot pane, tunda navigasi independen sampai control mode lulus |
| SSH command channel memakai shell remote | Injeksi pada argumen admin | Katalog perintah tetap, validasi ID/nama, quoting teruji, tidak menerima command bebas |
| Agent/SSH credential bocor | Akses shell host | Secret store terpisah, least privilege, rotasi/revoke, host key pinning, audit |
| Sleep, perubahan jaringan, tmux restart | State stale dan input ambigu | Epoch, rekonsiliasi, no replay, status jelas |
| Browser mobile/PWA dan Web Push bervariasi | UX atau notifikasi tidak konsisten | Feature detection, UI in-app sebagai baseline, uji perangkat nyata |
| Endpoint push eksternal | Metadata keluar dari tailnet | Opt-in, payload umum, tanpa terminal text; akses aplikasi tetap tailnet-only |
| Hook Codex/Claude berubah antarversi | Event hilang/berubah format | Adapter berversi, kontrak internal stabil, Unknown fallback |
| Output terminal memuat escape sequence atau tautan berbahaya | Clipboard/tautan dapat disalahgunakan lewat renderer | Nonaktifkan OSC 52, batasi handler tautan, minta tindakan pengguna untuk membuka URL eksternal, uji sequence bermasalah |
| Snapshot pane tidak selalu sama dengan layar aplikasi full-screen | Tampilan pertama pane dapat kehilangan sebagian state/warna | Tandai snapshot sebagai best-effort, uji `capture-pane` dan resinkronisasi renderer; jangan menjanjikan rekonstruksi historis sempurna |

Ketergantungan operasional: tailnet aktif di HP/hub/host; HTTPS Serve dan Grants yang tepat; sshd+tmux untuk SSH; tmux+Go Agent untuk host Agent; akun OS non-root; browser modern dengan WebSocket/PWA; penyimpanan aman untuk SQLite dan secret file. Instalasi PWA memerlukan manifest/HTTPS dan perilaku berbeda per browser; dukungan Web Push iOS berlaku untuk web app yang ditambahkan ke Home Screen dan diberi izin. Lihat [MDN PWA](https://developer.mozilla.org/en-US/docs/Web/Progressive_web_apps/Guides/Making_PWAs_installable) dan [WebKit Web Push](https://webkit.org/blog/13878/).

**Keputusan terbuka yang tidak menghalangi fase awal:** jumlah maksimum tab/viewer per pengguna; pilihan minimum versi tmux hasil spike; apakah deployment awal memakai systemd saja atau juga launchd untuk hub; kebijakan retensi audit (usulan awal 90 hari); apakah Web Push diaktifkan pada instalasi pribadi. Implementasi awal menggunakan default konservatif dan menjadikan semuanya konfigurasi terdokumentasi.
