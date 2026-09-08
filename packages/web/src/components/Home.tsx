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
        eyebrow="一起继续，已有的工作"
        title="协作空间"
        subtitle="从一个已有会话出发，把上下文和工作现场交给同事。"
      />
      <div className="entry-grid">
        <a href="#/create" className="entry-card">
          <div className="entry-icon blue">
            <Icon name="plus" size={26} />
          </div>
          <div>
            <h2>发起协作</h2>
            <p>选一个 Codex 会话，邀请同事一起继续。</p>
          </div>
          <Icon name="arrow" />
        </a>
        <a href="#/join" className="entry-card">
          <div className="entry-icon">
            <Icon name="join" size={26} />
          </div>
          <div>
            <h2>加入协作</h2>
            <p>有同事的邀请？用自己的 Codex 加入。</p>
          </div>
          <Icon name="arrow" />
        </a>
      </div>
      <div className="section-heading">
        <h2>
          最近协作 <span className="count">{data?.length || 0}</span>
        </h2>
        <div className="segmented" aria-label="筛选协作">
          {[
            ["all", "全部"],
            ["owner", "我发起的"],
            ["remote", "我加入的"],
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
                  <span>{projectName(c.repo)}</span>
                  <span className="separator">·</span>
                  <span title={c.host}>{c.host}</span>
                  <span className="separator">·</span>
                  <span>{c.role === "owner" ? "我发起的" : "我加入的"}</span>
                </div>
              </div>
              <div className="row-status">
                <Badge collaboration={c} />
                <span className="muted small-text">
                  {c.sharing ? "共享中" : "未共享"} ·{" "}
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
              filter === "all" ? "下一次协作，从这里开始" : "这里还没有协作"
            }
          >
            <p>已有的讨论、代码和思路，不必再从头解释。</p>
            {filter === "all" && (
              <a className="text-link" href="#/create">
                选择一个来源会话 <Icon name="arrow" size={16} />
              </a>
            )}
          </Empty>
        </div>
      )}
      <div className="quiet-note">
        <Icon name="desktop" size={17} />
        <span>
          共享会话在发起者的 Mac 上执行，同事使用自己的 Codex 客户端。
        </span>
      </div>
    </>
  );
}
