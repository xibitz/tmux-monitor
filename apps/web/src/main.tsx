import React, { lazy, Suspense, useEffect, useRef, useState } from "react";
import type { FormEvent, ReactNode } from "react";
import { createRoot } from "react-dom/client";
import { TmuxRename } from "./TmuxRename";
import { AgentSetup } from "./AgentSetup";
import { TmuxLayoutMutation, TmuxLifecycle } from "./TmuxLifecycle";
import { PaneSnapshotViewer } from "./PaneSnapshotViewer";
import type { PaneSnapshotTarget } from "./PaneSnapshotViewer";
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
import type {
  AuditEvent,
  Host,
  HostInput,
  HostOptions,
  Identity,
  Inventory,
  SSHMetadata,
} from "./api";
import "./styles.css";
const TerminalViewer = lazy(() =>
  import("./TerminalViewer").then((module) => ({
    default: module.TerminalViewer,
  })),
);

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
                  value={
                    loading
                      ? "—"
                      : hosts.filter((host) => host.state === "ONLINE").length
                  }
                  detail="Pemeriksaan terakhir berhasil"
                />
                <Stat
                  icon={<Layers3 size={19} />}
                  label="Session tmux"
                  value="—"
                  detail="Lihat inventaris di detail host"
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
                        <HostBadge host={host} />
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
                          <Clock3 size={14} />{" "}
                          {host.observedAt
                            ? formatTime(host.observedAt)
                            : "Belum diperiksa"}
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
                  <strong>Inventaris SSH sudah tersedia.</strong>
                  <p>
                    Siapkan key SSH dan host key terverifikasi melalui CLI
                    lokal, lalu periksa koneksi di detail host. Viewer read-only
                    eksperimental dapat diaktifkan operator. Agent menyusul.
                  </p>
                </div>
                <span className="subtle-tag">SSH WORKSPACE</span>
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
          onUpdated={(updated) => {
            setSelected(updated);
            setHosts((current) =>
              current.map((host) => (host.id === updated.id ? updated : host)),
            );
          }}
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
        "host.probe": "Pemeriksaan SSH dan inventaris",
        "ssh.configure": "Konfigurasi SSH dipasang",
        "ssh.revoke": "Konfigurasi SSH dicabut",
        "access.denied": "Permintaan akses ditolak",
      } as Record<string, string>
    )[action] ?? action
  );
}

