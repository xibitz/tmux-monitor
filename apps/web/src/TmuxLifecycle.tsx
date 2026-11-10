import { useEffect, useRef, useState } from "react";
import type { ReactNode } from "react";
import { api } from "./api";

type CommonProps = {
  hostId: string;
  epoch: string;
  revision: string;
  disabled: boolean;
  onRefresh: () => void;
};

type Props = CommonProps &
  (
    | { mode: "create"; session?: never }
    | { mode: "kill" | "create-window"; session: { id: string; name: string } }
  );

const splitDirections = [
  { value: "side-by-side", label: "Berdampingan" },
  { value: "stacked", label: "Atas dan bawah" },
];

type LayoutProps = CommonProps & {
  sessionId: string;
  window: { id: string; name: string; panes: { id: string }[] };
} & (
    | { mode: "kill-window"; pane?: never }
    | {
        mode: "kill-pane" | "split-pane";
        pane: { id: string; command: string };
      }
  );

export function TmuxLayoutMutation(props: LayoutProps) {
  const splitting = props.mode === "split-pane";
  const isWindow = props.mode === "kill-window";
  const { hostId, sessionId, window, pane } = props;
  const target = pane?.id ?? window.id;
  const kind = isWindow ? "window" : "pane";
  const action = splitting ? "Split pane" : `Hapus ${kind}`;
  const path = `/hosts/${encodeURIComponent(hostId)}/${kind}s/${encodeURIComponent(target)}`;
  const context = { sessionId, ...(pane ? { windowId: window.id } : {}) };
  const paneCount = isWindow ? window.panes.length : 1;

  return (
    <TmuxOperationForm
      {...props}
      action={action}
      targetLabel={`${action} ${isWindow ? window.name : target}`}
      buttonLabel={`${action} ${isWindow ? window.name : target}`}
      submitLabel={splitting ? "Buat pane baru" : action}
      prompt={
        splitting ? (
          <>
            Split pane <code>{target}</code> di window{" "}
            <strong>{window.name}</strong>. Layout berubah untuk semua klien
            yang melihat window ini. Perintah split tidak memilih pane baru.
          </>
        ) : (
          <>
            Hapus {kind} {isWindow && <strong>{window.name}</strong>} (
            <code>{target}</code>)
            {pane && (
              <>
                {" "}
                di window <strong>{window.name}</strong>
              </>
            )}
            ? Operasi ini menutup {paneCount} pane
            {pane?.command && (
              <>
                {" "}
                yang menjalankan <code>{pane.command}</code>
              </>
            )}
            . Proses di dalamnya dapat berhenti dan pilihan aktif klien bisa
            berubah.
          </>
        )
      }
      inputLabel={
        splitting ? "Arah split" : `Ketik ID ${kind} untuk konfirmasi`
      }
      choices={splitting ? splitDirections : undefined}
      confirmation={splitting ? undefined : target}
      consent={
        splitting
          ? "Saya memahami split mengubah layout bersama."
          : "Saya memahami proses di pane yang dihapus dapat berhenti."
      }
      method={splitting ? "POST" : "DELETE"}
      path={splitting ? `${path}/split` : path}
      payload={(value) => ({
        ...context,
        ...(splitting ? { direction: value } : { confirmation: value }),
      })}
    />
  );
}

export function TmuxLifecycle(props: Props) {
  const killing = props.mode === "kill";
  const creatingWindow = props.mode === "create-window";
  const target = props.session?.id ?? "";
  const resource = creatingWindow ? "window" : "session";
  const action = killing ? "Hentikan session" : `Buat ${resource}`;
  const sessionPath = target ? `/${encodeURIComponent(target)}` : "";
  const path = `/hosts/${encodeURIComponent(props.hostId)}/sessions${sessionPath}`;
  return (
    <TmuxOperationForm
      {...props}
      action={action}
      targetLabel={
        killing ? "Konfirmasi hentikan session" : `Buat ${resource} baru`
      }
      submitLabel={killing ? "Hentikan session ini" : `Buat ${resource} baru`}
      prompt={
        props.mode === "kill" ? (
          <>
            Hentikan <strong>{props.session.name}</strong> (
            <code>{props.session.id}</code>)? Klien pada session ini akan
            terlepas. Proses di window yang tidak dipakai session lain dapat
            berhenti.
          </>
        ) : props.mode === "create-window" ? (
          <>
            Buat window pada session <strong>{props.session.name}</strong> (
            <code>{props.session.id}</code>). Window baru terlihat oleh semua
            klien session dan dibuat tanpa memilihnya. Pembuatan tersedia untuk
            session yang tidak tergabung dalam grup tmux.
          </>
        ) : (
          <>
            Buat session baru pada server tmux ini. Session dimulai terpisah
            dari terminal yang sedang dibuka klien lain.
          </>
        )
      }
      inputLabel={
        killing ? "Ketik ID session untuk konfirmasi" : `Nama ${resource} baru`
      }
      confirmation={killing ? target : undefined}
      consent={
        killing
          ? "Saya memahami proses pada session ini dapat berhenti."
          : creatingWindow
            ? "Saya memahami window baru akan muncul dalam session ini."
            : "Saya memahami session baru akan dibuat pada host ini."
      }
      method={killing ? "DELETE" : "POST"}
      path={creatingWindow ? `${path}/windows` : path}
      payload={(value) => (killing ? { confirmation: value } : { name: value })}
    />
  );
}

