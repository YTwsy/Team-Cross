import type { ReactNode } from "react";
import { t, tr } from "../i18n";
import type { LibraryReference } from "../library";
import { relativeTime, type Collaboration } from "../types";
import type { WorkbenchView } from "../workbench";
import { agentRequestLabel } from "./AgentPairings";
import { SpaceAvatar, spaceMembers } from "./SpaceIdentity";
import { Icon } from "./ui";

export function SpaceOverview({
  collaboration: c,
  view,
  onRead,
  onBrief,
  onRequests,
  onRequest,
  management,
}: {
  collaboration: Collaboration;
  view?: WorkbenchView;
  onRead: (ref: LibraryReference) => void;
  onBrief: () => void;
  onRequests: () => void;
  onRequest: (id: string) => void;
  management?: ReactNode;
}) {
  const materials = (c.materials || [])
    .filter((m) => !m.withdrawnAt && m.versions.length)
    .sort((a, b) =>
      b.versions.at(-1)!.createdAt.localeCompare(a.versions.at(-1)!.createdAt),
    );
  const pending = view?.requests.find((r) =>
    ["unknown", "failed", "queued", "submitted", "received"].includes(r.state),
  );
  const members = spaceMembers(c);
  const sessions = c.sessions || [];
  const needsAttention = sessions.filter(
    (s) => s.error || s.approvals > 0 || s.nativeWaiting || s.inputRequested,
  );
  return (
    <>
      <div className="workbench-section-heading space-overview-heading">
        <div>
          <h2>{t("空间概览")}</h2>
          <p className="muted">{t("当前共识、待确认的问题与最近动态。")}</p>
        </div>
      </div>
      <section className="space-session-overview" aria-label={t("协作会话")}>
        <div className="space-section-heading">
          <h3>{t("协作会话")}</h3>
          <span>{c.sessions?.length || 0}</span>
        </div>
        <div className="execution-overview-counts">
          <span>{tr`${sessions.filter((s) => s.busy).length} 个正在执行`}</span>
          <span>{tr`${needsAttention.length} 个需要处理`}</span>
          <span>{tr`${sessions.filter((s) => s.paused).length} 个已暂停`}</span>
        </div>
        {needsAttention.map((s) => (
          <button
            key={s.id}
            className="execution-attention"
            onClick={() =>
              onRead({ spaceId: c.id, executionId: s.id, kind: "context" })
            }
          >
            <strong>{s.title}</strong>
            <span>
              {s.error
                ? t("运行需要处理")
                : s.approvals > 0 || s.nativeWaiting
                  ? t("等待回应")
                  : t("有人申请输入")}
            </span>
            <Icon name="arrow" size={16} />
          </button>
        ))}
      </section>
      <div className="space-overview-grid">
        <div className="space-overview-column">
          <section className="space-overview-topic">
            <div className="space-kicker">
              <Icon name="grid" size={15} />
              {t("本次协作")}
            </div>
            <h3>{view?.brief.topic || c.title}</h3>
            <div className="space-decisions">
              {view?.brief.decisions.map((item, i) => (
                <div key={i}>
                  <Icon name="check" size={16} />
                  <p>{item.text}</p>
                </div>
              ))}
              {view?.brief.questions.map((item, i) => (
                <div className="space-open-question" key={i}>
                  <Icon name="clock" size={16} />
                  <p>
                    {t("待确认")}：{item.text}
                  </p>
                </div>
              ))}
            </div>
            <div className="space-topic-footer">
              <span>
                {view?.brief.revision && view.brief.updatedAt
                  ? relativeTime(view.brief.updatedAt)
                  : t("简报尚未更新")}
              </span>
              <button className="text-link" onClick={onBrief}>
                {t("查看完整简报")}
                <Icon name="arrow" size={13} />
              </button>
            </div>
          </section>
          <section>
            <div className="space-section-heading">
              <h3>
                {t("最近材料")} <span>{materials.length}</span>
              </h3>
            </div>
            <div className="space-overview-materials">
              {materials.slice(0, 5).map((m) => {
                const v = m.versions.at(-1)!;
                return (
                  <button
                    key={m.id}
                    onClick={() =>
                      onRead({
                        spaceId: c.id,
                        kind: "material",
                        materialId: m.id,
                        version: v.version,
                      })
                    }
                  >
                    <span className="space-file-symbol">
                      <Icon name="file" size={18} />
                    </span>
                    <span className="space-overview-material-copy">
                      <strong>
                        {v.title}
                        <small>v{v.version}</small>
                      </strong>
                      <span>
                        {m.author} ·{" "}
                        {v.provider === "claude" ? "Claude Code" : "Codex"} ·{" "}
                        {tr`${v.turnCount || 0} 轮对话`}
                      </span>
                    </span>
                    <Icon name="arrow" size={14} />
                  </button>
                );
              })}
              {!materials.length && (
                <p className="muted">{t("还没有发布材料。")}</p>
              )}
            </div>
          </section>
          <section>
            <div className="space-section-heading">
              <h3>{t("最近动态")}</h3>
              <button className="text-link" onClick={onRequests}>
                {t("查看进展")}
                <Icon name="arrow" size={13} />
              </button>
            </div>
            <div className="space-overview-events">
              {view?.requests.slice(0, 4).map((r) => (
                <button key={r.id} onClick={() => onRequest(r.id)}>
                  <SpaceAvatar name={r.actor?.name || t("成员")} />
                  <span>
                    <strong>{r.instruction}</strong>
                    <small>
                      {r.actor?.name} · {agentRequestLabel(r)}
                    </small>
                  </span>
                  <time>{r.updatedAt ? relativeTime(r.updatedAt) : ""}</time>
                </button>
              ))}
              {!view?.requests.length && (
                <p className="muted">{t("还没有协作请求。")}</p>
              )}
            </div>
          </section>
        </div>
        <aside className="space-overview-side">
          {pending && (
            <section className="space-attention">
              <div className="space-kicker">
                <Icon name="clock" size={14} />
                {t("需要你关注")}
              </div>
              <h3>{agentRequestLabel(pending)}</h3>
              <p>{pending.instruction}</p>
              <button
                className="text-link"
                onClick={() => onRequest(pending.id)}
              >
                {t("查看请求")}
                <Icon name="arrow" size={13} />
              </button>
            </section>
          )}
          <section>
            <div className="space-section-heading">
              <h3>{t("成员与会话")}</h3>
              <span>{members.length}</span>
            </div>
            <div className="space-member-list">
              {members.map((m) => {
                const targets =
                  view?.targets.filter(
                    (target) => !target.removed && target.memberId === m.id,
                  ) || [];
                return (
                  <div className="space-member" key={m.id}>
                    <SpaceAvatar name={m.name} />
                    <div>
                      <strong>{m.name}</strong>
                      <small>
                        {m.online ? t("在线") : t("离线")}
                        {m.id === "owner" ? ` · ${t("发起者")}` : ""}
                      </small>
                      {targets.map((target) => (
                        <small key={target.id}>
                          <Icon name="terminal" size={12} />
                          {target.name} ·{" "}
                          {target.available ? t("可以接收") : t("暂不可接收")}
                        </small>
                      ))}
                    </div>
                  </div>
                );
              })}
            </div>
            <p className="space-overview-mode">
              {c.sessions?.length || c.hasExecution
                ? t("共同执行")
                : t("分享与审阅")}
            </p>
          </section>
        </aside>
      </div>
      {management && (
        <details className="space-management">
          <summary>
            {t("空间管理")}
            <Icon name="chevron" size={14} />
          </summary>
          <div>{management}</div>
        </details>
      )}
    </>
  );
}
