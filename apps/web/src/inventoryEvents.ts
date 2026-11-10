import { useEffect } from "react";

// The stream sends invalidations only. A reconnect or sequence gap always
// fetches an authorized snapshot, with polling even if WebSocket is unavailable.
export function useInventoryEvents(
  hostId: string,
  enabled: boolean,
  refresh: () => Promise<void>,
) {
  useEffect(() => {
    if (!enabled) return;
    let active = true;
    let pending = false;
    let dirty = false;
    let socket: WebSocket | undefined;
    let reconnect = 0;
    let delay = 2_000;
    let stream = "";
    let last = 0;
    let lastRefresh = 0;
    let lastSignal = 0;
    async function update() {
      dirty = true;
      if (pending || !active || document.visibilityState !== "visible") return;
      pending = true;
      try {
        do {
          dirty = false;
          lastRefresh = Date.now();
          await refresh();
        } while (dirty && active && document.visibilityState === "visible");
      } finally {
        pending = false;
      }
    }
    function connect() {
      if (!active) return;
      const url = new URL("/ws/v1/events", location.href);
      url.protocol = url.protocol === "https:" ? "wss:" : "ws:";
      url.searchParams.set("hostId", hostId);
      const started = Date.now();
      const connection = new WebSocket(url);
      socket = connection;
      stream = "";
      last = 0;
      connection.onmessage = ({ data }) => {
        if (!active || socket !== connection) return;
        try {
          if (typeof data !== "string" || data.length > 1024) throw Error();
          const { type, stream: next, sequence } = JSON.parse(data);
          if (
            typeof next !== "string" ||
            !/^stream_[a-f0-9]{32}$/.test(next) ||
            !Number.isSafeInteger(sequence) ||
            sequence < 1 ||
            !["resync_required", "inventory.changed"].includes(type)
          ) {
            throw Error();
          }
          lastSignal = Date.now();
          if (lastSignal - started > 10_000) delay = 2_000;
          if (stream && (next !== stream || sequence !== last + 1)) {
            void update();
            connection.close();
            return;
          }
          stream = next;
          last = sequence;
          void update();
        } catch {
          void update();
          connection.close();
        }
      };
      connection.onclose = () => {
        if (!active || socket !== connection) return;
        if (Date.now() - lastRefresh > 5_000) void update();
        reconnect = window.setTimeout(connect, delay);
        delay = Math.min(delay * 2, 30_000);
      };
    }
    const poll = window.setInterval(() => {
      if (Date.now() - Math.max(lastRefresh, lastSignal) >= 20_000)
        void update();
    }, 5_000);
    const visible = () => {
      if (document.visibilityState === "visible") void update();
    };
    document.addEventListener("visibilitychange", visible);
    if ("WebSocket" in window) connect();
    return () => {
      active = false;
      window.clearTimeout(reconnect);
      window.clearInterval(poll);
      document.removeEventListener("visibilitychange", visible);
      socket?.close();
    };
  }, [hostId, enabled, refresh]);
}
