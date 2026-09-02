import { useState } from "react";
import { api } from "../api";
import { SessionPicker } from "./SessionPicker";

export function SessionStart({
  onCreated,
  onCapture,
  onCancel,
}: {
  onCreated: (id: string) => void;
  onCapture: () => void;
  onCancel: () => void;
}) {
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  return (
    <section className="page">
      <header className="page-header">
        <div>
          <p className="eyebrow">Session-first review</p>
          <h1>从一个 Session 开始协作</h1>
          <p>分享、审阅和反馈，不改变你熟悉的 Agent 工作流。</p>
        </div>
        <button className="button ghost" onClick={onCancel} type="button">
          返回
        </button>
      </header>
      <SessionPicker
        onImport={async (provider, sessionId, title) => {
          const value = await api.createThreadFromSession(
            provider,
            sessionId,
            title,
          );
          onCreated(value.id);
        }}
      />
      <div className="review-alternatives surface">
        <div>
          <strong>从代码状态开始</strong>
          <p>捕获当前 Git 基线与改动，建立隔离 worktree。</p>
          <button
            className="button secondary"
            onClick={onCapture}
            type="button"
          >
            Capture Git workspace
          </button>
        </div>
        <div>
          <strong>接收离线交接包</strong>
          <p>
            创建独立 Fork Thread；导入不会启动 Agent，也不会复制发送方凭据。
          </p>
          <label className="bundle-upload">
            {busy ? "校验并导入…" : "选择交接包"}
            <input
              aria-label="选择交接包"
              type="file"
              accept=".json,.tcx,application/json"
              disabled={busy}
              onChange={async (event) => {
                const file = event.target.files?.[0];
                if (!file) return;
                if (file.size > 96 * 1024 * 1024) {
                  setError("交接包超过 96 MiB，请使用更小的导出范围。");
                  return;
                }
                setBusy(true);
                setError("");
                try {
                  const value = await api.importBundle(
                    JSON.parse(await file.text()),
                  );
                  onCreated(value.id);
                } catch (reason) {
                  setError(
                    reason instanceof Error ? reason.message : "交接包导入失败",
                  );
                } finally {
                  setBusy(false);
                }
              }}
            />
          </label>
        </div>
      </div>
      {error ? (
        <p className="form-error" role="alert">
          {error}
        </p>
      ) : null}
    </section>
  );
}
