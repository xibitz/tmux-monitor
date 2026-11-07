import { useEffect, useId, useRef, useState } from "react";
import { NO_MODIFIERS, PREFIX_OPTIONS } from "./terminalKeys";
import type { Modifiers, SpecialKey, TerminalKey } from "./terminalKeys";

const keys: { label: string; key: SpecialKey; name?: string }[] = [
  { label: "Esc", key: "Escape" },
  { label: "Tab", key: "Tab" },
  { label: "Ctrl+C", key: "Interrupt" },
  { label: "←", key: "ArrowLeft", name: "Panah kiri" },
  { label: "↑", key: "ArrowUp", name: "Panah atas" },
  { label: "↓", key: "ArrowDown", name: "Panah bawah" },
  { label: "→", key: "ArrowRight", name: "Panah kanan" },
  { label: "Enter", key: "Enter" },
  { label: "⌫", key: "Backspace", name: "Backspace" },
  { label: "Home", key: "Home" },
  { label: "End", key: "End" },
  { label: "Del", key: "Delete", name: "Delete" },
];

export function TerminalToolbar({
  enabled,
  prefix,
  onPrefix,
  onSend,
}: {
  enabled: boolean;
  prefix: string;
  onPrefix: (value: string) => void;
  onSend: (key: TerminalKey, modifiers: Modifiers) => boolean;
}) {
  const [modifiers, setModifiers] = useState<Modifiers>({ ...NO_MODIFIERS });
  const [draft, setDraft] = useState("");
  const [error, setError] = useState("");
  const characterRef = useRef<HTMLInputElement>(null);
  const composing = useRef(false);
  const id = useId();
  useEffect(() => {
    if (!enabled) {
      setModifiers({ ...NO_MODIFIERS });
      setDraft("");
      setError("");
      composing.current = false;
    }
  }, [enabled]);
  const active = Object.entries(modifiers)
    .filter(([, value]) => value)
    .map(
      ([name]) =>
        ({ ctrl: "Ctrl", alt: "Alt", shift: "Shift" })[name as keyof Modifiers],
    );
  function send(key: TerminalKey) {
    if (!enabled || composing.current) return;
    try {
      if (!onSend(key, modifiers)) {
        setError("Viewer belum siap menerima input.");
        return;
      }
      setModifiers({ ...NO_MODIFIERS });
      setDraft("");
      setError("");
    } catch (e) {
      setError((e as Error).message);
    }
  }
  function sendCharacter() {
    send({ kind: "character", value: draft });
  }
  return (
    <section className="terminal-toolbar" aria-label="Keyboard terminal">
      <div
        className="terminal-keys"
        role="group"
        aria-label="Modifier sekali pakai"
      >
        {(["ctrl", "alt", "shift"] as const).map((name) => (
          <button
            type="button"
            key={name}
            className="button secondary modifier-key"
            disabled={!enabled}
            aria-pressed={modifiers[name]}
            onClick={() => {
              setModifiers((current) => ({
                ...current,
                [name]: !current[name],
              }));
              setError("");
              characterRef.current?.focus();
            }}
          >
            {name === "ctrl" ? "Ctrl" : name === "alt" ? "Alt" : "Shift"}
          </button>
        ))}
        {active.length > 0 && (
          <button
            type="button"
            className="button secondary"
            disabled={!enabled}
            onClick={() => {
              setModifiers({ ...NO_MODIFIERS });
              setDraft("");
              setError("");
            }}
          >
            Reset modifier
          </button>
        )}
      </div>
      <p className="toolbar-status" role="status" aria-live="polite">
        {active.length
          ? "Sekali pakai: " + active.join(" + ")
          : "Tanpa modifier"}
      </p>
      <div className="key-composer">
        <label htmlFor={id + "-key"}>Tombol kombinasi</label>
        <div className="key-composer-row">
          <input
            id={id + "-key"}
            ref={characterRef}
            value={draft}
            disabled={!enabled}
            autoComplete="off"
            autoCorrect="off"
            autoCapitalize="off"
            spellCheck={false}
            maxLength={8}
            placeholder="Contoh: c"
            aria-describedby={id + "-help"}
            onChange={(e) => setDraft(e.target.value)}
            onCompositionStart={() => {
              composing.current = true;
            }}
            onCompositionEnd={() => {
              composing.current = false;
            }}
            onKeyDown={(e) => {
              if (
                e.key === "Enter" &&
                !e.nativeEvent.isComposing &&
                e.keyCode !== 229
              ) {
                e.preventDefault();
                sendCharacter();
              }
            }}
          />
          <button
            type="button"
            className="button secondary"
            disabled={!enabled || !draft}
            onClick={sendCharacter}
          >
            Kirim kombinasi
          </button>
        </div>
        <p id={id + "-help"} className="panel-description">
          Pilih modifier, isi satu karakter, lalu kirim. Keyboard terminal tetap
          bisa dipakai seperti biasa.
        </p>
      </div>
      <div className="terminal-keys" role="group" aria-label="Tombol terminal">
        {keys.map((key) => (
          <button
            type="button"
            className="button secondary"
            key={key.key}
            aria-label={key.name ?? key.label}
            disabled={!enabled}
            onMouseDown={(e) => e.preventDefault()}
            onClick={() => send({ kind: "special", value: key.key })}
          >
            {key.label}
          </button>
        ))}
      </div>
      <div className="prefix-setting">
        <label htmlFor={id + "-prefix"}>Prefix tmux</label>
        <select
          id={id + "-prefix"}
          value={prefix}
          disabled={!enabled}
          onChange={(e) => {
            onPrefix(e.target.value);
            setModifiers({ ...NO_MODIFIERS });
            setDraft("");
            setError("");
          }}
        >
          {PREFIX_OPTIONS.map((option) => (
            <option key={option.value} value={option.value}>
              {option.label}
            </option>
          ))}
        </select>
        <button
          type="button"
          className="button secondary"
          disabled={!enabled}
          onMouseDown={(e) => e.preventDefault()}
          onClick={() => send({ kind: "prefix", value: prefix })}
        >
          Kirim prefix{" "}
          {PREFIX_OPTIONS.find((option) => option.value === prefix)?.label}
        </button>
      </div>
      <p className="panel-description">
        Samakan pilihan prefix dengan tmux pada host Anda.
      </p>
      {error && (
        <p className="error-notice" role="alert">
          {error}
        </p>
      )}
    </section>
  );
}
