import { useEffect, useRef, useState } from "react";
import { api } from "./api";

export function SessionRename({
  hostId,
  session,
  epoch,
  disabled,
  onRefresh,
}: {
  hostId: string;
  session: { id: string; name: string; renameRevision: string };
  epoch: string;
  disabled: boolean;
  onRefresh: () => void;
}) {
  const [open, setOpen] = useState(false);
  const [name, setName] = useState("");
  const [confirmed, setConfirmed] = useState(false);
  const [busy, setBusy] = useState(false);
  const [attempted, setAttempted] = useState(false);
  const [error, setError] = useState("");
  const alive = useRef(true);
  const sending = useRef(false);
  const input = useRef<HTMLInputElement>(null);
  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
    };
  }, []);
  useEffect(() => {
    if (open) input.current?.focus();
  }, [open]);
  async function submit() {
    if (
      sending.current ||
      attempted ||
      !confirmed ||
      disabled ||
      !/^[A-Za-z0-9_][A-Za-z0-9_-]{0,63}$/.test(name)
    )
      return;
    sending.current = true;
    setAttempted(true);
    setBusy(true);
    setError("");
    try {
      const key = crypto.randomUUID().replaceAll("-", "");
      await api(
        `/hosts/${encodeURIComponent(hostId)}/sessions/${encodeURIComponent(session.id)}`,
        "PATCH",
        { name, epoch, revision: session.renameRevision, confirmed: true },
        key,
      );
      if (alive.current) onRefresh();
    } catch (e) {
      if (alive.current) {
        setError((e as Error).message);
        setName("");
        setConfirmed(false);
      }
    } finally {
      if (alive.current) setBusy(false);
    }
  }
  if (!open)
    return (
      <button
        type="button"
        className="button secondary"
        disabled={disabled || busy}
        onClick={() => (attempted ? onRefresh() : setOpen(true))}
      >
        {attempted ? "Periksa hasil ubah nama" : "Ubah nama"}
      </button>
    );
  return (
    <section
      className="session-rename"
      aria-label={`Ubah nama ${session.name}`}
    >
      <p>
        Ubah nama session <strong>{session.name}</strong> ({session.id}).
        Perubahan terlihat oleh semua klien pada host ini.
      </p>
      {!attempted && (
        <>
          <label>
            Nama session baru
            <input
              ref={input}
              value={name}
              disabled={busy || disabled}
              maxLength={64}
              autoComplete="off"
              autoCapitalize="off"
              spellCheck={false}
              onChange={(e) => setName(e.target.value)}
            />
          </label>
          <p className="panel-description">
            1–64 huruf ASCII, angka, _ atau -. Karakter pertama tidak boleh -.
          </p>
          <label className="rename-confirm">
            <input
              type="checkbox"
              checked={confirmed}
              disabled={busy || disabled}
              onChange={(e) => setConfirmed(e.target.checked)}
            />
            Saya memahami perubahan ini berlaku untuk semua klien.
          </label>
        </>
      )}
      {error && (
        <p className="error-notice" role="alert">
          {error} Periksa inventaris sebelum membuat permintaan baru.
        </p>
      )}
      <div className="modal-actions">
        <button
          type="button"
          className="button secondary"
          disabled={busy}
          onClick={() => {
            setOpen(false);
            setName("");
            setConfirmed(false);
          }}
        >
          Batal
        </button>
        {attempted ? (
          <button
            type="button"
            className="button secondary"
            disabled={busy || disabled}
            onClick={onRefresh}
          >
            {busy ? "Menyimpan…" : "Periksa inventaris"}
          </button>
        ) : (
          <button
            type="button"
            className="button primary"
            disabled={
              busy ||
              disabled ||
              !confirmed ||
              !/^[A-Za-z0-9_][A-Za-z0-9_-]{0,63}$/.test(name)
            }
            onClick={() => void submit()}
          >
            Simpan nama session
          </button>
        )}
      </div>
    </section>
  );
}
