import type { HostBridge } from "./bridge";
import {
  captureRefreshState,
  hasRefreshDrafts,
  type RefreshSnapshot,
} from "../refreshState";
import { t } from "../i18n";
import {
  clearReloadNotice,
  showReloadNotice,
  supportsBridge,
} from "./compatibility";

type Bundle = {
  protocol: number;
  requiredBridgeProtocol?: number;
  revision: string;
  script?: string;
  style?: string;
};
export type PanelRuntime = { dispose: () => void; mount: () => Promise<void> };
declare global {
  interface Window {
    __teamcrossRuntime?: PanelRuntime;
    __teamcrossCheckpoint?: RefreshSnapshot;
    __teamcrossPrivateState?: unknown;
    __teamcrossFailedRevision?: string;
    __teamcrossMountGeneration?: number;
    __teamcrossCancelMount?: () => void;
    __teamcrossUpdateFailure?: {
      revision: string;
      stage: string;
      message: string;
      at: string;
    };
  }
}

export async function validateBundle(bundle: Bundle, revision: string) {
  if (
    bundle.protocol !== 1 ||
    !supportsBridge(bundle.requiredBridgeProtocol, Number.MAX_SAFE_INTEGER) ||
    bundle.revision !== revision ||
    !bundle.script ||
    !bundle.style
  )
    throw new Error("Invalid panel bundle");
  const bytes = new TextEncoder().encode(bundle.style + "\0" + bundle.script);
  const digest = await crypto.subtle.digest("SHA-256", bytes);
  const actual = Array.from(new Uint8Array(digest), (b) =>
    b.toString(16).padStart(2, "0"),
  ).join("");
  if (actual !== revision) throw new Error("Panel bundle checksum mismatch");
}

// Execute only the exact locally embedded bundle returned by our authenticated
// Core. No URLs, remote imports, evaluation of user content, or host reload API.
export async function replacePanel(
  bundle: Bundle,
  checkpoint: RefreshSnapshot,
) {
  const previous = window.__teamcrossRuntime;
  if (!previous) throw new Error("Panel runtime unavailable");
  const style = document.getElementById("teamcross-panel-style");
  if (!style) throw new Error("Panel style unavailable");
  const oldStyle = style.textContent;
  const meta = document.querySelector<HTMLMetaElement>(
    'meta[name="teamcross-panel-revision"]',
  );
  const oldRevision = meta?.content || "";
  window.__teamcrossCheckpoint = checkpoint;
  previous.dispose();
  const loading = document.createElement("p");
  loading.setAttribute("role", "status");
  loading.textContent = t("正在更新界面，草稿已保留…");
  document.getElementById("root")?.replaceChildren(loading);
  style.textContent = bundle.style!;
  if (meta) meta.content = bundle.revision;
  const script = document.createElement("script");
  script.type = "module";
  const source = `teamcross-panel-${bundle.revision}.js`;
  script.textContent = bundle.script! + `\n//# sourceURL=${source}`;
  try {
    await new Promise<void>((resolve, reject) => {
      const cleanup = () => {
        clearTimeout(timer);
        clearInterval(poll);
        script.removeEventListener("teamcross-panel-ready", ready);
        window.removeEventListener("error", scriptFailed);
        script.onload = script.onerror = null;
      };
      const failed = (event: Event | string) => {
        cleanup();
        const error =
          typeof event === "string"
            ? new Error(event)
            : (event as CustomEvent<unknown>).detail;
        reject(
          error instanceof Error ? error : new Error("Panel startup failed"),
        );
      };
      const scriptFailed = (event: ErrorEvent) => {
        if (event.filename?.endsWith(source)) {
          cleanup();
          reject(event.error || new Error(event.message));
        }
      };
      const ready = () => {
        if (
          !window.__teamcrossRuntime ||
          window.__teamcrossRuntime === previous
        )
          return;
        cleanup();
        resolve();
      };
      // Inline modules need not emit load. Older bundles publish their runtime
      // after mount; newer ones also acknowledge the first React commit.
      const poll = setInterval(ready, 50);
      const timer = setTimeout(() => {
        cleanup();
        reject(new Error("Panel startup timed out"));
      }, 120000);
      script.onload = ready;
      script.onerror = failed;
      script.addEventListener("teamcross-panel-ready", ready);
      window.addEventListener("error", scriptFailed);
      document.body.appendChild(script);
    });
    delete window.__teamcrossFailedRevision;
    delete window.__teamcrossUpdateFailure;
  } catch (error) {
    window.__teamcrossFailedRevision = bundle.revision;
    // A timed-out bootstrap must not later replace the restored editor.
    window.__teamcrossMountGeneration =
      (window.__teamcrossMountGeneration || 0) + 1;
    window.__teamcrossCancelMount?.();
    if (window.__teamcrossRuntime !== previous)
      window.__teamcrossRuntime?.dispose();
    style.textContent = oldStyle;
    if (meta) meta.content = oldRevision;
    window.__teamcrossCheckpoint = checkpoint;
    await previous.mount();
    throw error;
  } finally {
    script.remove();
  }
}

