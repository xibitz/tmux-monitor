/// <reference types="vite/client" />

export function registerPWA() {
  if (
    import.meta.env.PROD &&
    location.protocol === "https:" &&
    "serviceWorker" in navigator
  ) {
    void navigator.serviceWorker
      .register("/sw.js", { updateViaCache: "none" })
      .catch(() => {
        // The online app is still available if installation is blocked.
      });
  }
}
