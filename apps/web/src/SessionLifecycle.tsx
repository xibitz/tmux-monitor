import { useEffect, useRef, useState } from "react";
import { api } from "./api";

type Props = {
  hostId: string;
  epoch: string;
  revision: string;
  disabled: boolean;
  onRefresh: () => void;
} & (
  | { mode: "create"; session?: never }
  | { mode: "kill"; session: { id: string; name: string } }
);

// Source addition only; validation/interaction tests are intentionally pending.
export function SessionLifecycle(props: Props) {
  const [open, setOpen] = useState(false);
  const [value, setValue] = useState("");
  const [confirmed, setConfirmed] = useState(false);
  const [busy, setBusy] = useState(false);
  const [attempted, setAttempted] = useState(false);
  const [error, setError] = useState("");
  const alive = useRef(true);
  const sending = useRef(false);
  const input = useRef<HTMLInputElement>(null);
  const killing = props.mode === "kill";
  const target = props.mode === "kill" ? props.session.id : "";
  const valid = killing
    ? value === target
    : /^[A-Za-z0-9_][A-Za-z0-9_-]{0,63}$/.test(value);
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
    if (sending.current || attempted || !confirmed || !valid || props.disabled)
      return;
    sending.current = true;
    setAttempted(true);
    setBusy(true);
    setError("");
    const payload = killing
      ? {
          epoch: props.epoch,
          revision: props.revision,
          confirmation: value,
          confirmed: true,
        }
      : {
          epoch: props.epoch,
          revision: props.revision,
          name: value,
          confirmed: true,
        };
    const suffix = killing ? `/${encodeURIComponent(target)}` : "";
    try {
      const key = crypto.randomUUID().replaceAll("-", "");
      await api(
        `/hosts/${encodeURIComponent(props.hostId)}/sessions${suffix}`,
        killing ? "DELETE" : "POST",
        payload,
        key,
      );
      if (alive.current) props.onRefresh();
    } catch (error) {
      if (alive.current) {
        setError((error as Error).message);
        setValue("");
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
        className={killing ? "button danger" : "button secondary"}
        disabled={props.disabled || busy}
        onClick={() => (attempted ? props.onRefresh() : setOpen(true))}
      >
        {attempted
          ? "Periksa hasil operasi"
          : killing
            ? "Hentikan session"
            : "Buat session"}
      </button>
    );
  return (
    <section
      className="session-rename"
      aria-label={killing ? "Konfirmasi hentikan session" : "Buat session baru"}
    >
      {props.mode === "kill" ? (
        <p>
          Hentikan <strong>{props.session.name}</strong> (
          <code>{props.session.id}</code>)? Klien pada session ini akan
          terlepas. Proses di window yang tidak dipakai session lain dapat
          berhenti.
        </p>
      ) : (
        <p>
          Buat session baru pada server tmux ini. Session dimulai terpisah dari
          terminal yang sedang dibuka klien lain.
        </p>
      )}
      {!attempted && (
        <>
          <label>
            {killing
              ? "Ketik ID session untuk konfirmasi"
              : "Nama session baru"}
            <input
              ref={input}
              value={value}
              maxLength={killing ? 24 : 64}
              disabled={busy || props.disabled}
              autoComplete="off"
              autoCapitalize="off"
              spellCheck={false}
              onChange={(e) => setValue(e.target.value)}
            />
          </label>
          <p className="panel-description">
            {killing
              ? `Ketik tepat ${target}. Perubahan ini berlaku bagi semua klien session tersebut.`
              : "1–64 huruf ASCII, angka, _ atau -. Karakter pertama tidak boleh -."}
          </p>
          <label className="rename-confirm">
            <input
              type="checkbox"
              checked={confirmed}
              disabled={busy || props.disabled}
              onChange={(e) => setConfirmed(e.target.checked)}
            />
            {killing
              ? "Saya memahami proses pada session ini dapat berhenti."
              : "Saya memahami session baru akan dibuat pada host ini."}
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
            setValue("");
            setConfirmed(false);
          }}
        >
          Batal
        </button>
        {attempted ? (
          <button
            type="button"
            className="button secondary"
            disabled={busy || props.disabled}
            onClick={props.onRefresh}
          >
            {busy ? "Memproses…" : "Periksa inventaris"}
          </button>
        ) : (
          <button
            type="button"
            className={killing ? "button danger" : "button primary"}
            disabled={props.disabled || busy || !confirmed || !valid}
            onClick={() => void submit()}
          >
            {killing ? "Hentikan session ini" : "Buat session baru"}
          </button>
        )}
      </div>
    </section>
  );
}
