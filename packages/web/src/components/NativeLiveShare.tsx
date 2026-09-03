import type {
  NativeLiveScope,
  SessionEntry,
  SessionFollow,
  SessionSnapshot,
} from "../types";

export interface NativeLiveDraft {
  enabled: boolean;
  followId: string;
  entryKinds: SessionEntry["kind"][];
  preview?: { snapshot: SessionSnapshot; epoch: number };
  confirmCurrent: boolean;
  confirmFuture: boolean;
}

export function emptyNativeLiveDraft(): NativeLiveDraft {
  return {
    enabled: false,
    followId: "",
    entryKinds: [],
    confirmCurrent: false,
    confirmFuture: false,
  };
}

function eligible(
  follow: SessionFollow | undefined,
  snapshots: SessionSnapshot[],
): boolean {
  return (
    !!follow &&
    follow.state === "active" &&
    !!follow.lastPolledAt &&
    snapshots.some((snapshot) => snapshot.id === follow.currentSnapshotId)
  );
}

export function nativeLiveScope(
  draft: NativeLiveDraft,
  follows: SessionFollow[],
  snapshots: SessionSnapshot[],
): NativeLiveScope | undefined {
  const follow = follows.find((item) => item.id === draft.followId);
  if (
    !draft.enabled ||
    !draft.preview ||
    !draft.entryKinds.length ||
    !draft.confirmCurrent ||
    !draft.confirmFuture ||
    !eligible(follow, snapshots) ||
    follow?.currentSnapshotId !== draft.preview.snapshot.id ||
    follow.epoch !== draft.preview.epoch
  )
    return undefined;
  return {
    followId: follow.id,
    expectedSnapshotId: draft.preview.snapshot.id,
    entryKinds: [...draft.entryKinds],
    confirmCurrentAndFuture: true,
  };
}

const kinds: Array<[SessionEntry["kind"], string]> = [
  ["message", "消息：用户输入与 Agent 回复"],
  ["tool", "工具：参数、结果、代码与路径"],
  ["notice", "通知：状态、缺口与其他历史记录"],
];

export function NativeLiveShare({
  draft,
  onChange,
  follows,
  snapshots,
  busy,
}: {
  draft: NativeLiveDraft;
  onChange: (value: NativeLiveDraft) => void;
  follows: SessionFollow[];
  snapshots: SessionSnapshot[];
  busy: boolean;
}) {
  const follow = follows.find((item) => item.id === draft.followId);
  const current = snapshots.find(
    (item) => item.id === follow?.currentSnapshotId,
  );
  const available = eligible(follow, snapshots);
  const previewFresh =
    available &&
    !!draft.preview &&
    current?.id === draft.preview.snapshot.id &&
    follow?.epoch === draft.preview.epoch;
  const entries =
    draft.preview?.snapshot.entries.filter((entry) =>
      draft.entryKinds.includes(entry.kind),
    ) ?? [];
  function reselect(update: Partial<NativeLiveDraft>) {
    onChange({
      ...draft,
      ...update,
      preview: undefined,
      confirmCurrent: false,
      confirmFuture: false,
    });
  }
  return (
    <section className="native-live-selection" aria-label="原生实时分享范围">
      <label className="inline-checkbox">
        <input
          type="checkbox"
          checked={draft.enabled}
          disabled={
            busy ||
            (!draft.enabled &&
              !follows.some((item) => eligible(item, snapshots)))
          }
          onChange={(event) => reselect({ enabled: event.target.checked })}
        />
        单独启用原生实时分享（当前窗口及后续窗口）
      </label>
      <p>
        默认关闭，与 Managed 事件流及控制权限无关。只读 Follow
        成功读取后才可选择。类别不是脱敏；未来同类内容也可能包含敏感信息。
      </p>
      {draft.enabled ? (
        <>
          <label>
            原生 Follow
            <select
              aria-label="实时分享的 Follow"
              value={draft.followId}
              disabled={busy}
              onChange={(event) => reselect({ followId: event.target.value })}
            >
              <option value="">选择已成功读取的 Follow</option>
              {follows.map((item) => (
                <option
                  key={item.id}
                  value={item.id}
                  disabled={!eligible(item, snapshots)}
                >
                  {item.source.title || item.source.sessionId} · {item.state}
                </option>
              ))}
            </select>
          </label>
          {kinds.map(([kind, label]) => (
            <label className="inline-checkbox" key={kind}>
              <input
                type="checkbox"
                checked={draft.entryKinds.includes(kind)}
                disabled={busy}
                onChange={(event) =>
                  reselect({
                    entryKinds: event.target.checked
                      ? [...draft.entryKinds, kind]
                      : draft.entryKinds.filter((item) => item !== kind),
                  })
                }
              />
              {label}
            </label>
          ))}
          {!available ? (
            <p className="review-warning">
              所选 Follow
              尚未成功读取、已暂停/停止，或当前窗口不可用；不能创建实时分享。
            </p>
          ) : null}
          {draft.preview && !previewFresh ? (
            <p className="review-warning" role="alert">
              Follow
              当前窗口或状态已变化。下方是旧预览；必须刷新并重新确认，不能自动扩大授权。
            </p>
          ) : null}
          <button
            type="button"
            className="button ghost compact"
            disabled={busy || !available || !draft.entryKinds.length}
            onClick={() => {
              if (current && follow && available)
                onChange({
                  ...draft,
                  preview: { snapshot: current, epoch: follow.epoch },
                  confirmCurrent: false,
                  confirmFuture: false,
                });
            }}
          >
            刷新并预览当前窗口
          </button>
          {draft.preview ? (
            <section
              className="native-live-preview"
              aria-label="当前原生窗口预览"
            >
              <strong>固定预览 · {draft.preview.snapshot.id}</strong>
              <p>
                {entries.length} 条所选类别记录；未选类别不分享。此窗口并非完整
                Session。
              </p>
              {draft.preview.snapshot.truncated ? (
                <p className="review-warning">此窗口已截断。</p>
              ) : null}
              {draft.preview.snapshot.warnings.map((warning, index) => (
                <p key={`${index}-${warning}`}>{warning}</p>
              ))}
              {entries.map((entry) => (
                <article key={entry.id}>
                  <strong>{entry.role || entry.kind}</strong>
                  <pre>{entry.text}</pre>
                </article>
              ))}
              {!entries.length ? (
                <p>当前窗口没有所选类别的记录；后续仍将按已选类别分享。</p>
              ) : null}
              <label className="inline-checkbox">
                <input
                  type="checkbox"
                  checked={previewFresh && draft.confirmCurrent}
                  disabled={busy || !previewFresh}
                  onChange={(event) =>
                    onChange({ ...draft, confirmCurrent: event.target.checked })
                  }
                />
                我已检查此固定窗口的筛选预览，允许分享当前内容
              </label>
              <label className="inline-checkbox">
                <input
                  type="checkbox"
                  checked={previewFresh && draft.confirmFuture}
                  disabled={busy || !previewFresh}
                  onChange={(event) =>
                    onChange({ ...draft, confirmFuture: event.target.checked })
                  }
                />
                我另行允许后续窗口按所选类别自动分享（不会逐条再确认）
              </label>
            </section>
          ) : null}
        </>
      ) : null}
    </section>
  );
}
