# Kualifikasi rilis P0

Kualifikasi lokal awal tercatat di [MEMORY.md](../MEMORY.md#checkpoint-build-dan-kualifikasi-10-november). Matriks staging T34 masih terbuka. Hasil dari satu transport atau
perangkat tidak berlaku sebagai bukti bagi yang lain.

## Persiapan lab

- Gunakan hub staging dengan direktori data, akun, token, key, dan SQLite
  terpisah. Buat server tmux uji dengan socket bernama khusus per profil dan
  proses penanda yang bisa diamati langsung. Catat PID proses dan ID session
  sebelum setiap gangguan.
- Siapkan setidaknya satu host nyata untuk setiap transport dan dua klien
  (desktop serta HP). Catat commit, skema, OS, versi tmux/Agent/browser,
  perangkat dan kondisi jaringan. Pastikan URL lab berbeda dari deployment.
- Buat 10 profil dengan 50 session total untuk kapasitas. Gunakan campuran SSH
  dan Agent. Jalankan tes output burst pada proses penanda di socket lab,
  dengan satu viewer lambat dan klien desktop. Data yang dicatat hanya metrik
  agregat, status, request ID, dan bukti yang sudah disunting dari secret.

## Matriks gangguan

Jalankan setiap baris yang relevan di SSH dan Agent. Catat hasil aktual,
tautan bukti, dan status `PASS`, `FAIL`, `BLOCKED`, atau `N/A` dengan alasan.

| Skenario lab | Cakupan | Bukti yang diharapkan |
|---|---|---|
| Buka viewer read-only, lalu tutup tab atau aplikasi | SSH + Agent | Viewer detach. PID/session penanda tetap hidup. Klien desktop mempertahankan pilihan aktif dan ukuran. |
| Aktifkan interactive, putus jaringan saat input berjalan | SSH + Agent | Input dihentikan. Tidak ada replay. Setelah koneksi pulih, buka ulang secara eksplisit dalam read-only. |
| Ganti Wi-Fi ↔ seluler dan tangguhkan/bangunkan HP | SSH + Agent | Ukur waktu sejak koneksi pulih sampai snapshot baru terlihat. Viewer lama tidak aktif dan mode baru read-only. |
| Putus jalur SSH lalu pulihkan | SSH | Inventaris lama berlabel stale atau kegagalan terlihat. Probe baru pulih setelah jalur kembali. |
| Suspend, restart Agent, dan jalankan proses Agent ganda | Agent | Hub mencapai unreachable setelah lease, lalu rekonsiliasi satu generation. Proses ganda ditolak dan inventaris lama tidak aktif kembali. |
| Restart hub ketika viewer kedua transport aktif | SSH + Agent | Kedua viewer lepas, session dan PID tetap. Agent kembali lewat generation baru. Browser memakai tiket baru. |
| Restart server tmux lab atau hapus target dari desktop | SSH + Agent | Target lama dan epoch lama ditolak. Tidak ada perubahan pada ID yang dipakai kembali. |
| Cabut akses viewer atau credential saat viewer aktif | SSH + Agent | Viewer lepas. Login/credential yang dicabut tidak dapat membuka viewer baru. Audit hanya metadata. |
| Putus respons setelah operasi mutasi lab | Operasi yang tersedia | Kunci sama tidak menjalankan operasi ulang. Hasil ambigu direkonsiliasi sebelum permintaan baru. Paritas Agent yang belum tersedia tercatat blocked. |
| Burst output dan perlambat viewer | SSH + Agent | Backpressure terukur, viewer lambat menerima status resync/putus. Viewer lain dan inventaris tetap tersedia. |
| Akses di luar Grants, tanpa login, dan tanpa izin host | SSH + Agent | Akses HTTP dan WebSocket yang dilindungi ditolak. Catat status dan request ID. |
| Origin berbeda atau CSRF tidak tersedia | SSH + Agent | Operasi browser yang dilindungi ditolak. Status target dan ledger tetap sesuai sebelum percobaan. |
| Pairing token sudah dipakai atau dicabut; SSH host key berubah | Sesuai transport | Koneksi ditolak dan credential/key tidak masuk log. |
| PWA Android/iOS, portrait dan landscape, offline lalu online | SSH + Agent | PWA dapat dipasang dan mode standalone dapat dipakai. Cold start offline hanya menampilkan shell statis. Cache Storage berisi aset publik saja. |
| Clean install, backup dan restore, rotasi serta rollback | Hub + Agent | Service berjalan dengan akun yang benar, health sesuai skema, backup dapat dipulihkan, akses tercabut tetap tertutup. |

## Kapasitas dan pemulihan

Gunakan akun lab yang memiliki izin baca pada tepat 10 profil. Script hanya
membuat dan menutup sesi login sendiri serta meminta daftar host dan inventaris.
Password diminta tanpa echo. Output JSON berisi hitungan dan timing agregat,
termasuk jumlah status stale/kegagalan dan P95 untuk sampel sukses.

```sh
python3 scripts/measure-capacity.py \
  --origin https://your-lab.your-tailnet.ts.net --username LAB_USER \
  --rounds 20 --workers 2 > capacity-idle.json
```

Jalankan pengukuran terpisah saat output burst berjalan di kedua transport.
Catat P95 HTTP per transport serta jumlah putaran lengkap dengan 50 session.
Hitung P95 browser dari minimal 20 pengukuran waktu membuka daftar host atau
detail sampai data terlihat. P95 memakai urutan ke `ceil(0,95 × n)` dari
sampel yang diurutkan. Catat kegagalan di luar P95 dan jangan menyembunyikan
kegagalan dengan hanya melaporkan sampel sukses.

| Metrik | Target awal PRD | Hasil |
|---|---:|---|
| P95 daftar host hingga terlihat | ≤2 detik | BELUM DIJALANKAN |
| P95 daftar session di detail hingga terlihat | ≤2 detik | BELUM DIJALANKAN |
| Pemulihan tampilan setelah koneksi tersedia | ≤10 detik pada jaringan uji | BELUM DIJALANKAN |
| Profil dan session pada putaran lengkap | 10 / 50 | BELUM DIJALANKAN |
| Penggunaan CPU/memori hub dan Agent saat burst | Catat puncak dan bandingkan dengan idle | BELUM DIJALANKAN |

Rekam commit, tanggal, lingkungan, rasio sampel sukses dan artefak JSON pada
laporan. Batas viewer, timeout, dan target hanya diubah berdasarkan hasil yang
diukur. Tahan rilis sampai seluruh FR-01–10, NFR-01–09, dan gate keamanan P0
di [rencana implementasi](../IMPLEMENTATION_PLAN.md#7-rencana-pengujian-dan-gate-penerimaan)
terbukti.

## Profil penggunaan tab dan event bridge (T60)

Gunakan host lab dengan beban sintetis yang sama untuk SSH dan Agent.
Catat versi hub/Agent/tmux/browser, OS, RTT, ukuran viewport dan interval
sampel. Catat pengukuran fixture lokal terpisah dari hasil staging.

| Skenario | Ukuran yang dicatat |
|---|---|
| Inventaris diam, control events nonaktif vs aktif | CPU dan RSS hub/Agent, byte WebSocket per menit, jumlah GET inventaris |
| Satu viewer read-only selama idle dan selama burst sintetis | CPU/RSS hub dan Agent, heap browser, byte terminal/menit, latensi render dan jumlah slow consumer |
| Empat tab session, ulang perpindahan 30 kali dengan satu viewer aktif | P50/P95 waktu sampai viewer siap, RSS awal/akhir, jumlah viewer yang dilepas dan puncak koneksi |
| Putus Wi-Fi lab saat viewer aktif, lalu buka ulang | Waktu pulih, mode setelah reconnect, byte input yang terkirim ulang dan jumlah viewer yatim |
| Perubahan eksternal serentak pada dua host | P95 sampai snapshot terbaru terlihat dan jumlah snapshot per host |

Ambil sampel tiap lima detik selama lima menit per skenario. Gunakan
`ps -p <pid> -o %cpu=,rss=` untuk proses, alat performa browser untuk heap,
dan penghitung byte atau metadata frame pada koneksi lab untuk bandwidth.
Catat hanya ukuran, durasi dan jumlah frame. Simpan hasil agregat tanpa
isi frame, alamat privat, atau identitas host. Jalankan setidaknya tiga kali,
laporkan median dan P95, lalu ulang setelah perubahan buffer/reconnect
yang dipilih berdasarkan hasil.

### Regresi browser lokal

Build aset dahulu dengan `make build`. Siapkan Playwright dan Chromium,
lalu jalankan `bash scripts/qualify-browser.sh` pada mesin pengembangan.
Tetapkan `PLAYWRIGHT_MODULE` jika modul berada di direktori terpisah dan
`PLAYWRIGHT_CHROMIUM_EXECUTABLE` bila memakai executable tertentu. Runner
membuka server HTTPS fixture pada port 18081 dengan sertifikat sementara,
kemudian menutupnya setelah pengujian. Uji ini memakai SSH/terminal
simulasi dan viewport 393×852. Hasil timing tab adalah baseline UI lokal.