function Modal({
  title,
  onClose,
  children,
  wide = false,
}: {
  title: string;
  onClose: () => void;
  children: ReactNode;
  wide?: boolean;
}) {
  const ref = useRef<HTMLDialogElement>(null);
  const closeRef = useRef(onClose);
  closeRef.current = onClose;
  useEffect(() => {
    ref.current?.scrollTo(0, 0);
  }, [wide]);
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
    <dialog
      ref={ref}
      className={wide ? "modal terminal-modal" : "modal"}
      aria-labelledby="modal-title"
    >
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
  const [options, setOptions] = useState<HostOptions>();
  const [optionsError, setOptionsError] = useState("");
  const [optionsAttempt, setOptionsAttempt] = useState(0);
  const [address, setAddress] = useState("");
  useEffect(() => {
    let active = true;
    setOptionsError("");
    api<HostOptions>("/hosts/options")
      .then((result) => {
        if (active) setOptions(result);
      })
      .catch((error: Error) => {
        if (active) setOptionsError(error.message);
      });
    return () => {
      active = false;
    };
  }, [optionsAttempt]);
  const addressAllowed =
    options?.allowedSSHHosts.includes(address.trim().toLowerCase()) ?? false;
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (transport === "ssh" && !addressAllowed) return;
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
          <>
            {optionsError ? (
              <div className="error-notice" role="alert">
                <span>{optionsError}</span>
                <button
                  type="button"
                  className="button secondary"
                  onClick={() => setOptionsAttempt((current) => current + 1)}
                >
                  Coba lagi
                </button>
              </div>
            ) : !options ? (
              <div className="info-notice" role="status">
                <LoaderCircle size={16} className="spin" />
                Memuat alamat SSH yang diizinkan…
              </div>
            ) : options.allowedSSHHosts.length === 0 ? (
              <div className="info-notice" role="status">
                <CircleHelp size={16} />
                <span>
                  Belum ada alamat SSH yang diizinkan. Tambahkan alamat
                  Tailscale host ke <code>TMUX_MONITOR_ALLOWED_SSH_HOSTS</code>{" "}
                  pada konfigurasi hub, lalu mulai ulang hub. File{" "}
                  <code>.env</code> tidak dimuat otomatis.
                </span>
              </div>
            ) : null}
            <label>
              Alamat tailnet
              <input
                name="address"
                required
                placeholder="laptop.tailnet.ts.net"
                maxLength={253}
                spellCheck={false}
                autoCapitalize="none"
                autoCorrect="off"
                list="allowed-ssh-addresses"
                value={address}
                onChange={(event) => setAddress(event.target.value)}
                disabled={!options || options.allowedSSHHosts.length === 0}
                aria-describedby="ssh-address-help"
              />
              <datalist id="allowed-ssh-addresses">
                {options?.allowedSSHHosts.map((host) => (
                  <option key={host} value={host} />
                ))}
              </datalist>
              <span className="field-help" id="ssh-address-help">
                {address.trim() && options && !addressAllowed
                  ? "Alamat belum diizinkan. Tambahkan ke TMUX_MONITOR_ALLOWED_SSH_HOSTS pada konfigurasi hub, lalu mulai ulang hub."
                  : options && options.allowedSSHHosts.length > 0
                    ? "Pilih alamat yang diizinkan dari saran kolom ini. Untuk alamat baru, tambahkan ke TMUX_MONITOR_ALLOWED_SSH_HOSTS pada konfigurasi hub lalu mulai ulang hub."
                    : "Alamat IP dan nama DNS harus sesuai dengan yang diizinkan di konfigurasi hub."}
              </span>
            </label>
          </>
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
          <button
            className="button primary"
            disabled={busy || (transport === "ssh" && !addressAllowed)}
          >
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
function formatTime(value: string) {
  return new Date(value).toLocaleString("id-ID", {
    day: "numeric",
    month: "short",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  });
}
function HostBadge({ host }: { host: Host }) {
  const online = host.state === "ONLINE" && !host.stale;
  const label =
    host.statusReason === "credentials_required"
      ? "Siapkan SSH"
      : host.statusReason === "connector_pending"
        ? "Belum tersedia"
        : host.statusReason === "agent_disabled"
          ? "Agent nonaktif"
          : online
            ? "Terakhir terhubung"
            : host.state === "ONLINE"
              ? "Periksa ulang"
              : host.state === "DEGRADED"
                ? "Perlu diperiksa"
                : "Belum terhubung";
  return (
    <span className={`status-badge ${online ? "verified" : ""}`}>
      <span />
      {label}
    </span>
  );
}
function HostDetails({
  host,
  canDelete,
  onClose,
  onDeleted,
  onUpdated,
}: {
  host: Host;
  canDelete: boolean;
  onClose: () => void;
  onDeleted: () => void;
  onUpdated: (host: Host) => void;
}) {
  const [confirm, setConfirm] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [metadata, setMetadata] = useState<SSHMetadata>();
  const [metadataLoading, setMetadataLoading] = useState(false);
  const [metadataAttempt, setMetadataAttempt] = useState(0);
  const [inventory, setInventory] = useState<Inventory>();
  const [terminalEnabled, setTerminalEnabled] = useState(false);
  const [paneSnapshotEnabled, setPaneSnapshotEnabled] = useState(false);
  const [paneSnapshotTarget, setPaneSnapshotTarget] =
    useState<PaneSnapshotTarget>();
  const [canRename, setCanRename] = useState(false);
  const [canRenameWindow, setCanRenameWindow] = useState(false);
  const [canCreateWindow, setCanCreateWindow] = useState(false);
  const [canCreate, setCanCreate] = useState(false);
  const [canKill, setCanKill] = useState(false);
  const [layoutOptions, setLayoutOptions] = useState<{
    hostId: string;
    canKillWindow?: boolean;
    canSplitPane?: boolean;
    canKillPane?: boolean;
  }>();
  useEffect(() => {
    let active = true;
    setCanRename(false);
    setCanRenameWindow(false);
    setCanCreateWindow(false);
    setCanCreate(false);
    setCanKill(false);
    setLayoutOptions(undefined);
    if (host.transport === "ssh")
      void api<{
        canRename: boolean;
        canCreate?: boolean;
        canKill?: boolean;
        canRenameWindow?: boolean;
        canCreateWindow?: boolean;
        canKillWindow?: boolean;
        canSplitPane?: boolean;
        canKillPane?: boolean;
      }>(`/hosts/${encodeURIComponent(host.id)}/session-options`)
        .then((options) => {
          if (active) {
            setCanRename(options.canRename);
            setCanRenameWindow(options.canRenameWindow === true);
            setCanCreateWindow(options.canCreateWindow === true);
            setCanCreate(options.canCreate === true);
            setCanKill(options.canKill === true);
            const { canKillWindow, canSplitPane, canKillPane } = options;
            setLayoutOptions({
              hostId: host.id,
              canKillWindow,
              canSplitPane,
              canKillPane,
            });
          }
        })
        .catch(() => {});
    return () => {
      active = false;
    };
  }, [host.id, host.transport]);
  const [terminalSession, setTerminalSession] = useState<{
    id: string;
    name: string;
  }>();
  useEffect(() => {
    let active = true;
    void api<{ enabled: boolean; paneSnapshotEnabled?: boolean }>(
      "/terminal/options",
    )
      .then((value) => {
        if (active) {
          setTerminalEnabled(value.enabled);
          setPaneSnapshotEnabled(value.paneSnapshotEnabled === true);
        }
      })
      .catch(() => {});
    return () => {
      active = false;
    };
  }, []);
  const [observationError, setObservationError] = useState("");
  const id = encodeURIComponent(host.id);
  useEffect(() => {
    if (host.transport !== "ssh" || !canDelete) return;
    let active = true;
    setMetadataLoading(true);
    api<SSHMetadata>(`/hosts/${encodeURIComponent(host.id)}/ssh`)
      .then((value) => {
        if (active) {
          setMetadata(value);
          setError("");
        }
      })
      .catch((error: Error) => {
        if (active) setError(error.message);
      })
      .finally(() => {
        if (active) setMetadataLoading(false);
      });
    return () => {
      active = false;
    };
  }, [host.id, host.transport, canDelete, metadataAttempt]);
  async function remove() {
    setBusy(true);
    setError("");
    try {
      await api(`/hosts/${id}`, "DELETE");
      onDeleted();
    } catch (error) {
      setError((error as Error).message);
      setBusy(false);
    }
  }
  async function inspect() {
    setPaneSnapshotTarget(undefined);
    setBusy(true);
    setObservationError("");
    try {
      if (canDelete) {
        const result = await api<{ host: Host; inventory: Inventory }>(
          `/hosts/${id}/probe`,
          "POST",
        );
        setInventory(result.inventory);
        onUpdated(result.host);
      } else {
        setInventory(await api<Inventory>(`/hosts/${id}/inventory`));
        const result = await api<{ hosts: Host[] }>("/hosts");
        const latest = result.hosts.find((value) => value.id === host.id);
        if (latest) onUpdated(latest);
      }
    } catch (error) {
      setObservationError((error as Error).message);
      setInventory((current) =>
        current
          ? { ...current, stale: true, statusReason: "refresh_failed" }
          : undefined,
      );
      try {
        const result = await api<{ hosts: Host[] }>("/hosts");
        const latest = result.hosts.find((value) => value.id === host.id);
        if (latest) onUpdated(latest);
      } catch {
        /* The request error above remains visible. */
      }
    } finally {
      setBusy(false);
    }
  }
  const selectedPaneSnapshot =
    paneSnapshotTarget?.hostId === host.id ? paneSnapshotTarget : undefined;
  const layout = layoutOptions?.hostId === host.id ? layoutOptions : undefined;
  const canChangeLayout =
    layout?.canKillWindow || layout?.canSplitPane || layout?.canKillPane;
  const layoutDisabled =
    busy ||
    !inventory ||
    inventory.hostId !== host.id ||
    inventory.stale ||
    !inventory.epoch ||
    inventory.capabilities.tmuxVersion !== "3.8";
  function refreshLayout() {
    setInventory(undefined);
    void inspect();
  }
  const windowLinks = new Map<string, number>();
  if ((canRenameWindow || canChangeLayout) && inventory?.hostId === host.id) {
    for (const session of inventory.sessions) {
      for (const window of session.windows) {
        windowLinks.set(window.id, (windowLinks.get(window.id) ?? 0) + 1);
      }
    }
  }
  return (
    <Modal
      title={confirm ? "Hapus profil host?" : host.label}
      wide={!!terminalSession || !!selectedPaneSnapshot}
      onClose={() => {
        if (!busy) onClose();
      }}
    >
      {error && <ErrorNotice>{error}</ErrorNotice>}
      {terminalSession && inventory ? (
        <Suspense fallback={<p>Memuat viewer…</p>}>
          <TerminalViewer
            hostId={host.id}
            hostLabel={host.label}
            sessionId={terminalSession.id}
            sessionName={terminalSession.name}
            epoch={inventory.epoch}
            onBack={() => setTerminalSession(undefined)}
          />
        </Suspense>
      ) : selectedPaneSnapshot ? (
        <PaneSnapshotViewer
          key={JSON.stringify([
            selectedPaneSnapshot.hostId,
            selectedPaneSnapshot.epoch,
            selectedPaneSnapshot.paneId,
            selectedPaneSnapshot.revision,
          ])}
          target={selectedPaneSnapshot}
          hostLabel={host.label}
          onBack={() => setPaneSnapshotTarget(undefined)}
        />
      ) : confirm ? (
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
            <HostBadge host={host} />
            <p>
              {host.observedAt
                ? `Pemeriksaan terakhir: ${formatTime(host.observedAt)}. Status ini adalah hasil observasi terakhir.`
                : "Koneksi dan inventaris tmux belum diperiksa."}
            </p>
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
          {host.transport === "ssh" ? (
            <section className="ssh-panel" aria-label="Koneksi SSH">
              <div className="panel-heading">
                <h3>Koneksi SSH</h3>
                {canDelete && (
                  <button
                    className="icon-button bordered"
                    aria-label="Muat ulang konfigurasi SSH"
                    disabled={busy || metadataLoading}
                    onClick={() => setMetadataAttempt((current) => current + 1)}
                  >
                    <RefreshCw
                      size={15}
                      className={metadataLoading ? "spin" : ""}
                    />
                  </button>
                )}
              </div>
              {metadataLoading ? (
                <p className="panel-description" role="status">
                  Memuat konfigurasi SSH…
                </p>
              ) : canDelete && metadata?.configured ? (
                <div className="ssh-configured">
                  <ShieldCheck size={17} />
                  <div>
                    <strong>
                      Host key terverifikasi · port {metadata.port}
                    </strong>
                    <code>{metadata.fingerprint}</code>
                  </div>
                </div>
              ) : canDelete && metadata ? (
                <div className="ssh-setup">
                  <p>
                    Pasang key SSH khusus hub dan public host key yang telah
                    diverifikasi melalui CLI lokal. Key tidak dikirim melalui
                    formulir.
                  </p>
                  <pre>
                    <code>{`./bin/hub ssh-configure \\
  --host-id ${host.id} \\
  --key-file /path/to/dedicated-key \\
  --host-key-file /path/to/verified-host.pub \\
  --expected-fingerprint SHA256:...`}</code>
                  </pre>
                  <p>
                    Gunakan direktori data yang sama dengan hub. Setelah
                    konfigurasi tersimpan, muat ulang konfigurasi SSH dengan
                    tombol di atas.
                  </p>
                </div>
              ) : null}
              {observationError && (
                <ErrorNotice>{observationError}</ErrorNotice>
              )}
              <button
                className="button secondary full"
                disabled={
                  busy ||
                  (canDelete && (!metadata?.configured || metadataLoading))
                }
                onClick={inspect}
              >
                {busy ? (
                  <LoaderCircle size={16} className="spin" />
                ) : (
                  <RefreshCw size={16} />
                )}
                {canDelete
                  ? "Periksa koneksi dan inventaris"
                  : "Muat inventaris"}
              </button>
            </section>
          ) : (
            <section className="ssh-panel" aria-label="Koneksi Agent">
              <div className="panel-heading">
                <h3>Koneksi Agent</h3>
              </div>
              {canDelete && (
                <AgentSetup
                  key={host.id}
                  host={host}
                  busy={busy}
                  onRevoked={() => {
                    setInventory(undefined);
                    setObservationError("");
                    onUpdated({
                      ...host,
                      state: "UNREACHABLE",
                      statusReason: "agent_offline",
                      observedAt: null,
                      stale: true,
                    });
                  }}
                />
              )}
              {observationError && (
                <ErrorNotice>{observationError}</ErrorNotice>
              )}
              <button
                className="button secondary full"
                disabled={busy}
                onClick={inspect}
              >
                {busy ? (
                  <LoaderCircle size={16} className="spin" />
                ) : (
                  <RefreshCw size={16} />
                )}
                {canDelete
                  ? "Periksa koneksi dan inventaris"
                  : "Muat inventaris"}
              </button>
            </section>
          )}
          {inventory && (
            <section className="inventory-panel" aria-label="Inventaris tmux">
              <div className="panel-heading">
                <h3>Inventaris tmux</h3>
                <span className="subtle-tag">
                  tmux {inventory.capabilities.tmuxVersion}
                </span>
              </div>
              <p className="panel-description">
                Diambil {formatTime(inventory.observedAt)} ·{" "}
                {inventory.sessions.length} session
              </p>
              {inventory.stale && (
                <div className="error-notice" role="status">
                  Pemeriksaan gagal ({inventory.statusReason}). Menampilkan
                  inventaris terakhir; data ini sudah stale.
                </div>
              )}
              {(canRename ||
                canCreate ||
                canKill ||
                canRenameWindow ||
                canCreateWindow ||
                canChangeLayout) &&
                inventory.capabilities.tmuxVersion !== "3.8" && (
                  <p className="panel-description">
                    Pengelolaan session, window, dan pane memerlukan tmux 3.8.
                  </p>
                )}
              {canCreate && (
                <TmuxLifecycle
                  key={inventory.observedAt + ":create"}
                  mode="create"
                  hostId={host.id}
                  epoch={inventory.epoch}
                  revision={inventory.createRevision}
                  disabled={
                    busy ||
                    inventory.stale ||
                    !inventory.capabilities.serverRunning ||
                    !inventory.epoch ||
                    !inventory.createRevision ||
                    inventory.capabilities.tmuxVersion !== "3.8"
                  }
                  onRefresh={() => {
                    setInventory(undefined);
                    void inspect();
                  }}
                />
              )}
              {canCreate && !inventory.capabilities.serverRunning && (
                <p className="panel-description">
                  Buat session awal melalui administrasi host, lalu periksa
                  inventaris kembali. Aplikasi tidak menyalakan server tmux baru
                  secara otomatis.
                </p>
              )}
              {inventory.sessions.length === 0 ? (
                <p className="inventory-empty">
                  {inventory.capabilities.serverRunning
                    ? "Belum ada session pada socket ini."
                    : "SSH terhubung. Belum ada server tmux aktif pada socket ini."}
                </p>
              ) : (
                <ul className="session-list">
                  {inventory.sessions.map((session) => (
                    <li key={session.id}>
                      <div className="session-heading">
                        <Terminal size={16} />
                        <strong>{session.name}</strong>
                        <code>{session.id}</code>
                        {terminalEnabled && host.transport === "ssh" && (
                          <button
                            className="button secondary"
                            disabled={inventory.stale || busy}
                            onClick={() => setTerminalSession(session)}
                          >
                            Buka terminal read-only
                          </button>
                        )}
                      </div>
                      {canRename && session.renameRevision && (
                        <TmuxRename
                          key={inventory.observedAt + session.id}
                          hostId={host.id}
                          target={{
                            kind: "session",
                            id: session.id,
                            name: session.name,
                            revision: session.renameRevision,
                          }}
                          epoch={inventory.epoch}
                          disabled={
                            inventory.stale ||
                            busy ||
                            inventory.capabilities.tmuxVersion !== "3.8"
                          }
                          onRefresh={() => {
                            setInventory(undefined);
                            void inspect();
                          }}
                        />
                      )}
                      {canKill && session.revision && (
                        <TmuxLifecycle
                          key={inventory.observedAt + session.id + ":kill"}
                          mode="kill"
                          hostId={host.id}
                          epoch={inventory.epoch}
                          revision={session.revision}
                          session={session}
                          disabled={
                            busy ||
                            inventory.stale ||
                            inventory.capabilities.tmuxVersion !== "3.8"
                          }
                          onRefresh={() => {
                            setInventory(undefined);
                            void inspect();
                          }}
                        />
                      )}
                      {canCreateWindow && session.revision && (
                        <TmuxLifecycle
                          key={
                            inventory.observedAt + session.id + ":create-window"
                          }
                          mode="create-window"
                          hostId={host.id}
                          epoch={inventory.epoch}
                          revision={session.revision}
                          session={session}
                          disabled={
                            busy ||
                            inventory.stale ||
                            !inventory.epoch ||
                            inventory.capabilities.tmuxVersion !== "3.8"
                          }
                          onRefresh={() => {
                            setInventory(undefined);
                            void inspect();
                          }}
                        />
                      )}
                      <ul>
                        {session.windows.map((window) => (
                          <li key={window.id}>
                            <div className="window-heading">
                              <span>
                                {window.index}: {window.name}
                              </span>
                              <code>{window.id}</code>
                              {window.active && (
                                <span className="active-label">aktif</span>
                              )}
                            </div>
                            {canRenameWindow &&
                              windowLinks.get(window.id) === 1 &&
                              window.revision && (
                                <TmuxRename
                                  key={inventory.observedAt + window.revision}
                                  hostId={host.id}
                                  target={{
                                    kind: "window",
                                    id: window.id,
                                    name: window.name,
                                    revision: window.revision,
                                    sessionId: session.id,
                                  }}
                                  epoch={inventory.epoch}
                                  disabled={
                                    busy ||
                                    inventory.stale ||
                                    !inventory.epoch ||
                                    inventory.capabilities.tmuxVersion !== "3.8"
                                  }
                                  onRefresh={() => {
                                    setInventory(undefined);
                                    void inspect();
                                  }}
                                />
                              )}
                            {layout?.canKillWindow &&
                              session.windows.length > 1 &&
                              windowLinks.get(window.id) === 1 &&
                              window.revision && (
                                <TmuxLayoutMutation
                                  key={`${inventory.observedAt}:${window.revision}:kill-window`}
                                  mode="kill-window"
                                  hostId={host.id}
                                  sessionId={session.id}
                                  window={window}
                                  epoch={inventory.epoch}
                                  revision={window.revision}
                                  disabled={layoutDisabled}
                                  onRefresh={refreshLayout}
                                />
                              )}
                            <ul>
                              {window.panes.map((pane) => (
                                <li className="pane-row" key={pane.id}>
                                  <code>{pane.id}</code>
                                  <span>{pane.command || "proses"}</span>
                                  <span>
                                    {pane.width} × {pane.height}
                                  </span>
                                  {pane.active && (
                                    <span className="active-label">aktif</span>
                                  )}
                                  {paneSnapshotEnabled &&
                                    host.transport === "ssh" &&
                                    inventory.hostId === host.id &&
                                    inventory.capabilities.tmuxVersion ===
                                      "3.8" && (
                                      <button
                                        type="button"
                                        className="button secondary pane-snapshot-button"
                                        aria-label={`Snapshot pane ${pane.id}`}
                                        disabled={
                                          busy ||
                                          inventory.stale ||
                                          !inventory.epoch ||
                                          !pane.revision
                                        }
                                        onClick={() =>
                                          setPaneSnapshotTarget({
                                            hostId: host.id,
                                            paneId: pane.id,
                                            windowName: window.name,
                                            epoch: inventory.epoch,
                                            revision: pane.revision,
                                          })
                                        }
                                      >
                                        Snapshot pane
                                      </button>
                                    )}
                                  {windowLinks.get(window.id) === 1 &&
                                    window.revision &&
                                    (layout?.canSplitPane ||
                                      (layout?.canKillPane &&
                                        window.panes.length > 1)) && (
                                      <div className="pane-mutations">
                                        {layout?.canSplitPane && (
                                          <TmuxLayoutMutation
                                            key={`${inventory.observedAt}:${window.revision}:split`}
                                            mode="split-pane"
                                            hostId={host.id}
                                            sessionId={session.id}
                                            window={window}
                                            pane={pane}
                                            epoch={inventory.epoch}
                                            revision={window.revision}
                                            disabled={layoutDisabled}
                                            onRefresh={refreshLayout}
                                          />
                                        )}
                                        {layout?.canKillPane &&
                                          window.panes.length > 1 && (
                                            <TmuxLayoutMutation
                                              key={`${inventory.observedAt}:${window.revision}:kill-pane`}
                                              mode="kill-pane"
                                              hostId={host.id}
                                              sessionId={session.id}
                                              window={window}
                                              pane={pane}
                                              epoch={inventory.epoch}
                                              revision={window.revision}
                                              disabled={layoutDisabled}
                                              onRefresh={refreshLayout}
                                            />
                                          )}
                                      </div>
                                    )}
                                </li>
                              ))}
                            </ul>
                          </li>
                        ))}
                      </ul>
                    </li>
                  ))}
                </ul>
              )}
            </section>
          )}
          <div className="info-notice">
            <Eye size={16} />
            <span>
              {terminalEnabled
                ? "Viewer dimulai dalam read-only. Input dapat diaktifkan secara sadar jika Anda memiliki izin mengirim input."
                : "Viewer terminal belum diaktifkan oleh operator hub. Inventaris tetap dapat diperiksa."}
            </span>
          </div>
          <div className="modal-actions">
            {canDelete && (
              <button
                className="button text-danger"
                disabled={busy}
                onClick={() => setConfirm(true)}
              >
                <Trash2 size={16} />
                Hapus profil
              </button>
            )}
            <button
              className="button secondary"
              disabled={busy}
              onClick={onClose}
            >
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
