import { useEffect, useState } from "react";
import { api } from "../api";
import type {
  Annotation,
  AnnotationTarget,
  SessionPreview,
  SessionSnapshot,
  SessionFollow,
  NativeLiveStatus,
} from "../types";
import { SessionFollowPanel } from "./SessionFollowPanel";
import { NativeSessionOpen } from "./NativeSessionOpen";

export function SessionTranscript({
  preview,
  snapshotId,
  annotations = [],
  onAnnotate,
  referenceEntryId,
}: {
  preview: SessionPreview;
  snapshotId?: string;
  annotations?: Annotation[];
  onAnnotate?: (target: AnnotationTarget) => void;
  referenceEntryId?: string;
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
          原生历史是有来源的参考材料，不会作为指令执行。此快照不证明历史代码状态，也不会被后续输出改写；只读
          Follow 需要单独启用。
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
            className={`review-entry surface ${entry.kind}${entry.id === referenceEntryId ? " referenced-entry" : ""}`}
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

interface SessionReviewProps {
  threadId?: string;
  snapshots: SessionSnapshot[];
  annotations: Annotation[];
  onAnnotate: (target: AnnotationTarget) => void;
  follows?: SessionFollow[];
  canManage?: boolean;
  onFollowChanged?: (follow: SessionFollow) => void;
  nativeLive?: NativeLiveStatus;
  reference?: SessionReviewReference;
}

export interface SessionReviewReference {
  threadId: string;
  snapshotId: string;
  entryId?: string;
  requestId: number;
}

export function SessionReview(props: SessionReviewProps) {
  const threadId = props.threadId ?? props.snapshots.at(-1)?.threadId ?? "";
  const reference =
    props.reference?.threadId === threadId ? props.reference : undefined;
  return (
    <SessionReviewContent
      key={`${threadId}:${reference?.requestId ?? "review"}`}
      {...props}
      threadId={threadId}
      reference={reference}
    />
  );
}

function rememberSnapshot(
  current: SessionSnapshot[],
  value: SessionSnapshot,
): SessionSnapshot[] {
  if (current.find((item) => item.id === value.id) === value) return current;
  return [...current.filter((item) => item.id !== value.id), value].slice(-3);
}

function SessionReviewContent({
  threadId = "",
  snapshots,
  annotations,
  onAnnotate,
  follows = [],
  canManage = false,
  onFollowChanged = () => {},
  nativeLive,
  reference,
}: SessionReviewProps) {
  const [selectedId, setSelectedId] = useState(
    () => reference?.snapshotId ?? snapshots.at(-1)?.id ?? "",
  );
  const [cache, setCache] = useState<SessionSnapshot[]>(() =>
    snapshots.filter((item) => item.id === selectedId).slice(-1),
  );
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [attempt, setAttempt] = useState(0);
  const selected =
    snapshots.find((item) => item.id === selectedId) ??
    cache.find((item) => item.id === selectedId);
  const options = [
    ...snapshots,
    ...cache.filter(
      (item) => !snapshots.some((snapshot) => snapshot.id === item.id),
    ),
  ];
  useEffect(() => {
    if (!selectedId && snapshots.length) setSelectedId(snapshots.at(-1)!.id);
  }, [selectedId, snapshots]);
  useEffect(() => {
    if (selected) setCache((current) => rememberSnapshot(current, selected));
  }, [selected]);
  useEffect(() => {
    if (!selectedId || selected) {
      setLoading(false);
      setError("");
      return;
    }
    let active = true;
    setLoading(true);
    setError("");
    void api
      .sessionSnapshot(threadId, selectedId)
      .then((value) => {
        if (!active) return;
        if (value.id !== selectedId || value.threadId !== threadId)
          throw new Error("服务端返回的快照身份不匹配；未展示替代内容。");
        setCache((current) => rememberSnapshot(current, value));
      })
      .catch((reason: unknown) => {
        if (active)
          setError(
            reason instanceof Error ? reason.message : "无法读取引用快照",
          );
      })
      .finally(() => {
        if (active) setLoading(false);
      });
    return () => {
      active = false;
    };
  }, [threadId, selectedId, selected, attempt]);
  function selectSnapshot(id: string) {
    const available = options.find((item) => item.id === id);
    if (available) setCache((current) => rememberSnapshot(current, available));
    setSelectedId(id);
    setError("");
  }
  if (!selectedId)
    return (
      <div className="tab-empty">
        <h2>尚无 Session 快照</h2>
        <p>导入 Native Session 以开始只读审阅；导入不会启动 Agent。</p>
      </div>
    );
  return (
    <section className="session-review">
      {nativeLive ? (
        <section
          className="native-live-status surface"
          aria-label="原生实时分享状态"
        >
          <strong>原生实时分享 · {nativeLive.state}</strong>
          <p>
            已授权类别：{nativeLive.entryKinds.join("、")} ·
            只读，不授予原生控制。
          </p>
          <p>
            最新窗口 <code>{nativeLive.latestSnapshotId}</code>
            ；当前阅读不会自动跳转。
          </p>
          {nativeLive.reason ? (
            <p className="review-warning">{nativeLive.reason}</p>
          ) : null}
          {nativeLive.latestSnapshotId &&
          nativeLive.latestSnapshotId !== selectedId ? (
            <button
              type="button"
              className="button ghost compact"
              onClick={() => selectSnapshot(nativeLive.latestSnapshotId)}
            >
              查看最新已分享窗口
            </button>
          ) : null}
        </section>
      ) : null}
      <label className="review-snapshot-select">
        Session 快照
        <select
          value={selectedId}
          onChange={(event) => selectSnapshot(event.target.value)}
        >
          {!options.some((item) => item.id === selectedId) ? (
            <option value={selectedId}>引用快照 · {selectedId}</option>
          ) : null}
          {options.map((snapshot) => (
            <option key={snapshot.id} value={snapshot.id}>
              {snapshot.source.title || snapshot.source.provider} ·{" "}
              {new Date(snapshot.capturedAt).toLocaleString()}
            </option>
          ))}
        </select>
      </label>
      {!selected ? (
        <div
          className="snapshot-unavailable surface"
          role={error ? "alert" : "status"}
        >
          <strong>
            精确快照 <code>{selectedId}</code>
          </strong>
          <p>
            {loading
              ? "正在读取获授权的引用快照…"
              : error || "等待读取引用快照"}
          </p>
          <p>没有用最新窗口替换此引用。快照可能未获授权或已不可访问。</p>
          {!loading ? (
            <button
              type="button"
              className="button ghost compact"
              onClick={() => setAttempt((value) => value + 1)}
            >
              重试读取此快照
            </button>
          ) : null}
        </div>
      ) : (
        <>
          {reference?.snapshotId === selected.id ? (
            <p className="reference-location">
              正在查看引用快照 <code>{selected.id}</code>
              {reference.entryId ? ` / 记录 ${reference.entryId}` : ""}
            </p>
          ) : null}
          {reference?.snapshotId === selected.id &&
          reference.entryId &&
          !selected.entries.some((entry) => entry.id === reference.entryId) ? (
            <p className="review-warning" role="alert">
              此投影中没有获授权的引用记录 {reference.entryId}
              ；不会用其他记录代替。
            </p>
          ) : null}
          {canManage || !nativeLive ? (
            <SessionFollowPanel
              key={`${threadId}:${selected.id}`}
              threadId={threadId}
              snapshot={selected}
              follows={follows}
              canManage={canManage}
              availableSnapshotIds={options.map((snapshot) => snapshot.id)}
              onChanged={onFollowChanged}
              onSelectSnapshot={selectSnapshot}
              hasNativeLiveShare={!!nativeLive}
            />
          ) : null}
          <NativeSessionOpen
            key={`open:${threadId}:${selected.id}`}
            threadId={threadId}
            snapshot={selected}
            canManage={canManage}
          />
          <SessionTranscript
            preview={selected}
            snapshotId={selected.id}
            annotations={annotations}
            onAnnotate={onAnnotate}
            referenceEntryId={
              reference?.snapshotId === selected.id
                ? reference.entryId
                : undefined
            }
          />
        </>
      )}
    </section>
  );
}
