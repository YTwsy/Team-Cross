import { useEffect, useState } from "react";
import { api } from "../api";
import type {
  Annotation,
  CodeAnnotation,
  Round,
  SealedCodeReview,
} from "../types";
import { DiffViewer } from "./DiffViewer";

export function CodeReviewPanel({
  threadId,
  rounds,
  active,
  canManage,
  livePatch,
  annotations,
  onAnnotate,
  reference,
}: {
  threadId: string;
  rounds: Round[];
  active: boolean;
  canManage: boolean;
  livePatch: string;
  annotations: Annotation[];
  onAnnotate: (anchor: CodeAnnotation) => void;
  reference?: CodeAnnotation;
}) {
  const [selectedId, setSelectedId] = useState(
    () => reference?.target.roundId ?? rounds.at(-1)?.id ?? "",
  );
  const [live, setLive] = useState(false);
  const [result, setResult] = useState<{
    threadId: string;
    code: SealedCodeReview;
  }>();
  const [error, setError] = useState("");
  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    if (reference) {
      setSelectedId(reference.target.roundId);
      setLive(false);
    }
  }, [reference]);
  useEffect(() => {
    if (!active || live || !selectedId) return;
    let current = true;
    setResult(undefined);
    setError("");
    void api
      .roundCode(threadId, selectedId)
      .then((code) => {
        if (!current) return;
        if (code.roundId !== selectedId)
          throw new Error("返回了不同的 Round；未替换引用代码。");
        setResult({ threadId, code });
      })
      .catch((reason: unknown) => {
        if (current)
          setError(
            reason instanceof Error ? reason.message : "封存代码读取失败",
          );
      });
    return () => {
      current = false;
    };
  }, [active, live, selectedId, threadId, attempt]);
  const code =
    result?.threadId === threadId && result.code.roundId === selectedId
      ? result.code
      : undefined;
  const referenceAvailable =
    !reference ||
    !code ||
    code.lines.some((line) =>
      reference.target.side === "old"
        ? line.oldPath === reference.file && line.oldLine === reference.line
        : line.newPath === reference.file && line.newLine === reference.line,
    );
  return (
    <section className="sealed-code-review" aria-label="封存代码审阅">
      <div className="panel-toolbar">
        <label>
          代码 Round
          <select
            aria-label="代码 Round"
            value={selectedId}
            onChange={(event) => {
              setSelectedId(event.target.value);
              setLive(false);
            }}
          >
            {!selectedId ? <option value="">暂无封存代码 Round</option> : null}
            {selectedId && !rounds.some((round) => round.id === selectedId) ? (
              <option value={selectedId}>引用 Round {selectedId}</option>
            ) : null}
            {rounds.map((round) => (
              <option key={round.id} value={round.id}>
                Round {round.sequence} · {round.summary}
              </option>
            ))}
          </select>
        </label>
        {canManage ? (
          <button
            type="button"
            className="text-button"
            onClick={() => setLive((value) => !value)}
          >
            {live ? "返回封存 Round 审阅" : "查看实时 worktree（不能批注）"}
          </button>
        ) : null}
      </div>
      {live && canManage ? (
        <>
          <p className="review-warning">
            实时 worktree 会变化，不代表所选 Round；此视图不提供代码批注。
          </p>
          <DiffViewer patch={livePatch} annotations={[]} />
        </>
      ) : (
        <>
          <p className="side-muted">
            批注只绑定所选不可变 Round 的文件、旧/新侧和行号。新增 Round
            不会自动替换当前选择。
          </p>
          {!selectedId ? (
            <p>暂无可审阅的封存代码；Session 历史不代表代码基线。</p>
          ) : error ? (
            <div role="alert">
              <p>{error} 不会用当前 worktree 或其他 Round 替代。</p>
              <button
                type="button"
                className="text-button"
                onClick={() => setAttempt((value) => value + 1)}
              >
                重试所选 Round
              </button>
            </div>
          ) : !code ? (
            <p role="status">正在读取封存代码…</p>
          ) : (
            <>
              <p>
                Round <code>{code.roundId}</code> · baseline{" "}
                <code>{code.baseline}</code>
              </p>
              {reference?.target.roundId === selectedId ? (
                <p className="reference-location">
                  代码引用：{reference.file} · {reference.target.side} ·{" "}
                  {reference.line}
                </p>
              ) : null}
              {!referenceAvailable &&
              reference?.target.roundId === selectedId ? (
                <p className="review-warning" role="alert">
                  原代码行不在获准的封存 diff 中；未重新定位。
                </p>
              ) : null}
              <DiffViewer
                patch={code.patch}
                review={code}
                annotations={annotations}
                onAnnotate={onAnnotate}
                reference={reference}
              />
            </>
          )}
        </>
      )}
    </section>
  );
}
