import { jobLabel } from "../attention";
import { formatDate, serviceText, t } from "../i18n";
import type { Shadow } from "../shadows";
import { MarkdownText } from "./MarkdownText";
import { ErrorBox, Loading } from "./ui";

type Segment = NonNullable<Shadow["segments"]>[number];

function ContextUsage({ segment }: { segment: Segment }) {
  const known = segment.usageValid && segment.contextWindow > 0;
  return (
    <div className="shadow-context-usage">
      {known ? (
        <>
          <progress
            aria-label={t("主会话上下文用量")}
            value={Math.min(segment.inputTokens, segment.contextWindow)}
            max={segment.contextWindow}
          />
          <span>
            {t("上下文")} · {segment.inputTokens.toLocaleString()} /{" "}
            {segment.contextWindow.toLocaleString()} tokens
          </span>
        </>
      ) : (
        <span>{t("等待原生端报告当前上下文用量。")}</span>
      )}
      <span>
        {segment.compactions} {t("次原生压缩")}
      </span>
    </div>
  );
}

export function ShadowSessions({ view }: { view: Shadow }) {
  // The list projection omits these collections while details are loading.
  if (!view.segments || !view.jobs) return <Loading />;
  const current = view.segments?.find((s) => s.id === view.current);
  const history = (view.segments || [])
    .filter((s) => s.id !== view.current)
    .sort((a, b) => a.number - b.number);
  // Jobs are retained across completion and S handoffs. Number all work jobs
  // by creation order (including those awaiting a thread), never by status.
  // A queued job reserves its place but is not presented as an existing W.
  const jobs = [...(view.jobs || [])].sort(
    (a, b) =>
      (a.createdAt || "").localeCompare(b.createdAt || "") ||
      a.id.localeCompare(b.id),
  );
  const work = jobs.filter((j) => j.kind !== "coordination");
  const coordination = jobs.filter((j) => j.kind === "coordination");
  const mainState = !view.online
    ? t("离线 · 最后记录")
    : view.busy
      ? t("正在处理输入")
      : !view.enabled
        ? t("已暂停接收")
        : t("空闲 · 可继续聊天");

  return (
    <div className="shadow-sessions">
      <p className="shadow-session-intro">
        {t("S 负责对话，W 独立执行工作。W 运行时，仍可向主会话发消息。")}
      </p>
      {!view.online && (
        <p className="muted">
          {t("分身离线，以下为最后同步的会话与任务记录。")}
        </p>
      )}
      <section aria-label={t("主会话")} className="shadow-session-section">
        <article className="shadow-session-card shadow-session-main">
          <header>
            <span className="shadow-session-code">
              {current ? `S${current.number}` : "S"}
            </span>
            <div className="shadow-session-title">
              <strong>{t("主会话")}</strong>
              <span>{current ? t("当前使用") : t("尚未创建主会话")}</span>
            </div>
            {current && (
              <span className="shadow-session-state">{mainState}</span>
            )}
          </header>
          <p className="shadow-session-description">
            {current
              ? t("接收聊天、@ 和直接回复，处理短问题，派发较长工作。")
              : t("首条聊天消息或需要直接处理的成员动态到达时，才创建 S1。")}
          </p>
          {current && (
            <>
              <div
                className="shadow-content-strip"
                aria-label={t("主会话中的内容")}
              >
                <span>{t("消息与作者来源")}</span>
                <span>{t("近期对话上下文")}</span>
                <span>{t("记忆与任务索引")}</span>
              </div>
              <ContextUsage segment={current} />
              <details className="shadow-session-reference">
                <summary>{t("创建时间与原生会话")}</summary>
                {current.createdAt && <p>{formatDate(current.createdAt)}</p>}
                <code>{current.id}</code>
              </details>
            </>
          )}
        </article>
        {!!history.length && (
          <details className="shadow-session-history">
            <summary>
              {t("以前的主会话段")} · {history.length}
            </summary>
            {history.map((s) => (
              <div key={s.id} className="shadow-history-row">
                <strong>S{s.number}</strong>
                <span>{s.closedAt ? t("已交接") : t("非当前段")}</span>
                <code>{s.id}</code>
              </div>
            ))}
            <p className="muted">
              {t("换段后继续同一共享聊天；工作任务和已保存记忆保留。")}
            </p>
          </details>
        )}
      </section>

      <section aria-label={t("工作会话")} className="shadow-session-section">
        <div className="shadow-session-section-heading">
          <h3>{t("工作会话")}</h3>
          <span>{t("按需创建 · 独立执行")}</span>
        </div>
        {!work.length && (
          <div className="shadow-session-empty">
            <span className="shadow-session-code">W</span>
            <div>
              <strong>{t("尚未创建工作会话")}</strong>
              <p>
                {t(
                  "明确交给分身的较长工作，轮到执行时才创建 W。普通聊天不创建 W。",
                )}
              </p>
            </div>
          </div>
        )}
        {work.map((job, i) => (
          <article
            key={job.id}
            className="shadow-session-card shadow-session-work"
            aria-label={`${job.threadId ? `W${i + 1}` : t("待建会话")} · ${job.title}`}
          >
            <header>
              <span className="shadow-session-code">
                {job.threadId ? `W${i + 1}` : "—"}
              </span>
              <div className="shadow-session-title">
                <strong>{job.title}</strong>
                <span>
                  {job.threadId ? t("独立工作会话") : t("尚未创建工作会话")} · v
                  {job.version}
                </span>
              </div>
              <span
                className={`shadow-session-state shadow-state-${job.state}`}
              >
                {jobLabel(job.state)}
              </span>
            </header>
            {job.threadId ? (
              <div
                className="shadow-content-strip"
                aria-label={t("工作会话中的内容")}
              >
                <span>{t("目标与完成条件")}</span>
                <span>{t("工具执行上下文")}</span>
                <span>{t("进度与结果")}</span>
              </div>
            ) : (
              <p className="shadow-session-description">
                {t("任务已登记；取得执行位置后才会建立原生会话。")}
              </p>
            )}
            {job.checkpoint?.summary && (
              <p className="shadow-session-description">
                {job.checkpoint.summary}
              </p>
            )}
            {job.checkpoint?.nextStep && (
              <p className="shadow-session-description">
                {t("下一步")} · {job.checkpoint.nextStep}
              </p>
            )}
            {job.result && (
              <details className="shadow-session-result">
                <summary>{t("查看结果")}</summary>
                <MarkdownText source={job.result} />
              </details>
            )}
            {job.error && <ErrorBox message={serviceText(job.error)} />}
            <details className="shadow-session-reference">
              <summary>{t("任务详情与原生会话")}</summary>
              {!!job.completionCriteria?.length && (
                <ul>
                  {job.completionCriteria.map((c, n) => (
                    <li key={n}>{c}</li>
                  ))}
                </ul>
              )}
              {job.createdAt && (
                <p>
                  {t("任务创建")} · {formatDate(job.createdAt)}
                </p>
              )}
              <p>
                {t("任务 ID")} · <code>{job.id}</code>
              </p>
              <p>
                {t("原生会话")} · <code>{job.threadId || t("尚未创建")}</code>
              </p>
            </details>
          </article>
        ))}
        {!!coordination.length && (
          <details className="shadow-session-history">
            <summary>
              {t("协作事项")} · {coordination.length}
            </summary>
            {coordination.map((job) => (
              <div key={job.id} className="shadow-fact">
                <strong>{job.title}</strong>
                <span>{jobLabel(job.state)}</span>
                {job.result && <MarkdownText source={job.result} />}
              </div>
            ))}
          </details>
        )}
      </section>

      <section
        className="shadow-storage-strip"
        aria-label={t("文件中保留的内容")}
      >
        <strong>{t("文件 · 跨会话保留")}</strong>
        <p>
          {t(
            "聊天原文、任务状态与结果、共享记忆、换段交接笔记。会话按需读取，不会把所有文件一直塞进上下文。",
          )}
        </p>
      </section>
    </div>
  );
}
