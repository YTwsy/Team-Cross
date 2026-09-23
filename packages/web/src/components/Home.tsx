import { t, tr } from "../i18n";
import { useState } from "react";
import { useResource } from "../api";
import { type Collaboration, projectName, relativeTime } from "../types";
import { Badge, Empty, ErrorBox, Icon, Loading, PageHeading } from "./ui";
export function Home() {
  const { data, error, loading, reload } = useResource<Collaboration[]>(
    "collaborations",
    5000,
  );
  const [filter, setFilter] = useState("all");
  const items = (data || []).filter(
    (c) => filter === "all" || c.role === filter,
  );
  return (
    <>
      <PageHeading
        eyebrow={t("一起继续，已有的工作")}
        title={t("协作空间")}
        subtitle={t("分享已完成的调查，汇集各自的分析，需要时再一起继续执行。")}
      />
      <div className="entry-grid">
        <a href="#/create" className="entry-card">
          <div className="entry-icon blue">
            <Icon name="plus" size={26} />
          </div>
          <div>
            <h2>{t("发起协作")}</h2>
            <p>{t("选择会话与公开范围，分享审阅或共同继续执行。")}</p>
          </div>
          <Icon name="arrow" />
        </a>
        <a href="#/join" className="entry-card">
          <div className="entry-icon">
            <Icon name="join" size={26} />
          </div>
          <div>
            <h2>{t("加入协作")}</h2>
            <p>{t("有同事的邀请？先看看共享上下文。")}</p>
          </div>
          <Icon name="arrow" />
        </a>
      </div>
      <div className="section-heading">
        <h2>
          {t("最近协作") + " "}
          <span className="count">{data?.length || 0}</span>
        </h2>
        <div className="segmented" aria-label={t("筛选协作")}>
          {[
            ["all", t("全部")],
            ["owner", t("我发起的")],
            ["remote", t("我加入的")],
          ].map(([value, label]) => (
            <button
              key={value}
              aria-pressed={filter === value}
              onClick={() => setFilter(value!)}
            >
              {label}
            </button>
          ))}
        </div>
      </div>
      <ErrorBox message={error} retry={reload} />
      {loading && !data ? (
        <Loading />
      ) : items.length ? (
        <div className="collaboration-list">
          {items.map((c) => (
            <a
              className="collaboration-row"
              href={`#/collaborations/${c.id}`}
              key={c.id}
            >
              <span className="project-icon">
                <Icon
                  name={c.workspaceMode === "worktree" ? "branch" : "folder"}
                />
              </span>
              <div className="row-main">
                <h3>{c.title}</h3>
                <div className="row-meta">
                  <span>
                    {c.hasExecution === false
                      ? tr`${c.materials?.filter((m) => !m.withdrawnAt).length || 0} 份会话材料`
                      : projectName(c.repo)}
                  </span>
                  <span className="separator">·</span>
                  <span title={c.host}>{c.host}</span>
                  <span className="separator">·</span>
                  <span>
                    {c.role === "owner" ? t("我发起的") : t("我加入的")}
                  </span>
                </div>
              </div>
              <div className="row-status">
                <Badge collaboration={c} />
                <span className="muted small-text">
                  {c.sharing ? t("共享中") : t("未共享")} ·{" "}
                  {relativeTime(c.updatedAt)}
                </span>
              </div>
              <Icon name="chevron" size={17} />
            </a>
          ))}
        </div>
      ) : (
        <div className="panel">
          <Empty
            icon="people"
            title={
              filter === "all"
                ? t("下一次协作，从这里开始")
                : t("这里还没有协作")
            }
          >
            <p>{t("已有的讨论、代码和思路，不必再从头解释。")}</p>
            {filter === "all" && (
              <a className="text-link" href="#/create">
                {t("选择一个来源会话") + " "}
                <Icon name="arrow" size={16} />
              </a>
            )}
          </Empty>
        </div>
      )}
      <div className="quiet-note">
        <Icon name="desktop" size={17} />
        <span>
          {t("共享会话在发起者的 Mac 上执行，同事使用本机的原生客户端。")}
        </span>
      </div>
    </>
  );
}
