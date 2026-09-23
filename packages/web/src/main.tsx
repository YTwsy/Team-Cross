import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import App from "./App";
import { browserLanguage, setLanguageInfo, type LanguageInfo } from "./i18n";
import "./styles.css";
import "./library.css";
try {
  const response = await fetch("/api/ui-language");
  if (!response.ok) throw new Error("language unavailable");
  const setting = (await response.json()) as LanguageInfo;
  setLanguageInfo(setting);
} catch {
  setLanguageInfo({ mode: "auto", resolved: browserLanguage() });
}
createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
