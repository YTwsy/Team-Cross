import { useId } from "react";
import { jobLabel } from "../attention";
import { serviceText, t } from "../i18n";
import type { Shadow } from "../shadows";
import { ErrorBox, Icon, Loading } from "./ui";

export type ShadowInspectorTab = "tasks" | "memory" | "runtime";

export function ShadowInspector({
  view,
  selfId,
  tab,
  onTab,
  onDetails,
}: {
  view: Shadow;
  selfId: string;
  tab: ShadowInspectorTab;
  onTab: (tab: ShadowInspectorTab) => void;
  onDetails: () => void;
}) {
  const uid = useId();
  const tabs = [
    { id: "tasks" as const, label: t("任务") },
    { id: "memory" as const, label: t("记忆") },
    { id: "runtime" as const, label: t("运行") },
  ];
  const jobs = view.jobs?.filter((j) => j.kind !== "coordination") || [];
  const active = jobs.filter(
    (j) => !["completed", "failed", "cancelled"].includes(j.state),
  );
  const recent = jobs
    .filter((j) => !active.includes(j))
    .slice(-3)
    .reverse();
  const current = view.segments?.find((s) => s.id === view.current);
  return (
    <div className="shadow-inspector-content">
      <div className="shadow-inspector-heading">
        <strong>{t("分身详情")}</strong>
        <button
          className="icon-button"
          aria-label={t("展开详细会话与任务")}
          title={t("展开详细会话与任务")}
          onClick={onDetails}
        >
          <Icon name="expand" size={15} />
        </button>
      </div>
      <div
        className="shadow-inspector-tabs"
        role="tablist"
        aria-label={t("分身详情分类")}
      >
        {tabs.map((item, i) => (
          <button
            key={item.id}
            id={`${uid}-${item.id}`}
            role="tab"
            aria-selected={tab === item.id}
            aria-controls={`${uid}-${item.id}-panel`}
            tabIndex={tab === item.id ? 0 : -1}
            onClick={() => onTab(item.id)}
            onKeyDown={(e) => {
              if (!["ArrowLeft", "ArrowRight", "Home", "End"].includes(e.key))
                return;
              e.preventDefault();
              const next =
                e.key === "Home"
                  ? 0
                  : e.key === "End"
                    ? tabs.length - 1
                    : (i + (e.key === "ArrowLeft" ? -1 : 1) + tabs.length) %
                      tabs.length;
              onTab(tabs[next]!.id);
              document.getElementById(`${uid}-${tabs[next]!.id}`)?.focus();
            }}
          >
            {item.label}
            {item.id === "tasks" && active.length > 0 && (
              <span>{active.length}</span>
            )}
          </button>
        ))}
      </div>
      <div className="shadow-inspector-scroll">
        <section
          id={`${uid}-tasks-panel`}
          role="tabpanel"
          aria-labelledby={`${uid}-tasks`}
          hidden={tab !== "tasks"}
        >
          {!view.jobs ? (
            <Loading />
          ) : (
            <>
              <p className="shadow-inspector-caption">
                {t("进行中")} · {active.length}
              </p>
              {active.map((job) => (
                <button
                  className="shadow-task-preview"
                  key={job.id}
                  onClick={onDetails}
                >
                  <span className="shadow-task-state">
                    <Icon name="clock" size={12} />
                    {jobLabel(job.state)}
                  </span>
                  <strong>{job.title}</strong>
                  {job.checkpoint?.summary && <p>{job.checkpoint.summary}</p>}
                  <span className="shadow-task-version">
                    v{job.version}
                    <Icon name="chevron" size={13} />
                  </span>
                </button>
              ))}
              {!active.length && (
                <p className="shadow-inspector-empty">
                  {t("没有进行中的后台任务")}
                </p>
              )}
              <p className="shadow-inspector-note">
                {t("任务在后台继续，你可以随时接着聊。")}
              </p>
              {!!recent.length && (
                <>
                  <p className="shadow-inspector-caption">{t("最近结束")}</p>
                  {recent.map((job) => (
                    <button
                      className="shadow-task-history"
                      key={job.id}
                      onClick={onDetails}
                    >
                      <Icon
                        name={job.state === "completed" ? "check" : "clock"}
                        size={15}
                      />
                      <span>
                        <strong>{job.title}</strong>
                        <small>{jobLabel(job.state)}</small>
                      </span>
                      <Icon name="chevron" size={13} />
                    </button>
                  ))}
                </>
              )}
              <button
                className="text-button shadow-inspector-more"
                onClick={onDetails}
              >
                {t("查看全部会话与任务")}
                <Icon name="arrow" size={14} />
              </button>
            </>
          )}
        </section>
        <section
          id={`${uid}-memory-panel`}
          role="tabpanel"
          aria-labelledby={`${uid}-memory`}
          hidden={tab !== "memory"}
        >
          {!view.memories ? (
            <Loading />
          ) : !view.memories.length ? (
            <p className="shadow-inspector-empty">{t("还没有记忆")}</p>
          ) : (
            view.memories.map((memory) => (
              <article
                className={`shadow-memory-preview ${memory.state === "candidate" ? "shadow-memory-candidate" : ""}`}
                key={memory.id}
              >
                <header>
                  <Icon
                    name={memory.scope === "space" ? "people" : "pin"}
                    size={13}
                  />
                  {memory.scope === "space"
                    ? t("空间")
                    : memory.subjectId === selfId
                      ? t("我的偏好")
                      : t("成员偏好")}
                </header>
                <p>
                  {memory.state === "forgotten" ? t("已忘掉") : memory.text}
                </p>
                <footer>
                  {memory.state === "candidate"
                    ? t("待确认")
                    : memory.state === "forgotten"
                      ? t("已失效")
                      : t("有效")}{" "}
                  · v{memory.revision}
                </footer>
              </article>
            ))
          )}
          <p className="shadow-inspector-note">
            {t("记住、纠正或忘掉信息，直接告诉分身。")}
          </p>
        </section>
        <section
          id={`${uid}-runtime-panel`}
          role="tabpanel"
          aria-labelledby={`${uid}-runtime`}
          hidden={tab !== "runtime"}
        >
          <dl className="shadow-runtime-summary">
            <div>
              <dt>{t("运行归属")}</dt>
              <dd>{view.owner.name} · Mac</dd>
            </div>
            <div>
              <dt>{t("连接状态")}</dt>
              <dd>
                {!view.online
                  ? t("离线")
                  : view.enabled
                    ? t("在线")
                    : t("已暂停接收")}
              </dd>
            </div>
            <div>
              <dt>{t("主会话")}</dt>
              <dd>{current ? `S${current.number}` : t("尚未创建主会话")}</dd>
            </div>
            <div>
              <dt>{t("运行时确认")}</dt>
              <dd>Codex · {view.model || t("等待原生会话确认模型")}</dd>
            </div>
            {view.modelProvider && (
              <div>
                <dt>Provider</dt>
                <dd>{view.modelProvider}</dd>
              </div>
            )}
            {view.reasoningEffort && (
              <div>
                <dt>{t("推理强度")}</dt>
                <dd>{view.reasoningEffort}</dd>
              </div>
            )}
          </dl>
          <ErrorBox message={serviceText(view.error)} />
          <p className="shadow-inspector-note">
            {t("整个空间共享一段对话，直接提问或接着聊。")}
          </p>
          <button
            className="text-button shadow-inspector-more"
            onClick={onDetails}
          >
            {t("会话与任务")}
            <Icon name="arrow" size={14} />
          </button>
        </section>
      </div>
    </div>
  );
}
