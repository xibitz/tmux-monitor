import assert from "node:assert/strict";
import test from "node:test";
import {
  encodeTerminalKey,
  NO_MODIFIERS,
  PREFIX_OPTIONS,
} from "../src/terminalKeys.ts";

const mods = (value = {}) => ({ ...NO_MODIFIERS, ...value });
const text = (value, modifiers = {}) =>
  encodeTerminalKey({ kind: "character", value }, mods(modifiers));
const key = (value, modifiers = {}, app = false) =>
  encodeTerminalKey({ kind: "special", value }, mods(modifiers), app);

test("control characters, NUL and Alt combinations preserve their byte values", () => {
  for (let i = 0; i < 26; i++)
    assert.equal(
      text(String.fromCharCode(97 + i), { ctrl: true }).charCodeAt(0),
      i + 1,
    );
  for (const [character, byte] of [
    [" ", 0],
    ["@", 0],
    ["[", 27],
    ["\\", 28],
    ["]", 29],
    ["^", 30],
    ["_", 31],
    ["?", 127],
  ]) {
    assert.equal(text(character, { ctrl: true }).charCodeAt(0), byte);
  }
  assert.equal(text("c", { ctrl: true, alt: true }), "\x1b\x03");
  assert.equal(text("2", { ctrl: true, shift: true }), "\x00");
  assert.equal(key("Interrupt", { alt: true, shift: true }), "\x03");
});

test("Shift and Unicode remain explicit, without lossy multi-character conversion", () => {
  assert.equal(text("a", { shift: true }), "A");
  assert.equal(text("1", { shift: true }), "!");
  assert.equal(text("/", { shift: true }), "?");
  assert.equal(text("é"), "é");
  assert.equal(text("😀"), "😀");
  assert.equal(text("秘", { alt: true }), "\x1b秘");
  for (const value of ["", "ab", "\r", "\x1b", "\x7f", "\ud800"])
    assert.throws(() => text(value));
  for (const value of ["é", "ß", "ſ", "😀"])
    assert.throws(() => text(value, { ctrl: true }));
  assert.throws(() => text("é", { shift: true }));
});

test("cursor keys honor DECCKM and the full xterm modifier bitset", () => {
  for (const [name, suffix] of [
    ["ArrowUp", "A"],
    ["ArrowDown", "B"],
    ["ArrowLeft", "D"],
    ["ArrowRight", "C"],
    ["Home", "H"],
    ["End", "F"],
  ]) {
    assert.equal(key(name), "\x1b[" + suffix);
    assert.equal(key(name, {}, true), "\x1bO" + suffix);
  }
  const variants = [
    [{ shift: true }, 2],
    [{ alt: true }, 3],
    [{ shift: true, alt: true }, 4],
    [{ ctrl: true }, 5],
    [{ ctrl: true, shift: true }, 6],
    [{ ctrl: true, alt: true }, 7],
    [{ ctrl: true, alt: true, shift: true }, 8],
  ];
  for (const [modifiers, number] of variants)
    assert.equal(key("ArrowUp", modifiers, true), "\x1b[1;" + number + "A");
  assert.equal(key("Delete", { ctrl: true }), "\x1b[3;5~");
});

test("fixed keys follow legacy terminal conventions", () => {
  assert.equal(key("Escape"), "\x1b");
  assert.equal(key("Escape", { alt: true }), "\x1b\x1b");
  assert.equal(key("Enter"), "\r");
  assert.equal(key("Enter", { alt: true }), "\x1b\r");
  assert.equal(key("Tab", { shift: true }), "\x1b[Z");
  assert.equal(key("Tab"), "\t");
  assert.equal(key("Backspace"), "\x7f");
  assert.equal(key("Backspace", { ctrl: true }), "\b");
  assert.throws(() => key("__proto__"));
  assert.throws(() => key("Unknown"));
});

test("prefix sends exactly one configured control byte, ignoring armed modifiers", () => {
  for (const option of PREFIX_OPTIONS) {
    const result = encodeTerminalKey(
      { kind: "prefix", value: option.value },
      mods({ ctrl: true, shift: true, alt: true }),
    );
    assert.equal(result.length, 1);
    assert.ok(result.charCodeAt(0) <= 31 || result.charCodeAt(0) === 127);
  }
  assert.equal(encodeTerminalKey({ kind: "prefix", value: "C-b" }), "\x02");
  assert.equal(encodeTerminalKey({ kind: "prefix", value: "C-a" }), "\x01");
  assert.equal(encodeTerminalKey({ kind: "prefix", value: "C-Space" }), "\x00");
  for (const value of ["C-b c", "C-b\n", "c-b", "M-x", "C-b;kill-server"])
    assert.throws(() => encodeTerminalKey({ kind: "prefix", value }));
});
