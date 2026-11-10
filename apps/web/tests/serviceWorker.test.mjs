import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { runInNewContext } from "node:vm";

const source = readFileSync(
  new URL("../public/sw.js", import.meta.url),
  "utf8",
);
const origin = "https://hub.fixture.ts.net";

test("push ignores supplied payload and always opens the same-origin inbox", async () => {
  const handlers = new Map();
  const notices = [];
  const opened = [];
  const self = {
    location: { origin },
    addEventListener: (type, handler) => handlers.set(type, handler),
    registration: {
      showNotification: async (title, options) =>
        notices.push({ title, ...options }),
    },
    clients: {
      matchAll: async () => [{ url: "https://outside.invalid/?view=inbox" }],
      openWindow: async (url) => opened.push(url),
    },
  };
  runInNewContext(source, { self, URL });
  let settled;
  const waitUntil = (promise) => {
    settled = promise;
  };
  const data = new Proxy(
    {},
    {
      get() {
        throw new Error("push payload must stay unread");
      },
    },
  );
  handlers.get("push")({ data, waitUntil });
  await settled;
  assert.equal(notices.length, 1);
  assert.equal(notices[0].title, "Tmux Monitor");
  assert.equal(notices[0].body, "Ada aktivitas baru di kotak masuk.");
  let closed = false;
  handlers.get("notificationclick")({
    notification: { tag: notices[0].tag, close: () => (closed = true) },
    waitUntil,
  });
  await settled;
  assert.equal(closed, true);
  assert.deepEqual(opened, [origin + "/?view=inbox"]);
});
