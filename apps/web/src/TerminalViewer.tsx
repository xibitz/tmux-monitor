import { useEffect, useRef, useState } from "react";
import { Eye, RefreshCw, ArrowLeft } from "lucide-react";
import { Terminal } from "@xterm/xterm";
import { api } from "./api";
import "@xterm/xterm/css/xterm.css";

const reasons: Record<string, string> = {
  terminal_size_policy:
    "Ukuran tmux belum aman untuk viewer. Diperlukan tmux 3.4+ dan window-size manual pada default global serta semua window session. Panduan ada di README bagian terminal read-only.",
  terminal_target_changed:
    "Session, window aktif, atau ukuran berubah. Periksa inventaris lalu buka ulang viewer.",
  access_revoked: "Akses atau konfigurasi SSH berubah. Viewer dilepas.",
  input_forbidden: "Viewer ini hanya dapat membaca output.",
  slow_consumer:
    "Viewer tertinggal menerima output. Buka ulang untuk tampilan baru.",
  ssh_closed:
    "Koneksi terminal SSH berakhir. Session tmux mungkin tetap berjalan.",
  disconnected: "Koneksi ke hub terputus. Session tmux tetap dikelola host.",
  timeout: "Batas waktu koneksi SSH terlampaui.",
  host_key_mismatch:
    "Host key tidak cocok dengan key terverifikasi. Koneksi dihentikan.",
  authentication_failed: "Autentikasi SSH gagal. Periksa konfigurasi host.",
  pty_unavailable: "Host SSH tidak menyediakan PTY yang diperlukan.",
  command_failed: "Session tmux tidak dapat diperiksa. Muat ulang inventaris.",
};

// xterm creates scoped styles at runtime. Give only those elements the nonce
// served with index.html; retain the production CSP without unsafe-inline.
function terminalDocument(): Document {
  const nonce = document.querySelector<HTMLMetaElement>(
    'meta[name="csp-nonce"]',
  )?.content;
  return new Proxy(document, {
    get(target, property) {
      if (property === "createElement") {
        return (tag: string, options?: ElementCreationOptions) => {
          const element = target.createElement(tag, options);
          if (
            tag.toLowerCase() === "style" &&
            nonce &&
            nonce !== "__CSP_NONCE__"
          )
            element.setAttribute("nonce", nonce);
          return element;
        };
      }
      const value = Reflect.get(target, property, target);
      return typeof value === "function" ? value.bind(target) : value;
    },
  });
}

function openTerminal(terminal: Terminal, element: HTMLElement) {
  // xterm 6.0's Viewport currently uses window.document even when a document
  // override was supplied. Scope its synchronous style creation to open(),
  // restoring the native method before yielding to any other browser work.
  const createElement = document.createElement;
  const nonce = document.querySelector<HTMLMetaElement>(
    'meta[name="csp-nonce"]',
  )?.content;
  document.createElement = function (
    tag: string,
    options?: ElementCreationOptions,
  ) {
    const created = createElement.call(document, tag, options);
    if (tag.toLowerCase() === "style" && nonce && nonce !== "__CSP_NONCE__")
      created.setAttribute("nonce", nonce);
    return created;
  } as typeof document.createElement;
  try {
    terminal.open(element);
  } finally {
    document.createElement = createElement;
  }
}

