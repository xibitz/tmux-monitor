export type Modifiers = { ctrl: boolean; alt: boolean; shift: boolean };
export const NO_MODIFIERS: Readonly<Modifiers> = Object.freeze({
  ctrl: false,
  alt: false,
  shift: false,
});
export type SpecialKey =
  | "Escape"
  | "Tab"
  | "Enter"
  | "Backspace"
  | "Delete"
  | "Home"
  | "End"
  | "ArrowLeft"
  | "ArrowUp"
  | "ArrowDown"
  | "ArrowRight"
  | "Interrupt";
export type TerminalKey =
  | { kind: "character"; value: string }
  | { kind: "special"; value: SpecialKey }
  | { kind: "prefix"; value: string };

const ESC = "\x1b";
const shifted: Record<string, string> = Object.fromEntries(
  [
    "1!",
    "2@",
    "3#",
    "4$",
    "5%",
    "6^",
    "7&",
    "8*",
    "9(",
    "0)",
    "-_",
    "=+",
    "[{",
    "]}",
    "\\|",
    ";:",
    "'\"",
    ",<",
    ".>",
    "/?",
    String.fromCharCode(96, 126),
  ].map((pair) => [pair[0], pair[1]]),
);

function character(value: string, mod: Modifiers): string {
  if (
    Array.from(value).length !== 1 ||
    /[\x00-\x1f\x7f-\x9f\uD800-\uDFFF]/u.test(value)
  )
    throw new Error("Masukkan satu karakter yang dapat dicetak.");
  let text = value;
  if (mod.shift) {
    if (/^[a-z]$/i.test(text)) text = text.toUpperCase();
    else if (shifted[text]) text = shifted[text];
    else if (!/^[ -~]$/.test(text))
      throw new Error(
        "Shift toolbar mendukung karakter ASCII. Gunakan keyboard biasa untuk karakter ini.",
      );
  }
  if (mod.ctrl) {
    const upper = text.toUpperCase();
    if (/^[a-z]$/i.test(text))
      text = String.fromCharCode(upper.charCodeAt(0) - 64);
    else {
      const control: Record<string, string> = {
        " ": "\x00",
        "@": "\x00",
        "2": "\x00",
        "[": "\x1b",
        "3": "\x1b",
        "\\": "\x1c",
        "4": "\x1c",
        "]": "\x1d",
        "5": "\x1d",
        "^": "\x1e",
        "6": "\x1e",
        _: "\x1f",
        "7": "\x1f",
        "?": "\x7f",
        "8": "\x7f",
      };
      if (!Object.hasOwn(control, text))
        throw new Error(
          "Kombinasi Ctrl ini tidak didukung. Gunakan huruf A–Z, Space, atau simbol kontrol.",
        );
      text = control[text];
    }
  }
  return (mod.alt ? ESC : "") + text;
}

export const PREFIX_OPTIONS = [
  ...Array.from({ length: 26 }, (_, i) => ({
    value: "C-" + String.fromCharCode(97 + i),
    label: "Ctrl+" + String.fromCharCode(65 + i),
  })),
  ...["Space", "[", "\\", "]", "^", "_", "?"].map((key) => ({
    value: "C-" + key,
    label: "Ctrl+" + key,
  })),
];

// Only explicit toolbar actions use this encoder. Physical keyboard, IME and
// paste remain encoded by xterm, with no toolbar modifier rewriting.
export function encodeTerminalKey(
  key: TerminalKey,
  mod: Modifiers = NO_MODIFIERS,
  applicationCursor = false,
): string {
  if (key.kind === "prefix") {
    if (!PREFIX_OPTIONS.some((option) => option.value === key.value))
      throw new Error("Prefix tmux tidak valid.");
    return character(key.value === "C-Space" ? " " : key.value.slice(2), {
      ctrl: true,
      alt: false,
      shift: false,
    });
  }
  if (key.kind === "character") return character(key.value, mod);
  const modifier =
    1 + Number(mod.shift) + 2 * Number(mod.alt) + 4 * Number(mod.ctrl);
  const cursor: Partial<Record<SpecialKey, string>> = {
    ArrowLeft: "D",
    ArrowRight: "C",
    ArrowUp: "A",
    ArrowDown: "B",
    Home: "H",
    End: "F",
  };
  const suffix = Object.hasOwn(cursor, key.value)
    ? cursor[key.value]
    : undefined;
  if (suffix)
    return modifier > 1
      ? ESC + "[1;" + modifier + suffix
      : ESC + (applicationCursor ? "O" : "[") + suffix;
  switch (key.value) {
    case "Escape":
      return mod.alt ? ESC + ESC : ESC;
    case "Tab":
      return mod.shift ? ESC + "[Z" : "\t";
    case "Enter":
      return (mod.alt ? ESC : "") + "\r";
    case "Backspace":
      return (mod.alt ? ESC : "") + (mod.ctrl ? "\b" : "\x7f");
    case "Delete":
      return modifier > 1 ? ESC + "[3;" + modifier + "~" : ESC + "[3~";
    case "Interrupt":
      return "\x03";
    default:
      throw new Error("Tombol terminal tidak didukung.");
  }
}
