# tmux monitor

Mobile-first workspace untuk mengelola tmux melalui tailnet pribadi.
Rancangan produk ada di [PRD](PRD.md) dan [rencana implementasi](IMPLEMENTATION_PLAN.md).

## Status implementasi

Fondasi pertama sudah mencakup hub Go dengan listener localhost, SQLite WAL dan
migrasi bernomor, bootstrap admin, login Argon2id, cookie sesi dan revoke,
pemeriksaan Origin/CSRF, izin per host, registri profil, audit, serta UI React
yang responsif. Asset web disertakan dalam binary hub setelah build.

**Konektor SSH/Agent dan terminal belum diimplementasikan.** Profil yang dibuat
belum dianggap online; inventaris mengembalikan `transport_unavailable`.
Belum ada service worker, control mode, pairing agent, atau notifikasi.
Ini hasil awal M0, belum memenuhi gate rilis P0 untuk terminal nyata.

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
Alamat SSH harus sama persis dengan allowlist admin. Pengaturan ini baru menyimpan
profil; koneksi SSH dan verifikasi host key menyusul pada M1.

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

## Pemeriksaan

```sh
make check
make test
make build
python3 scripts/tmux-spike.py
```

Test mencakup batas jaringan, login, expiry/revoke, CSRF/Origin, default-deny ACL,
validasi profil, audit atomik/redaksi, migrasi naik/turun, WAL, permission file,
dan batas request. Spike membuat server tmux dengan socket sementara tersendiri,
memeriksa klien read-only/ukuran/disconnect, lalu membersihkan server uji tersebut.
Tidak menyambung ke session tmux Anda. Matriks Linux/macOS dan perangkat nyata
masih diperlukan sebelum minimum versi tmux ditetapkan.

CI memeriksa format, analisis Go, TypeScript, race tests, build web, dan build
binary Linux tanpa CGO. Kontrak subset API ada di [docs/api-v1.md](docs/api-v1.md).

## Migrasi dan rollback

Hub menjalankan upgrade saat startup dan menolak database dari versi binary lebih
baru. Perintah `hub migrate --to 1` menjalankan upgrade tanpa server HTTP.
Rollback fondasi ke versi 0 **menghapus seluruh tabel aplikasi** dan memerlukan
`--allow-data-loss`; jangan jalankan pada data penting. Buat backup konsisten dulu
saat hub dihentikan, atau gunakan fasilitas SQLite backup. Test rollback memakai
database sementara. Runbook backup/restore produksi lengkap adalah pekerjaan M3.

## Tahap berikutnya

M1 menghubungkan profil SSH dengan secret reference dan host key terverifikasi,
melakukan probe kemampuan/inventaris, lalu membuka viewer PTY read-only.
Interactive dan reconnect hanya diaktifkan setelah pengujian keselamatan lulus.
