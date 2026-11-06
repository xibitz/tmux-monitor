import React, { useEffect, useRef, useState } from "react";
import type { FormEvent, ReactNode } from "react";
import { createRoot } from "react-dom/client";
import {
  Activity,
  ArrowLeft,
  ArrowRight,
  Check,
  ChevronRight,
  CircleHelp,
  Clock3,
  Cpu,
  Eye,
  Globe2,
  Layers3,
  LoaderCircle,
  LogOut,
  Menu,
  Plus,
  RefreshCw,
  Search,
  Server,
  ShieldCheck,
  Terminal,
  Trash2,
  WifiOff,
  X,
} from "lucide-react";
import { api, APIError, configureSession } from "./api";
import type { AuditEvent, Host, HostInput, Identity } from "./api";
import "./styles.css";

function App() {
  const [identity, setIdentity] = useState<Identity>();
  const [loading, setLoading] = useState(true);
  const [startupError, setStartupError] = useState("");
  const [expired, setExpired] = useState(false);
  const [attempt, setAttempt] = useState(0);
  function accept(value: Identity) {
    configureSession(value, () => {
      configureSession();
      setIdentity(undefined);
      setExpired(true);
    });
    setIdentity(value);
    setExpired(false);
  }
  useEffect(() => {
    let active = true;
    setLoading(true);
    setStartupError("");
    api<Identity>("/me")
      .then((value) => {
        if (active) accept(value);
      })
      .catch((error) => {
        if (active && (!(error instanceof APIError) || error.status !== 401))
          setStartupError(error.message);
      })
      .finally(() => {
        if (active) setLoading(false);
      });
    return () => {
      active = false;
    };
  }, [attempt]);
  if (loading)
    return (
      <div className="splash">
        <Brand />
        <LoaderCircle className="spin" aria-label="Memuat aplikasi" />
      </div>
    );
  if (startupError)
    return (
      <div className="splash">
        <Brand />
        <ErrorNotice>{startupError}</ErrorNotice>
        <button
          className="button primary"
          onClick={() => setAttempt(attempt + 1)}
        >
          Coba lagi
        </button>
      </div>
    );
  if (!identity) return <Login expired={expired} onLogin={accept} />;
  return (
    <Dashboard
      identity={identity}
      onLogout={() => {
        configureSession();
        setIdentity(undefined);
      }}
    />
  );
}

function Brand() {
  return (
    <div className="brand">
      <span className="brand-icon">
        <Terminal size={22} strokeWidth={2.5} />
      </span>
      <span>
        tmux<span className="brand-light">monitor</span>
        <span className="brand-dot">.</span>
      </span>
    </div>
  );
}
function ErrorNotice({ children }: { children: ReactNode }) {
  return (
    <div className="error-notice" role="alert">
      {children}
    </div>
  );
}

