import { useEffect, useRef, useState } from "react";
import { api } from "../api";
import type { SessionPreview, StoredSession } from "../types";
import { SessionTranscript } from "./SessionReview";

export function SessionPicker({
  onImport,
  submitLabel = "创建只读审阅 Thread",
  includeTitle = true,
}: {
  onImport: (
    provider: string,
    sessionId: string,
    title?: string,
  ) => Promise<void>;
  submitLabel?: string;
  includeTitle?: boolean;
}) {
  const [sessions, setSessions] = useState<StoredSession[]>([]);
  const [preview, setPreview] = useState<SessionPreview>();
  const [title, setTitle] = useState("");
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const selectionRequest = useRef(0);
  useEffect(() => {
    let active = true;
    void api
      .storedSessions()
      .then((items) => {
        if (active) setSessions(items);
      })
      .catch((reason: unknown) => {
        if (active)
          setError(
            reason instanceof Error
              ? reason.message
              : "无法读取 Native Session 列表",
          );
      })
      .finally(() => {
        if (active) setLoading(false);
      });
    return () => {
      active = false;
      selectionRequest.current += 1;
    };
  }, []);

  async function select(session: StoredSession) {
    const requestId = ++selectionRequest.current;
    setBusy(true);
    setPreview(undefined);
    setError("");
    try {
      const value = await api.sessionPreview(session.provider, session.id);
      if (requestId !== selectionRequest.current) return;
      setPreview(value);
      setTitle(session.title || value.source.title || "Session 审阅");
    } catch (reason) {
      if (requestId === selectionRequest.current)
        setError(reason instanceof Error ? reason.message : "读取预览失败");
    } finally {
      if (requestId === selectionRequest.current) setBusy(false);
    }
  }

  async function submit() {
    if (!preview || busy) return;
    setBusy(true);
    setError("");
    try {
      await onImport(
        preview.source.provider,
        preview.source.sessionId,
        title.trim(),
      );
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "导入失败");
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="session-picker">
      <p className="review-boundary">
        只读导入不会创建 Run、启动 Agent
        或修改原仓库。预览与保存之间，原生历史可能继续变化；保存后的快照保持不可变。
      </p>
      <div className="session-picker-grid">
        <div className="session-list">
          {loading ? <p role="status">正在读取已有 Session…</p> : null}
          {sessions.map((session) => (
            <button
              aria-pressed={
                preview?.source.sessionId === session.id &&
                preview.source.provider === session.provider
              }
              key={`${session.provider}-${session.id}`}
              onClick={() => void select(session)}
              type="button"
              disabled={busy}
            >
              <span className={`provider-logo ${session.provider}`}>
                {session.provider.slice(0, 1).toUpperCase()}
              </span>
              <span>
                <strong>{session.title || session.id}</strong>
                <small>
                  {session.provider} · {session.cwd || "原目录不可用"}
                </small>
              </span>
            </button>
          ))}
          {!loading && !sessions.length && !error ? (
            <p className="empty-inline">没有发现可读取的 Native Session。</p>
          ) : null}
        </div>
        <div className="session-preview">
          {preview ? (
            <SessionTranscript preview={preview} />
          ) : (
            <p className="empty-inline">
              {busy ? "正在读取历史快照…" : "选择一个 Session，先预览再导入。"}
            </p>
          )}
        </div>
      </div>
      {error ? (
        <p className="form-error" role="alert">
          {error}
        </p>
      ) : null}
      {preview && includeTitle ? (
        <label className="review-title">
          Thread 标题
          <input
            value={title}
            onChange={(event) => setTitle(event.target.value)}
            disabled={busy}
          />
        </label>
      ) : null}
      <button
        className="button primary"
        disabled={!preview || busy}
        onClick={() => void submit()}
        type="button"
      >
        {busy ? "读取中…" : submitLabel}
      </button>
    </div>
  );
}
