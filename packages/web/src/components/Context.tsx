import { useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { useResource } from "../api";
import { readingTitle } from "../reading";
import {
  codeLines,
  codeTargetMatches,
  diffLines,
  lineTarget,
  targetLabel,
  type CodeLine,
} from "../annotations";
import type {
  Annotation,
  AnnotationTarget,
  Changes,
  FileContext,
  History,
} from "../types";
import type { AnnotationRequest } from "./Annotations";
import {
  ReaderMessage,
  ReadingLayout,
  type Annotate,
  type Discuss,
} from "./Reading";
import { Empty, ErrorBox, Icon, Loading } from "./ui";

function parentElement(node: Node) {
  return node instanceof Element ? node : node.parentElement;
}

type ContextTab = "history" | "changes" | "file" | "technical";

export function Context({
  id,
  sessionId,
  sequence,
  online,
  closed,
  canAnnotate,
  onAnnotate,
  location,
  agentName = "Codex",
  technical,
  annotations = [],
  onDiscuss,
}: {
  id: string;
  sessionId: string;
  sequence: number;
  online: boolean;
  closed: boolean;
  canAnnotate: boolean;
  onAnnotate: Annotate;
  annotations?: Annotation[];
  onDiscuss?: Discuss;
  location?: AnnotationRequest;
  agentName?: string;
  technical?: ReactNode;
}) {
  const [tab, setTab] = useState<ContextTab>("history");
  const [path, setPath] = useState("");
  const [file, setFile] = useState("");
  const [cursor, setCursor] = useState("");
  const [selection, setSelection] = useState<AnnotationTarget>();
  const [selectionHint, setSelectionHint] = useState("");
  const [activeTarget, setActiveTarget] = useState<AnnotationTarget>();
  const [locationStatus, setLocationStatus] = useState("");
  const panel = useRef<HTMLElement>(null);
  const searched = useRef(new Set<string>());
  const finishedLocation = useRef(false);
  const contextPath =
    online && tab !== "technical" && (tab !== "file" || file)
      ? `collaborations/${id}/context?kind=${tab}&path=${encodeURIComponent(file)}&cursor=${encodeURIComponent(cursor)}`
      : null;
  const context = useResource<History | Changes | FileContext>(
    contextPath,
    0,
    sequence,
  );
  const latest = context.dataPath === contextPath ? context.data : undefined;
  const [historyView, setHistoryView] = useState<{
    path: string;
    data: History;
    signature: string;
  }>();
  const acceptNextHistory = useRef(false);
  const historySignature = useMemo(
    () => (latest && "thread" in latest ? JSON.stringify(latest) : ""),
    [latest],
  );
  useEffect(() => {
    if (!latest || !("thread" in latest) || !contextPath) return;
    if (
      !historyView ||
      historyView.path !== contextPath ||
      acceptNextHistory.current
    ) {
      setHistoryView({
        path: contextPath,
        data: latest,
        signature: historySignature,
      });
      acceptNextHistory.current = false;
    }
  }, [latest, contextPath, historySignature, historyView]);
  const data =
    tab === "history" && historyView?.path === contextPath
      ? historyView.data
      : latest;
  const historyPending =
    tab === "history" &&
    latest &&
    "thread" in latest &&
    historyView?.path === contextPath &&
    historyView.signature !== historySignature;
  function refresh() {
    acceptNextHistory.current = true;
    context.reload();
  }
  const lines = useMemo(() => {
    if (tab === "file" && data && "text" in data)
      return codeLines(data.text, data.path || file);
    if (tab === "changes" && data && "diff" in data)
      return diffLines(data.diff);
    return [];
  }, [data, tab, file]);
  const contentHash =
    data && "contentHash" in data ? data.contentHash : undefined;
  const codeMatch =
    activeTarget &&
    (!activeTarget.sessionId || activeTarget.sessionId === sessionId) &&
    activeTarget.kind === tab &&
    activeTarget.kind !== "history" &&
    (activeTarget.kind !== "changes" ||
      (data &&
        "baseRevision" in data &&
        activeTarget.baseRevision === data.baseRevision)) &&
    codeTargetMatches(activeTarget, lines, contentHash);

  useEffect(() => {
    setSelection(undefined);
    setSelectionHint("");
  }, [tab, file, cursor]);
  useEffect(() => {
    const target = location?.target;
    if (!target || target.kind === "material") return;
    setActiveTarget(target);
    setTab(target.kind);
    setCursor(target.cursor || "");
    if (target.kind === "file") {
      setPath(target.path || "");
      setFile(target.path || "");
    }
    setSelection(undefined);
    setLocationStatus("正在查找批注原文…");
    searched.current = new Set();
    finishedLocation.current = false;
    acceptNextHistory.current = true;
    context.reload();
    panel.current?.scrollIntoView?.({ block: "start", behavior: "smooth" });
  }, [location, context.reload]);

  useEffect(() => {
    if (!activeTarget || activeTarget.kind !== tab || !data || context.loading)
      return;
    if (activeTarget.sessionId && activeTarget.sessionId !== sessionId) {
      setLocationStatus("这条批注属于其他会话，以下保留批注时的原文。");
    } else if (tab === "history" && "thread" in data) {
      const turn = data.thread.turns?.find(
        (turn) => turn.id === activeTarget.turnId,
      );
      const item = turn?.items?.find((item) => item.id === activeTarget.itemId);
      if (
        !item &&
        !finishedLocation.current &&
        data.nextCursor &&
        !searched.current.has(data.nextCursor) &&
        searched.current.size < 20
      ) {
        searched.current.add(data.nextCursor);
        setCursor(data.nextCursor);
        return;
      }
      const text =
        item?.text ||
        item?.content?.map((part) => part.text || "").join("\n") ||
        "";
      setLocationStatus(
        item &&
          text.slice(activeTarget.startOffset || 0, activeTarget.endOffset) ===
            activeTarget.quote
          ? "已定位到批注原文。"
          : item
            ? "这条消息已变化，以下保留批注时的原文。"
            : "在已读取的历史中未找到原消息，以下保留批注时的原文。可以继续查看更早对话。",
      );
    } else if (tab !== "history") {
      setLocationStatus(
        codeMatch
          ? "已定位到批注原文。"
          : "文件或改动已变化，以下保留批注时的原文。请核对当前内容后再处理。",
      );
    } else return;
    if (!finishedLocation.current) {
      panel.current
        ?.querySelector<HTMLElement>("[data-annotation-highlight]")
        ?.scrollIntoView?.({ block: "nearest", inline: "nearest" });
    }
    finishedLocation.current = true;
  }, [activeTarget, tab, data, context.loading, codeMatch, sessionId]);

  function start(target: AnnotationTarget, element?: HTMLElement) {
    const range = window.getSelection();
    onAnnotate(
      { ...target, sessionId },
      element
        ? { element }
        : range?.rangeCount && panel.current
          ? { element: panel.current, range: range.getRangeAt(0).cloneRange() }
          : undefined,
    );
  }
  function changeTab(next: ContextTab) {
    setActiveTarget(undefined);
    setLocationStatus("");
    setTab(next);
  }
  function captureSelection() {
    const selected = window.getSelection();
    if (!selected || selected.isCollapsed || !selected.rangeCount) {
      setSelection(undefined);
      setSelectionHint("");
      return;
    }
    const range = selected.getRangeAt(0);
    const startElement = parentElement(range.startContainer);
    const endElement = parentElement(range.endContainer);
    const first = startElement?.closest<HTMLElement>("[data-code-row]");
    const last = endElement?.closest<HTMLElement>("[data-code-row]");
    if (
      first &&
      last &&
      panel.current?.contains(first) &&
      panel.current.contains(last)
    ) {
      const target = makeLineTarget(
        +first.dataset.codeRow!,
        +last.dataset.codeRow!,
      );
      if (target) {
        setSelection(target);
        setSelectionHint("");
        return;
      }
    }
    setSelection(undefined);
    setSelectionHint(
      "请选择同一条消息，或同一文件、同一侧的连续代码行（最多 8000 字）。",
    );
  }
  function makeLineTarget(start: number, end: number) {
    return lineTarget(lines, start, end, {
      kind: tab === "file" ? "file" : "changes",
      contentHash,
      baseRevision:
        data && "baseRevision" in data ? data.baseRevision : undefined,
    });
  }
  function renderCode(line: CodeLine, index: number) {
    const target =
      canAnnotate && line.number && line.path
        ? makeLineTarget(index, index)
        : undefined;
    const highlighted =
      !!codeMatch &&
      line.path === activeTarget?.path &&
      line.side === activeTarget.side &&
      line.number !== undefined &&
      line.number >= activeTarget.startLine! &&
      line.number <= activeTarget.endLine!;
    return (
      <div
        key={index}
        data-code-row={index}
        data-annotation-highlight={highlighted || undefined}
        className={`code-line ${line.side === "old" ? "removed" : line.raw.startsWith("+") && line.side ? "added" : ""} ${!line.number ? "code-header" : ""}`}
      >
        <span className="code-gutter" aria-hidden="true">
          {tab === "changes" && <span>{line.oldNumber}</span>}
          <span>{line.newNumber}</span>
        </span>
        {target ? (
          <button
            className="code-annotate"
            aria-label={`批注 ${targetLabel(target)}`}
            onClick={(event) =>
              start(
                target,
                event.currentTarget.closest<HTMLElement>("[data-code-row]")!,
              )
            }
          >
            <Icon name="plus" size={12} />
          </button>
        ) : (
          <span className="code-annotate-placeholder" />
        )}
        <code>{tab === "changes" ? line.raw : line.text || "\u200b"}</code>
      </div>
    );
  }

  let body;
  if (data) {
    if (tab === "history" && "thread" in data) {
      const turns = data.thread.turns || [];
      body = turns.length ? (
        <ReadingLayout
          outline={turns.map((turn, i) => ({
            id: turn.id,
            label: readingTitle(
              turn.items?.find((item) => item.type === "userMessage")?.text ||
                turn.items?.[0]?.text ||
                `第 ${i + 1} 轮`,
            ),
            onSelect: () =>
              Array.from(
                panel.current?.querySelectorAll<HTMLElement>(
                  "[data-reader-turn]",
                ) || [],
              )
                .find((el) => el.dataset.readerTurn === turn.id)
                ?.scrollIntoView({ block: "start", behavior: "smooth" }),
          }))}
        >
          <div className="history">
            {turns.map((turn, turnIndex) => (
              <div
                key={turn.id}
                className="history-turn"
                data-reader-turn={turn.id}
              >
                <div className="reader-turn-heading">第 {turnIndex + 1} 轮</div>
                {(turn.items || [])
                  .filter((item) =>
                    [
                      "userMessage",
                      "agentMessage",
                      "toolCall",
                      "toolResult",
                      "commandExecution",
                      "fileChange",
                    ].includes(item.type),
                  )
                  .map((item, index) => {
                    const text =
                      item.text ||
                      item.content?.map((part) => part.text || "").join("\n") ||
                      "";
                    return (
                      <ReaderMessage
                        key={item.id || index}
                        source={text}
                        label={
                          item.type === "userMessage"
                            ? "用户"
                            : item.type === "agentMessage"
                              ? agentName
                              : "工具过程"
                        }
                        target={{
                          kind: "history",
                          sessionId,
                          turnId: turn.id,
                          itemId: item.id,
                          cursor: cursor || undefined,
                          quote: text,
                        }}
                        activeTarget={activeTarget}
                        annotations={annotations}
                        onAnnotate={onAnnotate}
                        onDiscuss={onDiscuss}
                        disabled={!canAnnotate}
                      />
                    );
                  })}
              </div>
            ))}
          </div>
        </ReadingLayout>
      ) : (
        <Empty icon="comment" title="还没有新的活动">
          <p>在 {agentName} 中继续，最新的上下文会出现在这里。</p>
        </Empty>
      );
    } else if (tab === "changes" && "diff" in data) {
      body =
        data.diff || data.status ? (
          <>
            <pre className="diff-summary">
              {data.status}
              {data.stat}
            </pre>
            {data.diff ? (
              <div
                className="code-content annotated-code"
                onMouseUp={captureSelection}
                onKeyUp={captureSelection}
              >
                {lines.map(renderCode)}
              </div>
            ) : (
              <p className="muted small-text">
                文件状态已列出；未跟踪文件可通过文件入口查看。
              </p>
            )}
            {data.truncated && (
              <p className="muted small-text">
                改动较大，仅展示前 256 KiB。其余内容请查看文件或在 {agentName}
                中阅读。
              </p>
            )}
          </>
        ) : (
          <Empty icon="branch" title="当前没有代码改动" />
        );
    } else if (tab === "file" && "text" in data) {
      body = (
        <div
          className="code-content annotated-code"
          onMouseUp={captureSelection}
          onKeyUp={captureSelection}
        >
          {lines.map(renderCode)}
        </div>
      );
    }
  }
  return (
    <section className="panel context-panel" ref={panel}>
      <div className="panel-heading">
        <h2>协作上下文</h2>
        {tab !== "technical" && (
          <button
            className="icon-button"
            aria-label="刷新上下文"
            disabled={!online}
            onClick={refresh}
          >
            <Icon name="refresh" size={17} />
          </button>
        )}
      </div>
      <div className="tabs" role="tablist" aria-label="上下文类型">
        {[
          ["history", "最近对话"],
          ["changes", "代码改动"],
          ["file", "查看文件"],
          ...(technical ? [["technical", "技术信息"]] : []),
        ].map(([value, label]) => (
          <button
            role="tab"
            key={value}
            aria-selected={tab === value}
            onClick={() => changeTab(value as ContextTab)}
          >
            {label}
          </button>
        ))}
      </div>
      {tab === "file" && (
        <form
          className="file-search"
          onSubmit={(event) => {
            event.preventDefault();
            setActiveTarget(undefined);
            setLocationStatus("");
            setFile(path.trim());
          }}
        >
          <input
            aria-label="相对执行目录的文件路径"
            placeholder="相对执行目录的文件路径，例如 src/main.ts"
            value={path}
            onChange={(event) => setPath(event.target.value)}
          />
          <button className="button small" disabled={!path.trim() || !online}>
            查看
          </button>
        </form>
      )}
      {canAnnotate && tab !== "technical" && tab !== "history" && (
        <div
          className={`context-selection ${selection ? "has-selection" : ""}`}
        >
          <span>
            {selection
              ? targetLabel(selection)
              : selectionHint ||
                "点击行旁的 +，或拖选同一侧的连续代码行来批注。"}
          </span>
          {selection && (
            <button
              className="button small primary"
              onClick={() => start(selection)}
            >
              <Icon name="comment" size={14} />
              批注所选内容
            </button>
          )}
        </div>
      )}
      {activeTarget && (
        <div className="annotation-location" role="status">
          <div>
            <strong>{targetLabel(activeTarget)}</strong>
            <button
              className="icon-button"
              aria-label="关闭批注定位"
              onClick={() => {
                setActiveTarget(undefined);
                setLocationStatus("");
              }}
            >
              <Icon name="close" size={14} />
            </button>
          </div>
          <p>{locationStatus}</p>
          {!locationStatus.startsWith("已定位") && (
            <blockquote>{activeTarget.quote}</blockquote>
          )}
        </div>
      )}
      {tab === "technical" ? (
        technical
      ) : (
        <>
          <ErrorBox message={context.error} retry={refresh} />
          {historyPending && (
            <div className="reader-update" role="status">
              <span>对话有新内容，当前阅读位置已保留。</span>
              <button
                className="button small"
                onClick={() => {
                  if (latest && "thread" in latest && contextPath)
                    setHistoryView({
                      path: contextPath,
                      data: latest,
                      signature: historySignature,
                    });
                }}
              >
                显示新内容
              </button>
            </div>
          )}
          {!online ? (
            <Empty
              icon="link"
              title={
                closed
                  ? "使用新邀请加入后可查看上下文"
                  : "连接恢复后可查看上下文"
              }
            />
          ) : context.loading && !data ? (
            <Loading />
          ) : (
            body || <Empty icon="folder" title="输入相对路径以查看文件" />
          )}
          {tab === "history" &&
            data &&
            "thread" in data &&
            (data.nextCursor || cursor) && (
              <div className="context-pagination">
                {data.nextCursor && (
                  <button
                    className="button small"
                    disabled={context.loading}
                    onClick={() => {
                      setCursor(data.nextCursor!);
                      finishedLocation.current = false;
                    }}
                  >
                    更早对话
                  </button>
                )}
                {cursor && (
                  <button
                    className="text-link small-text"
                    onClick={() => {
                      setActiveTarget(undefined);
                      setLocationStatus("");
                      setCursor("");
                    }}
                  >
                    回到最近对话
                  </button>
                )}
              </div>
            )}
        </>
      )}
      <div className="panel-footnote">
        {tab === "technical"
          ? "这些信息来自当前协作记录和运行时最近确认的状态。"
          : `这里展示已读取的协作上下文；执行交互继续使用 ${agentName} 原生客户端。`}
      </div>
    </section>
  );
}
