import { useEffect, useRef, useState } from "react";
import type {
  Evidence,
  Participant,
  SessionSnapshot,
  Share,
  ShareOptions,
  SessionFollow,
  NativeLiveStatus,
} from "../types";
import { CopyIcon, ShareIcon } from "./Icons";
import {
  NativeLiveShare,
  emptyNativeLiveDraft,
  nativeLiveScope,
} from "./NativeLiveShare";

export function SharePanel({
  share,
  participants,
  canManage,
  onCreate,
  onRevoke,
  onRevokeControl,
  snapshots = [],
  evidence = [],
  canIncludeCode = false,
  canControl = false,
  sessionFollows = [],
  nativeLive,
}: {
  share?: Share;
  participants: Participant[];
  canManage: boolean;
  onCreate: (options: ShareOptions) => Promise<void>;
  onRevoke: () => Promise<void>;
  onRevokeControl: () => Promise<void>;
  snapshots?: SessionSnapshot[];
  evidence?: Evidence[];
  canIncludeCode?: boolean;
  canControl?: boolean;
  sessionFollows?: SessionFollow[];
  nativeLive?: NativeLiveStatus;
}) {
  const [busy, setBusy] = useState(false);
  const [copied, setCopied] = useState(false);
  const [degraded, setDegraded] = useState(false);
  const [snapshotId, setSnapshotId] = useState("");
  const [entryIds, setEntryIds] = useState<string[]>([]);
  const [evidenceIds, setEvidenceIds] = useState<string[]>([]);
  const [includeCode, setIncludeCode] = useState(false);
  const [includeEvents, setIncludeEvents] = useState(false);
  const [allowControl, setAllowControl] = useState(false);
  const [error, setError] = useState("");
  const [liveDraft, setLiveDraft] = useState(emptyNativeLiveDraft);
  const liveScope = nativeLiveScope(liveDraft, sessionFollows, snapshots);
  const active = useRef(true);
  useEffect(() => {
    active.current = true;
    return () => {
      active.current = false;
    };
  }, []);
  const liveFollow = sessionFollows.find(
    (item) => item.id === liveDraft.followId,
  );
  const livePreviewId = liveDraft.preview?.snapshot.id;
  const livePreviewEpoch = liveDraft.preview?.epoch;
  const livePreviewAvailable = snapshots.some(
    (item) => item.id === livePreviewId,
  );
  useEffect(() => {
    if (
      livePreviewId &&
      (!livePreviewAvailable ||
        !liveFollow?.lastPolledAt ||
        liveFollow.currentSnapshotId !== livePreviewId ||
        liveFollow?.state !== "active" ||
        liveFollow.epoch !== livePreviewEpoch)
    ) {
      setLiveDraft((current) => ({
        ...current,
        confirmCurrent: false,
        confirmFuture: false,
      }));
    }
  }, [
    livePreviewId,
    livePreviewEpoch,
    liveFollow?.currentSnapshotId,
    liveFollow?.state,
    liveFollow?.epoch,
    liveFollow?.lastPolledAt,
    livePreviewAvailable,
  ]);
  const snapshot = snapshots.find((item) => item.id === snapshotId);
  const selectedEntries =
    snapshot?.entries.filter((entry) => entryIds.includes(entry.id)) ?? [];
  const selectedEvidence = evidence.filter(
    (item) => item.kind !== "agent_transcript" && evidenceIds.includes(item.id),
  );
  const hasSelection =
    selectedEntries.length > 0 ||
    selectedEvidence.length > 0 ||
    includeCode ||
    includeEvents ||
    !!liveScope;
  const canCreate = hasSelection && (!liveDraft.enabled || !!liveScope);
  const remoteController = participants.find(
    (participant) => participant.role === "controller",
  );

  useEffect(() => {
    if (!copied) return;
    const timer = window.setTimeout(() => setCopied(false), 1400);
    return () => window.clearTimeout(timer);
  }, [copied]);

  async function run(action: () => Promise<void>) {
    if (busy) return;
    setBusy(true);
    setError("");
    try {
      await action();
    } catch (reason) {
      if (active.current)
        setError(reason instanceof Error ? reason.message : "分享操作失败");
    } finally {
      if (active.current) setBusy(false);
    }
  }

  async function copyInvite() {
    if (!share) return;
    try {
      await navigator.clipboard.writeText(share.invite);
      if (active.current) setCopied(true);
    } catch {
      if (active.current) setError("剪贴板不可用，请从主机重新复制邀请。");
    }
  }

  return (
    <section className="side-section">
      <div className="side-section-heading">
        <span>
          <ShareIcon size={16} /> Share
        </span>
        {share ? (
          <span className={`share-state ${share.status}`}>{share.status}</span>
        ) : null}
      </div>
      {share ? (
        <div className="share-card">
          {canManage && share.invite ? (
            <>
              <label>One-time invitation</label>
              <button
                className="invite-value"
                onClick={copyInvite}
                type="button"
              >
                <code>{share.invite.slice(0, 30)}…</code>
                <span>{copied ? "Copied" : <CopyIcon size={15} />}</span>
              </button>
            </>
          ) : null}
          <div className="transport-path">
            {["LAN", "Tailnet", "Tailcat"].map((name, index) => (
              <span
                className={
                  share.transports
                    .map((item) => item.toLowerCase())
                    .includes(name.toLowerCase())
                    ? "ready"
                    : "off"
                }
                key={name}
              >
                {index ? <i>→</i> : null}
                <b>{name}</b>
              </span>
            ))}
          </div>
          <small>
            Expires{" "}
            {new Date(share.expiresAt).toLocaleTimeString([], {
              hour: "2-digit",
              minute: "2-digit",
            })}
          </small>
          <p className="side-muted">
            {share.allowControl
              ? "已授权请求 Managed Agent 控制"
              : "仅查看与批注"}{" "}
            · 静态分享内容不会自动扩大到新的 Session 快照。
          </p>
          {share.scope?.nativeLive ? (
            <div className="native-live-status">
              <strong>
                已单独授权原生实时范围 · {nativeLive?.state || "状态待更新"}
              </strong>
              <p>
                Follow <code>{share.scope.nativeLive.followId}</code> ·
                当前窗口及后续窗口
              </p>
              <p>
                类别：{share.scope.nativeLive.entryKinds.join("、")}（不是脱敏）
              </p>
              <p>
                确认起点{" "}
                <code>{share.scope.nativeLive.expectedSnapshotId}</code>
              </p>
              {nativeLive ? (
                <p>
                  最新窗口 <code>{nativeLive.latestSnapshotId}</code> ·{" "}
                  {nativeLive.reason || "只按已授权类别交付，不授权控制"}
                </p>
              ) : null}
            </div>
          ) : null}
          {canManage ? (
            <button
              className="text-button danger"
              disabled={busy}
              onClick={() => run(onRevoke)}
              type="button"
            >
              Revoke share
            </button>
          ) : null}
        </div>
      ) : canManage ? (
        <div className="share-empty">
          <p>先确认分享范围。默认仅查看与批注，不授权 Agent 控制。</p>
          <label className="review-title">
            Session 快照
            <select
              aria-label="分享的 Session 快照"
              value={snapshotId}
              onChange={(event) => {
                setSnapshotId(event.target.value);
                setEntryIds([]);
              }}
            >
              <option value="">不分享 Session 历史</option>
              {snapshots.map((item) => (
                <option key={item.id} value={item.id}>
                  {item.source.title || item.source.provider} ·{" "}
                  {new Date(item.capturedAt).toLocaleString()}
                </option>
              ))}
            </select>
          </label>
          {snapshot ? (
            <div className="share-entry-selection">
              <div>
                <button
                  className="text-button"
                  onClick={() =>
                    setEntryIds(snapshot.entries.map((entry) => entry.id))
                  }
                  type="button"
                >
                  选择全部记录
                </button>
                <button
                  className="text-button"
                  onClick={() => setEntryIds([])}
                  type="button"
                >
                  清除
                </button>
              </div>
              {snapshot.entries.map((entry, index) => (
                <label className="inline-checkbox" key={entry.id}>
                  <input
                    type="checkbox"
                    checked={entryIds.includes(entry.id)}
                    onChange={(event) =>
                      setEntryIds((current) =>
                        event.target.checked
                          ? [...current, entry.id]
                          : current.filter((id) => id !== entry.id),
                      )
                    }
                  />
                  <span>
                    {index + 1}. {entry.role || entry.kind} ·{" "}
                    {entry.text.slice(0, 80)}
                  </span>
                </label>
              ))}
            </div>
          ) : null}
          {evidence
            .filter((item) => item.kind !== "agent_transcript")
            .map((item) => (
              <label className="inline-checkbox" key={item.id}>
                <input
                  checked={evidenceIds.includes(item.id)}
                  onChange={(event) =>
                    setEvidenceIds((current) =>
                      event.target.checked
                        ? [...current, item.id]
                        : current.filter((id) => id !== item.id),
                    )
                  }
                  type="checkbox"
                />
                Evidence · {item.name}
              </label>
            ))}
          {canIncludeCode ? (
            <label className="inline-checkbox">
              <input
                checked={includeCode}
                onChange={(event) => {
                  setIncludeCode(event.target.checked);
                  setAllowControl(false);
                }}
                type="checkbox"
              />
              包含当前已封存代码快照与 patch
            </label>
          ) : null}
          {canControl ? (
            <>
              <label className="inline-checkbox">
                <input
                  checked={includeEvents}
                  onChange={(event) => {
                    setIncludeEvents(event.target.checked);
                    setAllowControl(false);
                  }}
                  type="checkbox"
                />
                实时分享 Managed Agent 事件（含未来输出）
              </label>
              <label className="inline-checkbox">
                <input
                  checked={allowControl}
                  disabled={!includeCode || !includeEvents}
                  onChange={(event) => setAllowControl(event.target.checked)}
                  type="checkbox"
                />
                允许请求 Managed Agent 控制
              </label>
            </>
          ) : null}
          <NativeLiveShare
            draft={liveDraft}
            onChange={setLiveDraft}
            follows={sessionFollows}
            snapshots={snapshots}
            busy={busy}
          />
          <details className="share-preview">
            <summary>
              预览分享内容 · {selectedEntries.length} 条记录 /{" "}
              {selectedEvidence.length} 份 Evidence
            </summary>
            <p>
              静态范围中未选中的记录、其他 Session
              快照和原始历史文件不会分享。原生实时范围在上方单独确认。选择的记录与附件按完整内容分享，请检查其中的敏感信息。
            </p>
            {selectedEntries.map((entry) => (
              <pre key={entry.id}>{entry.text}</pre>
            ))}
            {selectedEvidence.map((item) => (
              <p key={item.id}>
                {item.name} · {item.size} bytes
              </p>
            ))}
            <p>
              {includeCode ? "包含封存代码" : "不包含代码"} ·{" "}
              {includeEvents ? "包含实时 Managed 输出" : "不包含事件流"} ·{" "}
              {allowControl ? "允许控制" : "仅查看与批注"}
            </p>
          </details>
          <label className="inline-checkbox">
            <input
              checked={degraded}
              onChange={(event) => setDegraded(event.target.checked)}
              type="checkbox"
            />
            Allow LAN/Tailnet-only if Tailcat fails
          </label>
          <button
            className="button secondary full"
            disabled={busy || !canCreate}
            onClick={() => {
              if (!canCreate || busy) return;
              void run(() =>
                onCreate({
                  allowDegraded: degraded,
                  allowControl,
                  scope: {
                    ...(snapshot && selectedEntries.length
                      ? {
                          snapshotId: snapshot.id,
                          entryIds: selectedEntries.map((entry) => entry.id),
                        }
                      : {}),
                    evidenceIds: selectedEvidence.map((item) => item.id),
                    includeCode,
                    includeEvents,
                    ...(liveScope ? { nativeLive: liveScope } : {}),
                  },
                }),
              );
            }}
            type="button"
          >
            {busy ? "Warming Tailcat…" : "Create share"}
          </button>
        </div>
      ) : (
        <p className="side-muted">This thread is not currently shared.</p>
      )}
      {error ? (
        <p className="form-error" role="alert">
          {error}
        </p>
      ) : null}

      <div className="participant-list">
        <div className="participant-list-heading">
          Participants · {participants.length}
          {canManage && remoteController ? (
            <button
              className="text-button danger"
              disabled={busy}
              onClick={() => run(onRevokeControl)}
              type="button"
            >
              Reclaim control
            </button>
          ) : null}
        </div>
        {participants.map((participant) => (
          <div className="participant" key={participant.id}>
            <span className="participant-avatar">
              {participant.name.slice(0, 1).toUpperCase()}
            </span>
            <span>
              <strong>{participant.name}</strong>
              <small>{participant.transport}</small>
            </span>
            <em>{participant.role}</em>
          </div>
        ))}
        {participants.length === 0 ? (
          <p className="side-muted">Only you are here.</p>
        ) : null}
      </div>
    </section>
  );
}
