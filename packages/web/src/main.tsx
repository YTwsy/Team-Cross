import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import App from "./App";
import { api, errorText } from "./api";
import { initializeEnvironment } from "./environment";
import { ErrorBox } from "./components/ui";
import { browserLanguage, setLanguageInfo, type LanguageInfo } from "./i18n";
import "./styles.css";
import "./library.css";
const root = createRoot(document.getElementById("root")!);
try {
  await initializeEnvironment();
  try {
    setLanguageInfo(await api<LanguageInfo>("ui-language"));
  } catch {
    setLanguageInfo({ mode: "auto", resolved: browserLanguage() });
  }
  root.render(
    <StrictMode>
      <App />
    </StrictMode>,
  );
} catch (error) {
  root.render(
    <ErrorBox message={errorText(error)} retry={() => location.reload()} />,
  );
}
