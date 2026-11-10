// Cache only public build assets and the static offline screen.
const CACHE_PREFIX = "tmux-monitor-pwa-";
const SHELL = CACHE_PREFIX + "shell-v1";
const ASSETS = CACHE_PREFIX + "assets-v1";
const OFFLINE = "/offline.html";
const SHELL_ASSETS = [OFFLINE, "/offline.css", "/icon.svg"];
const BUILD_ASSET = /^\/assets\/[\w.-]+-[\w-]{8,}\.(?:js|css)$/;
const MAX_ASSETS = 32;

self.addEventListener("install", (event) => {
  event.waitUntil(
    caches.open(SHELL).then((cache) =>
      cache.addAll(
        SHELL_ASSETS.map(
          (path) =>
            new Request(path, {
              cache: "reload",
              credentials: "omit",
              redirect: "error",
            }),
        ),
      ),
    ),
  );
});

self.addEventListener("activate", (event) => {
  event.waitUntil(
    caches
      .keys()
      .then((keys) =>
        Promise.all(
          keys
            .filter(
              (key) =>
                key.startsWith(CACHE_PREFIX) && key !== SHELL && key !== ASSETS,
            )
            .map((key) => caches.delete(key)),
        ),
      ),
  );
});

self.addEventListener("fetch", (event) => {
  const { request } = event;
  const url = new URL(request.url);
  if (request.method !== "GET" || url.origin !== self.location.origin) return;

  if (
    request.mode === "navigate" &&
    (url.pathname === "/" || url.pathname === "/index.html")
  ) {
    event.respondWith(appNavigation(request));
    return;
  }
  if (url.search || request.headers.has("Range")) return;
  if (SHELL_ASSETS.includes(url.pathname)) {
    event.respondWith(
      caches
        .open(SHELL)
        .then((cache) => cache.match(url.pathname))
        .then((response) => response ?? fetch(request)),
    );
    return;
  }
  if (
    BUILD_ASSET.test(url.pathname) &&
    (request.destination === "script" || request.destination === "style")
  ) {
    event.respondWith(buildAsset(request));
  }
});

async function appNavigation(request) {
  const response = await fetch(request).catch(() => undefined);
  if (response && ![502, 503, 504].includes(response.status)) return response;
  const cache = await caches.open(SHELL).catch(() => undefined);
  const offline = await cache?.match(OFFLINE).catch(() => undefined);
  return offline ?? response ?? Response.error();
}

async function buildAsset(request) {
  const cache = await caches.open(ASSETS).catch(() => undefined);
  const saved = await cache?.match(request).catch(() => undefined);
  if (saved) return saved;

  const response = await fetch(
    new Request(request, { credentials: "omit", redirect: "error" }),
  );
  const type = response.headers.get("Content-Type") ?? "";
  const expectedType =
    request.destination === "style"
      ? /^text\/css(?:;|$)/i
      : /^(?:text|application)\/(?:java|ecma)script(?:;|$)/i;
  if (
    cache &&
    response.ok &&
    response.url === request.url &&
    expectedType.test(type) &&
    /(?:^|,)\s*immutable\s*(?:,|$)/i.test(
      response.headers.get("Cache-Control") ?? "",
    )
  ) {
    try {
      await cache.put(request, response.clone());
      const keys = await cache.keys();
      await Promise.all(
        keys
          .slice(0, Math.max(0, keys.length - MAX_ASSETS))
          .map((key) => cache.delete(key)),
      );
    } catch {
      // The current online response remains usable when cache storage is full.
    }
  }
  return response;
}
