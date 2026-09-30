import { useCallback, useEffect, useRef, useState } from "react";
import { api, errorText } from "./api";
import { t } from "./i18n";
import { storageKey } from "./platform";
import { watchVisibleRefresh } from "./visibility";

export type Theme = "system" | "light" | "dark";
function validTheme(mode: unknown): mode is Theme {
  return mode === "system" || mode === "light" || mode === "dark";
}

export function useTheme() {
  const [theme, adopt] = useState<Theme>(() => {
    const cached = localStorage.getItem(storageKey("teamcross.theme.v1"));
    return validTheme(cached) ? cached : "system";
  });
  const [working, setWorking] = useState(false);
  const [error, setError] = useState("");
  const pending = useRef(false);
  const revision = useRef(0);
  useEffect(
    () =>
      watchVisibleRefresh(async (signal) => {
        if (pending.current) return;
        const version = revision.current;
        try {
          const next = await api<{ mode: Theme }>(
            "ui-theme",
            undefined,
            signal,
          );
          if (
            !signal.aborted &&
            version === revision.current &&
            validTheme(next.mode)
          )
            adopt(next.mode);
        } catch {
          // Keep the last confirmed appearance while Core is unavailable.
        }
      }, 5000),
    [],
  );
  const setTheme = useCallback(async (mode: Theme) => {
    if (pending.current || !validTheme(mode)) return;
    pending.current = true;
    revision.current++;
    setWorking(true);
    setError("");
    try {
      const next = await api<{ mode: Theme }>("ui-theme", { mode });
      if (next.mode !== mode)
        throw new Error(t("主题更改尚未确认，请查看当前设置后重试。"));
      adopt(next.mode);
    } catch (e) {
      setError(errorText(e));
    } finally {
      pending.current = false;
      setWorking(false);
    }
  }, []);
  useEffect(() => {
    const media = matchMedia("(prefers-color-scheme: dark)");
    const apply = () => {
      document.documentElement.dataset.theme =
        theme === "system" ? (media.matches ? "dark" : "light") : theme;
    };
    apply();
    // This cache is for the first/offline frame, never imported into Core.
    localStorage.setItem(storageKey("teamcross.theme.v1"), theme);
    media.addEventListener("change", apply);
    return () => media.removeEventListener("change", apply);
  }, [theme]);
  return { theme, setTheme, working, error };
}
