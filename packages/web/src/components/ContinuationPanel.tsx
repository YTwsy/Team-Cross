import { useState } from "react";
import { api } from "../api";
import type { Provider, ThreadDetail } from "../types";

function download(data: unknown, name: string, type: string) {
  const content = typeof data === "string" ? data : JSON.stringify(data);
  const url = URL.createObjectURL(new Blob([content], { type }));
  const link = document.createElement("a");
  link.href = url;
  link.download = name;
  link.click();
  window.setTimeout(() => URL.revokeObjectURL(url), 1000);
}

export function ContinuationPanel({
  thread,
  onResult,
}: {
  thread: ThreadDetail;
  onResult: (value: ThreadDetail) => void;
}) {
  const [selectedRound, setSelectedRound] = useState("");
  const [provider, setProvider] = useState<Provider>("codex");
  const [prompt, setPrompt] = useState("");
  const [networkEnabled, setNetworkEnabled] = useState(false);
  const [fork, setFork] = useState(false);
  const [confirmed, setConfirmed] = useState(false);
  const [exportConfirmed, setExportConfirmed] = useState(false);
  const [evidenceIds, setEvidenceIds] = useState<string[]>([]);
  const [snapshotIds, setSnapshotIds] = useState<string[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const latest = thread.rounds.at(-1);
  const round =
    thread.rounds.find((item) => item.id === selectedRound) ?? latest;
  const historical = !!round && round.id !== latest?.id;
  const willFork = historical || fork;
  const writable = !!thread.git.head && !thread.readOnly;
  const running =
    thread.agentRun?.status === "running" ||
    thread.agentRun?.status === "waiting";

  async function act(action: () => Promise<void>) {
    setBusy(true);
    setError("");
    try {
      await action();
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "操作失败");
    } finally {
      setBusy(false);
    }
  }

  if (!round || !writable)
    return (
      <section className="side-section">
        <strong>Continue from Round</strong>
        <p className="side-muted">
          此 Thread
          仅包含历史参考材料，没有可执行的代码基线。审阅与批注不需要启动 Agent。
        </p>
      </section>
    );
  return (
    <section className="side-section continuation-panel">
      <div className="side-section-heading">
        <span>Continue from Round</span>
      </div>
      <label>
        交接快照
        <select
          value={round.id}
          onChange={(event) => {
            setSelectedRound(event.target.value);
            setConfirmed(false);
            setExportConfirmed(false);
          }}
        >
          {thread.rounds.map((item) => (
            <option key={item.id} value={item.id}>
              Round {item.sequence} · {item.summary.slice(0, 48)}
            </option>
          ))}
        </select>
      </label>
      <p className="side-muted">不可变代码快照，不是当前原仓库状态。</p>
      <details>
        <summary>在本机创建新 Session</summary>
        <p>
          {willFork ? "创建独立 Fork Thread" : "在当前 Thread 顺序继续"}
          ；保留来源，不恢复原生 Session ID。
        </p>
        <label>
          Provider
          <select
            value={provider}
            onChange={(event) => setProvider(event.target.value as Provider)}
          >
            <option value="codex">Codex</option>
            <option value="claude">Claude</option>
            <option value="mock">Mock（测试）</option>
          </select>
        </label>
        <label>
          首条指令
          <textarea
            value={prompt}
            onChange={(event) => setPrompt(event.target.value)}
            placeholder="描述你希望接下来完成的工作…"
          />
        </label>
        <label className="inline-checkbox">
          <input
            type="checkbox"
            checked={willFork}
            disabled={historical}
            onChange={(event) => {
              setFork(event.target.checked);
              setConfirmed(false);
            }}
          />
          创建独立 Fork Thread
        </label>
        <label className="inline-checkbox">
          <input
            type="checkbox"
            checked={networkEnabled}
            onChange={(event) => {
              setNetworkEnabled(event.target.checked);
              setConfirmed(false);
            }}
          />
          允许工具访问网络（默认关闭）
        </label>
        <p className="side-muted">
          执行主机：当前主机。使用此主机的 Provider 凭据与配额。
          {willFork
            ? "将创建新的隔离 worktree。"
            : `隔离目录：${thread.worktree}`}
        </p>
        <label className="inline-checkbox">
          <input
            type="checkbox"
            checked={confirmed}
            onChange={(event) => setConfirmed(event.target.checked)}
          />
          我授权以上新 Session 执行；原始 checkout 不被自动修改。
        </label>
        <button
          className="button primary full"
          disabled={
            busy || !writable || running || !confirmed || !prompt.trim()
          }
          onClick={() =>
            void act(async () => {
              onResult(
                await api.continueFromRound(thread.id, {
                  roundId: round.id,
                  provider,
                  prompt: prompt.trim(),
                  networkEnabled,
                  expectedRevision: thread.revision,
                  fork: willFork,
                }),
              );
              setConfirmed(false);
              setPrompt("");
            })
          }
          type="button"
        >
          {busy ? "处理中…" : "创建新 Session 并继续"}
        </button>
        {running ? (
          <p className="side-muted">请先结束或中断当前 Turn。</p>
        ) : null}
      </details>
      <button
        className="text-button"
        disabled={busy || !writable || running}
        onClick={() =>
          void act(async () =>
            onResult(await api.forkThread(thread.id, round.id)),
          )
        }
        type="button"
      >
        仅 Fork Thread，不启动 Agent
      </button>
      <details className="bundle-options">
        <summary>导出离线交接包</summary>
        <p>
          包含 Git 基线所需历史与所选 Round 的代码。接收方可在本机独立
          Fork；发送方离线后仍可使用。
        </p>
        {(thread.sessionSnapshots ?? []).map((snapshot) => (
          <label className="inline-checkbox" key={snapshot.id}>
            <input
              type="checkbox"
              checked={snapshotIds.includes(snapshot.id)}
              onChange={(event) => {
                setSnapshotIds((current) =>
                  event.target.checked
                    ? [...current, snapshot.id]
                    : current.filter((id) => id !== snapshot.id),
                );
                setExportConfirmed(false);
              }}
            />
            完整快照：{snapshot.source.title || snapshot.source.provider}
          </label>
        ))}
        {thread.evidence
          .filter((item) => item.kind !== "agent_transcript")
          .map((item) => (
            <label className="inline-checkbox" key={item.id}>
              <input
                type="checkbox"
                checked={evidenceIds.includes(item.id)}
                onChange={(event) => {
                  setEvidenceIds((current) =>
                    event.target.checked
                      ? [...current, item.id]
                      : current.filter((id) => id !== item.id),
                  );
                  setExportConfirmed(false);
                }}
              />
              Evidence：{item.name}
            </label>
          ))}
        <label className="inline-checkbox">
          <input
            type="checkbox"
            checked={exportConfirmed}
            onChange={(event) => setExportConfirmed(event.target.checked)}
          />
          已检查代码与所选内容；交付的副本无法随 Share 撤销而收回。
        </label>
        <button
          className="button secondary full"
          disabled={busy || !writable || !exportConfirmed}
          onClick={() =>
            void act(async () => {
              const bundle = await api.exportBundle(thread.id, {
                roundId: round.id,
                evidenceIds,
                snapshotIds,
                confirmExport: true,
              });
              download(
                bundle,
                `teamcross-${thread.id}-${round.id}.tcx`,
                "application/vnd.teamcross.bundle+json",
              );
            })
          }
          type="button"
        >
          导出所选交接包
        </button>
      </details>
      {error ? (
        <p className="form-error" role="alert">
          {error}
        </p>
      ) : null}
    </section>
  );
}
