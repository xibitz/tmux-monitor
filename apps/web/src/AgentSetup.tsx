import { useEffect, useRef, useState } from "react";
import { api } from "./api";
import type { Host } from "./api";

type Metadata = { enabled: boolean; agentId?: string };
type Enrollment = { token: string; expiresAt: string };

export function AgentSetup({
  host,
  busy,
  onRevoked,
}: {
  host: Host;
  busy: boolean;
  onRevoked: () => void;
}) {
  const [metadata, setMetadata] = useState<Metadata>();
  const [revision, setRevision] = useState(0);
  const [ip, setIP] = useState("");
  const [enrollment, setEnrollment] = useState<Enrollment>();
  const [confirmRevoke, setConfirmRevoke] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const alive = useRef(false);
  const sending = useRef(false);
  const base = `/hosts/${encodeURIComponent(host.id)}/agent`;

  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
    };
  }, []);

  useEffect(() => {
    let current = true;
    api<Metadata>(base)
      .then((value) => {
        if (current) setMetadata(value);
      })
      .catch((e: Error) => {
        if (current) setError(e.message);
      });
    return () => {
      current = false;
    };
  }, [base, revision]);

  useEffect(() => {
    if (!enrollment) return;
    const expire = () => {
      if (Date.now() >= Date.parse(enrollment.expiresAt))
        setEnrollment(undefined);
    };
    const timeout = window.setTimeout(
      expire,
      Math.max(0, Date.parse(enrollment.expiresAt) - Date.now()),
    );
    const hide = () => setEnrollment(undefined);
    window.addEventListener("pageshow", expire);
    window.addEventListener("pagehide", hide);
    return () => {
      clearTimeout(timeout);
      window.removeEventListener("pageshow", expire);
      window.removeEventListener("pagehide", hide);
    };
  }, [enrollment]);

  async function change(action: "issue" | "revoke") {
    if (sending.current || busy) return;
    sending.current = true;
    setSaving(true);
    setError("");
    setEnrollment(undefined);
    try {
      if (action === "issue") {
        const result = await api<Enrollment>("/agents/enrollments", "POST", {
          hostId: host.id,
          tailnetIp: ip.trim(),
        });
        if (alive.current) setEnrollment(result);
      } else if (metadata?.agentId) {
        await api(
          `/agents/${encodeURIComponent(metadata.agentId)}/revoke`,
          "POST",
        );
        if (alive.current) {
          setMetadata({ enabled: metadata.enabled });
          setConfirmRevoke(false);
          onRevoked();
        }
      }
    } catch (e) {
      if (alive.current) setError((e as Error).message);
    } finally {
      sending.current = false;
      if (alive.current) setSaving(false);
    }
  }

  return (
    <div className="ssh-setup">
      {error && (
        <p className="error-notice" role="alert">
          {error}
        </p>
      )}
      {!metadata && !error && <p role="status">Memuat konfigurasi Agent…</p>}
      {metadata && !metadata.enabled && (
        <p>Aktifkan Agent melalui konfigurasi hub dalam mode HTTPS.</p>
      )}
      {metadata?.agentId ? (
        <>
          <p>
            Identitas Agent terdaftar <code>{metadata.agentId}</code>.
          </p>
          {confirmRevoke ? (
            <>
              <p>Cabut identitas ini dan putus koneksi Agent?</p>
              <button
                type="button"
                className="button secondary"
                disabled={saving}
                onClick={() => setConfirmRevoke(false)}
              >
                Batal
              </button>
              <button
                type="button"
                className="button danger"
                disabled={busy || saving}
                onClick={() => void change("revoke")}
              >
                Cabut akses Agent
              </button>
            </>
          ) : (
            <button
              type="button"
              className="button secondary"
              disabled={busy || saving}
              onClick={() => setConfirmRevoke(true)}
            >
              Cabut identitas…
            </button>
          )}
        </>
      ) : (
        metadata?.enabled && (
          <>
            <label>
              IP Tailscale mesin Agent
              <input
                value={ip}
                maxLength={45}
                autoComplete="off"
                autoCapitalize="off"
                spellCheck={false}
                disabled={saving}
                onChange={(event) => setIP(event.target.value)}
              />
            </label>
            <button
              type="button"
              className="button secondary"
              disabled={!ip.trim() || busy || saving}
              onClick={() => void change("issue")}
            >
              Buat token pairing
            </button>
            {enrollment && (
              <>
                <p>
                  Token sekali pakai, berlaku sampai{" "}
                  {new Date(enrollment.expiresAt).toLocaleTimeString()}.
                </p>
                <pre>
                  <code>{enrollment.token}</code>
                </pre>
                <p>
                  Masukkan token saat diminta di mesin Agent. Jalankan sebagai
                  akun <strong>{host.osUser}</strong>.
                </p>
                <pre>
                  <code>{`./bin/agent enroll --hub ${window.location.origin} --host-id ${host.id} --socket ${host.socketName} --credential-file "$HOME/.config/tmux-monitor/agent.json"`}</code>
                </pre>
                <button
                  type="button"
                  className="button secondary"
                  onClick={() => setEnrollment(undefined)}
                >
                  Sembunyikan token
                </button>
              </>
            )}
          </>
        )
      )}
      <button
        type="button"
        className="button secondary"
        disabled={busy || saving}
        onClick={() => {
          setEnrollment(undefined);
          setError("");
          setConfirmRevoke(false);
          setRevision((current) => current + 1);
        }}
      >
        Muat ulang konfigurasi Agent
      </button>
    </div>
  );
}