type FormProps = Pick<
  CommonProps,
  "epoch" | "revision" | "disabled" | "onRefresh"
> & {
  action: string;
  targetLabel: string;
  buttonLabel?: string;
  submitLabel: string;
  prompt: ReactNode;
  inputLabel: string;
  confirmation?: string;
  choices?: { value: string; label: string }[];
  consent: string;
  method: "POST" | "DELETE";
  path: string;
  payload: (value: string) => object;
};

function TmuxOperationForm({
  epoch,
  revision,
  disabled,
  onRefresh,
  action,
  targetLabel,
  buttonLabel,
  submitLabel,
  prompt,
  inputLabel,
  confirmation,
  choices,
  consent,
  method,
  path,
  payload,
}: FormProps) {
  const [open, setOpen] = useState(false);
  const [value, setValue] = useState("");
  const [confirmed, setConfirmed] = useState(false);
  const [busy, setBusy] = useState(false);
  const [attempted, setAttempted] = useState(false);
  const [error, setError] = useState("");
  const alive = useRef(true);
  const sending = useRef(false);
  const field = useRef<HTMLLabelElement>(null);
  const killing = method === "DELETE";
  const valid = choices
    ? choices.some((choice) => choice.value === value)
    : confirmation !== undefined
      ? value === confirmation
      : /^[A-Za-z0-9_][A-Za-z0-9_-]{0,63}$/.test(value);
  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
    };
  }, []);
  useEffect(() => {
    if (open)
      field.current?.querySelector<HTMLElement>("input, select")?.focus();
  }, [open]);

  async function submit() {
    if (sending.current || attempted || !confirmed || !valid || disabled)
      return;
    sending.current = true;
    setAttempted(true);
    setBusy(true);
    setError("");
    try {
      const key = crypto.randomUUID().replaceAll("-", "");
      await api(
        path,
        method,
        { epoch, revision, confirmed: true, ...payload(value) },
        key,
      );
      if (alive.current) onRefresh();
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
        aria-label={attempted ? undefined : buttonLabel}
        disabled={disabled || busy}
        onClick={() => (attempted ? onRefresh() : setOpen(true))}
      >
        {attempted ? "Periksa hasil operasi" : action}
      </button>
    );
  return (
    <section className="session-rename" aria-label={targetLabel}>
      <p>{prompt}</p>
      {!attempted && (
        <>
          <label ref={field}>
            {inputLabel}
            {choices ? (
              <select
                value={value}
                disabled={busy || disabled}
                onChange={(event) => setValue(event.target.value)}
              >
                <option value="" disabled>
                  Pilih arah
                </option>
                {choices.map((choice) => (
                  <option key={choice.value} value={choice.value}>
                    {choice.label}
                  </option>
                ))}
              </select>
            ) : (
              <input
                value={value}
                maxLength={killing ? 24 : 64}
                disabled={busy || disabled}
                autoComplete="off"
                autoCapitalize="off"
                spellCheck={false}
                onChange={(event) => setValue(event.target.value)}
              />
            )}
          </label>
          {!choices && (
            <p className="panel-description">
              {confirmation !== undefined
                ? `Ketik tepat ${confirmation}. Perubahan ini berlaku bagi semua klien session tersebut.`
                : "1–64 huruf ASCII, angka, _ atau -. Karakter pertama tidak boleh -."}
            </p>
          )}
          <label className="rename-confirm">
            <input
              type="checkbox"
              checked={confirmed}
              disabled={busy || disabled}
              onChange={(event) => setConfirmed(event.target.checked)}
            />
            {consent}
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
            disabled={busy || disabled}
            onClick={onRefresh}
          >
            {busy ? "Memproses…" : "Periksa inventaris"}
          </button>
        ) : (
          <button
            type="button"
            className={killing ? "button danger" : "button primary"}
            disabled={disabled || busy || !confirmed || !valid}
            onClick={() => void submit()}
          >
            {submitLabel}
          </button>
        )}
      </div>
    </section>
  );
}
