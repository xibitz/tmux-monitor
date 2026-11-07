import assert from "node:assert/strict";
import test from "node:test";
import {
  TerminalLiveness,
  TerminalReplacement,
  TERMINAL_LEASE_MS,
} from "../src/terminalLiveness.ts";

test("silent output stays healthy with timely heartbeats", () => {
  let time = 0;
  const live = new TerminalLiveness(() => time);
  for (let n = 0; n < 100; n++) {
    time += 5000;
    assert.equal(live.current(), true);
    assert.equal(live.heartbeat(), true);
  }
});

test("connection/setup and input expire at the deadline without a timer callback", () => {
  let time = 100;
  const live = new TerminalLiveness(() => time);
  time += TERMINAL_LEASE_MS - 1;
  assert.equal(live.current(), true);
  time++;
  assert.equal(live.current(), false);
  assert.equal(live.heartbeat(), false);
});

test("queued heartbeats after sleep cannot revive an expired viewer", () => {
  let time = 0;
  const live = new TerminalLiveness(() => time);
  time = 5000;
  assert.equal(live.heartbeat(), true);
  time += TERMINAL_LEASE_MS + 30_000;
  assert.equal(live.heartbeat(), false);
  time++;
  assert.equal(live.heartbeat(), false);
  assert.equal(live.current(), false);
  assert.equal(new TerminalLiveness(() => time).current(), true);
});

test("disconnect remains terminal even if the clock or network recovers", () => {
  let time = 10_000;
  const live = new TerminalLiveness(() => time);
  live.stop();
  assert.equal(live.current(), false);
  assert.equal(live.heartbeat(), false);
  time++;
  assert.equal(live.current(), false);
});

test("clock rollback fails closed instead of extending an interactive lease", () => {
  let time = 10_000;
  const live = new TerminalLiveness(() => time);
  time--;
  assert.equal(live.current(), false);
  time += 1000;
  assert.equal(live.heartbeat(), false);
});

test("a mode response belongs to one bounded attempt", () => {
  let time = 0;
  const replacement = new TerminalReplacement(() => time);
  assert.equal(replacement.current(), false);
  replacement.begin();
  time = 5000;
  assert.equal(replacement.current(), true);
  replacement.begin(); // A retry cannot extend the original deadline.
  time = TERMINAL_LEASE_MS;
  assert.equal(replacement.current(), false);
  assert.equal(replacement.pending(), false);
});

test("offline during mode replacement discards the late response after online", () => {
  let time = 0;
  const replacement = new TerminalReplacement(() => time);
  replacement.begin();
  assert.equal(replacement.pending(), true); // Expected mode_changed close.
  replacement.invalidate(); // Offline/pagehide or unexpected disconnect.
  time = 1000; // Network recovers before the response arrives.
  replacement.begin();
  assert.equal(replacement.current(), false);
  assert.equal(replacement.pending(), false);
});

test("a delayed response after sleep or clock rollback cannot activate input", () => {
  for (const elapsed of [TERMINAL_LEASE_MS + 60_000, -1]) {
    let time = 1000;
    const replacement = new TerminalReplacement(() => time);
    replacement.begin();
    time += elapsed;
    assert.equal(replacement.current(), false);
    time = 2000;
    assert.equal(replacement.current(), false);
  }
});
