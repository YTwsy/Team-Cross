import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import App from "./App";
import { api, errorText } from "./api";
import { initializeEnvironment } from "./environment";
import { ErrorBox } from "./components/ui";
import { browserLanguage, setLanguageInfo, type LanguageInfo } from "./i18n";
import "./styles.css";
import "./library.css";
import "./space.css";
async function mount() {
  const generation = (window.__teamcrossMountGeneration || 0) + 1;
  window.__teamcrossMountGeneration = generation;
  let cleanup: (() => void) | undefined;
  try {
    cleanup = await initializeEnvironment();
    try {
      setLanguageInfo(await api<LanguageInfo>("ui-language"));
    } catch {
      setLanguageInfo({ mode: "auto", resolved: browserLanguage() });
    }
    if (window.__teamcrossMountGeneration !== generation) {
      cleanup?.();
      return;
    }
    const root = createRoot(document.getElementById("root")!);
    root.render(
      <StrictMode>
        <App />
      </StrictMode>,
    );
    window.__teamcrossRuntime = {
      mount,
      dispose: () => {
        root.unmount();
        cleanup?.();
      },
    };
    window.__teamcrossCheckpoint = undefined;
  } catch (error) {
    cleanup?.();
    throw error;
  }
}
try {
  await mount();
} catch (error) {
  if (!window.__teamcrossRuntime)
    createRoot(document.getElementById("root")!).render(
      <ErrorBox message={errorText(error)} retry={() => location.reload()} />,
    );
  throw error;
}
