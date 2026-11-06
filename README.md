# tmux monitor

Mobile-first workspace untuk mengelola tmux melalui tailnet pribadi.
Rancangan produk ada di [PRD](PRD.md) dan [rencana implementasi](IMPLEMENTATION_PLAN.md).

## Status implementasi

Hub Go dengan SQLite, login/ACL/CSRF, registri host, audit dan UI responsif tersedia.
Konektor SSH sekarang mendukung key khusus hub, pin host key, probe serta inventaris
session/window/pane tmux dengan epoch dan cache stale. Asset web disertakan dalam
binary hub setelah build. Kredensial dipasang melalui CLI lokal, bukan formulir.

**Terminal PTY/WebSocket dan konektor Agent belum tersedia.** SSH saat ini hanya
membaca inventaris; tidak attach atau memodifikasi session. Belum ada service
worker, control mode, pairing agent, atau notifikasi. Gate rilis P0 belum lulus.

Agent berikutnya mulai dari [MEMORY.md](MEMORY.md). Status setiap task dan gate
berada di [IMPLEMENTATION_PLAN.md](IMPLEMENTATION_PLAN.md).

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

   Salin file public key tersebut ke hub. `ssh-keyscan` saja tidak membuktikan
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

Mencabut konfigurasi lokal:

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
stdout 1 MiB serta stderr 16 KiB per command. Tidak ada raw-shell API, command
input, attach PTY atau pemutaran ulang input pada tahap ini. Jika penghapusan
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
validasi profil, audit atomik/redaksi, migrasi 001/002 naik/turun, WAL, permission
file, host key mismatch, autentikasi SSH, tailnet-only dial, timeout/cancellation,
output limit dan cache stale/revoke. Integrasi SSH memakai key/server protokol
sementara dan tmux nyata pada socket test terpisah. CI memasang tmux di Linux.
Spike juga membuat server tmux dengan socket sementara tersendiri,
memeriksa klien read-only/ukuran/disconnect, lalu membersihkan server uji tersebut.
Tidak menyambung ke session tmux Anda. Matriks Linux/macOS dan perangkat nyata
masih diperlukan sebelum minimum versi tmux ditetapkan.

CI memeriksa format, analisis Go, TypeScript, race tests, build web, dan build
binary Linux tanpa CGO. Kontrak subset API ada di [docs/api-v1.md](docs/api-v1.md).

## Migrasi dan rollback

Hub menjalankan upgrade saat startup dan menolak database dari versi binary lebih
baru. Perintah `hub migrate --to 2` menjalankan upgrade tanpa server HTTP.
Rollback fondasi ke versi 0 **menghapus seluruh tabel aplikasi** dan memerlukan
`--allow-data-loss`; jangan jalankan pada data penting. Buat backup konsisten dulu
saat hub dihentikan, atau gunakan fasilitas SQLite backup. Test rollback memakai
database sementara. Runbook backup/restore produksi lengkap adalah pekerjaan M3.

## Tahap berikutnya

T12 berikutnya menambahkan viewer PTY read-only setelah kebijakan ukuran tanpa
desktop dan gate akses dipenuhi. Interactive dan reconnect hanya diaktifkan
setelah pengujian keselamatan lulus. Lihat MEMORY.md untuk titik lanjut agent.
