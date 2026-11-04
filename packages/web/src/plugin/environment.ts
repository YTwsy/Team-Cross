import {
  HostBridge,
  MessageDeliveryError,
  restoreState,
  saveState,
} from "./bridge";
import { setHostedStorage, type StoredValues } from "../storage";
import {
  setCurrentConversation,
  setConversationPanel,
  type DeliveryState,
} from "../currentConversation";

import { bindSandboxForms } from "./forms";
import { beginRefreshState, endRefreshState } from "../refreshState";
import { watchPanelUpdates } from "./update";
import { requiredBridgeProtocol } from "./protocol";
import {
  clearReloadNotice,
  reloadRequiredError,
  showReloadNotice,
  supportsBridge,
} from "./compatibility";
import { setLanguageInfo, type LanguageInfo } from "../i18n";
import { APIError } from "../api";

type Snapshot = {
  version: 2;
  scope: string;
  route: string;
  storage: StoredValues;
  deliveries: Record<string, DeliveryState>;
};
export async function initializeEnvironment(signal?: AbortSignal) {
  const bridge = new HostBridge();
  const abort = () => bridge.dispose();
  signal?.addEventListener("abort", abort, { once: true });
  try {
    await bridge.initialize(signal);
    type Bootstrap = {
      scope: string;
      bridgeProtocol?: number;
      language?: LanguageInfo;
    };
    let bootstrap: Bootstrap;
    try {
      bootstrap = await bridge.call<Bootstrap>(
        "teamcross_ui_read",
        {
          path: "plugin/bootstrap",
          body: { requiredBridgeProtocol },
        },
        signal,
      );
    } catch (error) {
      // Historical MCPs allow bootstrap only without a body. Probe that exact
      // read once; never fall back or replay a business read/write this way.
      if (
        !(error instanceof APIError) ||
        error.message !== "WebGUI route is not allowed"
      )
        throw error;
      bootstrap = await bridge.call<Bootstrap>(
        "teamcross_ui_read",
        { path: "plugin/bootstrap" },
        signal,
      );
    }
    signal?.throwIfAborted();
    if (bootstrap.language) setLanguageInfo(bootstrap.language);
    if (!supportsBridge(requiredBridgeProtocol, bootstrap.bridgeProtocol))
      throw reloadRequiredError();
    bridge.protocol = bootstrap.bridgeProtocol!;
    clearReloadNotice();
    const { scope } = bootstrap;
    const saved = restoreState<Snapshot>();
    const state: Snapshot = {
      version: 2,
      scope,
      route: "",
      storage: { local: {}, session: {} },
      deliveries: {},
    };
    if (saved.version === 2 && saved.scope === scope) {
      if (typeof saved.route === "string" && saved.route.startsWith("#/"))
        state.route = saved.route;
      for (const area of ["local", "session"] as const) {
        for (const [key, value] of Object.entries(
          saved.storage?.[area] || {},
        )) {
          if (key.startsWith("teamcross.") && typeof value === "string")
            state.storage[area][key] = value;
        }
      }
      for (const [code, status] of Object.entries(saved.deliveries || {})) {
        if (
          (code.startsWith("TC-") || code.startsWith("connect:")) &&
          (status === "sent" || status === "unknown")
        )
          state.deliveries[code] = status;
      }
    }
    const unbindForms = bindSandboxForms();
    beginRefreshState(scope, window.__teamcrossCheckpoint);
    const restoredPosition =
      window.__teamcrossCheckpoint?.scope === scope
        ? window.__teamcrossCheckpoint.scrollY
        : undefined;
    let positionAttempts = 0;
    const restorePosition = setInterval(() => {
      if (restoredPosition === undefined || ++positionAttempts > 50) {
        clearInterval(restorePosition);
        return;
      }
      if (
        document.documentElement.scrollHeight >=
        restoredPosition + innerHeight
      ) {
        window.scrollTo(0, restoredPosition);
        clearInterval(restorePosition);
      }
    }, 100);
    const cancelPosition = () => clearInterval(restorePosition);
    window.addEventListener("pointerdown", cancelPosition, { once: true });
    window.addEventListener("wheel", cancelPosition, { once: true });
    const persist = () => saveState(state);
    setHostedStorage(state.storage, persist);
    if (!location.hash && state.route) location.hash = state.route;
    const routeChanged = () => {
      state.route = location.hash;
      persist();
    };
    window.addEventListener("hashchange", routeChanged);
    setConversationPanel(true);
    if (bridge.host?.hostCapabilities?.message) {
      setCurrentConversation({
        connectionStatus: true,
        status: (code) => state.deliveries[code],
        allowConnectAgain(spaceId) {
          delete state.deliveries[`connect:${spaceId}`];
          persist();
        },
        async connect(spaceId, name) {
          const key = `connect:${spaceId}`;
          if (state.deliveries[key]) return;
          state.deliveries[key] = "unknown";
          persist();
          try {
            await bridge.connectConversation(spaceId, name);
            state.deliveries[key] = "sent";
          } catch (error) {
            if (error instanceof MessageDeliveryError && !error.dispatched)
              delete state.deliveries[key];
            throw error;
          } finally {
            persist();
          }
        },
        async send(bundle, prompt) {
          if (state.deliveries[bundle.code]) return;
          // Persist the uncertain state before dispatch; reopening never resends.
          state.deliveries[bundle.code] = "unknown";
          persist();
          try {
            await bridge.sendSelection(bundle.code, bundle.references, prompt);
            state.deliveries[bundle.code] = "sent";
          } catch (error) {
            if (error instanceof MessageDeliveryError && !error.dispatched)
              delete state.deliveries[bundle.code];
            throw error;
          } finally {
            persist();
          }
        },
      });
    }
    const stopUpdates = watchPanelUpdates(bridge, persist);
    let disposed = false;
    const dispose = () => {
      if (disposed) return;
      disposed = true;
      signal?.removeEventListener("abort", abort);
      routeChanged();
      stopUpdates();
      clearInterval(restorePosition);
      window.removeEventListener("pointerdown", cancelPosition);
      window.removeEventListener("wheel", cancelPosition);
      endRefreshState();
      unbindForms();
      window.removeEventListener("hashchange", routeChanged);
      window.removeEventListener("pagehide", dispose);
      setHostedStorage();
      setCurrentConversation();
      setConversationPanel(false);
      bridge.dispose();
    };
    window.addEventListener("pagehide", dispose, { once: true });
    return dispose;
  } catch (error) {
    setConversationPanel(false);
    if ((error as { code?: string })?.code === "plugin_reload_required") {
      if (window.__teamcrossRuntime) showReloadNotice();
      else {
        // App cannot mount on an incompatible bridge; keep its startup guidance
        // legible in the host's theme without restoring another Core's state.
        const theme = bridge.host?.hostContext?.theme;
        document.documentElement.dataset.theme =
          theme === "dark" || theme === "light"
            ? theme
            : matchMedia("(prefers-color-scheme: dark)").matches
              ? "dark"
              : "light";
      }
    }
    signal?.removeEventListener("abort", abort);
    bridge.dispose();
    throw error;
  }
}
