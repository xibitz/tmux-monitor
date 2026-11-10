# Notifikasi Codex dan Claude Code

Source T50–T52 belum dikualifikasi. Aktifkan fitur ini pada hub dan profil
Agent lab lebih dahulu. Pairing dan autentikasi Agent tetap memakai HTTPS
Tailscale Serve seperti di [README](../README.md).

1. Set `TMUX_MONITOR_CODING_HOOKS=true` pada hub, lalu restart dengan versi
   yang mendukung schema008. Perbarui hub sebelum mengaktifkan flag baru pada
   Agent.
2. Pada mesin tempat coding CLI berjalan, buat direktori privat sebagai
   pengguna OS pemilik tmux. Gunakan satu path socket per profil Agent. Socket
   dibuat saat Agent terhubung dan hub menyetujui capability hook.

   ```sh
   mkdir -p "$HOME/.local/state/tmux-monitor"
   chmod 700 "$HOME/.local/state/tmux-monitor"
   /path/to/agent run --credential-file /path/to/agent.json \
     --hook-socket "$HOME/.local/state/tmux-monitor/hooks.sock"
   ```

3. Tambahkan hook di konfigurasi pengguna CLI. Ganti path contoh dengan path
   absolut yang sama pada Agent dan hook. Gabungkan dengan konfigurasi yang
   sudah ada, terutama jika sudah memiliki hook atau `notify` sendiri.

   Codex, di `~/.codex/config.toml`

   ```toml
   notify = ["/path/to/agent", "notify", "--socket", "/home/operator/.local/state/tmux-monitor/hooks.sock", "--source", "codex"]
   ```

   Codex menambahkan satu argumen JSON. Adaptor v1 membaca jenis
   `agent-turn-complete` dan menggunakan identitas turn untuk deduplikasi.
   [OpenAI Docs](https://learn.chatgpt.com/docs/config-file/config-advanced#notifications)

   Claude Code, di `~/.claude/settings.json`

   ```json
   {
     "hooks": {
       "Notification": [
         { "hooks": [{ "type": "command", "command": "/path/to/agent notify --socket /home/operator/.local/state/tmux-monitor/hooks.sock --source claude" }] }
       ],
       "Stop": [
         { "hooks": [{ "type": "command", "command": "/path/to/agent notify --socket /home/operator/.local/state/tmux-monitor/hooks.sock --source claude" }] }
       ],
       "StopFailure": [
         { "hooks": [{ "type": "command", "command": "/path/to/agent notify --socket /home/operator/.local/state/tmux-monitor/hooks.sock --source claude" }] }
       ]
     }
   }
   ```

   Claude Code memberikan JSON via stdin. Adaptor v1 memetakan
   `Notification` ke perhatian, `Stop` ke penyelesaian dan `StopFailure`
   ke kesalahan. Daftarkan hanya event yang didukung versi Claude Code
   terpasang. [Claude Code Hooks](https://code.claude.com/docs/en/hooks)

4. Buka **Notifikasi** di dashboard. Inbox menampilkan maksimal 50 peristiwa
   terakhir yang boleh Anda baca. Atribusi session dan pane tampil jika socket,
   PID, epoch, dan pane unik dapat dicocokkan. Gunakan coding CLI dari dalam
   pane tmux pada socket profil tersebut. Jika konteks ambigu atau inventaris
   Agent belum segar, peristiwa tetap masuk dengan lokasi belum diketahui.

Pemilik akun OS adalah batas kepercayaan socket lokal. Adaptor hanya
mengirim enum, sidik deduplikasi, dan metadata lokasi yang terverifikasi;
prompt, respons, direktori kerja, transcript dan output terminal tetap di
mesin asal. Peristiwa dibatasi lajunya dan dilepas bila Agent atau hub putus,
tanpa replay. Socket akan dilepas saat shutdown normal. Jika Agent berhenti
paksa dan socket tertinggal, pastikan proses Agent untuk profil itu sudah
berhenti sebelum menghapus file socket dan menjalankan ulang.

## Web Push opsional

Set `TMUX_MONITOR_WEB_PUSH=true` pada hub HTTPS dengan coding hooks aktif.
Di halaman **Notifikasi**, pilih **Aktifkan Web Push di perangkat ini**.
Pada iOS, gunakan PWA dari Home Screen. Setelah memperbarui PWA, tutup semua
jendela versi lama agar service worker baru dapat aktif. Bila browser atau
provider belum kompatibel, izin ditolak, atau push tidak sampai, gunakan
inbox. [Apple Developer](https://developer.apple.com/documentation/usernotifications/sending-web-push-notifications-in-web-apps-and-browsers)

Hub mencoba provider yang diizinkan di source (FCM, Mozilla, Apple) melalui
HTTPS. Permintaan push memakai VAPID dengan body kosong. Service worker
menampilkan judul umum dan membuka inbox dari origin yang sama. Akses inbox
tetap memerlukan login. Matikan Web Push dari halaman **Notifikasi** sebelum
menyerahkan perangkat bersama. [VAPID](https://datatracker.ietf.org/doc/rfc8292/)

File `web-push.key` di data directory hub berisi key privat VAPID dan
melindungi endpoint yang tersimpan terenkripsi di SQLite. Simpan file ini
sebagai secret mode `0600` jika ingin mempertahankan langganan saat restore.
Alat backup hub saat ini mencakup DB dan key SSH. Untuk pemulihan tanpa key
push, hapus baris `push_subscriptions` pada database pemulihan selagi hub
berhenti, lalu matikan dan aktifkan ulang langganan dari setiap browser.
Inbox tetap dapat dipulihkan dari SQLite.
