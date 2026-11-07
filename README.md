# tmux monitor

Mobile-first workspace untuk mengelola tmux melalui tailnet pribadi.
Rancangan produk ada di [PRD](PRD.md) dan [rencana implementasi](IMPLEMENTATION_PLAN.md).

## Status implementasi

Hub Go dengan SQLite, login/ACL/CSRF, registri host, audit dan UI responsif tersedia.
Konektor SSH sekarang mendukung key khusus hub, pin host key, probe serta inventaris
session/window/pane tmux dengan epoch dan cache stale. Asset web disertakan dalam
binary hub setelah build. Kredensial dipasang melalui CLI lokal, bukan formulir.

**Terminal SSH tersedia secara eksperimental dan nonaktif secara default.**
Viewer selalu dimulai read-only; input interaktif memerlukan izin dan aktivasi
per viewer. Konektor Agent belum tersedia. Belum ada service
worker, control mode, pairing agent, atau notifikasi. Gate rilis P0 belum lulus.

Agent berikutnya mulai dari [MEMORY.md](MEMORY.md). Status setiap task dan gate
berada di [IMPLEMENTATION_PLAN.md](IMPLEMENTATION_PLAN.md).

## Terminal SSH eksperimental

Setelah SSH/inventaris berfungsi, operator dapat mencoba viewer dengan
`TMUX_MONITOR_TERMINAL_ENABLED=true` pada environment hub, lalu restart backend.
Muat `.env` seperti langkah development di bawah. Jangan menganggap fitur ini
sudah lulus pengujian HP nyata atau akses Serve/Grants; gate tersebut masih terbuka.
Backend baru menjalankan migrasi **003** untuk target audit. Backup data sebelum
upgrade. CLI dan server harus tetap memakai direktori data yang sama.

Di detail host, klik **Periksa koneksi** untuk inventaris baru, lalu **Buka
terminal read-only** di session. Viewer hanya menampilkan session aktif bersama
klien tmux lain. Ia tidak menyediakan pane independen. Input keyboard memerlukan aktivasi terpisah.
Ukuran mengikuti window remote; layar ponsel dapat digeser. Tutup/Kembali hanya
detach. Bila putus, **Buka ulang** membuat viewer baru tanpa replay input.

Viewer sekarang memeriksa koneksi dengan heartbeat. Input dihentikan jika
koneksi tidak terkonfirmasi selama 15 detik, perangkat offline, atau halaman
ditinggalkan. Saat kembali online, klik **Buka ulang**; mode selalu kembali
read-only dan perlu aktivasi lagi untuk mengetik. Terminal yang diam tanpa output
tetap sehat selama heartbeat berjalan. Jika perangkat tidur, pemeriksaan
dilanjutkan saat browser aktif lagi; ketikan lama tidak dikirim ulang.
Web terbaru memakai protokol `tmux.v2`: bangun ulang/restart hub bersama asset web
baru. Hub baru tetap menerima browser lama (`tmux.v1`) tanpa heartbeat aplikasi.

Prasyarat ukuran: tmux **3.7+**, default global `window-size manual` dan semua
window pada session yang dilihat juga efektif `manual`. Tanpa itu, viewer menolak
attach dengan pesan kebijakan ukuran. Aplikasi **tidak mengubah opsi tmux host**.
Operator yang memilih kebijakan ini dapat menjalankan perintah berikut pada host
target (sesuaikan socket dan native session ID dari inventaris):

