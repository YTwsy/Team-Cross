import { useEffect, useRef, useState } from "react";
import { api } from "../api";
import type { SessionFollow, SessionSnapshot } from "../types";

const stateLabels: Record<SessionFollow["state"], string> = {
  active: "跟随中",
  retrying: "等待重试",
  stopped: "已停止",
};

function displayTime(value?: string): string {
  if (!value) return "尚无记录";
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? "时间不可用" : date.toLocaleString();
}

export function SessionFollowPanel({
  threadId,
  snapshot,
  follows,
  canManage,
  availableSnapshotIds,
  onChanged,
  onSelectSnapshot,
  hasNativeLiveShare = false,
}: {
  threadId: string;
  snapshot: SessionSnapshot;
  follows: SessionFollow[];
  canManage: boolean;
  availableSnapshotIds: string[];
  onChanged: (follow: SessionFollow) => void;
  onSelectSnapshot: (snapshotId: string) => void;
  hasNativeLiveShare?: boolean;
}) {
  const [confirmed, setConfirmed] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const mounted = useRef(true);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);

  const related = follows.filter(
    (follow) =>
      follow.threadId === threadId &&
      follow.source.provider === snapshot.source.provider &&
      follow.source.sessionId === snapshot.source.sessionId &&
      follow.source.identityKind === snapshot.source.identityKind,
  );
  const alreadyFollowing = related.some((follow) => follow.state !== "stopped");
  const supported = snapshot.capabilities?.follow === true;
  const unavailableReason =
    snapshot.capabilities?.reason ||
    "尚未验证此来源和版本的只读 Follow 能力。读取历史成功不代表可以跟随。";

  async function update(action: () => Promise<SessionFollow>) {
    if (busy || !canManage) return;
    setBusy(true);
    setError("");
    try {
      const value = await action();
      if (!mounted.current) return;
      onChanged(value);
      setConfirmed(false);
    } catch (reason) {
      if (mounted.current)
        setError(reason instanceof Error ? reason.message : "Follow 操作失败");
    } finally {
      if (mounted.current) setBusy(false);
    }
  }

  return (
    <section
      className="session-follow-panel surface"
      aria-label="原生 Session 只读 Follow"
    >
      <header>
        <strong>只读 Follow</strong>
        <span>原生 Session</span>
      </header>
      <p>
        只观察后续输出并追加不可变快照，不创建 Run、不发送
        prompt，也不取得原生输入权。
        {hasNativeLiveShare
          ? "静态 Share 范围不变；原生实时分享仅按另行确认的类别交付。"
          : "新快照不会扩大当前 Share 的内容范围。"}
      </p>
      {related.map((follow) => (
        <article
          className={`follow-state-card ${follow.state}`}
          key={follow.id}
          aria-label={`Follow ${follow.id}`}
        >
          <div className="follow-state-heading">
            <strong>
              {stateLabels[follow.state]} · {follow.state}
            </strong>
            <code>epoch {follow.epoch}</code>
          </div>
          <dl>
            <div>
              <dt>当前快照</dt>
              <dd>
                <code>{follow.currentSnapshotId || "尚未捕获"}</code>
              </dd>
            </div>
            <div>
              <dt>起点快照</dt>
              <dd>
                <code>{follow.sourceSnapshotId}</code>
              </dd>
            </div>
            <div>
              <dt>最近读取</dt>
              <dd>{displayTime(follow.lastPolledAt)}</dd>
            </div>
            <div>
              <dt>状态更新</dt>
              <dd>{displayTime(follow.updatedAt)}</dd>
            </div>
          </dl>
          {follow.reason ? (
            <p className="follow-reason">{follow.reason}</p>
          ) : null}
          {follow.gaps.length ? (
            <div className="follow-gaps" role="note">
              <strong>数据缺口 / 不完整历史</strong>
              <ul>
                {follow.gaps.map((gap, index) => (
                  <li key={`${index}-${gap}`}>{gap}</li>
                ))}
              </ul>
              <p>不能据此认定已经读取全部后续输出。</p>
            </div>
          ) : null}
          <div className="follow-actions">
            {follow.currentSnapshotId &&
            follow.currentSnapshotId !== snapshot.id ? (
              <button
                className="button ghost compact"
                disabled={
                  !availableSnapshotIds.includes(follow.currentSnapshotId)
                }
                onClick={() => onSelectSnapshot(follow.currentSnapshotId)}
                type="button"
              >
                查看跟随快照
              </button>
            ) : null}
            {canManage && follow.state !== "stopped" ? (
              <button
                className="button secondary compact"
                disabled={busy}
                onClick={() =>
                  void update(() => api.stopSessionFollow(threadId, follow.id))
                }
                type="button"
              >
                停止 Follow
              </button>
            ) : null}
          </div>
          {follow.state === "stopped" ? (
            <p>已停止后续读取；已捕获的不可变快照与批注继续保留。</p>
          ) : null}
        </article>
      ))}
      {canManage ? (
        <div className="follow-start">
          {!supported ? (
            <p className="follow-unavailable" role="note">
              当前来源不能启用 Follow：{unavailableReason}
            </p>
          ) : null}
          {!alreadyFollowing ? (
            <>
              <label className="inline-checkbox">
                <input
                  type="checkbox"
                  checked={confirmed}
                  disabled={busy || !supported}
                  onChange={(event) => setConfirmed(event.target.checked)}
                />
                确认只读跟随此 Session，不扩大现有分享范围
              </label>
              <button
                className="button secondary compact"
                disabled={busy || !supported || !confirmed}
                onClick={() => {
                  if (supported && confirmed)
                    void update(() =>
                      api.startSessionFollow(threadId, snapshot.id),
                    );
                }}
                type="button"
              >
                {busy ? "处理中…" : "开始只读 Follow"}
              </button>
            </>
          ) : (
            <p>此原生 Session 已在跟随，不会重复启动。</p>
          )}
        </div>
      ) : (
        <p className="follow-readonly">
          只有主机 Owner 可以管理原生 Follow。当前 Share
          仍只提供已获授权的内容，不会订阅未来原生输出。
        </p>
      )}
      {error ? (
        <p className="form-error" role="alert">
          {error}
        </p>
      ) : null}
    </section>
  );
}