function Login({
  expired,
  onLogin,
}: {
  expired: boolean;
  onLogin: (identity: Identity) => void;
}) {
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const form = event.currentTarget;
    const data = new FormData(form);
    setBusy(true);
    setError("");
    try {
      const result = await api<Identity>("/auth/login", "POST", {
        username: data.get("username"),
        password: data.get("password"),
      });
      form.reset();
      onLogin(result);
    } catch (error) {
      setError((error as Error).message);
    } finally {
      setBusy(false);
    }
  }
  return (
    <div className="login-layout">
      <header className="login-header">
        <Brand />
        <span className="private-label">
          <ShieldCheck size={15} /> Ruang kerja privat
        </span>
      </header>
      <main className="login-main">
        <div className="login-story">
          <span className="eyebrow">
            <span className="small-dot" /> YOUR TERMINAL, WITH YOU
          </span>
          <h1>
            Pekerjaan tetap jalan.
            <br />
            <span>Anda tetap terhubung.</span>
          </h1>
          <p>
            Satu tempat untuk memantau tmux di semua host pribadi Anda. Dari
            meja kerja, atau dari mana saja.
          </p>
          <div className="terminal-illustration" aria-hidden="true">
            <div className="terminal-top">
              <i />
              <i />
              <i />
              <span>workspace / tmux</span>
            </div>
            <div className="terminal-body">
              <span className="muted"># satu ruang untuk semua session</span>
              <br />
              <span className="prompt">❯</span> tmux attach-session
              <br />
              <span className="cursor" />
            </div>
            <div className="terminal-status">
              <span>tmux monitor</span>
              <span>private workspace</span>
            </div>
          </div>
          <span className="illustration-caption">
            Tutup viewer kapan saja. Session tmux tetap milik host.
          </span>
        </div>
        <section className="login-card">
          <span className="card-icon">
            <ShieldCheck size={24} />
          </span>
          <h2>Selamat datang kembali</h2>
          <p>Masuk ke ruang kerja Anda.</p>
          {expired && (
            <div className="info-notice" role="status">
              Sesi Anda berakhir. Silakan masuk kembali.
            </div>
          )}
          {error && <ErrorNotice>{error}</ErrorNotice>}
          <form onSubmit={submit}>
            <label>
              Nama pengguna
              <input
                name="username"
                autoComplete="username"
                maxLength={64}
                required
                autoFocus
                placeholder="Nama pengguna Anda"
              />
            </label>
            <label>
              Kata sandi
              <input
                name="password"
                type="password"
                autoComplete="current-password"
                maxLength={1024}
                required
                placeholder="Masukkan kata sandi"
              />
            </label>
            <button className="button primary full" disabled={busy}>
              {busy ? (
                <LoaderCircle size={17} className="spin" />
              ) : (
                <>
                  Masuk ke workspace <ArrowRight size={17} />
                </>
              )}
            </button>
          </form>
          <div className="login-footnote">
            <ShieldCheck size={16} />
            <span>Akses dibatasi oleh Tailscale dan akun aplikasi.</span>
          </div>
        </section>
      </main>
      <footer className="login-footer">
        SELF-HOSTED. PRIVATE BY DESIGN.
        <span>Mobile Tmux Manager · Fondasi 0.1</span>
      </footer>
    </div>
  );
}

