import {
  HostBridge,
  MessageDeliveryError,
  restoreState,
  saveState,
} from "./bridge";
import { setHostedStorage, type StoredValues } from "../storage";
import {
  setCurrentConversation,
  type DeliveryState,
} from "../currentConversation";

import { bindSandboxForms } from "./forms";

type Snapshot = {
  version: 2;
  scope: string;
  route: string;
  storage: StoredValues;
  deliveries: Record<string, DeliveryState>;
};
export async function initializeEnvironment() {
  const bridge = new HostBridge();
  try {
    await bridge.initialize();
    const { scope } = await bridge.call<{ scope: string }>(
      "teamcross_ui_read",
      { path: "plugin/bootstrap" },
    );
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
    const persist = () => saveState(state);
    setHostedStorage(state.storage, persist);
    if (!location.hash && state.route) location.hash = state.route;
    const routeChanged = () => {
      state.route = location.hash;
      persist();
    };
    window.addEventListener("hashchange", routeChanged);
    if (bridge.host?.hostCapabilities?.message) {
      setCurrentConversation({
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
    window.addEventListener(
      "pagehide",
      () => {
        routeChanged();
        unbindForms();
        window.removeEventListener("hashchange", routeChanged);
        setHostedStorage();
        setCurrentConversation();
        bridge.dispose();
      },
      { once: true },
    );
  } catch (error) {
    bridge.dispose();
    throw error;
  }
}
