// The browser and desktop share the same business API. Native credentials never
// enter this module; the meta value only authorises the private asset transport.
function capability() {
  return document.querySelector<HTMLMetaElement>(
    'meta[name="teamcross-desktop"]',
  )?.content;
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
