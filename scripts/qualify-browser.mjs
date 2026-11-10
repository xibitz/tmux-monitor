import assert from "node:assert/strict";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || "playwright");
const browser = await chromium.launch({
  executablePath: process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE,
  args: ["--ignore-certificate-errors"],
});
const origin = "https://localhost:18081";
const context = await browser.newContext({
  viewport: { width: 393, height: 852 },
  isMobile: true,
  hasTouch: true,
  ignoreHTTPSErrors: true,
});
try {
  await context.addInitScript(() => {
    Object.defineProperty(Notification, "requestPermission", {
      value: () => Promise.resolve("denied"),
      configurable: true,
    });
    Element.prototype.requestFullscreen = () =>
      Promise.reject(new Error("fixture fallback"));
  });
  const page = await context.newPage();
  page.setDefaultTimeout(8_000);
  const cdp = await context.newCDPSession(page);
  await cdp.send("Performance.enable");
  async function browserMetrics() {
    const { metrics } = await cdp.send("Performance.getMetrics");
    return Object.fromEntries(metrics.map(({ name, value }) => [name, value]));
  }
  let terminalBytes = 0;
  page.on("websocket", (socket) => {
    if (new URL(socket.url()).pathname !== "/ws/v1/terminal") return;
    socket.on("framereceived", ({ payload }) => {
      terminalBytes += Buffer.byteLength(payload);
    });
  });
  const uncaught = [];
  page.on("pageerror", (e) => uncaught.push(e.message));
  let snapshotRequests = 0;
  let inventorySocket;
  let inventoryConnections = 0;
  page.on("request", (r) => {
    if (
      r.method() === "GET" &&
      new URL(r.url()).pathname.endsWith("/inventory")
    )
      snapshotRequests++;
  });
  await page.routeWebSocket("**/ws/v1/events?*", (ws) => {
    inventorySocket = ws;
    inventoryConnections++;
    ws.send(
      JSON.stringify({
        type: "resync_required",
        stream: "stream_" + "a".repeat(32),
        sequence: 1,
      }),
    );
  });

  await page.goto(origin);
  const manifest = await page.evaluate(async () => {
    const link = document.querySelector("link[rel=manifest]");
    const response = await fetch(link.href);
    return [response.headers.get("content-type"), await response.json()];
  });
  assert.match(manifest[0], /manifest\+json/);
  assert.equal(manifest[1].display, "standalone");
  assert.ok(manifest[1].icons.some((icon) => icon.sizes === "192x192"));
  assert.ok(manifest[1].icons.some((icon) => icon.sizes === "512x512"));
  await page.waitForFunction(
    async () => (await navigator.serviceWorker.getRegistration("/"))?.active,
  );
  await page.reload();
  await page.waitForFunction(() => navigator.serviceWorker.controller);
  await page.locator("input[name=username]").fill("admin");
  await page.locator("input[name=password]").fill("test-only-password-12345");
  await page.getByRole("button", { name: "Masuk ke workspace" }).click();

  const openHost = () =>
    page.getByRole("button", { name: /Host uji SSH/ }).click();
  const readOnly = () => page.getByText(/^Terhubung ·/).waitFor();
  await openHost();
  await page.getByRole("button", { name: "Tandai favorit" }).click();
  await page
    .getByRole("button", { name: "Periksa koneksi dan inventaris" })
    .click();
  await page.getByText(/Live pane independen dinonaktifkan/).waitFor();
  await page.waitForTimeout(500);
  assert.ok(
    inventorySocket && snapshotRequests > 0,
    "initial stream did not fetch an inventory",
  );
  const beforeGap = snapshotRequests;
  inventorySocket.send(
    JSON.stringify({
      type: "inventory.changed",
      stream: "stream_" + "a".repeat(32),
      sequence: 3,
    }),
  );
  for (
    let i = 0;
    i < 35 && (snapshotRequests <= beforeGap || inventoryConnections < 2);
    i++
  )
    await page.waitForTimeout(100);
  assert.ok(
    snapshotRequests > beforeGap && inventoryConnections >= 2,
    "sequence gap did not resync and reconnect",
  );

  const search = page.getByRole("textbox", {
    name: "Cari session, window, atau pane",
  });
  await search.fill("fixture-4");
  assert.equal(
    await page
      .getByRole("button", { name: "Buka terminal read-only", exact: true })
      .count(),
    1,
  );
  await search.clear();
  const sessionOpen = (name) =>
    page
      .locator(".session-heading")
      .filter({ hasText: name })
      .getByRole("button", { name: "Buka terminal read-only" });
  const tabs = page.getByRole("navigation", { name: "Tab terminal" });
  await sessionOpen("session-untuk-uji").click();
  await readOnly();
  await page
    .getByRole("button", { name: "Perbesar tampilan terminal" })
    .click();
  await page.locator("dialog.viewer-expanded").waitFor();
  await page
    .getByRole("button", { name: "Kembalikan ukuran tampilan" })
    .click();
  await page
    .getByRole("button", { name: "Aktifkan input", exact: true })
    .click();
  await page.getByRole("button", { name: "Aktifkan untuk viewer ini" }).click();
  await page.getByText("Interaktif", { exact: true }).waitFor();
  for (const name of ["fixture-1", "fixture-2", "fixture-3"]) {
    await tabs.getByRole("button", { name: "Inventaris" }).click();
    await sessionOpen(name).click();
    await readOnly();
    await page.getByText("Read-only", { exact: true }).waitFor();
  }
  const beforeSwitches = await browserMetrics();
  const bytesBeforeSwitches = terminalBytes;
  const timings = [];
  for (let i = 0; i < 30; i++) {
    const started = performance.now();
    const name = i % 2 === 0 ? "session-untuk-uji" : "fixture-3";
    await tabs.getByRole("button", { name, exact: true }).click();
    await readOnly();
    await page.getByText("Read-only", { exact: true }).waitFor();
    timings.push(performance.now() - started);
  }
  const afterSwitches = await browserMetrics();
  const switchTerminalBytes = terminalBytes - bytesBeforeSwitches;
  assert.ok(
    switchTerminalBytes > 0,
    "fixture viewer received no terminal frames",
  );
  await tabs.getByRole("button", { name: "Inventaris" }).click();
  assert.ok(await sessionOpen("fixture-4").isDisabled());
  const beforePolling = snapshotRequests;
  for (let i = 0; i < 125 && snapshotRequests <= beforePolling; i++)
    await page.waitForTimeout(200);
  assert.ok(
    snapshotRequests > beforePolling,
    "silent stream did not fall back to polling",
  );
  await page.getByRole("button", { name: "Tutup dialog" }).click();
  await page.getByRole("button", { name: "Favorit", exact: true }).click();
  await page.getByRole("button", { name: /Host uji SSH/ }).waitFor();

  await page.getByRole("button", { name: "Buka navigasi" }).click();
  await page.getByRole("button", { name: "Notifikasi", exact: true }).click();
  await page.getByText(/Session dan pane belum diketahui/).waitFor();
  await page
    .getByRole("button", { name: "Aktifkan Web Push di perangkat ini" })
    .click();
  await page.getByText(/Izin Web Push belum diberikan/).waitFor();
  await page.getByText("Giliran selesai", { exact: true }).waitFor();

  const unsupported = await context.newPage();
  await unsupported.addInitScript(() =>
    Reflect.deleteProperty(window, "PushManager"),
  );
  await unsupported.goto(origin + "/?view=inbox");
  await unsupported.getByText(/Web Push tidak tersedia/).waitFor();
  await unsupported.getByText("Giliran selesai", { exact: true }).waitFor();
  await unsupported.close();

  const cachePaths = await page.evaluate(async () => {
    const urls = [];
    for (const key of await caches.keys())
      for (const entry of await (await caches.open(key)).keys())
        urls.push(new URL(entry.url).pathname);
    return urls;
  });
  assert.ok(cachePaths.includes("/offline.html"));
  assert.ok(cachePaths.some((path) => path.startsWith("/assets/")));
  assert.ok(
    cachePaths.every((path) =>
      /^\/(?:offline\.(?:html|css)|icon\.svg|assets\/.+\.(?:css|js))$/.test(
        path,
      ),
    ),
  );
  await context.setOffline(true);
  await page.reload({ waitUntil: "domcontentloaded" });
  await page
    .getByRole("heading", { name: "Hub belum dapat dijangkau" })
    .waitFor();
  assert.equal(
    await page.getByText("Host uji SSH", { exact: true }).count(),
    0,
  );
  await context.setOffline(false);
  await page.getByRole("link", { name: "Coba lagi" }).click();
  await page.getByRole("heading", { name: "Host saya." }).waitFor();
  assert.deepEqual(uncaught, []);
  timings.sort((a, b) => a - b);
  console.log(
    JSON.stringify({
      result: "PASS",
      viewport: "393×852",
      chromium: browser.version(),
      cachePaths: cachePaths.length,
      tabSwitches: timings.length,
      tabSwitchP95Ms: Math.round(timings[Math.ceil(timings.length * 0.95) - 1]),
      rendererTaskMs: Math.round(
        (afterSwitches.TaskDuration - beforeSwitches.TaskDuration) * 1000,
      ),
      rendererHeapBeforeBytes: beforeSwitches.JSHeapUsedSize,
      rendererHeapAfterBytes: afterSwitches.JSHeapUsedSize,
      switchTerminalBytes,
    }),
  );
} finally {
  await browser.close();
}