Minimum 3.7 diperlukan karena versi lama dapat mengalami crash server ketika
membuat window/session dengan default manual; lihat [bug upstream tmux #4849](https://github.com/tmux/tmux/pull/4849).

```sh
# Periksa versi dahulu. Jangan set global manual pada tmux di bawah 3.7.
tmux -V
# Catat opsi lama dahulu untuk dikembalikan sesuai konfigurasi Anda.
tmux -L default show-options -gwv window-size
tmux -L default list-windows -t '$0' -F '#{window_id} #{window-size} #{window_width}x#{window_height}'

# Default untuk window baru; berdampak pada seluruh socket ini.
tmux -L default set-option -gw window-size manual
# Ulangi untuk SETIAP native window ID dalam session yang akan dilihat.
tmux -L default set-option -w -t '@0' window-size manual
```

Mode manual menghentikan penyesuaian ukuran otomatis dari klien desktop. Ubah
ukuran secara sadar dengan `resize-window` bila diperlukan. Hub tidak meresize
window. Viewer ditutup bila window aktif, ukuran, epoch, opsi atau akses berubah;
pengecekan dijadwalkan setiap 2 detik dengan batas waktu 2 detik untuk masing-masing
pemeriksaan izin dan SSH. Perubahan eksternal dapat
memengaruhi tmux sebelum pemeriksaan berikutnya. Grid dibatasi 500 kolom dan
200 baris window ditambah maksimum 5 baris status.

Untuk mengetik, klik **Aktifkan input**, baca dampak terhadap session bersama,
lalu **Aktifkan untuk viewer ini**. Admin memiliki izin ini; akun lain memerlukan
`tmux:read` dan `terminal:write` pada host tersebut. **Kembali read-only** melepas
koneksi interaktif dan membuka viewer baru. Pergantian mode hanya menyentuh
klien milik viewer itu, bukan klien desktop. Jika pergantian gagal, gunakan
**Buka ulang** yang selalu memulai read-only. Input yang mungkin sudah diproses
sebelum gangguan tidak pernah dikirim ulang otomatis.

Keyboard mendukung Unicode dan control keys. Toolbar menyediakan Esc, Tab,
Ctrl+C, panah, Enter, Backspace, Home, End dan Delete. Untuk kombinasi khusus,
pilih **Ctrl/Alt/Shift**, isi satu karakter di **Tombol kombinasi**, lalu klik
**Kirim kombinasi**. Modifier berlaku sekali pada tombol toolbar/kolom tersebut;
statusnya terlihat dan kembali netral setelah dikirim. Input keyboard fisik dan
IME tetap ditangani xterm seperti biasa. Shift toolbar memakai pemetaan ASCII.

**Prefix tmux** default Ctrl+B; sesuaikan dengan prefix pada host (misalnya
Ctrl+A), lalu klik **Kirim prefix** sebelum tombol perintah berikutnya. Pilihan
ini hanya mengatur byte yang dikirim, tidak mengubah konfigurasi tmux host.
Pilihan tersimpan dalam memory selama tampilan target terbuka dan kembali ke
Ctrl+B setelah ditutup/berganti target. Draft/modifier dibuang saat mode atau
koneksi berubah. Toolbar hanya aktif sesudah aktivasi input.

Paste maksimal 16 KiB; teks dengan baris baru
selalu meminta tinjauan dan konfirmasi sebelum dikirim. Clipboard OSC52 dan
tautan dari output terminal tetap tidak otomatis diaktifkan. Navigasi/input tmux
interaktif dapat memengaruhi session yang sedang dipakai klien desktop juga.

Tiket browser sekali pakai berlaku 30 detik; viewer maksimal 30 menit. Batas
aktif: 8 global, 4 per pengguna, 4 per host. Input ditolak saat read-only; permintaan resize viewport tetap ditolak.
Read-only memakai `read-only,ignore-size`; interactive memakai klien baru dengan
`ignore-size`. Tiket pergantian mode hanya berlaku 15 detik dan sekali pakai.
Input interaktif dibatasi 16 KiB per frame, 64 KiB/detik dengan burst 32 KiB,
serta pemeriksaan izin/kredensial pada setiap frame sebelum menulis ke SSH. Output hanya transit,
buffer terbatas, tanpa rekaman di database atau browser persistence.

## Menjalankan secara lokal

Prasyarat: Go 1.27.3+, Node.js 22.12+ (CI memakai Node 24), npm, dan `make`.

```sh
make setup
make build
./bin/hub bootstrap --username admin
TMUX_MONITOR_DEV=true \
TMUX_MONITOR_ORIGIN=http://127.0.0.1:8080 \
TMUX_MONITOR_ALLOWED_SSH_HOSTS=laptop.your-tailnet.ts.net \
./bin/hub serve
```

Buka `http://127.0.0.1:8080`, lalu masuk dengan admin yang dibuat. Bootstrap
meminta kata sandi secara tersembunyi dan hanya dapat dijalankan sebelum ada user.
Kata sandi minimal 12 byte. Jangan masukkan kata sandi sebagai argumen perintah.
Untuk otomasi lokal, CLI menyediakan `--password-stdin`; gunakan input privat.

Mode pengembangan dengan hot reload, setelah bootstrap:

```sh
# Terminal pertama
set -a
source .env
set +a
make dev
# Terminal kedua
npm run dev --prefix apps/web
```

Buka `http://127.0.0.1:5173`. Mode HTTP hanya tersedia melalui setting development
yang eksplisit. Browser tidak menyimpan credential, data API, atau output terminal
di localStorage/service worker. Browser boleh menampilkan dialog penyimpanan
password miliknya sendiri.

## Konfigurasi dan akses tailnet

Variabel tersedia di [.env.example](.env.example); hub tidak memuat `.env`
secara otomatis. `TMUX_MONITOR_DATA_DIR` default `.data` dan menyimpan SQLite.
Direktori menggunakan permission `0700`, database `0600`. Direktori ini diabaikan Git.
Alamat SSH harus sama persis dengan allowlist admin. Saat dipakai, semua IP hasil
resolusi harus berada di ruang alamat Tailscale; localhost/LAN/publik ditolak.

Contoh produksi setelah login/konfigurasi diverifikasi di lingkungan uji:

```sh
TMUX_MONITOR_ORIGIN=https://your-hub.your-tailnet.ts.net ./bin/hub serve
tailscale serve --bg http://127.0.0.1:8080
tailscale serve status
```

Atur Tailscale Grants agar hanya perangkat yang diizinkan dapat menjangkau hub.
Jangan gunakan Funnel. Hub menolak listener selain `127.0.0.1`; cookie produksi
selalu Secure. Contoh ini belum membuktikan konfigurasi Grants atau akses dari HP
di instalasi Anda. [Dokumentasi Tailscale Serve](https://tailscale.com/docs/features/tailscale-serve).

## Jika alamat SSH ditolak

Pesan "Alamat SSH belum diizinkan" berarti alamat yang diketik belum tercantum
di `TMUX_MONITOR_ALLOWED_SSH_HOSTS` pada proses hub yang sedang berjalan.
Menambah profil memerlukan akun admin; daftar alamatnya tetap dikelola melalui
konfigurasi hub. Formulir menampilkan saran dari daftar tersebut. Jika kosong,
formulir memberi petunjuk konfigurasi sebelum profil SSH dapat disimpan.

Hentikan hub, lalu mulai ulang dengan alamat Tailscale yang ingin digunakan:

```sh
# Ganti kedua alamat contoh ini dengan alamat host Anda.
TMUX_MONITOR_ALLOWED_SSH_HOSTS=100.100.100.10,laptop.your-tailnet.ts.net make dev
```

Untuk menjalankan binary langsung, tetapkan variabel tersebut bersama
`TMUX_MONITOR_ORIGIN` dan pengaturan lain pada perintah `./bin/hub serve`.
Jika memakai service manager, tambahkan variabel pada konfigurasi service lalu
restart. Nama DNS dan IP diperiksa sebagai alamat terpisah; jika keduanya akan
dipakai, cantumkan keduanya, dipisahkan koma. Spasi di tepi dan kapitalisasi nama
DNS dinormalisasi. File `.env`/`.env.example` tidak dibaca otomatis; menulis nilai
di sana saja belum mengubah konfigurasi proses. Muat ulang halaman/formulir
setelah hub dimulai ulang agar saran alamat diperbarui.

## Menyiapkan koneksi SSH

1. Buat profil SSH melalui aplikasi dengan alamat tailnet, akun OS pemilik tmux,
   dan socket yang benar. Buka detailnya untuk melihat ID profil dan petunjuk CLI.
2. Siapkan private key khusus hub, tanpa passphrase, dengan permission `0600`.
   Public key pasang pada `authorized_keys` akun OS yang sesuai. Key terenkripsi,
   ssh-agent dan login password belum didukung pada irisan ini.
3. Ambil **public host key** melalui kanal tepercaya (misalnya console host),
   serta catat fingerprint SHA256-nya secara independen. Di console host:

   ```sh
   ssh-keygen -lf /etc/ssh/ssh_host_ed25519_key.pub -E sha256
   ```

   Untuk OpenSSH biasa, salin file public key tersebut ke hub, misalnya menjadi
   `~/.ssh/target-host.pub`. Nama ini hanya lokasi salinan; file tidak dibuat oleh
   `ssh-keygen -lf` maupun `hub ssh-configure`. `ssh-keyscan` saja tidak membuktikan
   identitas host; CLI tidak melakukan trust-on-first-use dari hasil scan jaringan.
4. Hentikan backend lama sebelum upgrade binary/skema, buat backup konsisten data
   penting, jalankan `make build`, lalu muat environment yang menunjuk direktori
   data hub yang sama. Konfigurasikan profil (ganti placeholder berikut):

   ```sh
   ./bin/hub ssh-configure \
     --host-id h_ID_DARI_DETAIL \
     --key-file /path/to/dedicated-key \
     --host-key-file /path/to/verified-host.pub \
     --expected-fingerprint SHA256:FINGERPRINT_TERVERIFIKASI \
     --port 22
   ```

   CLI menyalin private key ke `.data/secrets/ssh/` (`0700`/`0600`) dan hanya
   menyimpan reference serta public pin di SQLite. Perintah ini tidak membuka
   koneksi SSH. Gunakan `TMUX_MONITOR_DATA_DIR` yang sama jika bukan `.data`.
5. Jalankan kembali backend, buka detail host, klik muat ulang konfigurasi SSH,
   lalu **Periksa koneksi dan inventaris**. Hasil memuat timestamp; kegagalan
   berikutnya mempertahankan snapshot terakhir dengan label stale. Tanpa server
   tmux aktif, SSH tetap online dengan daftar session kosong.

### Jika host memakai Tailscale SSH

[Tailscale SSH](https://tailscale.com/docs/features/tailscale-ssh) memakai host key
sendiri untuk endpoint yang ditanganinya. Key tersebut dapat berbeda dari
`/etc/ssh/ssh_host_ed25519_key.pub` milik OpenSSH. Pin harus sesuai dengan layanan
SSH yang benar-benar menjawab pada alamat/port profil.

Verifikasi public key node melalui metadata klien Tailscale yang sudah
terautentikasi (`sshHostKeys` pada peer yang sesuai di `tailscale status --json`),
lalu cocokkan fingerprint dengan key koneksi tujuan. Simpan public key yang
terverifikasi ke file `--host-key-file`; jangan mengganti fingerprint hanya
berdasarkan hasil scan jaringan. Jangan menyalin private host key dari `/etc/ssh`.

Pada Tailscale SSH, kebijakan Tailscale menentukan autentikasi/otorisasi;
`authorized_keys` adalah pengaturan OpenSSH biasa. CLI hub saat ini tetap meminta
private key lokal, dan kesesuaian mode autentikasi Tailscale SSH perlu diuji
terhadap policy instalasi. Pengujian otomatis saat ini memakai server SSH test,
bukan bukti seluruh mode policy Tailscale SSH sudah diterima.

### Mencabut konfigurasi lokal

```sh
./bin/hub ssh-revoke --host-id h_ID_DARI_DETAIL
```

Ini mencabut penggunaan key di hub. Untuk mencabut key pada host, hapus public key
terkait dari `authorized_keys` melalui administrasi host.

Rotasi dilakukan dengan `ssh-configure` kembali dan fingerprint terverifikasi.
Rotasi/revoke menghapus cache lama dan menonaktifkan reference sebelumnya.
Host key yang berubah ditolak sampai admin memasang pin baru secara eksplisit.
Jangan menggunakan data stale untuk menyimpulkan session/proses telah berhenti.

Observasi dibatasi 10 detik, maksimum empat request global dan satu per host,
stdout 1 MiB serta stderr 16 KiB per command. API observasi tidak menerima raw
shell atau input terminal. Viewer PTY terpisah memerlukan opt-in dan aktivasi
seperti panduan di atas; input tidak diputar ulang. Jika penghapusan
file secret gagal setelah revoke/rotasi, reference tetap nonaktif; periksa dan
bersihkan file yatim saat maintenance dengan hub berhenti.

## Pemeriksaan

```sh
make check
make test
make build
python3 scripts/tmux-spike.py
```

Test mencakup batas jaringan, login, expiry/revoke, CSRF/Origin, default-deny ACL,
validasi profil, audit atomik/redaksi, migrasi 001–003 naik/turun, WAL, permission
file, host key mismatch, autentikasi SSH, tailnet-only dial, timeout/cancellation,
output limit dan cache stale/revoke. Integrasi SSH memakai key/server protokol
sementara dan tmux nyata pada socket test terpisah. CI membangun rilis resmi
tmux 3.8 dengan checksum tetap di Linux.
Spike juga membuat server tmux dengan socket sementara tersendiri,
memeriksa klien read-only/ukuran/disconnect, lalu membersihkan server uji tersebut.
Tidak menyambung ke session tmux Anda. Viewer eksperimental mensyaratkan tmux
3.7+; matriks versi/OS yang lebih luas dan perangkat nyata tetap diperlukan
untuk dukungan rilis P0.

CI memeriksa format, analisis Go, TypeScript, race tests, matriks byte tombol
toolbar melalui Node test runner, build web, dan build
binary Linux tanpa CGO. Kontrak subset API ada di [docs/api-v1.md](docs/api-v1.md).

## Migrasi dan rollback

Hub menjalankan upgrade saat startup dan menolak database dari versi binary lebih
baru. Perintah `hub migrate --to 2` menjalankan upgrade tanpa server HTTP.
Rollback fondasi ke versi 0 **menghapus seluruh tabel aplikasi** dan memerlukan
`--allow-data-loss`; jangan jalankan pada data penting. Buat backup konsisten dulu
saat hub dihentikan, atau gunakan fasilitas SQLite backup. Test rollback memakai
database sementara. Runbook backup/restore produksi lengkap adalah pekerjaan M3.

## Tahap berikutnya

Viewer SSH read-only dan aktivasi interactive sudah tersedia secara eksperimental.
Berikutnya lengkapi gate perangkat/akses/jaringan, aksesibilitas toolbar, lalu
mutasi tmux dan transport Agent. Lihat MEMORY.md untuk titik lanjut agent.
