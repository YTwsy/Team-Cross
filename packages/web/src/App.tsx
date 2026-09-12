import { useEffect, useState } from "react";
import { Home } from "./components/Home";
import { Create } from "./components/Create";
import { Detail } from "./components/Detail";
import { Join } from "./components/Join";
import { Settings, type Theme } from "./components/Settings";
import { Icon } from "./components/ui";
export default function App() {
  const [route, setRoute] = useState(() => location.hash.slice(1) || "/");
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
  const detailId = route.match(/^\/collaborations\/([^/]+)$/)?.[1];
  const page =
    route === "/create" ? (
      <Create />
    ) : route === "/join" || route.startsWith("/join/") ? (
      <Join
        pendingId={route.startsWith("/join/") ? route.slice(6) : undefined}
      />
    ) : route === "/settings" ? (
      <Settings theme={theme} setTheme={setTheme} />
    ) : detailId ? (
      <Detail key={detailId} id={detailId} />
    ) : (
      <Home />
    );
  return (
    <div className="app-shell">
      <a
        className="skip-link"
        href="#main-content"
        onClick={(e) => {
          e.preventDefault();
          document.getElementById("main-content")?.focus();
        }}
      >
        跳到主要内容
      </a>
      <aside className="sidebar">
        <a className="brand" href="#/" aria-label="Team Cross 首页">
          <span className="brand-mark">
            <i />
            <i />
            <i />
            <i />
          </span>
          <span>
            Team Cross<small>一起继续</small>
          </span>
        </a>
        <nav aria-label="主导航">
          <a
            className={route !== "/settings" ? "active" : ""}
            href="#/"
            aria-current={route === "/" ? "page" : undefined}
          >
            <Icon name="grid" />
            协作空间
          </a>
          <a
            className={route === "/settings" ? "active" : ""}
            href="#/settings"
            aria-current={route === "/settings" ? "page" : undefined}
          >
            <Icon name="settings" />
            设置与连接
          </a>
        </nav>
        <div className="sidebar-bottom">
          <div className="local-label">
            <span className="dot green-dot" />
            本地运行 <span className="experiment">实验版</span>
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
            aria-label={`切换主题，当前${theme === "system" ? "跟随系统" : theme === "light" ? "浅色" : "深色"}`}
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
              ? "跟随系统"
              : theme === "light"
                ? "浅色外观"
                : "深色外观"}
          </button>
        </div>
      </aside>
      <main id="main-content" tabIndex={-1} className="main-content">
        <div className="topbar">
          <span>你的工作现场，与同事相连</span>
          <span className="local-pill">
            <Icon name="desktop" size={14} />
            macOS · 原生会话
          </span>
        </div>
        <div className="page" key={route}>
          {page}
        </div>
      </main>
    </div>
  );
}
