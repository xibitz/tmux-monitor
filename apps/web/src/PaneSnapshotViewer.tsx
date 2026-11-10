import { useEffect, useRef, useState } from "react";
import { api } from "./api";

export type PaneSnapshotTarget = {
  hostId: string;
  paneId: string;
  windowName: string;
  epoch: string;
  revision: string;
};

type Snapshot = {
  paneId: string;
  capturedAt: string;
  text: string;
  truncated: boolean;
};

export function PaneSnapshotViewer({
  target,
  hostLabel,
  onBack,
}: {
  target: PaneSnapshotTarget;
  hostLabel: string;
  onBack: () => void;
}) {
  const [snapshot, setSnapshot] = useState<Snapshot>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const active = useRef(false);
  const pending = useRef(false);
  const sequence = useRef(0);
  const captureButton = useRef<HTMLButtonElement>(null);

  useEffect(() => {
    active.current = true;
    captureButton.current?.focus();
    function discard() {
      sequence.current++;
      pending.current = false;
      setSnapshot(undefined);
      setBusy(false);
      setError("");
    }
    window.addEventListener("pagehide", discard);
    return () => {
      active.current = false;
      sequence.current++;
      pending.current = false;
      window.removeEventListener("pagehide", discard);
    };
  }, []);

  async function capture() {
    if (!active.current || pending.current) return;
    pending.current = true;
    const request = ++sequence.current;
    const current = () => active.current && request === sequence.current;
    setSnapshot(undefined);
    setError("");
    setBusy(true);
    try {
      const result = await api<Snapshot>(
        `/hosts/${encodeURIComponent(target.hostId)}/panes/${encodeURIComponent(target.paneId)}/snapshot`,
        "POST",
        { epoch: target.epoch, revision: target.revision },
      );
      if (!current()) return;
      if (
        !result ||
        result.paneId !== target.paneId ||
        typeof result.text !== "string" ||
        result.text.length > 64 * 1024 ||
        typeof result.truncated !== "boolean" ||
        !Number.isFinite(Date.parse(result.capturedAt))
      ) {
        throw new Error("Snapshot yang diterima tidak valid.");
      }
      setSnapshot(result);
    } catch (err) {
      if (current()) setError((err as Error).message);
    } finally {
      if (current()) {
        pending.current = false;
        setBusy(false);
      }
    }
  }

  return (
    <section className="pane-snapshot" aria-label="Snapshot pane">
      <h3>Snapshot pane {target.paneId}</h3>
      <p className="panel-description">
        {hostLabel} / {target.windowName}
      </p>
      <p className="panel-description">
        Tampilan teks sekali ambil. Snapshot menampilkan isi pane dan hingga 100
        baris riwayat, tanpa mengubah pane aktif.
      </p>
      {busy && <p role="status">Mengambil snapshot…</p>}
      {error && (
        <p className="error-notice" role="alert">
          {error}
        </p>
      )}
      {snapshot && (
        <>
          <p className="panel-description" role="status">
            Diambil{" "}
            <time dateTime={snapshot.capturedAt}>
              {new Date(snapshot.capturedAt).toLocaleString("id-ID")}
            </time>
            {snapshot.truncated &&
              " · Isi terpotong; bagian terbaru ditampilkan."}
          </p>
          <pre tabIndex={0} aria-label="Isi snapshot pane">
            {snapshot.text || "Pane kosong."}
          </pre>
        </>
      )}
      <div className="modal-actions">
        <button type="button" className="button secondary" onClick={onBack}>
          Kembali ke inventaris
        </button>
        <button
          type="button"
          className="button primary"
          ref={captureButton}
          disabled={busy}
          onClick={() => void capture()}
        >
          {busy ? "Mengambil…" : snapshot ? "Ambil lagi" : "Ambil snapshot"}
        </button>
      </div>
    </section>
  );
}
