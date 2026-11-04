import { useEffect, useState } from "react";
import { api, errorText } from "../api";
import { useActivityResource } from "../activity";
import { t, tr } from "../i18n";
import type { MemberAttention, WaitingItem, WaitingPage } from "../waiting";
import { ErrorBox, Loading } from "./ui";

function waitingState(state: string) {
  if (state === "answer_received") return t("答复已到，待分身核对");
  if (state === "delivery_unknown") return t("发送状态待核对");
  if (state === "held_for_owner") return t("本人已接回，尚未发送");
  if (state === "unavailable") return t("来源暂不可用");
  return t("等待答复");
}
export function MemberWaiting({
  spaceId,
  title,
  direction,
  onOpen,
}: {
  spaceId: string;
  title?: string;
  direction: string;
  onOpen: (item: WaitingItem) => void;
}) {
  const path = `collaborations/${spaceId}/attention?direction=${direction}`;
  const data = useActivityResource<MemberAttention>(path);
  const [extra, setExtra] = useState<WaitingPage>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  useEffect(() => {
    setExtra(undefined);
    setError("");
  }, [path, data.data?.waiting.revision]);
  const page = data.data?.waiting;
  const seen = new Set<string>();
  const items = [...(page?.items || []), ...(extra?.items || [])].filter(
    (item) => !seen.has(item.id) && !!seen.add(item.id),
  );
  const cursor = extra ? extra.nextCursor : page?.nextCursor;
  async function more() {
    if (!cursor || busy) return;
    setBusy(true);
    try {
      const next = await api<MemberAttention>(
        `${path}&cursor=${encodeURIComponent(cursor)}`,
      );
      setExtra((old) => ({
        ...next.waiting,
        items: [...(old?.items || []), ...next.waiting.items],
      }));
    } catch (e) {
      setError(errorText(e));
      setExtra(undefined);
      data.reload();
    } finally {
      setBusy(false);
    }
  }
  return (
    <section className="member-waiting" aria-label={title || t("等待事项")}>
      {title && <h3>{title}</h3>}
      <ErrorBox message={error || data.error} />
      {data.loading && !data.data && <Loading />}
      {page && !items.length && <p className="muted">{t("暂无等待事项")}</p>}
      {items.map((item) => (
        <article className="waiting-item" key={item.id}>
          <div className="waiting-item-heading">
            <strong>{item.question}</strong>
            <span className="muted small-text">{waitingState(item.state)}</span>
          </div>
          {item.tasks.map((task) => (
            <p
              className="small-text"
              key={`${task.shadowId}:${task.jobId}:${task.dependencyId}`}
            >
              <strong>{task.title}</strong>
              {task.state === "paused" && ` · ${t("已暂停")}`}
              <br />
              {task.condition}
            </p>
          ))}
          {item.totalTasks > item.tasks.length && (
            <p className="muted small-text">{tr`共关联 ${item.totalTasks} 项等待条件`}</p>
          )}
          {item.error && <p className="notice small-text">{item.error}</p>}
          {(item.source.annotationId || item.source.requestId) && (
            <button className="text-link" onClick={() => onOpen(item)}>
              {t("查看原问题")}
            </button>
          )}
        </article>
      ))}
      {cursor && (
        <button
          className="text-link"
          disabled={busy}
          onClick={() => void more()}
        >
          {t("加载更多")}
        </button>
      )}
    </section>
  );
}
