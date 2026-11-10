# Operasi dan pemulihan tmux monitor

Template dan alat pada dokumen ini belum dijalankan pada mesin uji. Gunakan
profil dan direktori lab untuk kualifikasi sebelum deployment produksi.

## Paket dan hub Linux

`make package` menghasilkan arsip `bin/tmux-monitor-OS-ARCH.tar.gz` yang berisi
binary hub, binary Agent, template service, dan runbook. Hub memakai aset web
yang disematkan di binary. Pilih paket yang sesuai dengan OS dan arsitektur.
Ekstrak ke direktori kerja operator.

Jalankan persiapan berikut sekali pada host Linux baru. Jika akun dan direktori
sudah ada, gunakan instalasi yang ada dan ikuti bagian upgrade.

```sh
sudo useradd --system --user-group --home-dir /var/lib/tmux-monitor \
  --shell /usr/sbin/nologin tmux-monitor
sudo install -d -m 0700 -o tmux-monitor -g tmux-monitor /var/lib/tmux-monitor
sudo install -d -m 0700 /etc/tmux-monitor
sudo install -m 0755 hub /usr/local/bin/tmux-monitor-hub
sudo install -m 0600 deploy/hub.env.example /etc/tmux-monitor/hub.env
sudo install -m 0644 deploy/systemd/tmux-monitor-hub.service /etc/systemd/system/
```

Edit `/etc/tmux-monitor/hub.env` sebagai root. Gunakan origin HTTPS Serve yang
tepat, mode produksi dan listener `127.0.0.1:8080`. Isi allowlist SSH jika
diperlukan. Aktifkan Agent dan viewer dengan flag di [README](../README.md).
Pastikan Tailscale sudah aktif dan Grants mengizinkan hanya node yang diperlukan.

```sh
sudo -u tmux-monitor env TMUX_MONITOR_DATA_DIR=/var/lib/tmux-monitor \
  /usr/local/bin/tmux-monitor-hub bootstrap
sudo systemctl daemon-reload
sudo systemctl enable --now tmux-monitor-hub.service
curl --fail http://127.0.0.1:8080/healthz
sudo tailscale serve --bg http://127.0.0.1:8080
sudo tailscale serve status
```

