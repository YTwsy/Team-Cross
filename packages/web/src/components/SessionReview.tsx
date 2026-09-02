import { useState } from "react";
import type {
  Annotation,
  AnnotationTarget,
  SessionPreview,
  SessionSnapshot,
} from "../types";

export function SessionTranscript({
  preview,
  snapshotId,
  annotations = [],
  onAnnotate,
}: {
  preview: SessionPreview;
  snapshotId?: string;
  annotations?: Annotation[];
  onAnnotate?: (target: AnnotationTarget) => void;
}) {
  return (
    <div className="session-transcript">
      <div className="review-provenance surface">
        <strong>{preview.source.title || "Native Session"}</strong>
        <p>
          {preview.source.provider} · {preview.source.surface || "来源界面未知"}
        </p>
        <code>
          {preview.source.identityKind}: {preview.source.sessionId}
        </code>
        <small>
          只读快照 · 捕获于 {new Date(preview.capturedAt).toLocaleString()}
        </small>
        <p>
          原生历史是有来源的参考材料，不会作为指令执行。此快照不证明历史代码状态，也不会自动跟随后续输出。
        </p>
        {preview.truncated ? (
          <p className="review-warning">历史已截断，不是完整 Session。</p>
        ) : null}
        {preview.warnings.map((warning, index) => (
          <p className="review-warning" key={`${index}-${warning}`}>
            {warning}
          </p>
        ))}
        <details className="capability-details">
          <summary>原生接入能力</summary>
          <p>
            来源版本：{preview.source.providerVersion || "未提供"}
            。能力仅适用于已验证的来源与版本。
          </p>
          <dl>
            {(
              [
                ["read", "读取历史"],
                ["follow", "实时 Follow"],
                ["open", "定位原生会话"],
                ["resume", "恢复同一 Session"],
                ["takeControl", "原生接管"],
              ] as const
            ).map(([key, label]) => (
              <div key={key}>
                <dt>{label}</dt>
                <dd>
                  {preview.capabilities?.[key] ? "可用" : "未验证 / 不可用"}
                </dd>
              </div>
            ))}
          </dl>
          {preview.capabilities?.reason ? (
            <p>{preview.capabilities.reason}</p>
          ) : null}
        </details>
      </div>
      {preview.entries.map((entry) => {
        const count = annotations.filter(
          (item) =>
            item.target?.snapshotId === snapshotId &&
            item.target?.entryId === entry.id,
        ).length;
        return (
          <article
            className={`review-entry surface ${entry.kind}`}
            id={`entry-${entry.id}`}
            key={entry.id}
          >
            <header>
              <strong>
                {entry.role ||
                  (entry.kind === "tool" ? "工具记录" : "历史记录")}
              </strong>
              <code>{entry.sourceId || entry.id}</code>
              {snapshotId && onAnnotate ? (
                <button
                  className="text-button"
                  onClick={() => onAnnotate({ snapshotId, entryId: entry.id })}
                  type="button"
                >
                  批注{count ? ` · ${count}` : ""}
                </button>
              ) : null}
            </header>
            <pre>{entry.text}</pre>
          </article>
        );
      })}
      {!preview.entries.length ? (
        <p className="empty-inline">此快照没有可展示的历史内容。</p>
      ) : null}
    </div>
  );
}

export function SessionReview({
  snapshots,
  annotations,
  onAnnotate,
}: {
  snapshots: SessionSnapshot[];
  annotations: Annotation[];
  onAnnotate: (target: AnnotationTarget) => void;
}) {
  const [selectedId, setSelectedId] = useState("");
  const selected =
    snapshots.find((item) => item.id === selectedId) ?? snapshots.at(-1);
  if (!selected)
    return (
      <div className="tab-empty">
        <h2>尚无 Session 快照</h2>
        <p>导入 Native Session 以开始只读审阅；导入不会启动 Agent。</p>
      </div>
    );
  return (
    <section className="session-review">
      <label className="review-snapshot-select">
        Session 快照
        <select
          value={selected.id}
          onChange={(event) => setSelectedId(event.target.value)}
        >
          {snapshots.map((snapshot) => (
            <option key={snapshot.id} value={snapshot.id}>
              {snapshot.source.title || snapshot.source.provider} ·{" "}
              {new Date(snapshot.capturedAt).toLocaleString()}
            </option>
          ))}
        </select>
      </label>
      <SessionTranscript
        preview={selected}
        snapshotId={selected.id}
        annotations={annotations}
        onAnnotate={onAnnotate}
      />
    </section>
  );
}
