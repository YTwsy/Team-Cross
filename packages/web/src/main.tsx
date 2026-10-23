import { StrictMode, useLayoutEffect } from "react";
import { createRoot, type Root } from "react-dom/client";
import App from "./App";
import { api, APIError, errorText } from "./api";
import { initializeEnvironment } from "./environment";
import { ErrorBox } from "./components/ui";
import { browserLanguage, setLanguageInfo, t, type LanguageInfo } from "./i18n";
import "./styles.css";
import "./library.css";
import "./space.css";

// currentScript is null for modules. Our self-contained entry is the last
// module at evaluation time, including replacements inserted by older panels.
const entryScript = [
  ...document.querySelectorAll<HTMLScriptElement>('script[type="module"]'),
].at(-1);
function Mounted({ ready }: { ready: () => void }) {
  useLayoutEffect(ready, [ready]);
  return null;
}
function StartupError({ error }: { error: unknown }) {
  const reloadRequired =
    error instanceof APIError && error.code === "plugin_reload_required";
  return (
    <div className="page">
      <ErrorBox
        title={reloadRequired ? t("插件连接需要重新加载") : undefined}
        message={reloadRequired ? error.recovery : errorText(error)}
        retry={reloadRequired ? undefined : () => location.reload()}
      />
    </div>
  );
}
async function mount() {
  const generation = (window.__teamcrossMountGeneration || 0) + 1;
  window.__teamcrossMountGeneration = generation;
  const controller = new AbortController();
  const cancel = () => controller.abort();
  window.__teamcrossCancelMount = cancel;
  let cleanup: (() => void) | undefined;
  let root: Root | undefined;
  const check = () => {
    controller.signal.throwIfAborted();
    if (window.__teamcrossMountGeneration !== generation)
      throw new DOMException("Superseded panel startup", "AbortError");
  };
  try {
    cleanup = await initializeEnvironment(controller.signal);
    check();
    try {
      setLanguageInfo(
        await api<LanguageInfo>("ui-language", undefined, controller.signal),
      );
    } catch {
      setLanguageInfo({ mode: "auto", resolved: browserLanguage() });
    }
    check();
    await new Promise<void>((resolve, reject) => {
      const aborted = () => reject(new DOMException("Aborted", "AbortError"));
      controller.signal.addEventListener("abort", aborted, { once: true });
      const ready = () => {
        controller.signal.removeEventListener("abort", aborted);
        resolve();
      };
      root = createRoot(document.getElementById("root")!, {
        onUncaughtError: reject,
      });
      root.render(
        <StrictMode>
          <App />
          <Mounted ready={ready} />
        </StrictMode>,
      );
    });
    check();
    window.__teamcrossRuntime = {
      mount,
      dispose: () => {
        root?.unmount();
        cleanup?.();
      },
    };
    window.__teamcrossCheckpoint = undefined;
  } catch (error) {
    root?.unmount();
    cleanup?.();
    throw error;
  } finally {
    if (window.__teamcrossCancelMount === cancel)
      delete window.__teamcrossCancelMount;
  }
}
try {
  await mount();
  entryScript?.dispatchEvent(new Event("teamcross-panel-ready"));
  // Migrate already-open v1 panels without restarting their host: their loader
  // waits for load, which inline modules do not emit on their own.
  entryScript?.dispatchEvent(new Event("load"));
} catch (error) {
  const reloadRequired =
    error instanceof APIError && error.code === "plugin_reload_required";
  entryScript?.dispatchEvent(new CustomEvent("error", { detail: error }));
  if (!window.__teamcrossRuntime)
    createRoot(document.getElementById("root")!).render(
      <StartupError error={error} />,
    );
  if (!reloadRequired) throw error;
}
