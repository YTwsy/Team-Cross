import { useEffect, useState } from "react";
import { useResource } from "../api";
import { t, tr } from "../i18n";
import { openFullLibrary } from "../library";
import { hasNativeWindows, windowAction } from "../platform";
import { type Collaboration, projectName, relativeTime } from "../types";
import { Library, SelectionTray } from "./Library";
import { Badge, Empty, ErrorBox, Icon, Loading } from "./ui";

function QuickServiceMenu() {
  return hasNativeWindows() ? (
    <button
      className="quick-service-menu text-link"
      onClick={() => windowAction("menu")}
    >
      {t("服务与设置")}
    </button>
  ) : null;
}

export function QuickLook() {
  const [view, setView] = useState<"collaborations" | "resources">(
    "collaborations",
  );
  const [pinned, setPinned] = useState(false);
  const [hostError, setHostError] = useState(false);
  const native = hasNativeWindows();
  useEffect(() => {
    const pin = (event: Event) =>
      setPinned(!!(event as CustomEvent<boolean>).detail);
    const error = (event: Event) =>
      setHostError(!!(event as CustomEvent<boolean>).detail);
    window.addEventListener("teamcross-pinned", pin);
    window.addEventListener("teamcross-window-error", error);
    return () => {
      window.removeEventListener("teamcross-pinned", pin);
      window.removeEventListener("teamcross-window-error", error);
    };
  }, []);
  return (
    <main className="quick-shell">
      <header className="quick-heading">
        <span className="eyebrow">TEAM CROSS</span>
        <div className="library-header-actions">
          {native && (
            <button
              className="icon-button"
              aria-label={pinned ? t("取消固定浮窗") : t("固定为浮窗")}
              aria-pressed={pinned}
              onClick={() => windowAction("pin")}
            >
              <Icon name="pin" size={17} />
            </button>
          )}
          <button
            className="text-link"
            onClick={() =>
              openFullLibrary(view === "resources" ? "/library" : "/")
            }
          >
            {view === "resources" ? t("打开资源库") : t("打开协作空间")}
            <Icon name="arrow" size={14} />
          </button>
        </div>
      </header>
      <ErrorBox message={hostError ? t("桌面操作未完成，请重试。") : ""} />
      <nav className="quick-sections" aria-label={t("速览视图")}>
        {(
          [
            ["collaborations", t("当前协作")],
            ["resources", t("资源速览")],
          ] as const
        ).map(([id, label]) => (
          <button
            key={id}
            aria-pressed={view === id}
            onClick={() => {
              setView(id);
              window.scrollTo(0, 0);
            }}
          >
            {label}
          </button>
        ))}
      </nav>
      <CurrentCollaborations hidden={view !== "collaborations"} />
      <Library
        quick
        hidden={view !== "resources"}
        quickActions={<QuickServiceMenu />}
      />
      <SelectionTray quick />
    </main>
  );
}

function CurrentCollaborations({ hidden }: { hidden: boolean }) {
  const { data, error, loading, reload } = useResource<Collaboration[]>(
    hidden ? null : "collaborations",
    5000,
  );
  const [filter, setFilter] = useState("all");
  useEffect(() => {
    if (hidden) return;
    window.addEventListener("focus", reload);
    return () => window.removeEventListener("focus", reload);
  }, [hidden, reload]);
  const items = (data || []).filter(
    (c) =>
      c.state !== "ended" &&
      c.state !== "left" &&
      (filter === "all" || c.role === filter),
  );
  return (
    <section hidden={hidden} aria-label={t("当前协作")}>
      <div className="quick-toolbar">
        <nav className="library-views" aria-label={t("筛选协作")}>
          {[
            ["all", t("全部")],
            ["owner", t("我发起的")],
            ["remote", t("我加入的")],
          ].map(([id, label]) => (
            <button
              key={id}
              className={filter === id ? "active" : ""}
              aria-pressed={filter === id}
              onClick={() => setFilter(id!)}
            >
              {label}
            </button>
          ))}
        </nav>
        <QuickServiceMenu />
      </div>
      <ErrorBox message={error} retry={reload} />
      {loading && !data ? (
        <Loading />
      ) : items.length ? (
        <div className="quick-collaborations">
          {items.map((c) => (
            <button
              className="quick-collaboration"
              key={c.id}
              onClick={() => openFullLibrary(`/collaborations/${c.id}`)}
            >
              <span className="quick-collaboration-title">
                <strong>{c.title}</strong>
                <Icon name="arrow" size={16} />
              </span>
              <span className="quick-collaboration-meta">
                <span>
                  {c.role === "owner" ? t("我发起的") : t("我加入的")}
                </span>
                <span>
                  {c.hasExecution === false
                    ? tr`${c.materials?.filter((m) => !m.withdrawnAt).length || 0} 份会话材料`
                    : projectName(c.repo)}
                </span>
                {c.host && <span title={c.host}>{c.host}</span>}
              </span>
              <span className="quick-collaboration-status">
                <Badge collaboration={c} />
                <span>{relativeTime(c.updatedAt)}</span>
              </span>
            </button>
          ))}
        </div>
      ) : !error ? (
        <Empty icon="people" title={t("这里还没有进行中的协作")}>
          <p>{t("打开协作空间，发起、加入或恢复一次协作。")}</p>
          <button className="text-link" onClick={() => openFullLibrary("/")}>
            {t("打开协作空间")}
            <Icon name="arrow" size={14} />
          </button>
        </Empty>
      ) : null}
    </section>
  );
}
