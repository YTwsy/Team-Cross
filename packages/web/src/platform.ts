// The browser and desktop share the same business API. Native credentials never
// enter this module; the meta value only authorises the private asset transport.
function capability() {
  return document.querySelector<HTMLMetaElement>(
    'meta[name="teamcross-desktop"]',
  )?.content;
}

// Retained while the installed Swift host is still supported during cutover.
declare global {
  interface Window {
    webkit?: {
      messageHandlers?: {
        teamcross?: {
          postMessage: (body: { action: string; route?: string }) => void;
        };
      };
    };
  }
}

export function hasNativeWindows() {
  return !!(capability() || window.webkit?.messageHandlers?.teamcross);
}

export function windowAction(action: "open" | "pin" | "menu", route?: string) {
  const proof = capability();
  if (!proof) {
    const bridge = window.webkit?.messageHandlers?.teamcross;
    if (bridge) bridge.postMessage({ action, ...(route ? { route } : {}) });
    else if (action === "open" && route) location.hash = route;
    return;
  }
  // Acceptance only means the UI command was queued. No business operation or
  // arbitrary URL is available through these fixed host endpoints.
  void fetch(`/desktop/${action}`, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      "X-TeamCross-Desktop": proof,
    },
    body: JSON.stringify(route ? { route } : {}),
  })
    .then((response) => {
      if (!response.ok) throw new Error("Desktop action unavailable");
      window.dispatchEvent(
        new CustomEvent("teamcross-window-error", { detail: false }),
      );
    })
    .catch(() => {
      window.dispatchEvent(
        new CustomEvent("teamcross-window-error", { detail: true }),
      );
    });
}

export async function apiFetch(path: string, init?: RequestInit) {
  const proof = capability();
  if (!proof) return fetch(path, init);
  if (!path.startsWith("/api/")) throw new Error("Invalid local API path");
  const headers = new Headers(init?.headers);
  headers.set("X-TeamCross-Desktop", proof);
  return fetch(path, { ...init, headers });
}

export async function copyText(text: string) {
  const proof = capability();
  if (!proof) return navigator.clipboard.writeText(text);
  const response = await fetch("/desktop/clipboard", {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      "X-TeamCross-Desktop": proof,
    },
    body: JSON.stringify({ text }),
  });
  if (!response.ok) throw new Error("Clipboard unavailable");
}

// WKWebView storage is scoped separately for each normalised Core directory.
// Browser keys retain their existing names and remain in the browser profile.
export function storageKey(key: string) {
  const scope = document.querySelector<HTMLMetaElement>(
    'meta[name="teamcross-storage-scope"]',
  )?.content;
  return scope ? `teamcross.desktop.${scope}.${key}` : key;
}
