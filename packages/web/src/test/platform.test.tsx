import { afterEach, describe, expect, it, vi } from "vitest";
import { apiFetch, copyText, storageKey } from "../platform";

function desktop(scope = "directory-a") {
  for (const [name, content] of [
    ["teamcross-desktop", "private-resource-proof"],
    ["teamcross-storage-scope", scope],
  ]) {
    const meta = document.createElement("meta");
    meta.name = name!;
    meta.content = content!;
    document.head.appendChild(meta);
  }
}
afterEach(() => {
  document
    .querySelectorAll('meta[name^="teamcross-"]')
    .forEach((m) => m.remove());
  vi.unstubAllGlobals();
});

describe("shared browser and desktop transport", () => {
  it("keeps browser requests and storage unchanged", async () => {
    const fetch = vi.fn().mockResolvedValue(new Response("{}"));
    vi.stubGlobal("fetch", fetch);
    const options = { method: "POST", body: "{}" };
    await apiFetch("/api/spaces", options);
    expect(fetch).toHaveBeenCalledWith("/api/spaces", options);
    expect(storageKey("teamcross.theme.v1")).toBe("teamcross.theme.v1");
  });
  it("adds only the local asset proof and preserves cancellation", async () => {
    desktop();
    const fetch = vi.fn().mockResolvedValue(new Response("{}"));
    vi.stubGlobal("fetch", fetch);
    const signal = new AbortController().signal;
    await apiFetch("/api/spaces", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: "{}",
      signal,
    });
    const [path, options] = fetch.mock.calls[0]!;
    expect(path).toBe("/api/spaces");
    expect(options.signal).toBe(signal);
    expect(options.headers.get("X-TeamCross-Desktop")).toBe(
      "private-resource-proof",
    );
    expect(options.headers.get("Content-Type")).toBe("application/json");
    expect(options.headers.get("Authorization")).toBeNull();
    await expect(apiFetch("https://example.com/api/spaces")).rejects.toThrow();
    expect(fetch).toHaveBeenCalledTimes(1);
  });
  it("does not replay a failed desktop write", async () => {
    desktop();
    const fetch = vi.fn().mockRejectedValue(new TypeError("response lost"));
    vi.stubGlobal("fetch", fetch);
    await expect(
      apiFetch("/api/spaces", { method: "POST", body: "{}" }),
    ).rejects.toThrow("response lost");
    expect(fetch).toHaveBeenCalledTimes(1);
  });
  it("copies through the fixed native endpoint and scopes browser state", async () => {
    desktop();
    const fetch = vi.fn().mockResolvedValue(new Response("{}"));
    vi.stubGlobal("fetch", fetch);
    await copyText("中文 🧪");
    expect(fetch).toHaveBeenCalledWith(
      "/desktop/clipboard",
      expect.objectContaining({
        method: "POST",
        body: JSON.stringify({ text: "中文 🧪" }),
      }),
    );
    expect(storageKey("teamcross.theme.v1")).toBe(
      "teamcross.desktop.directory-a.teamcross.theme.v1",
    );
    document.querySelector<HTMLMetaElement>(
      'meta[name="teamcross-storage-scope"]',
    )!.content = "directory-b";
    expect(storageKey("teamcross.theme.v1")).toBe(
      "teamcross.desktop.directory-b.teamcross.theme.v1",
    );
  });
});
