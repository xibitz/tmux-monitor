import { useEffect, useRef, useState } from "react";
import { Eye, RefreshCw, ArrowLeft, Keyboard } from "lucide-react";
import { Terminal } from "@xterm/xterm";
import { api } from "./api";
import { TerminalToolbar } from "./TerminalToolbar";
import { encodeTerminalKey } from "./terminalKeys";
import type { Modifiers, TerminalKey } from "./terminalKeys";
import "@xterm/xterm/css/xterm.css";

const reasons: Record<string, string> = {
  terminal_size_policy:
    "Ukuran tmux belum aman untuk viewer. Diperlukan tmux 3.7+ dan window-size manual pada default global serta semua window session. Panduan ada di README bagian terminal SSH.",
  terminal_target_changed:
    "Session, window aktif, atau ukuran berubah. Periksa inventaris lalu buka ulang viewer.",
  access_revoked: "Akses atau konfigurasi SSH berubah. Viewer dilepas.",
  mode_changed: "Menyiapkan viewer dengan mode baru…",
  input_uncertain:
    "Koneksi input berakhir. Sebagian input mungkin sudah diproses; tidak akan dikirim ulang.",
  input_invalid:
    "Frame input tidak sesuai dengan viewer ini. Buka ulang dalam read-only.",
  input_limit: "Batas input terlampaui. Viewer dilepas tanpa mengulang input.",
  hub_shutdown:
    "Hub dihentikan. Buka ulang setelah hub tersedia; mode kembali read-only.",
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

type ViewerControl = {
  viewer: string;
  alive: boolean;
  paused: boolean;
  interactive: boolean;
  scope: string;
  cursorMode: () => boolean;
  focus: () => void;
  send: (data: Uint8Array) => void;
  paste: (text: string) => void;
};

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
  const [connection, setConnection] = useState<{
    attempt: number;
    ticket?: string;
    scope?: string;
  }>({ attempt: 0 });
  const scope = JSON.stringify([hostId, sessionId, epoch]);
  const [prefix, setPrefix] = useState("C-b");
  useEffect(() => setPrefix("C-b"), [scope]);
  const [interactive, setInteractive] = useState(false);
  const [canWrite, setCanWrite] = useState(false);
  const [switching, setSwitching] = useState(false);
  const [confirmInput, setConfirmInput] = useState(false);
  const [modeError, setModeError] = useState("");
  const [pastePending, setPastePending] = useState<string>();
  const pasteCancel = useRef<HTMLButtonElement>(null);
  useEffect(() => {
    if (pastePending !== undefined) pasteCancel.current?.focus();
  }, [pastePending]);
  const controlRef = useRef<ViewerControl | undefined>(undefined);
  async function changeMode(mode: "interactive" | "read_only") {
    const control = controlRef.current;
    if (!control?.alive) return;
    control.paused = true;
    setConfirmInput(false);
    setPastePending(undefined);
    setSwitching(true);
    setModeError("");
    try {
      const result = await api<{ ticket: string }>(
        `/terminal/viewers/${control.viewer}/mode-tickets`,
        "POST",
        { mode },
      );
      if (controlRef.current === control)
        setConnection((current) => ({
          attempt: current.attempt + 1,
          ticket: result.ticket,
          scope: control.scope,
        }));
    } catch (error) {
      if (controlRef.current === control) {
        setModeError((error as Error).message);
        control.paused = false;
        setSwitching(false);
      }
    }
  }

  useEffect(() => {
    let active = true;
    let socket: WebSocket | undefined;
    let terminal: Terminal | undefined;
    let viewer = "";
    let sequence = 0n;
    let pending = 0;
    setConnected(false);
    setInteractive(false);
    setCanWrite(false);
    setSwitching(false);
    setConfirmInput(false);
    setPastePending(undefined);
    setModeError("");
    let control: ViewerControl | undefined;
    let pasteHandler: ((event: ClipboardEvent) => void) | undefined;
    setStatus("Memverifikasi host dan session…");
    function finish(reason: string) {
      if (!active) return;
      setConnected(false);
      setInteractive(false);
      setCanWrite(false);
      setPastePending(undefined);
      if (control) control.alive = false;
      if (terminal) terminal.options.disableStdin = true;
      setStatus(
        reasons[reason] ??
          "Viewer berakhir. Periksa koneksi dan inventaris sebelum membuka ulang.",
      );
      socket?.close();
    }
    void (async () => {
      try {
        const { ticket } =
          connection.ticket && connection.scope === scope
            ? { ticket: connection.ticket }
            : await api<{ ticket: string }>("/terminal/tickets", "POST", {
                hostId,
                sessionId,
                epoch,
              });
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
                (message.mode === "read_only" ||
                  message.mode === "interactive") &&
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
                  disableStdin: message.mode !== "interactive",
                  cursorBlink: message.mode === "interactive",
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
                let inputSequence = 0n;
                const writable = message.mode === "interactive";
                control = {
                  viewer,
                  alive: true,
                  paused: false,
                  interactive: writable,
                  scope,
                  cursorMode: () =>
                    terminal?.modes.applicationCursorKeysMode ?? false,
                  focus: () => terminal?.focus(),
                  send: (bytes) => {
                    if (
                      !active ||
                      !control?.alive ||
                      control.paused ||
                      !writable ||
                      socket?.readyState !== WebSocket.OPEN
                    )
                      return;
                    if (
                      bytes.length > 32768 ||
                      socket.bufferedAmount + bytes.length > 65536
                    ) {
                      finish("input_limit");
                      return;
                    }
                    for (
                      let offset = 0;
                      offset < bytes.length;
                      offset += 16384
                    ) {
                      const chunk = bytes.subarray(offset, offset + 16384);
                      const frame = new Uint8Array(30 + chunk.length);
                      const header = new DataView(frame.buffer);
                      frame[0] = 1;
                      frame[1] = 1;
                      header.setUint32(2, 1);
                      header.setBigUint64(6, ++inputSequence);
                      frame.set(
                        Uint8Array.from(viewer.match(/../g)!, (hex) =>
                          parseInt(hex, 16),
                        ),
                        14,
                      );
                      frame.set(chunk, 30);
                      try {
                        socket.send(frame);
                      } catch {
                        finish("input_uncertain");
                        return;
                      }
                    }
                  },
                  paste: (text) => {
                    if (
                      active &&
                      control?.alive &&
                      !control.paused &&
                      writable
                    ) {
                      terminal?.paste(text);
                      terminal?.focus();
                    }
                  },
                };
                controlRef.current = control;
                terminal.onData((data) =>
                  control?.send(new TextEncoder().encode(data)),
                );
                terminal.onBinary((data) =>
                  control?.send(
                    Uint8Array.from(data, (char) => char.charCodeAt(0)),
                  ),
                );
                openTerminal(terminal, element.current!);
                pasteHandler = (event: ClipboardEvent) => {
                  event.preventDefault();
                  event.stopImmediatePropagation();
                  if (!control?.alive || control.paused || !writable) return;
                  const text = event.clipboardData?.getData("text/plain") ?? "";
                  if (new TextEncoder().encode(text).length > 16384) {
                    setModeError(
                      "Paste maksimal 16 KiB. Kurangi teks lalu coba lagi.",
                    );
                    return;
                  }
                  if (/[\r\n]/.test(text)) setPastePending(text);
                  else control.paste(text);
                };
                element.current!.addEventListener("paste", pasteHandler, true);
                setInteractive(writable);
                setCanWrite(message.canWrite === true);

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
            setInteractive(false);
            setCanWrite(false);
            setPastePending(undefined);
            if (control) control.alive = false;
            if (terminal) terminal.options.disableStdin = true;
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
      if (controlRef.current === control) controlRef.current = undefined;
      if (control) control.alive = false;
      if (pasteHandler)
        element.current?.removeEventListener("paste", pasteHandler, true);
      if (socket?.readyState === WebSocket.OPEN)
        socket.send(JSON.stringify({ type: "terminal.detach" }));
      socket?.close();
      terminal?.dispose();
    };
  }, [hostId, sessionId, epoch, connection]);
  return (
    <section className="terminal-viewer" aria-label="Terminal session">
      <div className="viewer-header">
        <div className="terminal-context">
          <span className="viewer-mode">
            {interactive ? <Keyboard size={14} /> : <Eye size={14} />}{" "}
            {interactive ? "Interaktif" : "Read-only"}
          </span>
          <strong>
            {hostLabel} / {sessionName}
          </strong>
          <code>{sessionId}</code>
        </div>
        <p className="viewer-status" role="status">
          {status}
        </p>
        {canWrite && connected && (
          <button
            className="button secondary"
            disabled={switching}
            onClick={() =>
              interactive ? void changeMode("read_only") : setConfirmInput(true)
            }
          >
            {switching
              ? "Mengganti mode…"
              : interactive
                ? "Kembali read-only"
                : "Aktifkan input"}
          </button>
        )}
      </div>
      {modeError && (
        <p className="error-notice" role="alert">
          {modeError}
        </p>
      )}
      {confirmInput && (
        <div className="input-confirmation">
          <p>
            Aktifkan input untuk {hostLabel} / {sessionName}? Input dan navigasi
            tmux dapat memengaruhi klien desktop pada session yang sama.
          </p>
          <div className="modal-actions">
            <button
              className="button secondary"
              onClick={() => setConfirmInput(false)}
            >
              Batal
            </button>
            <button
              className="button primary"
              onClick={() => void changeMode("interactive")}
            >
              Aktifkan untuk viewer ini
            </button>
          </div>
        </div>
      )}
      {pastePending !== undefined && (
        <div
          className="input-confirmation"
          role="region"
          aria-label="Konfirmasi paste"
        >
          <p>
            Tinjau teks sebelum ditempel ke {hostLabel} / {sessionName}.
          </p>
          <pre>{pastePending}</pre>
          <div className="modal-actions">
            <button
              className="button secondary"
              ref={pasteCancel}
              onClick={() => setPastePending(undefined)}
            >
              Batalkan paste
            </button>
            <button
              className="button primary"
              disabled={!connected || !interactive || switching}
              onClick={() => {
                controlRef.current?.paste(pastePending);
                setPastePending(undefined);
              }}
            >
              Tempel teks ini
            </button>
          </div>
        </div>
      )}
      <div className="terminal-scroll">
        <div ref={element} />
      </div>
      {interactive && (
        <TerminalToolbar
          key={scope + ":" + connection.attempt}
          enabled={connected && !switching}
          prefix={prefix}
          onPrefix={setPrefix}
          onSend={(key: TerminalKey, modifiers: Modifiers) => {
            const control = controlRef.current;
            if (
              !control?.alive ||
              control.paused ||
              !control.interactive ||
              control.scope !== scope
            )
              return false;
            const data = encodeTerminalKey(
              key,
              modifiers,
              control.cursorMode(),
            );
            control.send(new TextEncoder().encode(data));
            control.focus();
            return true;
          }}
        />
      )}
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
          disabled={connected || switching}
          onClick={() =>
            setConnection((current) => ({ attempt: current.attempt + 1 }))
          }
        >
          <RefreshCw size={16} /> Buka ulang
        </button>
      </div>
    </section>
  );
}
