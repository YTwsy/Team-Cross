import { useState } from "react";
import { useActivityResource, type ActivitySource } from "../activity";
import {
  type ActivityProcessing as Processing,
  jobLabel,
  processingHold,
  processingLabel,
} from "../attention";
import { serviceText, t, tr } from "../i18n";
import { ErrorBox, Loading } from "./ui";

export function ActivityProcessing({
  info,
  event,
  onOpen,
}: {
  info?: Processing;
  event?: { spaceId: string; id: string };
  onOpen: (source: ActivitySource) => void;
}) {
  const [open, setOpen] = useState(false);
  const data = useActivityResource<Processing>(
    open && event
      ? `collaborations/${event.spaceId}/attention/event?eventId=${encodeURIComponent(event.id)}`
      : null,
  );
  if (!info) return null;
  const detailInfo = data.data || info;
  const jobs = detailInfo.jobs || [],
    actions = detailInfo.actions || [];
  const label = `${info.shadowName ? `${info.shadowName} · ` : ""}${processingLabel(info.state)}`;
  const hold = processingHold(info.hold);
  const detail =
    info.details || jobs.length > 0 || actions.length > 0 || !!info.error;
  function toolLabel(tool: string) {
    return (
      (
        {
          reply_to_annotation: t("回复讨论"),
          add_annotation: t("发起讨论"),
          send_space_request: t("向会话发起协作"),
          send_space_shadow: t("向分身发送消息"),
          send_space_note: t("向会话留言"),
          update_space_brief: t("更新空间简报"),
        } as Record<string, string>
      )[tool] || t("协作动作")
    );
  }
  function actionLabel(state: string) {
    return state === "saved"
      ? t("已取得回执")
      : state === "held_for_owner"
        ? t("已交给本人回复")
        : t("送达待核对");
  }
  function receiptLabel(state?: string) {
    if (state === "conflict") return t("简报已变化，等待合并");
    if (state === "sendFailed" || state === "failed")
      return t("该次回执未确认成功");
    if (state === "sending" || state === "pending")
      return t("该次回执仍在等待送达");
    if (state === "delivered") return t("该次回执确认对方已接收");
    return "";
  }
  return (
    <div
      className={`activity-processing ${info.state === "error" ? "needs-check" : ""}`}
    >
      {detail ? (
        <details onToggle={(e) => setOpen(e.currentTarget.open)}>
          <summary>
            {label}
            {hold && ` · ${hold}`}
          </summary>
          {data.loading && open && event && !data.data && <Loading />}
          <ErrorBox message={data.error} />
          {detailInfo.error && (
            <p className="notice">
              {serviceText(detailInfo.error, "处理需要核对")}
            </p>
          )}
          {jobs.length > 0 && (
            <ul>
              {jobs.map((j) => (
                <li key={`${j.shadowId}:${j.id}`}>
                  {j.title} · {jobLabel(j.state)}
                  {j.waiting === "needs_owner" && ` · ${t("待你回答")}`}
                </li>
              ))}
            </ul>
          )}
          {detailInfo.totalJobs > jobs.length && (!event || data.data) && (
            <p className="muted small-text">{tr`共 ${detailInfo.totalJobs} 项关联任务，更多问题可在等待列表查看。`}</p>
          )}
          {actions.length > 0 && (
            <ul>
              {actions.map((a) => (
                <li key={`${a.tool}:${a.id}`}>
                  {toolLabel(a.tool)} · {actionLabel(a.state)}
                  {receiptLabel(a.receiptState) &&
                    ` · ${receiptLabel(a.receiptState)}`}
                  {a.sources?.[0] && (
                    <button
                      className="text-link"
                      onClick={() => onOpen(a.sources![0]!)}
                    >
                      {t("查看讨论来源")}
                    </button>
                  )}
                </li>
              ))}
            </ul>
          )}
          {detailInfo.totalActions > actions.length &&
            (!event || data.data) && (
              <p className="muted small-text">{tr`共 ${detailInfo.totalActions} 项协作动作，显示最近的回执。`}</p>
            )}
          {actions.some((a) => a.tool.startsWith("send_space_")) && (
            <p className="muted small-text">
              {t("取得发送回执不代表对方已完成工作。")}
            </p>
          )}
        </details>
      ) : (
        <span>
          {label}
          {hold && ` · ${hold}`}
        </span>
      )}
    </div>
  );
}
