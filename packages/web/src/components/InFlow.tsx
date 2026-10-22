import { useRefreshState, useRefreshGuard } from "../refreshState";
import { useState } from "react";
import { api, APIError, errorText } from "../api";
import { t, formatDate } from "../i18n";
import type { SpaceNote } from "../workbench";
import { ErrorBox, Modal } from "./ui";

export function InFlowControl({
  spaceId,
  targetId,
  executionId,
  enabled,
  canConfigure,
  disabled,
  onChanged,
}: {
  spaceId: string;
  targetId?: string;
  executionId?: string;
  enabled: boolean;
  canConfigure: boolean;
  disabled: boolean;
  onChanged: () => void;
}) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  async function toggle() {
    if (busy) return;
    setBusy(true);
    setError("");
    try {
      await api(`collaborations/${spaceId}/workbench/inflow-config`, {
        targetId,
        executionId,
        enabled: !enabled,
      });
      onChanged();
    } catch (e) {
      setError(errorText(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <div className="inflow-control">
      <label>
        <input
          type="checkbox"
          checked={enabled}
          disabled={disabled || busy || !canConfigure}
          onChange={toggle}
        />
        {t("顺流感知")}
      </label>
      <span className="muted small-text">
        {executionId
          ? t("下次交互附带简报和本会话的新评论、回复。")
          : t("下次交互附带简报、定向留言和本人请求结果。")}
      </span>
      {error && <ErrorBox message={error} />}
    </div>
  );
}

export function LeaveSpaceNote({
  spaceId,
  targetId,
  name,
  enabled,
  disabled,
  notes,
  onChanged,
}: {
  spaceId: string;
  targetId: string;
  name: string;
  enabled: boolean;
  disabled: boolean;
  notes: SpaceNote[];
  onChanged: () => void;
}) {
  const refreshKey = "LeaveSpaceNote:v1:" + (spaceId + ":" + targetId);
  const [open, setOpen] = useRefreshState(refreshKey + ":open", false);
  const [text, setText] = useRefreshState(refreshKey + ":text", "");
  const [requestId, setRequestId] = useRefreshState(
    refreshKey + ":requestId",
    "",
  );
  const [canRetry, setCanRetry] = useRefreshState(
    refreshKey + ":canRetry",
    false,
  );
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  async function submit() {
    if (busy) return;
    const id = requestId || crypto.randomUUID();
    setRequestId(id);
    setBusy(true);
    setError("");
    try {
      if (requestId && !canRetry)
        await api(`collaborations/${spaceId}/workbench/inflow-note-read`, {
          requestId: id,
        });
      else
        await api(`collaborations/${spaceId}/workbench/inflow-note`, {
          requestId: id,
          targetId,
          instruction: text.trim(),
        });
      setRequestId("");
      setCanRetry(false);
      setText("");
      onChanged();
    } catch (e) {
      setError(errorText(e));
      if (e instanceof APIError && e.code === "inflow_note_rejected") {
        setRequestId("");
        setCanRetry(false);
      } else {
        setCanRetry(
          e instanceof APIError && e.code === "inflow_note_not_found",
        );
      }
    } finally {
      setBusy(false);
    }
  }
  useRefreshGuard(refreshKey, !!text || !!requestId);
  return (
    <>
      <button className="button small" onClick={() => setOpen(true)}>
        {t("留言")}
      </button>
      {open && (
        <Modal title={t("给会话留言")} onClose={() => setOpen(false)}>
          <p>
            <strong>{name}</strong>
          </p>
          <p className="muted small-text">
            {t("留言对空间成员可见，只在目标会话下次交互时附带，不启动模型。")}
          </p>
          {!enabled && <p>{t("接收会话尚未开启顺流感知。")}</p>}
          <textarea
            aria-label={t("留言正文")}
            maxLength={300}
            rows={3}
            value={text}
            disabled={disabled || busy || !!requestId || !enabled}
            onChange={(e) => setText(e.target.value)}
          />
          {error && <ErrorBox message={error} />}
          {!!requestId && (
            <p className="muted small-text">
              {canRetry
                ? t("尚未找到留言，可用原标识重试保存相同内容。")
                : t("发送结果不明，保留原标识核对，不会自动重发。")}
            </p>
          )}
          <button
            className="button primary"
            onClick={submit}
            disabled={
              disabled || busy || (!requestId && (!enabled || !text.trim()))
            }
          >
            {canRetry
              ? t("重试保存留言")
              : requestId
                ? t("核对留言状态")
                : t("保存留言")}
          </button>
          <div className="inflow-notes">
            {notes
              .filter((n) => n.targetId === targetId)
              .map((note) => (
                <article key={note.id}>
                  <p>{note.text}</p>
                  <p className="muted small-text">
                    {note.actor.name} · {formatDate(note.createdAt)} ·{" "}
                    {note.deliveredAt ? t("已投递") : t("待投递")}
                  </p>
                </article>
              ))}
          </div>
          <p className="muted small-text">
            {t("已投递表示提醒已写出，不代表已读或完成。")}
          </p>
        </Modal>
      )}
    </>
  );
}
