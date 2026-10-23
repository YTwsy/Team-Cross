import { useEffect, useRef, useState } from "react";
import { api, errorText } from "../api";
import { t, tr } from "../i18n";
import { ErrorBox, Icon } from "./ui";
import type { ReaderResource } from "./ReaderMedia";

export type ReaderMatch = {
  turnId: string;
  itemId: string;
  startOffset: number;
  endOffset: number;
  quote: string;
  snippet: string;
  pageCursor?: string;
};
type SearchPage = {
  matches: ReaderMatch[];
  nextCursor: string;
  complete: boolean;
};

export function ReaderFind({
  resource,
  onSelect,
  onActiveChange,
}: {
  resource: ReaderResource;
  onSelect: (match: ReaderMatch, signal: AbortSignal) => Promise<void>;
  onActiveChange?: (open: boolean) => void;
}) {
  const [open, setOpen] = useState(false),
    [query, setQuery] = useState(""),
    [tools, setTools] = useState(false);
  const [matches, setMatches] = useState<ReaderMatch[]>([]),
    [current, setCurrent] = useState(-1);
  const [busy, setBusy] = useState(false),
    [locating, setLocating] = useState(false),
    [complete, setComplete] = useState(false),
    [error, setError] = useState("");
  const [batch, setBatch] = useState(0);
  const [more, setMore] = useState(false),
    [continuation, setContinuation] = useState(0);
  const scan = useRef({ key: "", cursor: "" });
  const root = useRef<HTMLDivElement>(null),
    input = useRef<HTMLInputElement>(null);
  const callbacks = useRef({ onSelect, onActiveChange });
  callbacks.current = { onSelect, onActiveChange };
  const generation = useRef(0);
  const selection = useRef<AbortController | undefined>(undefined);
  const openRef = useRef(false);
  const key = JSON.stringify(resource);
  const toggle = (value: boolean) => {
    if (openRef.current === value) return;
    openRef.current = value;
    if (!value) selection.current?.abort();
    setOpen(value);
    callbacks.current.onActiveChange?.(value);
  };
  useEffect(() => {
    const keydown = (e: KeyboardEvent) => {
      if (
        !(e.metaKey || e.ctrlKey) ||
        e.key.toLowerCase() !== "f" ||
        root.current?.closest("[hidden]")
      )
        return;
      if (
        document.querySelector("dialog[open]") &&
        !root.current?.closest("dialog[open]")
      )
        return;
      if (root.current && root.current.getBoundingClientRect().width > 0) {
        e.preventDefault();
        toggle(true);
        requestAnimationFrame(() => input.current?.focus());
      }
    };
    document.addEventListener("keydown", keydown);
    return () => document.removeEventListener("keydown", keydown);
  }, []);
  useEffect(() => {
    if (open) input.current?.focus();
  }, [open]);
  useEffect(() => {
    selection.current?.abort();
    const serial = ++generation.current,
      abort = new AbortController();
    const searchKey = JSON.stringify([open, query.trim(), tools, key, batch]);
    if (scan.current.key !== searchKey) {
      scan.current = { key: searchKey, cursor: "" };
      setMatches([]);
      setCurrent(-1);
      setComplete(false);
    }
    setError("");
    setBusy(false);
    setMore(false);
    if (!open || !query.trim()) return () => abort.abort();
    const timer = setTimeout(() => {
      setBusy(true);
      void (async () => {
        let cursor = scan.current.cursor,
          count = 0;
        const seen = new Set<string>();
        do {
          const search = { query: query.trim(), includeTools: tools, cursor };
          const path = resource.body
            ? resource.path
            : `${resource.path}&${new URLSearchParams({ search: search.query, includeTools: String(tools), searchCursor: cursor })}`;
          const response = await api<{ search: SearchPage }>(
            path,
            resource.body ? { ...resource.body, search } : undefined,
            abort.signal,
          );
          if (abort.signal.aborted || serial !== generation.current) return;
          const page = response.search;
          if (!page || !Array.isArray(page.matches))
            throw new Error(t("搜索响应无效"));
          setMatches((previous) => [...previous, ...page.matches]);
          count += page.matches.length;
          setComplete(page.complete);
          cursor = page.nextCursor;
          if (!page.complete && (!cursor || seen.has(cursor)))
            throw new Error(t("搜索分页没有推进"));
          seen.add(cursor);
          if (page.complete) break;
          if (count >= 1024) {
            scan.current.cursor = cursor;
            setMore(true);
            break;
          }
        } while (!abort.signal.aborted);
      })()
        .catch((e) => {
          if (!abort.signal.aborted) setError(errorText(e));
        })
        .finally(() => {
          if (serial === generation.current) setBusy(false);
        });
    }, 200);
    return () => {
      abort.abort();
      selection.current?.abort();
      clearTimeout(timer);
      generation.current++;
    };
  }, [open, query, tools, key, batch, continuation]);
  async function select(index: number) {
    if (!matches.length || locating) return;
    const next = (index + matches.length) % matches.length;
    const abort = new AbortController();
    selection.current?.abort();
    selection.current = abort;
    setLocating(true);
    setError("");
    try {
      await callbacks.current.onSelect(matches[next]!, abort.signal);
      if (!abort.signal.aborted) setCurrent(next);
    } catch (e) {
      if (!abort.signal.aborted) setError(errorText(e));
    } finally {
      setLocating(false);
    }
  }
  return (
    <div
      ref={root}
      className="reader-find"
      data-reader-find-active={open || undefined}
    >
      {!open ? (
        <button className="text-button" onClick={() => toggle(true)}>
          <Icon name="search" size={14} />
          {t("搜索正文")}
        </button>
      ) : (
        <>
          <div className="reader-find-controls">
            <input
              ref={input}
              type="search"
              aria-label={t("搜索当前会话或材料版本")}
              placeholder={t("搜索正文")}
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter") {
                  e.preventDefault();
                  void select(current + (e.shiftKey ? -1 : 1));
                } else if (e.key === "Escape") {
                  e.stopPropagation();
                  toggle(false);
                }
              }}
            />
            <label>
              <input
                type="checkbox"
                checked={tools}
                onChange={(e) => setTools(e.target.checked)}
              />
              {t("包含工具输出")}
            </label>
            <button
              disabled={!matches.length || locating}
              onClick={() => void select(current - 1)}
              aria-label={t("上一个命中")}
            >
              ↑
            </button>
            <button
              disabled={!matches.length || locating}
              onClick={() => void select(current + 1)}
              aria-label={t("下一个命中")}
            >
              ↓
            </button>
            <button onClick={() => toggle(false)} aria-label={t("关闭搜索")}>
              <Icon name="close" size={14} />
            </button>
          </div>
          {!!query.trim() && (
            <div className="reader-find-status" role="status">
              {complete
                ? tr`共 ${matches.length} 处命中`
                : tr`已找到 ${matches.length} 处，搜索尚未完成`}
              {current >= 0 && ` · ${current + 1}/${matches.length}`}
              {busy && ` · ${t("搜索中…")}`}
              {locating && ` · ${t("正在定位…")}`}
              <button
                className="text-button"
                onClick={() => setBatch((n) => n + 1)}
              >
                {t("重新搜索")}
              </button>
            </div>
          )}
          {current >= 0 && matches[current] && (
            <p className="reader-find-snippet">{matches[current]!.snippet}</p>
          )}
          {more && (
            <button
              className="text-button"
              onClick={() => setContinuation((n) => n + 1)}
            >
              {t("继续搜索更多命中")}
            </button>
          )}
          <ErrorBox message={error} retry={() => setBatch((n) => n + 1)} />
        </>
      )}
    </div>
  );
}