export function watchPanelUpdates(
  bridge: HostBridge,
  persist: () => void,
  replace = replacePanel,
) {
  let current = document.querySelector<HTMLMetaElement>(
    'meta[name="teamcross-panel-revision"]',
  )?.content;
  if (!current) return () => {};
  let disposed = false,
    checking = false,
    applying = false,
    failed = false,
    composing = false;
  let candidate: Bundle | undefined;
  let reloadRequired = false;
  let lastCheck = 0;
  const banner = document.createElement("aside");
  banner.className = "plugin-update-banner";
  banner.setAttribute("role", "status");
  banner.hidden = true;
  const label = document.createElement("span"),
    button = document.createElement("button");
  button.type = "button";
  button.className = "button small";
  banner.append(label, button);
  document.body.append(banner);
  const busy = () =>
    bridge.hasPendingWrites || composing || bridge.reconnecting;
  function show() {
    const parent =
      [...document.querySelectorAll("dialog[open]")].at(-1) || document.body;
    if (banner.parentElement !== parent) parent.appendChild(banner);
    banner.hidden = !candidate && !bridge.reconnecting;
    if (failed && window.__teamcrossUpdateFailure)
      banner.dataset.updateFailure = JSON.stringify(
        window.__teamcrossUpdateFailure,
      );
    else delete banner.dataset.updateFailure;
    button.hidden = !!bridge.reconnecting;
    if (bridge.reconnecting) {
      label.textContent = t("服务正在更新，正在重新连接；草稿已保留。");
      return;
    }
    const message = failed
      ? t("刷新未完成，草稿已保留，请重试。")
      : busy()
        ? t("新版已就绪，当前操作结束后更新。")
        : t("新版已就绪，你的草稿将保留。");
    if (label.textContent !== message) label.textContent = message;
    const action = hasRefreshDrafts() ? t("保留草稿并刷新") : t("刷新到新版");
    if (button.textContent !== action) button.textContent = action;
    button.disabled = applying || busy();
  }
  async function apply(explicit = false) {
    if (
      !candidate ||
      applying ||
      busy() ||
      reloadRequired ||
      (!explicit && (failed || hasRefreshDrafts()))
    )
      return;
    applying = true;
    show();
    const revision = candidate.revision;
    let stage = "download";
    try {
      const bundle = await bridge.call<Bundle>("teamcross_ui_read", {
        path: `plugin/panel-bundle?revision=${revision}`,
      });
      stage = "validation";
      await validateBundle(bundle, revision);
      if (!supportsBridge(bundle.requiredBridgeProtocol, bridge.protocol)) {
        reloadRequired = true;
        showReloadNotice();
        return;
      }
      // The user may have started typing or submitting while the bundle loaded.
      if (disposed || busy() || (!explicit && hasRefreshDrafts())) return;
      stage = "checkpoint";
      persist();
      const checkpoint = captureRefreshState();
      stage = "startup";
      await replace(bundle, checkpoint);
      current = revision;
      candidate = undefined;
      failed = false;
      delete window.__teamcrossFailedRevision;
      delete window.__teamcrossUpdateFailure;
    } catch (error) {
      failed = true;
      window.__teamcrossFailedRevision = revision;
      window.__teamcrossUpdateFailure = {
        revision,
        stage,
        message: error instanceof Error ? error.message : String(error),
        at: new Date().toISOString(),
      };
      console.error(
        "[Team Cross] Panel update failed",
        window.__teamcrossUpdateFailure,
      );
    } finally {
      applying = false;
      if (!disposed) show();
    }
  }
  button.onclick = () => void apply(true);
  async function tick() {
    if (disposed || applying || document.visibilityState === "hidden") return;
    show();
    if (candidate && !bridge.reconnecting && !reloadRequired) {
      if (!busy() && !failed) void apply();
    }
    if (checking || Date.now() - lastCheck < 15000) return;
    checking = true;
    lastCheck = Date.now();
    try {
      const next = await bridge.call<Bundle>("teamcross_ui_read", {
        path: "plugin/panel-version",
      });
      if (!disposed && /^[a-f0-9]{64}$/.test(next.revision)) {
        reloadRequired =
          next.protocol !== 1 ||
          !supportsBridge(next.requiredBridgeProtocol, bridge.protocol);
        if (reloadRequired) {
          candidate = next;
          showReloadNotice();
          return;
        }
        clearReloadNotice();
        if (next.revision === current) {
          candidate = undefined;
          failed = false;
          show();
          return;
        }
        if (candidate?.revision !== next.revision)
          failed = window.__teamcrossFailedRevision === next.revision;
        candidate = next;
        show();
        void apply();
      }
    } catch {
      /* A restarting/offline Core must not discard the current editor. */
    } finally {
      checking = false;
    }
  }
  const startComposition = () => {
    composing = true;
  };
  const endComposition = () => {
    composing = false;
  };
  const wake = () => {
    lastCheck = 0;
    void tick();
  };
  document.addEventListener("compositionstart", startComposition);
  document.addEventListener("compositionend", endComposition);
  document.addEventListener("visibilitychange", wake);
  window.addEventListener("focus", wake);
  const timer = setInterval(() => void tick(), 1000);
  // Wait for the first React commit and restored drafts before checking.
  const initial = setTimeout(() => void tick(), 1500);
  return () => {
    disposed = true;
    clearInterval(timer);
    clearTimeout(initial);
    banner.remove();
    document.removeEventListener("compositionstart", startComposition);
    document.removeEventListener("compositionend", endComposition);
    document.removeEventListener("visibilitychange", wake);
    window.removeEventListener("focus", wake);
  };
}
