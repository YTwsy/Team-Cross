import { useRef, useState } from "react";
import { api, errorText } from "../api";
import {
  activityChanged,
  useActivityResource,
  type ActivitySource,
} from "../activity";
import { t } from "../i18n";
import type { QuestionHandling as Handling } from "../waiting";
import { ErrorBox } from "./ui";

export function QuestionHandling({
  source,
  question,
}: {
  source: ActivitySource;
  question?: string;
}) {
  const path = `collaborations/${source.spaceId}/attention/handling`;
  const query = new URLSearchParams({
    annotationId: source.annotationId || "",
    replyId: source.replyId || "",
    executionId: source.executionId || "",
  });
  const data = useActivityResource<Handling>(`${path}?${query}`);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const pending = useRef<{
    requestId: string;
    handler: string;
    baseRevision: number;
  }>(undefined);
  async function handoff() {
    const current = data.data;
    if (busy || !current?.canHandoff) return;
    const handler = current.handler === "owner" ? "shadow" : "owner";
    if (
      !pending.current ||
      pending.current.handler !== handler ||
      pending.current.baseRevision !== current.revision
    )
      pending.current = {
        requestId: crypto.randomUUID(),
        handler,
        baseRevision: current.revision,
      };
    setBusy(true);
    setError("");
    try {
      await api(path, { ...pending.current, source });
      pending.current = undefined;
      activityChanged();
      data.reload();
    } catch (e) {
      setError(errorText(e));
      data.reload();
    } finally {
      setBusy(false);
    }
  }
  return (
    <div className="question-handling">
      {data.data?.canHandoff && (
        <>
          <div>
            {question && (
              <p className="handling-source-question">
                {Array.from(question).slice(0, 240).join("")}
                {Array.from(question).length > 240 ? "…" : ""}
              </p>
            )}
            <strong>
              {data.data.handler === "owner"
                ? t("这条问题由你回复")
                : t("这条问题由分身处理")}
            </strong>
            <p className="muted small-text">
              {data.data.handler === "owner"
                ? t("调查记录会保留，分身继续处理其他问题。")
                : t("你可以随时接回这条问题的回复。")}
            </p>
          </div>
          <button
            className="button small"
            disabled={busy}
            onClick={() => void handoff()}
          >
            {data.data.handler === "owner" ? t("交回分身") : t("这条我来")}
          </button>
        </>
      )}
      <ErrorBox message={error || data.error} />
    </div>
  );
}
