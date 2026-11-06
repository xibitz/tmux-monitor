# ADR 0002 — SSH onboarding dan inventaris tanpa terminal

Tanggal: 6 November 2026. Status: diterapkan untuk irisan T10–T11.

Private key dipasang melalui CLI lokal dengan permission owner-only, lalu disalin
ke secret store milik hub. DB hanya menyimpan opaque reference. Public host key
dan expected SHA256 fingerprint diberikan secara eksplisit dari kanal tepercaya.
Implementasi memakai `ssh.FixedHostKey`, bukan scan/TOFU, dan menolak perubahan
key sampai admin mengonfigurasi ulang. Ini padanan pin langsung; import database
`known_hosts`, SSH agent, encrypted key dan password auth belum diimplementasikan.
Negosiasi dibatasi ke algoritme milik public pin yang diverifikasi, sehingga
server multi-key tidak memilih key lain. RSA memakai SHA2; signature SHA1/DSA
dan host certificate tidak diaktifkan pada onboarding ini.

Konektor membuka koneksi on-demand, membatasi empat observasi global dan satu per
host, lalu menutup channel/koneksi setelah observasi. Tidak ada attach PTY,
pilihan pane/window, input, mutasi atau keepalive session tmux pada irisan ini.
Semua command berasal dari katalog tetap: identitas OS, versi binary tmux,
fingerprint server dan list session/window/pane. Format memakai escaping `q:`
untuk metadata; parser menolak input malformed, control characters, ID tidak
valid, orphan records dan epoch berubah. Server tmux yang versinya berbeda dari
binary lokal tetap dapat diamati jika format yang dibutuhkan valid.
Fingerprint memakai delimiter printable karena display-message menormalisasi
tab ketika tidak ada session. Untuk respons `no current target`, konektor
mengulang pemeriksaan read-only tanpa list window/pane; session yang muncul di
tengah pemeriksaan menghasilkan conflict inventaris, bukan daftar kosong palsu.

Daftar IP diresolusi sekali, diperiksa seluruhnya, lalu IP valid didial langsung.
Deadline total 10 detik, maksimum 3 detik per percobaan TCP, maksimum 16 alamat.
Fallback hanya antarsesama IP yang telah diverifikasi; bukan ganti transport.
Batas rentang mengikuti [alamat resmi Tailscale](https://tailscale.com/docs/reference/reserved-ip-addresses).
**Pemeriksaan CIDR tidak membuktikan routing/keanggotaan tailnet.** Validasi
operasional Tailscale/Grants dan konflik CGNAT tetap gate T01/T03; pin host key
tetap wajib meskipun IP berada dalam rentang tersebut.

Snapshot memuat epoch dari host/socket + PID/start_time/socket_path/server version,
dan revision metadata. Cache hanya menyimpan inventaris metadata, tanpa byte
terminal. Kegagalan mempertahankan snapshot terakhir dengan timestamp lama dan
`stale:true`. Hasil in-flight setelah revoke/rotasi dibuang sebelum cache ditulis.
Konfigurasi/probe dan audit disimpan atomik; perubahan credential menghapus cache.
Fingerprint epoch cukup untuk pengamatan tahap ini; race/ID reuse untuk mutasi
destruktif harus ditutup sebelum T20. Tidak ada mutasi yang mengandalkan cache.

Uji lokal memakai key yang dibuat oleh test, server protokol SSH lokal, serta tmux
nyata di TMUX_TMPDIR/socket sementara. Fixture browser hanya memalsukan hasil
observasi SSH; login, CSRF, API dan cache berjalan sebenarnya. Fixture hanya
dikompilasi oleh `go test`, tidak ikut binary hub. Pengujian ini tidak mengklaim
sshd/Tailscale host pengguna atau HP nyata sudah diuji.

Referensi format: [manual tmux](https://man7.org/linux/man-pages/man1/tmux.1.html).
Risiko ukuran/PTY dan gate terminal tetap ada di ADR 0001 dan T12–T13.
