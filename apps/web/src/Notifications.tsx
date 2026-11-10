import { useEffect, useRef, useState } from "react";
import { Bell, RefreshCw } from "lucide-react";
import { api } from "./api";

type Notice = {
  id: number;
  hostLabel: string;
  source: "codex" | "claude";
  kind: "turn_completed" | "attention_required" | "error" | "unknown";
  confidence: "verified_pane" | "unknown";
  sessionId?: string;
  paneId?: string;
  occurredAt: string;
};

const labels = {
  turn_completed: "Giliran selesai",
  attention_required: "Perlu perhatian",
  error: "Terjadi kesalahan",
  unknown: "Jenis peristiwa belum diketahui",
};

export function useNotifications(online: boolean, reading: boolean) {
  const [notices, setNotices] = useState<Notice[]>([]);
  const [enabled, setEnabled] = useState(false);
  const [unread, setUnread] = useState(false);
  const [error, setError] = useState("");
  const latest = useRef<number | undefined>(undefined);
  const isReading = useRef(reading);
  const refresh = useRef<() => void>(() => {});
  useEffect(() => {
    isReading.current = reading;
    if (reading) {
      setUnread(false);
      refresh.current();
    }
  }, [reading]);
  useEffect(() => {
    if (!online) {
      setNotices([]);
      setUnread(false);
      latest.current = undefined;
      return;
    }
    let active = true;
    let pending = false;
    async function load() {
      if (pending || document.visibilityState !== "visible") return;
      pending = true;
      try {
        const result = await api<{ enabled: boolean; notices: Notice[] }>(
          "/notifications",
        );
        if (!active) return;
        setEnabled(result.enabled);
        setNotices(result.notices);
        setError("");
        const head = result.notices[0]?.id ?? 0;
        if (
          latest.current !== undefined &&
          head > latest.current &&
          !isReading.current
        )
          setUnread(true);
        latest.current = head;
      } catch (error) {
        if (active) setError((error as Error).message);
      } finally {
        pending = false;
      }
    }
    refresh.current = () => void load();
    void load();
    const timer = window.setInterval(refresh.current, 15_000);
    document.addEventListener("visibilitychange", refresh.current);
    return () => {
      active = false;
      window.clearInterval(timer);
      document.removeEventListener("visibilitychange", refresh.current);
      refresh.current = () => {};
    };
  }, [online]);
  return { notices, enabled, unread, error, reload: () => refresh.current() };
}

export function Notifications({
  notices,
  enabled,
  error,
  reload,
}: ReturnType<typeof useNotifications>) {
  return (
    <section className="audit-panel" aria-label="Kotak masuk coding agent">
      <div className="panel-heading">
        <h2>Kotak masuk</h2>
        <button
          className="icon-button bordered"
          onClick={reload}
          aria-label="Muat ulang kotak masuk"
        >
          <RefreshCw size={16} />
        </button>
      </div>
      {enabled && <PushControl />}
      {error && (
        <p className="error-notice" role="status">
          {error}
        </p>
      )}
      {!enabled && !error && (
        <p className="panel-description">Hook notifikasi belum diaktifkan.</p>
      )}
      {enabled && notices.length === 0 && !error && (
        <p className="panel-description">Belum ada notifikasi.</p>
      )}
      <div className="audit-list">
        {notices.map((notice) => (
          <div className="audit-row" key={notice.id}>
            <span className="audit-icon">
              <Bell size={16} />
            </span>
            <div>
              <strong>{labels[notice.kind]}</strong>
              <span>
                {notice.source === "codex" ? "Codex" : "Claude Code"} ·{" "}
                {notice.hostLabel} ·{" "}
                {notice.confidence === "verified_pane"
                  ? `Session ${notice.sessionId} / pane ${notice.paneId}`
                  : "Session dan pane belum diketahui"}
              </span>
            </div>
            <time dateTime={notice.occurredAt}>
              {new Date(notice.occurredAt).toLocaleString("id-ID")}
            </time>
          </div>
        ))}
      </div>
    </section>
  );
}

function PushControl() {
  const [key, setKey] = useState("");
  const [subscription, setSubscription] = useState<PushSubscription | null>(
    null,
  );
  const [message, setMessage] = useState("");
  const [busy, setBusy] = useState(false);
  const registration = useRef<ServiceWorkerRegistration | undefined>(undefined);
  const supported =
    window.isSecureContext &&
    "serviceWorker" in navigator &&
    "PushManager" in window &&
    "Notification" in window;
  useEffect(() => {
    if (!supported) return;
    let active = true;
    async function load() {
      try {
        const config = await api<{ enabled: boolean; publicKey?: string }>(
          "/notifications/push",
        );
        const worker = await navigator.serviceWorker.getRegistration("/");
        if (!active) return;
        if (!worker?.active) {
          if (config.enabled)
            setMessage(
              "Muat ulang PWA setelah service worker aktif untuk menyalakan Web Push.",
            );
          return;
        }
        registration.current = worker;
        const existing = await worker.pushManager.getSubscription();
        if (!active) return;
        setSubscription(existing);
        setKey(config.enabled ? (config.publicKey ?? "") : "");
      } catch (error) {
        if (active) setMessage((error as Error).message);
      }
    }
    void load();
    return () => {
      active = false;
    };
  }, [supported]);
  async function toggle() {
    const worker = registration.current;
    if (!worker || busy) return;
    setBusy(true);
    setMessage("");
    try {
      if (subscription) {
        await api("/notifications/push", "POST", {
          endpoint: subscription.endpoint,
          enabled: false,
        });
        await subscription.unsubscribe();
        setSubscription(null);
        return;
      }
      if (!key || (await Notification.requestPermission()) !== "granted") {
        setMessage(
          "Izin Web Push belum diberikan. Kotak masuk tetap tersedia.",
        );
        return;
      }
      const created = await worker.pushManager.subscribe({
        userVisibleOnly: true,
        applicationServerKey: Uint8Array.from(
          atob(key.replace(/-/g, "+").replace(/_/g, "/")),
          (char) => char.charCodeAt(0),
        ),
      });
      try {
        await api("/notifications/push", "POST", {
          endpoint: created.endpoint,
          enabled: true,
        });
        setSubscription(created);
      } catch (error) {
        await created.unsubscribe().catch(() => {});
        throw error;
      }
    } catch (error) {
      setMessage((error as Error).message);
    } finally {
      setBusy(false);
    }
  }
  return (
    <div className="panel-description">
      {supported ? (
        <>
          {(key || subscription) && (
            <button
              className="button secondary"
              disabled={busy}
              onClick={() => void toggle()}
            >
              {subscription
                ? "Matikan Web Push di perangkat ini"
                : "Aktifkan Web Push di perangkat ini"}
            </button>
          )}
          {message && <p role="status">{message}</p>}
        </>
      ) : (
        <p>Web Push tidak tersedia di perangkat ini. Gunakan kotak masuk.</p>
      )}
    </div>
  );
}
