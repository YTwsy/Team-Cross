import {
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  type RefObject,
} from "react";
import {
  captureReadingPosition,
  readingViewport,
  restoreReadingPosition,
  type ReadingPosition,
} from "./reading-position";

// Shared by the document and nested material/publication readers. Follow is
// driven by the reader's scroll intent, never by arrival of a network response.
export function useReaderFollow(
  root: RefObject<HTMLElement | null>,
  key: string,
  live = false,
  suspended = false,
) {
  const following = useRef(false),
    position = useRef<ReadingPosition | undefined>(undefined);
  const [atEnd, setAtEnd] = useState(false);
  const state = useRef({ live, suspended });
  state.current = { live, suspended };
  function blocked() {
    return (
      state.current.suspended ||
      (!window.getSelection()?.isCollapsed &&
        !!window.getSelection()?.toString()) ||
      !!document.querySelector("dialog[open]") ||
      !!document.activeElement?.matches(
        "input, textarea, [contenteditable=true]",
      )
    );
  }
  function canFollow() {
    return state.current.live && following.current && !blocked();
  }
  function toEnd() {
    const element = root.current;
    if (!element || element.closest("[hidden]")) return;
    following.current = true;
    setAtEnd(true);
    const viewport = readingViewport(element);
    const end =
      viewport.container?.getBoundingClientRect().bottom || window.innerHeight;
    const top =
      viewport.scrollTop + element.getBoundingClientRect().bottom - end;
    if (viewport.container) viewport.container.scrollTop = top;
    else window.scrollTo({ top: Math.max(0, top), behavior: "instant" });
    position.current = captureReadingPosition(element);
  }
  useEffect(() => {
    const element = root.current;
    if (!element) return;
    following.current = false;
    setAtEnd(false);
    const viewport = readingViewport(element),
      scroll = viewport.container || window;
    const remember = () => {
      if (element.closest("[hidden]") || state.current.suspended) return;
      position.current = captureReadingPosition(element);
      const rect = element.getBoundingClientRect();
      const bottom =
        viewport.container?.getBoundingClientRect().bottom ||
        window.innerHeight;
      const end =
        rect.height > 0 && rect.bottom <= bottom + 80 && rect.bottom > 0;
      following.current = end;
      setAtEnd(end);
    };
    const interact = (event: Event) => {
      position.current = captureReadingPosition(element);
      if (
        event.target instanceof Element &&
        event.target.closest(
          "button, summary, input, textarea, [data-message-text]",
        )
      ) {
        following.current = false;
        setAtEnd(false);
      }
    };
    const selection = () => {
      if (window.getSelection()?.toString()) {
        following.current = false;
        setAtEnd(false);
      }
    };
    scroll.addEventListener("scroll", remember, { passive: true });
    element.addEventListener("pointerdown", interact, true);
    element.addEventListener("keydown", interact, true);
    document.addEventListener("selectionchange", selection);
    let frame = 0,
      initialized = false;
    const observer =
      typeof ResizeObserver === "undefined"
        ? undefined
        : new ResizeObserver(() => {
            cancelAnimationFrame(frame);
            frame = requestAnimationFrame(() => {
              if (element.closest("[hidden]") || state.current.suspended)
                return;
              if (
                !initialized &&
                element.querySelector("[data-reading-key]") &&
                element.getBoundingClientRect().height > 0
              ) {
                initialized = true;
                remember();
                return;
              }
              if (canFollow()) toEnd();
              else restoreReadingPosition(element, position.current);
              position.current = captureReadingPosition(element);
            });
          });
    observer?.observe(element);
    position.current = captureReadingPosition(element);
    return () => {
      observer?.disconnect();
      cancelAnimationFrame(frame);
      scroll.removeEventListener("scroll", remember);
      element.removeEventListener("pointerdown", interact, true);
      element.removeEventListener("keydown", interact, true);
      document.removeEventListener("selectionchange", selection);
    };
  }, [key]);
  useLayoutEffect(() => {
    if (!position.current)
      position.current = captureReadingPosition(root.current);
  });
  return { atEnd, canFollow, toEnd };
}