function Dashboard({
  identity,
  onLogout,
}: {
  identity: Identity;
  onLogout: () => void;
}) {
  const [tab, setTab] = useState<"hosts" | "audit">("hosts");
  const [hosts, setHosts] = useState<Host[]>([]);
  const [events, setEvents] = useState<AuditEvent[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [query, setQuery] = useState("");
  const [filter, setFilter] = useState("all");
  const [adding, setAdding] = useState(false);
  const [selected, setSelected] = useState<Host>();
  const [menuOpen, setMenuOpen] = useState(false);
  const [notice, setNotice] = useState("");
  const [loggingOut, setLoggingOut] = useState(false);
  const [online, setOnline] = useState(navigator.onLine);
  const [mobile, setMobile] = useState(
    () => window.matchMedia("(max-width: 800px)").matches,
  );
  const loadSequence = useRef(0);
  async function refresh() {
    const sequence = ++loadSequence.current;
    setLoading(true);
    setError("");
    try {
      if (tab === "hosts") {
        const result = await api<{ hosts: Host[] }>("/hosts");
        if (sequence === loadSequence.current) setHosts(result.hosts);
      } else {
        const result = await api<{ events: AuditEvent[] }>("/audit");
        if (sequence === loadSequence.current) setEvents(result.events);
      }
    } catch (error) {
      if (sequence === loadSequence.current) setError((error as Error).message);
    } finally {
      if (sequence === loadSequence.current) setLoading(false);
    }
  }
  useEffect(() => {
    void refresh();
  }, [tab]);
  useEffect(() => {
    const update = () => setOnline(navigator.onLine);
    window.addEventListener("online", update);
    window.addEventListener("offline", update);
    return () => {
      window.removeEventListener("online", update);
      window.removeEventListener("offline", update);
    };
  }, []);
  useEffect(() => {
    const media = window.matchMedia("(max-width: 800px)");
    const update = () => {
      setMobile(media.matches);
      setMenuOpen(false);
    };
    media.addEventListener("change", update);
    return () => media.removeEventListener("change", update);
  }, []);
  useEffect(() => {
    if (!menuOpen) return;
    const escape = (event: KeyboardEvent) => {
      if (event.key === "Escape") setMenuOpen(false);
    };
    window.addEventListener("keydown", escape);
    return () => window.removeEventListener("keydown", escape);
  }, [menuOpen]);
  async function logout() {
    setLoggingOut(true);
    setError("");
    try {
      await api("/auth/logout", "POST");
      onLogout();
    } catch (error) {
      setError((error as Error).message);
    } finally {
      setLoggingOut(false);
    }
  }
  const visibleHosts = hosts.filter(
    (host) =>
      (filter === "all" || host.transport === filter) &&
      `${host.label} ${host.tailnetAddress} ${host.environment}`
        .toLowerCase()
        .includes(query.toLowerCase()),
  );
  return (
    <div className="app-layout">
      {menuOpen && (
        <button
          className="sidebar-backdrop"
          aria-label="Tutup navigasi"
          onClick={() => setMenuOpen(false)}
        />
      )}
      <aside
        className={`sidebar ${menuOpen ? "is-open" : ""}`}
        inert={mobile && !menuOpen}
      >
        <Brand />
        <div className="workspace-label">PERSONAL WORKSPACE</div>
        <nav aria-label="Navigasi utama">
          <button
            className={tab === "hosts" ? "nav-item active" : "nav-item"}
            aria-current={tab === "hosts" ? "page" : undefined}
            onClick={() => {
              setTab("hosts");
              setMenuOpen(false);
              setNotice("");
            }}
          >
            <Server size={18} />
            Host saya<span className="nav-count">{hosts.length}</span>
          </button>
          {identity.user.isAdmin && (
            <button
              className={tab === "audit" ? "nav-item active" : "nav-item"}
              aria-current={tab === "audit" ? "page" : undefined}
              onClick={() => {
                setTab("audit");
                setMenuOpen(false);
                setNotice("");
              }}
            >
              <Activity size={18} />
              Log aktivitas
            </button>
          )}
        </nav>
        <div className="sidebar-bottom">
          <div className="private-card">
            <span className="private-card-icon">
              <ShieldCheck size={19} />
            </span>
            <strong>Workspace privat</strong>
            <p>Akses host melalui tailnet Anda.</p>
            <span className="subtle-tag">SELF-HOSTED</span>
          </div>
          <div className="account-row">
            <span className="avatar">
              {identity.user.username.slice(0, 2).toUpperCase()}
            </span>
            <div>
              <strong>{identity.user.username}</strong>
              <span>{identity.user.isAdmin ? "Administrator" : "Anggota"}</span>
            </div>
            <button
              className="icon-button"
              title="Keluar"
              aria-label="Keluar"
              onClick={logout}
              disabled={loggingOut}
            >
              <LogOut size={17} />
            </button>
          </div>
        </div>
      </aside>
      <div className="main-column">
        <header className="topbar">
          <div className="breadcrumb">
            <button
              className="icon-button mobile-menu"
              aria-label="Buka navigasi"
              onClick={() => setMenuOpen(true)}
            >
              <Menu size={20} />
            </button>
            <span>Workspace</span>
            <ChevronRight size={14} />
            <strong>{tab === "hosts" ? "Host saya" : "Log aktivitas"}</strong>
          </div>
          <span className="topbar-label">
            <span className="small-dot" /> PRIVATE WORKSPACE
          </span>
        </header>
        <main className="dashboard-main">
          <div className="page-heading">
            <div>
              <span className="eyebrow">WORKSPACE OVERVIEW</span>
              <h1>
                {tab === "hosts" ? "Host saya" : "Log aktivitas"}
                <span className="heading-dot">.</span>
              </h1>
              <p>
                {tab === "hosts"
                  ? "Semua host Anda, dalam satu tempat."
                  : "Jejak akses dan perubahan di ruang kerja Anda."}
              </p>
            </div>
            {tab === "hosts" && identity.user.isAdmin && (
              <button
                className="button primary"
                onClick={() => {
                  setAdding(true);
                  setNotice("");
                }}
              >
                <Plus size={18} /> Tambah host
              </button>
            )}
          </div>
          {!online && (
            <ErrorNotice>
              <WifiOff size={17} /> Anda sedang offline. Data yang terlihat
              mungkin sudah berubah.
            </ErrorNotice>
          )}
          {error && <ErrorNotice>{error}</ErrorNotice>}
          {notice && (
            <div className="success-notice" role="status">
              <Check size={17} />
              {notice}
            </div>
          )}
          {tab === "hosts" ? (
            <>
              <div className="stats-grid">
                <Stat
                  icon={<Server size={19} />}
                  label="Total host"
                  value={loading ? "—" : hosts.length}
                  detail="Profil yang terdaftar"
                />
                <Stat
                  icon={<Globe2 size={19} />}
                  label="Koneksi terverifikasi"
                  value="—"
                  detail="Probe host belum tersedia"
                />
                <Stat
                  icon={<Layers3 size={19} />}
                  label="Session tmux"
                  value="—"
                  detail="Menunggu konektor host"
                />
              </div>
              <div className="list-toolbar">
                <div className="segmented" aria-label="Filter transport">
                  {[
                    ["all", "Semua host"],
                    ["ssh", "SSH"],
                    ["agent", "Agent"],
                  ].map(([value, label]) => (
                    <button
                      key={value}
                      aria-pressed={filter === value}
                      className={filter === value ? "selected" : ""}
                      onClick={() => setFilter(value)}
                    >
                      {label}
                    </button>
                  ))}
                </div>
                <div className="search-actions">
                  <div className="search-field">
                    <Search size={16} />
                    <input
                      aria-label="Cari host"
                      placeholder="Cari host…"
                      value={query}
                      onChange={(e) => setQuery(e.target.value)}
                    />
                  </div>
                  <button
                    className="icon-button bordered"
                    aria-label="Muat ulang host"
                    onClick={refresh}
                    disabled={loading}
                  >
                    <RefreshCw size={16} className={loading ? "spin" : ""} />
                  </button>
                </div>
              </div>
              {loading ? (
                <div className="loading-area" role="status">
                  <LoaderCircle className="spin" size={22} />
                  Memuat host…
                </div>
              ) : error ? (
                <div className="empty-state">
                  <WifiOff size={32} />
                  <h2>Daftar host belum dapat dimuat</h2>
                  <p>Periksa koneksi lalu coba lagi.</p>
                  <button className="button secondary" onClick={refresh}>
                    Coba lagi
                  </button>
                </div>
              ) : hosts.length === 0 ? (
                <div className="empty-state">
                  <span className="empty-icon">
                    <Server size={32} />
                  </span>
                  <span className="eyebrow">MULAI DARI SATU HOST</span>
                  <h2>Ruang kerja Anda dimulai di sini</h2>
                  <p>
                    Tambahkan profil VPS atau laptop Anda.
                    <br />
                    Pilih SSH langsung atau Agent untuk setiap host.
                  </p>
                  {identity.user.isAdmin && (
                    <button
                      className="button secondary"
                      onClick={() => setAdding(true)}
                    >
                      <Plus size={17} /> Tambah host pertama
                    </button>
                  )}
                  <span className="empty-detail">
                    <ShieldCheck size={14} /> Profil tersimpan privat di hub
                    Anda
                  </span>
                </div>
              ) : visibleHosts.length === 0 ? (
                <div className="empty-state compact">
                  <Search size={28} />
                  <h2>Tidak ada host yang cocok</h2>
                  <p>Coba kata pencarian atau filter lain.</p>
                </div>
              ) : (
                <div className="host-grid">
                  {visibleHosts.map((host) => (
                    <button
                      className="host-card"
                      key={host.id}
                      onClick={() => setSelected(host)}
                    >
                      <div className="host-card-top">
                        <span className="host-icon">
                          <Server size={22} />
                        </span>
                        <span className="status-badge">
                          <span />
                          Belum terhubung
                        </span>
                      </div>
                      <h2>{host.label}</h2>
                      <p className="host-address">
                        {host.tailnetAddress || "Koneksi melalui Agent"}
                      </p>
                      <div className="host-tags">
                        <span>{host.transport.toUpperCase()}</span>
                        {host.environment && <span>{host.environment}</span>}
                      </div>
                      <div className="host-card-bottom">
                        <span>
                          <Clock3 size={14} /> Belum diperiksa
                        </span>
                        <ArrowRight size={16} />
                      </div>
                    </button>
                  ))}
                </div>
              )}
              <div className="foundation-note">
                <CircleHelp size={18} />
                <div>
                  <strong>Fondasi workspace sudah siap.</strong>
                  <p>
                    Profil dan akses tersimpan. Koneksi SSH, Agent, dan terminal
                    tersedia pada tahap implementasi berikutnya.
                  </p>
                </div>
                <span className="subtle-tag">M0 / FOUNDATION</span>
              </div>
            </>
          ) : (
            <section className="audit-panel">
              <div className="panel-heading">
                <h2>Aktivitas terbaru</h2>
                <button
                  className="icon-button bordered"
                  aria-label="Muat ulang aktivitas"
                  onClick={refresh}
                  disabled={loading}
                >
                  <RefreshCw size={16} className={loading ? "spin" : ""} />
                </button>
              </div>
              <p className="panel-description">
                Maksimal 50 aktivitas terakhir. Isi terminal dan kredensial
                tidak direkam.
              </p>
              {loading ? (
                <div className="loading-area">
                  <LoaderCircle className="spin" />
                  Memuat aktivitas…
                </div>
              ) : events.length === 0 ? (
                <div className="empty-state compact">
                  <Activity />
                  <p>Belum ada aktivitas yang dapat ditampilkan.</p>
                </div>
              ) : (
                <div className="audit-list">
                  {events.map((event) => (
                    <div className="audit-row" key={event.id}>
                      <span
                        className={`audit-icon ${event.result === "success" ? "" : "failure"}`}
                      >
                        <Activity size={17} />
                      </span>
                      <div>
                        <strong>{actionLabel(event.action)}</strong>
                        <span>
                          {event.result === "success"
                            ? "Berhasil"
                            : "Ditolak atau gagal"}
                          {event.hostId
                            ? ` · ${event.hostId.slice(0, 12)}…`
                            : ""}
                        </span>
                      </div>
                      <time dateTime={event.at}>
                        {new Date(event.at).toLocaleString("id-ID", {
                          day: "numeric",
                          month: "short",
                          hour: "2-digit",
                          minute: "2-digit",
                        })}
                      </time>
                    </div>
                  ))}
                </div>
              )}
            </section>
          )}
          <footer className="workspace-footer">
            <span>
              <ShieldCheck size={13} /> Session tmux tetap berjalan di host
            </span>
            <span>
              tmux monitor <span className="version">v0.1</span>
            </span>
          </footer>
        </main>
      </div>
      {adding && (
        <HostForm
          onClose={() => setAdding(false)}
          onCreated={(host) => {
            setHosts((current) =>
              [...current, host].sort((a, b) => a.label.localeCompare(b.label)),
            );
            setAdding(false);
            setNotice("Profil host berhasil ditambahkan.");
          }}
        />
      )}
      {selected && (
        <HostDetails
          host={selected}
          canDelete={identity.user.isAdmin}
          onClose={() => setSelected(undefined)}
          onDeleted={() => {
            setHosts((current) =>
              current.filter((host) => host.id !== selected.id),
            );
            setSelected(undefined);
            setNotice("Profil host dihapus dari workspace.");
          }}
        />
      )}
    </div>
  );
}
function Stat({
  icon,
  label,
  value,
  detail,
}: {
  icon: ReactNode;
  label: string;
  value: string | number;
  detail: string;
}) {
  return (
    <div className="stat-card">
      <div className="stat-top">
        <span>{label}</span>
        {icon}
      </div>
      <strong>{value}</strong>
      <span className="stat-detail">{detail}</span>
    </div>
  );
}
function actionLabel(action: string) {
  return (
    (
      {
        "auth.login": "Login aplikasi",
        "auth.logout": "Keluar dari aplikasi",
        "admin.bootstrap": "Administrator dibuat",
        "host.create": "Profil host ditambahkan",
        "host.update": "Profil host diubah",
        "host.delete": "Profil host dihapus",
        "access.denied": "Permintaan akses ditolak",
      } as Record<string, string>
    )[action] ?? action
  );
}

function Modal({
  title,
  onClose,
  children,
}: {
  title: string;
  onClose: () => void;
  children: ReactNode;
}) {
  const ref = useRef<HTMLDialogElement>(null);
  const closeRef = useRef(onClose);
  closeRef.current = onClose;
  useEffect(() => {
    const dialog = ref.current!;
    dialog.showModal();
    const cancel = (event: Event) => {
      event.preventDefault();
      closeRef.current();
    };
    dialog.addEventListener("cancel", cancel);
    return () => {
      dialog.removeEventListener("cancel", cancel);
      dialog.close();
    };
  }, []);
  return (
    <dialog ref={ref} className="modal" aria-labelledby="modal-title">
      <div className="modal-header">
        <h2 id="modal-title">{title}</h2>
        <button
          className="icon-button"
          aria-label="Tutup dialog"
          onClick={onClose}
        >
          <X size={20} />
        </button>
      </div>
      {children}
    </dialog>
  );
}
function HostForm({
  onClose,
  onCreated,
}: {
  onClose: () => void;
  onCreated: (host: Host) => void;
}) {
  const [transport, setTransport] = useState<"ssh" | "agent">("ssh");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const data = new FormData(event.currentTarget);
    const body: HostInput = {
      label: String(data.get("label")),
      transport,
      tailnetAddress: transport === "ssh" ? String(data.get("address")) : "",
      osUser: String(data.get("osUser")),
      socketName: String(data.get("socket") || "default"),
      environment: String(data.get("environment") || ""),
    };
    setBusy(true);
    setError("");
    try {
      onCreated(await api<Host>("/hosts", "POST", body));
    } catch (error) {
      setError((error as Error).message);
      setBusy(false);
    }
  }
  return (
    <Modal
      title="Tambah host"
      onClose={() => {
        if (!busy) onClose();
      }}
    >
      <p className="modal-description">Simpan profil host di workspace Anda.</p>
      {error && <ErrorNotice>{error}</ErrorNotice>}
      <form onSubmit={submit}>
        <label>
          Nama host
          <input
            name="label"
            maxLength={80}
            required
            placeholder="Contoh: Laptop kerja"
            autoFocus
          />
        </label>
        <fieldset>
          <legend>Transport</legend>
          <div className="transport-options">
            <button
              type="button"
              className={transport === "ssh" ? "chosen" : ""}
              aria-pressed={transport === "ssh"}
              onClick={() => setTransport("ssh")}
            >
              <Terminal size={18} />
              <strong>SSH langsung</strong>
              <span>Melalui tailnet</span>
            </button>
            <button
              type="button"
              className={transport === "agent" ? "chosen" : ""}
              aria-pressed={transport === "agent"}
              onClick={() => setTransport("agent")}
            >
              <Cpu size={18} />
              <strong>Go Agent</strong>
              <span>Koneksi dari host</span>
            </button>
          </div>
        </fieldset>
        {transport === "ssh" && (
          <label>
            Alamat tailnet
            <input
              name="address"
              required
              placeholder="laptop.tailnet.ts.net"
              maxLength={253}
              spellCheck={false}
            />
            <span className="field-help">
              Alamat harus sudah diizinkan dalam konfigurasi hub.
            </span>
          </label>
        )}
        <div className="form-columns">
          <label>
            Akun OS
            <input
              name="osUser"
              required
              maxLength={64}
              pattern="[A-Za-z0-9._][A-Za-z0-9._\-]{0,63}"
              placeholder="developer"
              spellCheck={false}
            />
          </label>
          <label>
            Socket tmux
            <input
              name="socket"
              required
              defaultValue="default"
              maxLength={64}
              pattern="[A-Za-z0-9._][A-Za-z0-9._\-]{0,63}"
              spellCheck={false}
            />
          </label>
        </div>
        <label>
          Lingkungan <span className="optional">opsional</span>
          <input
            name="environment"
            maxLength={40}
            placeholder="Contoh: development"
          />
        </label>
        <div className="info-notice">
          <Eye size={16} />
          <span>
            Profil disimpan lebih dahulu.{" "}
            {transport === "ssh"
              ? "Verifikasi host key dan koneksi SSH"
              : "Pemasangan Agent"}{" "}
            menyusul pada tahap berikutnya.
          </span>
        </div>
        <div className="modal-actions">
          <button
            type="button"
            className="button secondary"
            onClick={onClose}
            disabled={busy}
          >
            Batal
          </button>
          <button className="button primary" disabled={busy}>
            {busy ? (
              <LoaderCircle size={17} className="spin" />
            ) : (
              <>
                <Plus size={17} />
                Simpan profil
              </>
            )}
          </button>
        </div>
      </form>
    </Modal>
  );
}
function HostDetails({
  host,
  canDelete,
  onClose,
  onDeleted,
}: {
  host: Host;
  canDelete: boolean;
  onClose: () => void;
  onDeleted: () => void;
}) {
  const [confirm, setConfirm] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  async function remove() {
    setBusy(true);
    setError("");
    try {
      await api(`/hosts/${encodeURIComponent(host.id)}`, "DELETE");
      onDeleted();
    } catch (error) {
      setError((error as Error).message);
      setBusy(false);
    }
  }
  return (
    <Modal
      title={confirm ? "Hapus profil host?" : host.label}
      onClose={() => {
        if (!busy) onClose();
      }}
    >
      {error && <ErrorNotice>{error}</ErrorNotice>}
      {confirm ? (
        <>
          <p className="modal-description">
            Profil <strong>{host.label}</strong> akan dihapus dari workspace.
            Session dan proses tmux di host tetap berjalan.
          </p>
          <div className="modal-actions">
            <button
              className="button secondary"
              disabled={busy}
              onClick={() => setConfirm(false)}
            >
              <ArrowLeft size={16} />
              Kembali
            </button>
            <button className="button danger" disabled={busy} onClick={remove}>
              {busy ? (
                <LoaderCircle className="spin" size={16} />
              ) : (
                <Trash2 size={16} />
              )}
              Hapus profil
            </button>
          </div>
        </>
      ) : (
        <>
          <div className="detail-status">
            <span className="status-badge">
              <span />
              Belum terhubung
            </span>
            <p>Koneksi dan inventaris tmux belum diperiksa.</p>
          </div>
          <dl className="detail-list">
            <dt>Transport</dt>
            <dd>{host.transport.toUpperCase()}</dd>
            <dt>Alamat</dt>
            <dd>{host.tailnetAddress || "Melalui Agent"}</dd>
            <dt>Akun OS</dt>
            <dd>{host.osUser}</dd>
            <dt>Socket tmux</dt>
            <dd>{host.socketName}</dd>
            <dt>Lingkungan</dt>
            <dd>{host.environment || "—"}</dd>
          </dl>
          <div className="info-notice">
            <CircleHelp size={17} />
            <span>
              Konektor {host.transport === "ssh" ? "SSH" : "Agent"} sedang
              menunggu tahap implementasi berikutnya. Terminal belum tersedia.
            </span>
          </div>
          <div className="modal-actions">
            {canDelete && (
              <button
                className="button text-danger"
                onClick={() => setConfirm(true)}
              >
                <Trash2 size={16} />
                Hapus profil
              </button>
            )}
            <button className="button secondary" onClick={onClose}>
              Tutup
            </button>
          </div>
        </>
      )}
    </Modal>
  );
}

createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>,
);
