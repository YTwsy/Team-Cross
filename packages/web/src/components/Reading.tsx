import {
  useCallback,
  useEffect,
  useId,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { createPortal } from "react-dom";
import {
  autoUpdate,
  flip,
  inline,
  offset,
  shift,
  useFloating,
} from "@floating-ui/react";
import type { Annotation, AnnotationTarget } from "../types";
import { mappedSelection, sameMessage } from "../reading";
import { MarkdownText } from "./MarkdownText";
import { Icon } from "./ui";

export type AnnotationOrigin = { element: HTMLElement; range?: Range };
export type Annotate = (
  target: AnnotationTarget,
  origin?: AnnotationOrigin,
) => void;
export type Discuss = (annotationId: string, origin: AnnotationOrigin) => void;

export function useReadingPosition(origin?: AnnotationOrigin, toolbar = false) {
  const floating = useFloating({
    placement: toolbar ? "bottom-start" : "right-start",
    strategy: "fixed",
    middleware: [
      inline(),
      offset(10),
      flip({
        fallbackPlacements: ["bottom-start", "top-start"],
        padding: 12,
        crossAxis: toolbar,
      }),
      shift({ padding: 12 }),
    ],
    whileElementsMounted: (reference, element, update) =>
      autoUpdate(reference, element, update, {
        elementResize: typeof ResizeObserver !== "undefined",
        layoutShift: typeof IntersectionObserver !== "undefined",
      }),
  });
  const setReference = floating.refs.setPositionReference;
  useEffect(() => {
    const bounds = () => {
      const element = origin!.element.getBoundingClientRect();
      const selected = origin!.range?.getBoundingClientRect?.();
      const valid = selected && (selected.width || selected.height);
      return toolbar
        ? valid
          ? selected
          : element
        : new DOMRect(
            element.x,
            valid ? selected.top : element.top,
            element.width,
            valid ? selected.height : 0,
          );
    };
    setReference(
      origin?.element.getClientRects().length
        ? {
            contextElement: origin.element,
            getBoundingClientRect: bounds,
            getClientRects: () =>
              toolbar
                ? origin.range?.getClientRects?.() ||
                  origin.element.getClientRects()
                : [bounds()],
          }
        : null,
    );
  }, [origin, toolbar, setReference]);
  return floating;
}

export function SelectionAction({
  origin,
  onClick,
}: {
  origin?: AnnotationOrigin;
  onClick: () => void;
}) {
  const { refs, floatingStyles } = useReadingPosition(origin, true);
  if (!origin) return null;
  return createPortal(
    <div
      ref={refs.setFloating}
      style={floatingStyles}
      className="selection-action"
    >
      <button
        className="button primary small"
        onMouseDown={(e) => e.preventDefault()}
        onClick={onClick}
      >
        <Icon name="comment" size={14} />
        批注所选内容
      </button>
    </div>,
    document.body,
  );
}

export function AnchoredNote({
  origin,
  children,
  onClose,
  label,
}: {
  origin: AnnotationOrigin;
  children: ReactNode;
  onClose: () => void;
  label: string;
}) {
  const { refs, floatingStyles } = useReadingPosition(origin);
  const [mount, setMount] = useState<HTMLElement>(document.body);
  const card = useRef<HTMLDivElement>(null);
  const setFloating = refs.setFloating;
  const setCard = useCallback(
    (node: HTMLDivElement | null) => {
      card.current = node;
      setFloating(mount === document.body ? node : null);
    },
    [mount, setFloating],
  );
  useEffect(() => {
    (
      card.current?.querySelector<HTMLElement>("textarea") ||
      card.current?.querySelector<HTMLElement>("button")
    )?.focus({ preventScroll: true });
    if (mount !== document.body)
      card.current?.scrollIntoView?.({ block: "nearest", behavior: "smooth" });
  }, [mount]);
  useEffect(() => {
    const narrow = window.matchMedia("(max-width: 1100px)");
    const host = document.createElement("div");
    host.className = "annotation-inline-mount";
    const place = () => {
      if (narrow.matches && origin.element.isConnected) {
        const endpoint = origin.range?.endContainer;
        const parent =
          endpoint instanceof Element ? endpoint : endpoint?.parentElement;
        const block =
          parent?.closest(".reader-code, .reader-table, blockquote, ul, ol") ||
          parent?.closest("p, h1, h2, h3, h4, h5, h6");
        const anchor =
          block && origin.element.contains(block)
            ? block
            : origin.element.closest(".reader-message") || origin.element;
        anchor.after(host);
        setMount(host);
      } else setMount(document.body);
    };
    place();
    narrow.addEventListener("change", place);
    const observer = new MutationObserver(() => {
      if (!origin.element.isConnected) onClose();
    });
    observer.observe(document.body, { childList: true, subtree: true });
    return () => {
      narrow.removeEventListener("change", place);
      observer.disconnect();
      host.remove();
    };
  }, [origin, onClose]);
  return createPortal(
    <div
      ref={setCard}
      style={mount === document.body ? floatingStyles : undefined}
      className={`annotation-popover ${mount === document.body ? "" : "annotation-inline"}`}
      role="region"
      aria-label={label}
      onKeyDown={(e) => {
        if (e.key === "Escape" && !e.nativeEvent.isComposing) {
          e.stopPropagation();
          onClose();
        }
      }}
    >
      <div className="annotation-popover-heading">
        <strong>{label}</strong>
        <button
          type="button"
          className="icon-button"
          aria-label="收起原文旁批注"
          onClick={onClose}
        >
          <Icon name="close" size={15} />
        </button>
      </div>
      {children}
    </div>,
    mount,
  );
}

export function ReadingLayout({
  outline,
  children,
  label = "本篇目录",
}: {
  outline: { id: string; label: string; onSelect: () => void }[];
  children: ReactNode;
  label?: string;
}) {
  const [focus, setFocus] = useState(false);
  return (
    <div className={`reading-surface ${focus ? "reader-focus" : ""}`}>
      <div className="reader-toolbar">
        <span>选中文字可批注</span>
        <button
          className="text-button"
          onClick={() => setFocus(!focus)}
          aria-pressed={focus}
        >
          {focus ? "退出专注阅读" : "专注阅读"}
        </button>
      </div>
      <div className="reader-layout">
        {!!outline.length && (
          <details className="reader-outline" open>
            <summary>
              {label} <span className="count">{outline.length}</span>
            </summary>
            <nav aria-label={label}>
              {outline.map((turn, i) => (
                <button key={turn.id} onClick={turn.onSelect}>
                  <span>{String(i + 1).padStart(2, "0")}</span>
                  {turn.label}
                </button>
              ))}
            </nav>
          </details>
        )}
        <div className="reader-content">{children}</div>
      </div>
    </div>
  );
}

export function ReaderMessage({
  source,
  target,
  activeTarget,
  label,
  annotations = [],
  onAnnotate,
  onDiscuss,
  disabled,
  notice,
  actionLabel = "批注这条消息",
}: {
  source: string;
  target: AnnotationTarget;
  activeTarget?: AnnotationTarget;
  label: string;
  annotations?: Annotation[];
  onAnnotate: Annotate;
  onDiscuss?: Discuss;
  disabled?: boolean;
  notice?: string;
  actionLabel?: string;
}) {
  const [raw, setRaw] = useState(false),
    [hint, setHint] = useState("");
  const [selected, setSelected] = useState<{
    target: AnnotationTarget;
    origin: AnnotationOrigin;
  }>();
  const content = useRef<HTMLDivElement>(null),
    message = useRef<HTMLDivElement>(null);
  const id = useId();
  const base = target.startOffset || 0;
  const targetKey = JSON.stringify([
    target.kind,
    target.materialId,
    target.version,
    target.sessionId,
    target.turnId,
    target.itemId,
  ]);
  const notes = useMemo(
    () => annotations.filter((a) => a.target && sameMessage(a.target, target)),
    [annotations, targetKey],
  );
  const highlights = useMemo(
    () =>
      [
        ...notes.map((n) => n.target!),
        ...(activeTarget && sameMessage(activeTarget, target)
          ? [activeTarget]
          : []),
      ]
        .filter((t) => {
          const start = (t.startOffset || 0) - base,
            end = (t.endOffset || 0) - base;
          return (
            start >= 0 &&
            end <= source.length &&
            source.slice(start, end) === t.quote
          );
        })
        .map((t) => ({
          start: (t.startOffset || 0) - base,
          end: (t.endOffset || 0) - base,
          active: t === activeTarget,
        })),
    [source, base, activeTarget, targetKey, notes],
  );
  useEffect(() => {
    if (!selected) return;
    const clear = () => {
      if (window.getSelection()?.isCollapsed) setSelected(undefined);
    };
    document.addEventListener("selectionchange", clear);
    return () => document.removeEventListener("selectionchange", clear);
  }, [selected]);
  function capture(event: { target: EventTarget | null }) {
    if (
      event.target instanceof Element &&
      event.target.closest(".annotation-inline-mount, button, textarea, input")
    )
      return;
    if (disabled || !target.itemId || !content.current) return;
    const range = window.getSelection();
    const value = mappedSelection(content.current, source);
    if (!value || !range?.rangeCount) {
      setSelected(undefined);
      setHint(
        range?.toString().trim()
          ? "请选择同一条消息中的文字（最多 8000 字）。复杂排版可切换到原文后选择。"
          : "",
      );
      return;
    }
    setHint("");
    setSelected({
      target: {
        ...target,
        ...value,
        startOffset: base + value.startOffset,
        endOffset: base + value.endOffset,
      },
      origin: {
        element: content.current,
        range: range.getRangeAt(0).cloneRange(),
      },
    });
  }
  const contentBody = (
    <>
      <div className="message-heading">
        <span className="reader-role">{label}</span>
        <div className="reader-message-actions">
          <button
            className="text-button"
            onClick={() => {
              setRaw(!raw);
              setSelected(undefined);
            }}
            aria-pressed={raw}
          >
            {raw ? "阅读排版" : "显示原文"}
          </button>
          {!disabled && target.itemId && source.trim() && (
            <button
              className="message-annotate"
              onClick={() => {
                const quote = Array.from(source).slice(0, 8000).join("");
                onAnnotate(
                  {
                    ...target,
                    quote,
                    startOffset: base,
                    endOffset: base + quote.length,
                  },
                  { element: content.current! },
                );
              }}
            >
              <Icon name="comment" size={13} />
              {actionLabel}
            </button>
          )}
        </div>
      </div>
      {notice && <p className="inline-note">{notice}</p>}
      <div
        ref={content}
        data-message-text
        data-turn-id={target.turnId}
        data-item-id={target.itemId}
        onMouseUp={capture}
        onKeyUp={capture}
      >
        <MarkdownText
          source={source}
          highlights={highlights}
          raw={
            raw ||
            ![
              "用户",
              "Codex",
              "Claude Code",
              "回复",
              "提问",
              "用户提问",
              "助手回复",
            ].includes(label)
          }
        />
      </div>
      {hint && (
        <p className="small-text muted" role="status">
          {hint}
        </p>
      )}
      {!!notes.length && (
        <div className="reader-note-links" aria-label="原文批注">
          {notes.map((note) => (
            <button
              key={note.id}
              className="text-button"
              onClick={() =>
                onDiscuss?.(note.id, { element: message.current! })
              }
            >
              <Icon name="comment" size={13} />
              {note.author} · {note.text.slice(0, 30)}
              {note.text.length > 30 ? "…" : ""}
            </button>
          ))}
        </div>
      )}
      <SelectionAction
        origin={selected?.origin}
        onClick={() => {
          if (selected) {
            onAnnotate(selected.target, selected.origin);
            setSelected(undefined);
          }
        }}
      />
    </>
  );
  const tool = ![
    "用户",
    "Codex",
    "Claude Code",
    "回复",
    "提问",
    "用户提问",
    "助手回复",
  ].includes(label);
  return (
    <div
      ref={message}
      id={id}
      className={`reader-message ${tool ? "reader-tool-message" : ""}`}
    >
      {tool ? (
        <details
          className="reader-tool"
          open={highlights.some((h) => h.active) || undefined}
        >
          <summary>
            {label} <span>查看已保存的过程</span>
          </summary>
          {contentBody}
        </details>
      ) : (
        contentBody
      )}
    </div>
  );
}