export function TerminalViewer({
  hostId,
  hostLabel,
  sessionId,
  sessionName,
  epoch,
  onBack,
}: {
  hostId: string;
  hostLabel: string;
  sessionId: string;
  sessionName: string;
  epoch: string;
  onBack: () => void;
}) {
  const element = useRef<HTMLDivElement>(null);
  const [status, setStatus] = useState("Menghubungkan…");
  const [connected, setConnected] = useState(false);
  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    let active = true;
    let socket: WebSocket | undefined;
    let terminal: Terminal | undefined;
    let viewer = "";
    let sequence = 0n;
    let pending = 0;
    setConnected(false);
    setStatus("Memverifikasi host dan session…");
    function finish(reason: string) {
      if (!active) return;
      setConnected(false);
      setStatus(
        reasons[reason] ??
          "Viewer berakhir. Periksa koneksi dan inventaris sebelum membuka ulang.",
      );
      socket?.close();
    }
    void (async () => {
      try {
        const { ticket } = await api<{ ticket: string }>(
          "/terminal/tickets",
          "POST",
          { hostId, sessionId, epoch },
        );
        if (!active) return;
        const url = new URL("/ws/v1/terminal", window.location.href);
        url.protocol = url.protocol === "https:" ? "wss:" : "ws:";
        socket = new WebSocket(url, ["tmux.v1", `ticket.${ticket}`]);
        socket.binaryType = "arraybuffer";
        socket.onmessage = (event: MessageEvent<string | ArrayBuffer>) => {
          if (!active) return;
          if (typeof event.data === "string") {
            try {
              const message = JSON.parse(event.data);
              if (
                message.type === "terminal.opened" &&
                !terminal &&
                message.mode === "read_only" &&
                message.generation === 1 &&
                message.sequence === 0 &&
                /^[0-9a-f]{32}$/.test(message.viewerId) &&
                Number.isInteger(message.cols) &&
                message.cols >= 2 &&
                message.cols <= 500 &&
                Number.isInteger(message.rows) &&
                message.rows >= 2 &&
                message.rows <= 205
              ) {
                viewer = message.viewerId;
                terminal = new Terminal({
                  cols: message.cols,
                  rows: message.rows,
                  disableStdin: true,
                  cursorBlink: false,
                  scrollback: 1000,
                  fontSize: 12,
                  fontFamily: "monospace",
                  documentOverride: terminalDocument(),
                  linkHandler: { activate: () => {} },
                  windowOptions: {},
                  logLevel: "off",
                  theme: { background: "#101813", foreground: "#e3eadd" },
                });
                terminal.parser.registerOscHandler(52, () => true);
                terminal.attachCustomKeyEventHandler(() => false);
                openTerminal(terminal, element.current!);
                setStatus(`Terhubung · ${message.cols} × ${message.rows}`);
                setConnected(true);
              } else if (
                message.type === "terminal.closed" ||
                message.type === "resync_required" ||
                message.type === "error"
              ) {
                finish(message.reason ?? message.code);
              } else finish("protocol_invalid");
            } catch {
              finish("protocol_invalid");
            }
            return;
          }
          const bytes = new Uint8Array(event.data);
          if (!terminal || bytes.length <= 30 || bytes.length > 65536) {
            finish("protocol_invalid");
            return;
          }
          const header = new DataView(event.data);
          const id = Array.from(bytes.subarray(14, 30), (b) =>
            b.toString(16).padStart(2, "0"),
          ).join("");
          if (
            bytes[0] !== 1 ||
            bytes[1] !== 0 ||
            header.getUint32(2) !== 1 ||
            header.getBigUint64(6) !== sequence + 1n ||
            id !== viewer
          ) {
            finish("protocol_invalid");
            return;
          }
          sequence++;
          const data = bytes.subarray(30);
          pending += data.length;
          if (pending > 1024 * 1024) {
            finish("slow_consumer");
            return;
          }
          terminal.write(data, () => {
            pending -= data.length;
          });
        };
        socket.onerror = () => finish("disconnected");
        socket.onclose = () => {
          if (active) {
            setConnected(false);
            setStatus((current) =>
              current.startsWith("Terhubung") ||
              current.startsWith("Memverifikasi")
                ? reasons.disconnected
                : current,
            );
          }
        };
      } catch (error) {
        if (active) setStatus((error as Error).message);
      }
    })();
    return () => {
      active = false;
      if (socket?.readyState === WebSocket.OPEN)
        socket.send(JSON.stringify({ type: "terminal.detach" }));
      socket?.close();
      terminal?.dispose();
    };
  }, [hostId, sessionId, epoch, attempt]);
  return (
    <section className="terminal-viewer" aria-label="Terminal read-only">
      <div className="terminal-context">
        <span className="viewer-mode">
          <Eye size={14} /> Read-only
        </span>
        <strong>
          {hostLabel} / {sessionName}
        </strong>
        <code>{sessionId}</code>
      </div>
      <p className="viewer-status" role="status">
        {status}
      </p>
      <div className="terminal-scroll">
        <div ref={element} />
      </div>
      <p className="panel-description">
        Geser untuk melihat layar remote. Ukuran mengikuti tmux di host. Menutup
        viewer hanya melepas koneksi.
      </p>
      <div className="modal-actions">
        <button className="button secondary" onClick={onBack}>
          <ArrowLeft size={16} /> Kembali
        </button>
        <button
          className="button secondary"
          disabled={connected}
          onClick={() => setAttempt((value) => value + 1)}
        >
          <RefreshCw size={16} /> Buka ulang
        </button>
      </div>
    </section>
  );
}
