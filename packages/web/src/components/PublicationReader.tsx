import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { api, errorText } from "../api";
import {
  itemLabel,
  joinMaterialSegments,
  mergeMaterialSegments,
  readingTitle,
} from "../reading";
import type { PublicationDraftPage, PublicationDraftSummary } from "../types";
import { ReaderMessage, ReadingLayout } from "./Reading";
import { ErrorBox, Loading } from "./ui";

export type PublicationScope = {
  start: string;
  end: string;
  reading: string;
  onStart: (id: string) => void;
  onEnd: (id: string) => void;
  onReading: (id: string) => void;
};
type ReadRequest = {
  turnId?: string;
  itemId?: string;
  startOffset?: number;
  cursor?: string;
};
type Failure = { request: ReadRequest; mode: "replace" | "append" | "item" };

// The editor reads the private frozen draft; confirmation reads a different,
// server-scoped preview ID. A cursor is never reused across these two readers.
export function PublicationReader({
  draft,
  scope,
  disabled = false,
  originalTurns = draft.turns,
  onReadyChange,
}: {
  draft: PublicationDraftSummary;
  scope?: PublicationScope;
  disabled?: boolean;
  originalTurns?: PublicationDraftSummary["turns"];
  onReadyChange?: (ready: boolean) => void;
}) {
  const [current, setCurrent] = useState(scope?.start || draft.startTurnId);
  const [from, setFrom] = useState(current);
  const [page, setPage] = useState<PublicationDraftPage>();
  useEffect(() => {
    onReadyChange?.(page?.draftId === draft.id && page.hash === draft.hash);
  }, [page, draft.id, draft.hash, onReadyChange]);
  const [loading, setLoading] = useState(false);
  const [itemsLoading, setItemsLoading] = useState<string[]>([]);
  const [error, setError] = useState("");
  const [failure, setFailure] = useState<Failure>();
  const serial = useRef(0);
  const abort = useRef<AbortController | undefined>(undefined);
  const panel = useRef<HTMLDivElement>(null);
  const scrollTo = useRef<string | undefined>(undefined);
  const scopeStart = draft.turns.findIndex((t) => t.id === scope?.start);
  const scopeEnd = draft.turns.findIndex((t) => t.id === scope?.end);
  const selected = (index: number) =>
    !scope || (scopeStart >= 0 && index >= scopeStart && index <= scopeEnd);
  const number = (id: string) =>
    originalTurns.findIndex((t) => t.id === id) + 1;
  const label = (id: string) => {
    const index = draft.turns.findIndex((t) => t.id === id);
    if (!scope) return draft.readingStartId === id ? "建议先读" : "将分享";
    if (!selected(index)) return "不分享";
    const boundary =
      id === scope.start && id === scope.end
        ? "起点 / 终点"
        : id === scope.start
          ? "起点"
          : id === scope.end
            ? "终点"
            : "将分享";
    return `${boundary}${id === scope.reading ? " · 建议先读" : ""}`;
  };
  async function read(
    request: ReadRequest,
    mode: Failure["mode"],
    token = serial.current,
  ) {
    const itemKey = `${request.turnId}:${request.itemId}`;
    if (mode === "item") setItemsLoading((keys) => [...keys, itemKey]);
    else setLoading(true);
    setError("");
    setFailure(undefined);
    try {
      const next = await api<PublicationDraftPage>(
        "publications/read-draft",
        {
          draftId: draft.id,
          ...request,
        },
        abort.current?.signal,
      );
      if (token !== serial.current) return;
      if (next.draftId !== draft.id || next.hash !== draft.hash)
        throw new Error("读取的内容与当前预览不一致，请重新选择来源。");
      setPage((previous) =>
        mode === "replace" || !previous
          ? next
          : {
              ...(mode === "append" ? next : previous),
              segments: mergeMaterialSegments(previous.segments, next.segments),
            },
      );
    } catch (e) {
      if (token === serial.current && !abort.current?.signal.aborted) {
        setError(errorText(e));
        setFailure({ request, mode });
      }
    } finally {
      if (token === serial.current) {
        if (mode === "item")
          setItemsLoading((keys) => keys.filter((key) => key !== itemKey));
        else setLoading(false);
      }
    }
  }
  useEffect(() => {
    abort.current = new AbortController();
    const token = ++serial.current;
    setItemsLoading([]);
    void read({ turnId: from }, "replace", token);
    return () => {
      serial.current++;
      abort.current?.abort();
    };
  }, [draft.id, from]);
  const findTurn = (id: string) =>
    Array.from(
      panel.current?.querySelectorAll<HTMLElement>("[data-publication-turn]") ||
        [],
    ).find((el) => el.dataset.publicationTurn === id);
  useLayoutEffect(() => {
    if (!page || !scrollTo.current) return;
    const element = findTurn(scrollTo.current);
    if (element) {
      element.scrollIntoView?.({ block: "start" });
      scrollTo.current = undefined;
    }
  }, [page]);
  function navigate(id: string) {
    setCurrent(id);
    const element = findTurn(id);
    if (element)
      element.scrollIntoView?.({ block: "start", behavior: "smooth" });
    else {
      scrollTo.current = id;
      setPage(undefined);
      if (id === from) void read({ turnId: id }, "replace");
      else setFrom(id);
    }
  }
  const segments = joinMaterialSegments(page?.segments || []);
  const visibleTurns = draft.turns.filter((t) =>
    segments.some((s) => s.turnId === t.id),
  );
  return (
    <div className="publication-reader" ref={panel}>
      <ReadingLayout
        label={scope ? "会话目录" : "分享内容目录"}
        toolbar={
          scope
            ? "点击目录查看正文，在每轮开头选择范围"
            : "同事只能读取下方范围，折叠的工具过程也会分享"
        }
        outline={draft.turns.map((turn, index) => ({
          id: turn.id,
          label: readingTitle(turn.label),
          number: number(turn.id),
          current: current === turn.id,
          selected: selected(index),
          meta: `${label(turn.id)}${turn.noticeCount ? ` · ${turn.noticeCount} 处导出说明` : ""}`,
          onSelect: () => navigate(turn.id),
        }))}
      >
        {visibleTurns.map((turn) => {
          const index = draft.turns.findIndex((t) => t.id === turn.id);
          const included = selected(index);
          return (
            <section
              className="publication-turn"
              data-publication-turn={turn.id}
              key={turn.id}
              aria-label={`第 ${number(turn.id)} 轮`}
            >
              <header className="publication-turn-heading">
                <div className="publication-turn-meta">
                  <span>第 {number(turn.id)} 轮</span>
                  <span className={`badge ${included ? "blue" : "muted"}`}>
                    {label(turn.id)}
                  </span>
                  {turn.status !== "completed" && <span>已结束但未完成</span>}
                </div>
                <h3>{readingTitle(turn.label, 100)}</h3>
                {scope && (
                  <div className="publication-boundaries">
                    <button
                      className="button small"
                      disabled={disabled}
                      aria-pressed={scope.start === turn.id}
                      onClick={() => scope.onStart(turn.id)}
                    >
                      从这轮开始
                    </button>
                    <button
                      className="button small"
                      disabled={disabled}
                      aria-pressed={scope.end === turn.id}
                      onClick={() => scope.onEnd(turn.id)}
                    >
                      到这轮结束
                    </button>
                    {included && (
                      <button
                        className="text-link small-text"
                        disabled={disabled}
                        aria-pressed={scope.reading === turn.id}
                        onClick={() =>
                          scope.onReading(
                            scope.reading === turn.id ? "" : turn.id,
                          )
                        }
                      >
                        {scope.reading === turn.id
                          ? "取消建议阅读起点"
                          : "建议同事从这轮读起"}
                      </button>
                    )}
                  </div>
                )}
              </header>
              {segments
                .filter((s) => s.turnId === turn.id)
                .map((segment) => (
                  <ReaderMessage
                    key={`${segment.itemId}:${segment.startOffset}`}
                    source={segment.text}
                    label={
                      segment.collapsed
                        ? `${itemLabel(segment.type)} · 已读 ${segment.endOffset} / ${segment.length} 字`
                        : itemLabel(segment.type)
                    }
                    notice={segment.notice}
                    target={{
                      kind: "material",
                      turnId: segment.turnId,
                      itemId: segment.itemId,
                      startOffset: segment.startOffset,
                      endOffset: segment.endOffset,
                      quote: segment.text,
                    }}
                    disabled
                    onAnnotate={() => {}}
                    onExpand={
                      segment.collapsed && segment.endOffset < segment.length
                        ? () =>
                            void read(
                              {
                                turnId: segment.turnId,
                                itemId: segment.itemId,
                                startOffset: segment.endOffset,
                              },
                              "item",
                            )
                        : undefined
                    }
                    expanding={itemsLoading.includes(
                      `${segment.turnId}:${segment.itemId}`,
                    )}
                    shownLength={segment.endOffset}
                  />
                ))}
            </section>
          );
        })}
        <ErrorBox
          message={error}
          retry={
            failure ? () => void read(failure.request, failure.mode) : undefined
          }
        />
        {loading && <Loading text="正在读取会话正文…" />}
        {page?.nextCursor && (
          <div className="publication-page-actions">
            <button
              className="button small"
              disabled={loading}
              onClick={() => void read({ cursor: page.nextCursor }, "append")}
            >
              继续读取正文
            </button>
            {!page.pageEndsAtTurnBoundary && (
              <span className="small-text muted">本轮尚未读完</span>
            )}
          </div>
        )}
        {page && !page.nextCursor && (
          <p className="small-text muted publication-reader-end">
            {scope ? "已到本次固定历史末尾。" : "已到分享范围末尾。"}
          </p>
        )}
      </ReadingLayout>
    </div>
  );
}
