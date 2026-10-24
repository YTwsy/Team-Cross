import { useState } from "react";
import {
  useActivityResource,
  type ActivitySource as Source,
} from "../activity";
import { jobLabel, processingSourceItem } from "../attention";
import { t, tr } from "../i18n";
import {
  questionLabel,
  type QuestionDetail,
  type QuestionPage,
} from "../questions";
import { ActivitySource } from "./MemberActivity";
import { ErrorBox, Loading, Modal } from "./ui";

export function ShadowQuestions({
  spaceId,
  shadowId,
}: {
  spaceId: string;
  shadowId: string;
}) {
  const [history, setHistory] = useState(false);
  const [cursor, setCursor] = useState("");
  const [selected, select] = useState("");
  const base = `collaborations/${spaceId}/attention`;
  const data = useActivityResource<QuestionPage>(
    `${base}/questions?shadowId=${encodeURIComponent(shadowId)}&history=${history}&cursor=${encodeURIComponent(cursor)}`,
  );
  const page = data.error ? undefined : data.data;
  return (
    <section className="shadow-question-list" aria-label={t("提问与接续")}>
      <h3>{t("提问与接续")}</h3>
      <label className="small-text">
        <input
          type="checkbox"
          checked={history}
          onChange={(e) => {
            setHistory(e.target.checked);
            setCursor("");
          }}
        />
        {t("包含已结束任务")}
      </label>
      <ErrorBox message={data.error} />
      {data.loading && !page && <Loading />}
      {page && !page.items.length && (
        <p className="muted">{t("暂无相关提问")}</p>
      )}
      {page?.items.map((q) => (
        <div className="shadow-fact" key={q.id}>
          <button
            className="text-link question-summary"
            onClick={() => select(q.id)}
          >
            {q.text}
          </button>
          <span>{questionLabel(q.state)}</span>
          <span>{tr`关联 ${q.totalTasks} 项任务，其中 ${q.activeTasks} 项未结束`}</span>
        </div>
      ))}
      {page?.nextCursor && (
        <button
          className="text-link"
          onClick={() => setCursor(page.nextCursor!)}
        >
          {t("下一页提问")}
        </button>
      )}
      {(cursor || data.error) && (
        <button
          className="text-link"
          onClick={() => {
            setCursor("");
            data.reload();
          }}
        >
          {t("返回最新")}
        </button>
      )}
      {selected && (
        <QuestionDetails
          key={selected}
          base={base}
          shadowId={shadowId}
          id={selected}
          onClose={() => select("")}
        />
      )}
    </section>
  );
}

function QuestionDetails({
  base,
  shadowId,
  id,
  onClose,
}: {
  base: string;
  shadowId: string;
  id: string;
  onClose: () => void;
}) {
  const [cursor, setCursor] = useState("");
  const [source, setSource] = useState<Source>();
  const data = useActivityResource<QuestionDetail>(
    `${base}/question?shadowId=${encodeURIComponent(shadowId)}&id=${encodeURIComponent(id)}&cursor=${encodeURIComponent(cursor)}`,
  );
  const detail = data.error ? undefined : data.data;
  return (
    <Modal title={t("提问与接续")} onClose={onClose} wide>
      <ErrorBox message={data.error} />
      {data.loading && !detail && <Loading />}
      {detail && (
        <div className="question-detail">
          <p>{detail.text}</p>
          <p className="muted">{questionLabel(detail.state)}</p>
          {detail.error && <ErrorBox message={detail.error} />}
          {detail.source && (
            <button
              className="text-link"
              onClick={() => setSource(detail.source)}
            >
              {t("查看已发问题")}
            </button>
          )}
          {detail.respondsTo.length > 0 && (
            <>
              <h3>{t("关联的原问题")}</h3>
              {detail.respondsTo.map((r) => (
                <div className="shadow-fact" key={JSON.stringify(r.source)}>
                  {r.question && <p>{r.question}</p>}
                  <button
                    className="text-link"
                    onClick={() => setSource(r.source)}
                  >
                    {t("打开原问题")}
                  </button>
                </div>
              ))}
            </>
          )}
          <h3>{t("各任务的等待条件")}</h3>
          <p className="muted small-text">
            {t("收到同一条答复后，各任务仍按自己的条件继续。")}
          </p>
          {detail.tasks.map((task) => (
            <div
              className="shadow-fact"
              key={`${task.id}:${task.dependencyId}`}
            >
              <strong>{task.title}</strong>
              <span>
                {jobLabel(task.state)} · v{task.version}
              </span>
              <p>{task.condition}</p>
              <p className="small-text">
                {task.dependencyState === "satisfied"
                  ? t("此条件已满足")
                  : task.unavailable
                    ? t("来源暂不可用")
                    : task.answerReceived
                      ? t("答复已到，待分身核对")
                      : t("此条件尚未满足")}
              </p>
              {task.nextStep && (
                <p className="small-text">{tr`接下来：${task.nextStep}`}</p>
              )}
            </div>
          ))}
          {detail.nextCursor && (
            <button
              className="text-link"
              onClick={() => setCursor(detail.nextCursor!)}
            >
              {t("更多关联条件")}
            </button>
          )}
        </div>
      )}
      {(cursor || data.error) && (
        <button
          className="text-link"
          onClick={() => {
            setCursor("");
            data.reload();
          }}
        >
          {t("返回首批条件")}
        </button>
      )}
      {source && (
        <Modal
          title={t("查看讨论来源")}
          onClose={() => setSource(undefined)}
          wide
        >
          <ActivitySource
            item={processingSourceItem(source)}
            onClose={() => setSource(undefined)}
          />
        </Modal>
      )}
    </Modal>
  );
}
