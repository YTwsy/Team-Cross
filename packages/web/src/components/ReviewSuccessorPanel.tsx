import { useEffect, useState } from "react";
import { api } from "../api";
import type {
  CapturePreview,
  ReviewSuccessorInput,
  ReviewSuccessorPreview,
  ThreadDetail,
} from "../types";

export function ReviewSuccessorPanel({
  thread,
  onResult,
}: {
  thread: ThreadDetail;
  onResult: (value: ThreadDetail) => void;
}) {
  const [roundId, setRoundId] = useState(() => thread.rounds.at(-1)?.id ?? "");
  const [repo, setRepo] = useState("");
  const [goal, setGoal] = useState("");
  const [untracked, setUntracked] = useState<string[]>([]);
  const [scan, setScan] = useState<{
    repo: string;
    value: CapturePreview;
  } | null>(null);
  const [preview, setPreview] = useState<{
    key: string;
    value: ReviewSuccessorPreview;
  } | null>(null);
  const [confirmedKey, setConfirmedKey] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const input: ReviewSuccessorInput = {
    roundId,
    repo,
    goal,
    untracked,
    expectedRevision: thread.revision,
  };
  const key = JSON.stringify([thread.id, input]);
  const activeScan = scan?.repo === repo ? scan.value : null;
  const activePreview = preview?.key === key ? preview.value : null;
  const confirmed = !!activePreview && confirmedKey === key;
  const roundExists = thread.rounds.some((round) => round.id === roundId);

  useEffect(() => {
    setPreview(null);
    setConfirmedKey(null);
  }, [key]);

  async function act(action: () => Promise<void>) {
    setBusy(true);
    setError("");
    try {
      await action();
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "后继工作准备失败");
    } finally {
      setBusy(false);
    }
  }

  return (
    <details className="continuation-panel">
      <summary>为审阅内容选择代码基线</summary>
      <p className="side-muted">
        将所选 Round 中已保存的 Session 快照带到独立后继
        Thread。精确指向这些材料的批注会封存为只读反馈
        Evidence；未锚定批注不自动带入。无需再次读取原生历史。
      </p>
      <label>
        来源审阅 Round
        <select
          value={roundId}
          disabled={busy}
          onChange={(event) => setRoundId(event.target.value)}
        >
          {!roundExists ? <option value="">选择一个已封存 Round</option> : null}
          {thread.rounds.map((round) => (
            <option key={round.id} value={round.id}>
              Round {round.sequence} · {round.summary}
            </option>
          ))}
        </select>
      </label>
      <label>
        后续工作目标
        <textarea
          value={goal}
          disabled={busy}
          onChange={(event) => setGoal(event.target.value)}
          placeholder="保存目标，不会自动发送给 Agent"
        />
      </label>
      <label>
        代码仓库路径
        <input
          value={repo}
          disabled={busy}
          onChange={(event) => {
            setRepo(event.target.value);
            setUntracked([]);
            setScan(null);
          }}
          placeholder="明确选择本机 Git 仓库"
        />
      </label>
      <button
        type="button"
        className="button secondary full"
        disabled={busy || !repo.trim()}
        onClick={() =>
          void act(async () => {
            const value = await api.preview(repo);
            setScan({ repo, value });
            setUntracked([]);
            setPreview(null);
            setConfirmedKey(null);
          })
        }
      >
        检查代码仓库
      </button>
      {activeScan ? (
        <>
          <p className="side-muted">
            当前 HEAD：{activeScan.head || "无首个 commit"}
            。当前改动将单独捕获，不代表历史 Session 的代码状态。
          </p>
          <p className="side-muted">
            包含全部已跟踪改动；未跟踪文件只包含下方明确勾选的项。
          </p>
          <pre className="side-muted">
            {activeScan.status || "没有已跟踪或未跟踪改动"}
          </pre>
          {activeScan.untracked.map((file) => (
            <label className="inline-checkbox" key={file.path}>
              <input
                type="checkbox"
                disabled={busy}
                checked={untracked.includes(file.path)}
                onChange={(event) => {
                  setUntracked(
                    event.target.checked
                      ? [...untracked, file.path].sort()
                      : untracked.filter((path) => path !== file.path),
                  );
                }}
              />
              捕获 {file.path}（{file.size} bytes）
            </label>
          ))}
          <button
            type="button"
            className="button secondary full"
            disabled={busy || activeScan.unborn || !roundExists || !goal.trim()}
            onClick={() =>
              void act(async () => {
                const value = await api.previewReviewSuccessor(
                  thread.id,
                  input,
                );
                setPreview({ key, value });
                setConfirmedKey(null);
              })
            }
          >
            预览后继 Thread
          </button>
        </>
      ) : null}
      {activePreview ? (
        <>
          <p>{activePreview.warning}</p>
          <p className="side-muted">
            基线：{activePreview.baseline}
            <br />
            {activePreview.snapshotCount} 份 Session 快照 ·{" "}
            {activePreview.feedbackCount} 条批注。执行位置是当前主机的新隔离
            worktree。
          </p>
          {activePreview.untracked
            .filter((file) => !file.captured)
            .map((file) => (
              <p role="alert" key={file.path}>
                {file.path} 未捕获：{file.reason}
              </p>
            ))}
          <label className="inline-checkbox">
            <input
              type="checkbox"
              checked={confirmed}
              disabled={busy}
              onChange={(event) =>
                setConfirmedKey(event.target.checked ? key : null)
              }
            />
            我确认这是另行选择的代码基线；仅创建后继 Thread，不启动 Agent。
          </label>
          <button
            type="button"
            className="button primary full"
            disabled={busy || !confirmed}
            onClick={() =>
              void act(async () => {
                if (!activePreview || !confirmed) return;
                const value = await api.createReviewSuccessor(thread.id, {
                  ...input,
                  previewHash: activePreview.previewHash,
                  confirmSeparateBaseline: true,
                });
                setPreview(null);
                setConfirmedKey(null);
                onResult(value);
              })
            }
          >
            创建后继 Thread，不启动 Agent
          </button>
        </>
      ) : null}
      {error ? (
        <p className="form-error" role="alert">
          {error}
        </p>
      ) : null}
    </details>
  );
}
