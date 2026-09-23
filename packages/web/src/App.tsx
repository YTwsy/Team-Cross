import {
  languageInfo,
  setLanguageInfo,
  t,
  tr,
  type LanguageInfo,
} from "./i18n";
import { useEffect, useState } from "react";
import { api } from "./api";
import { Home } from "./components/Home";
import { StartSpace } from "./components/Publisher";
import { Create } from "./components/Create";
import { Detail } from "./components/Detail";
import { Join } from "./components/Join";
import { Settings, type Theme } from "./components/Settings";
import { Icon } from "./components/ui";
import { LibraryProvider } from "./library";
import { Library, SelectionTray } from "./components/Library";
import { QuickLook } from "./components/QuickLook";
export default function App() {
  return (
    <LibraryProvider>
      <AppShell />
    </LibraryProvider>
  );
}
function AppShell() {
  const [route, setRoute] = useState(() => location.hash.slice(1) || "/");
  const [uiLanguage, setUILanguage] = useState<LanguageInfo>(languageInfo);
  function adoptLanguage(next: LanguageInfo) {
    if (setLanguageInfo(next)) setUILanguage(languageInfo());
  }
  useEffect(() => {
    let mounted = true;
    const refresh = async () => {
      try {
        const next = await api<LanguageInfo>("ui-language");
        if (mounted) adoptLanguage(next);
      } catch {
        // Keep the last confirmed language while the local Core is unavailable.
      }
    };
    const interval = setInterval(() => void refresh(), 5000);
    return () => {
      mounted = false;
      clearInterval(interval);
    };
  }, []);
  const [theme, setTheme] = useState<Theme>(() => {
    const t = localStorage.getItem("teamcross.theme.v1");
    return t === "light" || t === "dark" ? t : "system";
  });
  useEffect(() => {
    const handler = () => {
      setRoute(location.hash.slice(1) || "/");
      window.scrollTo(0, 0);
    };
    window.addEventListener("hashchange", handler);
    return () => window.removeEventListener("hashchange", handler);
  }, []);
  useEffect(() => {
    const media = matchMedia("(prefers-color-scheme: dark)");
    const apply = () => {
      document.documentElement.dataset.theme =
        theme === "system" ? (media.matches ? "dark" : "light") : theme;
    };
    apply();
    localStorage.setItem("teamcross.theme.v1", theme);
    media.addEventListener("change", apply);
    return () => media.removeEventListener("change", apply);
  }, [theme]);
  useEffect(() => {
    const timer = setTimeout(
      () =>
        document
          .querySelector<HTMLElement>("h1")
          ?.focus({ preventScroll: true }),
      0,
    );
    return () => clearTimeout(timer);
  }, [route]);
  const libraryRoute = route.split("?")[0];
  const quick = libraryRoute === "/library/quick";
  const detailId = route.match(/^\/collaborations\/([^/]+)$/)?.[1];
  const page =
    libraryRoute === "/library" ? (
      <Library
        initialKey={
          new URLSearchParams(route.split("?")[1]).get("item") || undefined
        }
      />
    ) : route === "/create" ? (
      <StartSpace />
    ) : route.startsWith("/execute/") ? (
      <Create spaceId={route.slice(9)} />
    ) : route === "/join" || route.startsWith("/join/") ? (
      <Join
        pendingId={route.startsWith("/join/") ? route.slice(6) : undefined}
      />
    ) : route === "/settings" ? (
      <Settings
        theme={theme}
        setTheme={setTheme}
        uiLanguage={uiLanguage}
        onLanguageChange={adoptLanguage}
      />
    ) : detailId ? (
      <Detail key={detailId} id={detailId} />
    ) : (
      <Home />
    );
  if (quick) return <QuickLook />;
  return (
    <div
      className={`app-shell ${libraryRoute === "/library" ? "library-shell" : ""}`}
    >
      <a
        className="skip-link"
        href="#main-content"
        onClick={(e) => {
          e.preventDefault();
          document.getElementById("main-content")?.focus();
        }}
      >
        {t("跳到主要内容") + " "}
      </a>
      <aside className="sidebar">
        <a className="brand" href="#/" aria-label={t("Team Cross 首页")}>
          <span className="brand-mark">
            <i />
            <i />
            <i />
            <i />
          </span>
          <span>
            Team Cross<small>{t("一起继续")}</small>
          </span>
        </a>
        <nav aria-label={t("主导航")}>
          <a
            className={
              route !== "/settings" && libraryRoute !== "/library"
                ? "active"
                : ""
            }
            href="#/"
            aria-current={route === "/" ? "page" : undefined}
          >
            <Icon name="grid" />
            {t("协作空间") + " "}
          </a>
          <a
            className={libraryRoute === "/library" ? "active" : ""}
            href="#/library"
            aria-current={libraryRoute === "/library" ? "page" : undefined}
          >
            <Icon name="book" />
            {t("资源库") + " "}
          </a>
          <a
            className={route === "/settings" ? "active" : ""}
            href="#/settings"
            aria-current={route === "/settings" ? "page" : undefined}
          >
            <Icon name="settings" />
            {t("设置与连接") + " "}
          </a>
        </nav>
        <div className="sidebar-bottom">
          <div className="local-label">
            <span className="dot green-dot" />
            {t("本地运行") + " "}
            <span className="experiment">{t("实验版")}</span>
          </div>
          <button
            className="theme-toggle"
            onClick={() =>
              setTheme(
                theme === "system"
                  ? "light"
                  : theme === "light"
                    ? "dark"
                    : "system",
              )
            }
            aria-label={tr`切换主题，当前${theme === "system" ? t("跟随系统") : theme === "light" ? t("浅色") : t("深色")}`}
          >
            <Icon
              name={
                theme === "dark"
                  ? "moon"
                  : theme === "light"
                    ? "sun"
                    : "desktop"
              }
              size={17}
            />
            {theme === "system"
              ? t("跟随系统")
              : theme === "light"
                ? t("浅色外观")
                : t("深色外观")}
          </button>
        </div>
      </aside>
      <main id="main-content" tabIndex={-1} className="main-content">
        {!detailId && (
          <div className="topbar">
            <span>{t("你的工作现场，与同事相连")}</span>
            <span className="local-pill">
              <Icon name="desktop" size={14} />
              {t("macOS · 原生会话") + " "}
            </span>
          </div>
        )}
        <div className="page" key={route}>
          {page}
        </div>
        <SelectionTray />
      </main>
    </div>
  );
}