Gunakan Serve privat dan verifikasi penolakan dari luar Grants sebelum
mengaktifkan profil penting. `journalctl -u tmux-monitor-hub.service` memuat log
hub. Pantau kapasitas volume data dan status health lokal dari sistem monitoring.
[Referensi systemd.exec](https://freedesktop.org/software/systemd/man/latest/systemd.exec.html) · [Referensi Tailscale Serve](https://tailscale.com/docs/reference/tailscale-cli/serve)

## Agent per akun pemilik tmux

Lakukan pairing terlebih dahulu dengan binary `agent` dan simpan credential di
`$HOME/.config/tmux-monitor/agent.json` dengan mode `0600`. Gunakan akun OS yang
menjalankan tmux. Hub v2 harus tersedia sebelum Agent v2 dimulai.

**Linux dengan systemd user**

```sh
install -d -m 0700 "$HOME/.local/bin" "$HOME/.config/systemd/user"
install -m 0755 agent "$HOME/.local/bin/tmux-monitor-agent"
install -m 0644 deploy/systemd/tmux-monitor-agent.service "$HOME/.config/systemd/user/"
systemctl --user daemon-reload
systemctl --user enable --now tmux-monitor-agent.service
systemctl --user status tmux-monitor-agent.service
```

Untuk menyertakan PTY, tambahkan `--terminal` pada `ExecStart` melalui drop-in
systemd dan aktifkan flag hub yang sesuai. Sesuaikan `PATH` dan `TMUX_TMPDIR`
jika instalasi tmux atau socket memerlukannya. Agent memakai namespace
`/tmp` akun yang sama dengan tmux. Untuk mulai saat boot tanpa login, atur
lingering sesuai kebijakan host.

**macOS dengan launchd**

Pasang binary sebagai `$HOME/.local/bin/tmux-monitor-agent`. Salin
`deploy/launchd/id.tmux-monitor.agent.plist` ke `$HOME/Library/LaunchAgents/`,
ganti semua path `/Users/CHANGE_ME` dengan path absolut akun pemilik tmux, lalu
opsional tambahkan argumen `--terminal` sebagai elemen `<string>` tersendiri
setelah `run`.

```sh
launchctl bootstrap "gui/$(id -u)" "$HOME/Library/LaunchAgents/id.tmux-monitor.agent.plist"
launchctl print "gui/$(id -u)/id.tmux-monitor.agent"
```

Agent menangani retry jaringan sendiri. Jika credential atau proses ganda
ditolak, periksa status unit, perbaiki penyebabnya dan mulai service kembali.
Template Agent sengaja tidak mengulang exit permanen secara otomatis.
[Panduan LaunchAgents Apple](https://developer.apple.com/library/archive/documentation/MacOSX/Conceptual/BPSystemStartup/Chapters/CreatingLaunchdJobs.html)

## Backup konsisten dan terenkripsi

Gunakan Python 3.9+ dan `age` pada host operator. Siapkan recipient age
untuk database dan secret. Simpan private identity terpisah dari backup.
Jalankan pekerjaan di volume lokal terenkripsi karena proses backup SQLite
memakai staging plaintext privat sementara. Perlakukan kedua ciphertext
sebagai satu set dan simpan dengan kontrol akses serta retensi yang sesuai.
[Referensi CLI age](https://github.com/FiloSottile/age)

Hentikan hub dan pastikan operator lain tidak menjalankan CLI yang menulis data
hub sampai backup selesai. Ini menjaga konsistensi database dan file key.

```sh
sudo systemctl stop tmux-monitor-hub.service
sudo python3 scripts/hub-backup.py backup \
  --data-dir /var/lib/tmux-monitor \
  --output /srv/private-backups/tmux-monitor-NEW \
  --db-recipient age1REPLACE_DB_RECIPIENT \
  --secrets-recipient age1REPLACE_SECRET_RECIPIENT
sudo systemctl start tmux-monitor-hub.service
```

Script memakai SQLite backup API agar snapshot mencakup transaksi yang sudah
commit, termasuk WAL bila ada. Hasilnya `hub.sqlite.age` dan
`ssh-secrets.json.age`. Hash snapshot dan daftar key yang direferensikan
memastikan kedua file cocok saat restore. Gunakan direktori output baru.
Simpan versi binary dan konfigurasi hub secara terpisah, termasuk allowlist
dan origin. File credential Agent lokal berada di host masing-masing dan
dapat dipasangkan ulang. Backup aplikasi tidak menyimpan proses tmux.

## Restore dan rollback

1. Hentikan hub dan pastikan Serve tidak dapat meneruskan ke hub selama
   recovery. Pilih backup yang diketahui berasal dari operator. Enkripsi
   recipient menjaga kerahasiaan, sehingga asal artefak tetap perlu diverifikasi.
2. Jalankan restore ke direktori staging baru pada volume privat. Script
   memeriksa integritas SQLite, pasangan hash dan daftar SSH key, lalu
   menulis key dengan mode `0600`.

   ```sh
   sudo python3 scripts/hub-backup.py restore \
     --backup-dir /srv/private-backups/tmux-monitor-NEW \
     --db-identity /path/to/db-age-identity \
     --secrets-identity /path/to/secrets-age-identity \
     --output /var/lib/tmux-monitor-restored-NEW
   ```

3. Sebelum re-expose, review audit sejak waktu backup dan terapkan ulang
   pencabutan akses. Pada staging schema 006+, prosedur konservatif berikut
   membatalkan sesi login dan pairing yang dipulihkan serta mewajibkan pairing
   baru untuk semua Agent. Ledger operasi tmux dipertahankan. Catat pekerjaan
   offline ini pada log perubahan operator. Untuk skema lama, pakai hanya
   perintah untuk tabel yang tersedia.

   ```sh
   sudo sqlite3 /var/lib/tmux-monitor-restored-NEW/hub.sqlite <<'SQL'
   .bail on
   BEGIN IMMEDIATE;
   DELETE FROM app_sessions;
   DELETE FROM enrollment_tokens;
   UPDATE agent_identities
      SET revoked_at=strftime('%Y-%m-%dT%H:%M:%fZ','now')
    WHERE revoked_at IS NULL;
   DELETE FROM inventory_cache;
   DELETE FROM host_observations;
   COMMIT;
   SQL
   ```

4. Untuk rollback, pilih binary hub yang cocok dengan snapshot pra-upgrade.
   Simpan data sekarang di direktori berbeda yang belum ada, lalu pindahkan
   staging ke path `StateDirectory` yang tercantum pada unit. Berikan
   kepemilikan direktori dan file hanya ke akun `tmux-monitor`. Jalankan hub
   pada loopback, periksa `/healthz`, lalu pulihkan Serve privat dan pairing
   sesuai kebutuhan. Tahan mutasi pada host dengan operasi berstatus ambigu
   sampai hasil diperiksa langsung pada tmux.

Untuk upgrade normal, buat backup sebelum mengganti binary, mulai hub dahulu,
lalu upgrade Agent satu host per waktu. Pulihkan binary dan backup yang cocok
jika migrasi gagal. Perintah `hub migrate --to N --allow-data-loss` hanya untuk
rollback skema eksplisit setelah meninjau migrasi turun dan backup.

## Rotasi dan revoke

- **SSH.** Jalankan `hub ssh-revoke --host-id ID` sebagai akun service dengan
  direktori data yang sama. Hapus public key lama dari `authorized_keys`
  host jika akses itu perlu dicabut, lalu pasang key baru lewat
  `ssh-configure` dengan fingerprint host yang diverifikasi. Lihat
  [panduan SSH](../README.md#menyiapkan-koneksi-ssh).
- **Agent.** Cabut identitas dari detail host. Hentikan service Agent,
  pindahkan file credential lama dari path aktif, lalu pair dengan token baru
  dan mulai ulang service. Jalur heartbeat akan menerima penolakan credential
  jika proses lama mencoba koneksi kembali.
- **Insiden.** Batasi Serve/Grants, tutup viewer, cabut akses yang terdampak
  dan review audit metadata. Pertahankan session/proses tmux selama pemulihan.
