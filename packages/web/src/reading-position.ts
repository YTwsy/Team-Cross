// Reading can live in the document, the library preview, or a publication dialog.
// A reading pane is deliberately not a scroll container.
export function readingViewport(element: HTMLElement) {
  const container = element.closest<HTMLElement>(".library-preview, dialog");
  const workspace = element.closest<HTMLElement>(".collaboration-reading");
  const heading = workspace?.querySelector<HTMLElement>(".reading-heading");
  const stickyTop = workspace
    ? parseFloat(
        getComputedStyle(workspace).getPropertyValue("--reading-sticky-top"),
      ) || 0
    : 0;
  return {
    container,
    workspace,
    stickyTop,
    top:
      Math.max(
        container?.getBoundingClientRect().top || 0,
        heading ? stickyTop + heading.getBoundingClientRect().height : 0,
      ) + 12,
    scrollTop: container ? container.scrollTop : window.scrollY,
  };
}

export type ReadingPosition = {
  scrollTop: number;
  key?: string;
  block?: number;
  offset?: number;
};

const blocks = (element: HTMLElement) =>
  Array.from(
    element.querySelectorAll<HTMLElement>(
      ".reader-markdown p, .reader-markdown li, .reader-markdown h1, .reader-markdown h2, .reader-markdown h3, .reader-markdown h4, .reader-markdown pre, .reader-markdown table, .reader-raw",
    ),
  );

export function captureReadingPosition(
  root: HTMLElement | null,
): ReadingPosition | undefined {
  if (!root || root.closest("[hidden]")) return;
  const viewport = readingViewport(root);
  const position: ReadingPosition = { scrollTop: viewport.scrollTop };
  // While the overview is visible, switching content must not scroll past it.
  if (
    viewport.workspace &&
    viewport.workspace.getBoundingClientRect().top > viewport.stickyTop
  )
    return position;
  const anchor = Array.from(
    root.querySelectorAll<HTMLElement>("[data-reading-key]"),
  ).find((element) => element.getBoundingClientRect().bottom > viewport.top);
  if (!anchor) return position;
  const children = blocks(anchor);
  const block = children.findIndex(
    (element) => element.getBoundingClientRect().bottom > viewport.top,
  );
  const target = block >= 0 ? children[block]! : anchor;
  return {
    ...position,
    key: anchor.dataset.readingKey,
    block,
    offset: target.getBoundingClientRect().top - viewport.top,
  };
}

export function restoreReadingPosition(
  root: HTMLElement | null,
  position?: ReadingPosition,
) {
  if (!root || !position || root.closest("[hidden]")) return;
  const viewport = readingViewport(root);
  const anchor =
    position.key &&
    Array.from(root.querySelectorAll<HTMLElement>("[data-reading-key]")).find(
      (element) => element.dataset.readingKey === position.key,
    );
  const target = anchor && (blocks(anchor)[position.block ?? -1] || anchor);
  const top = target
    ? viewport.scrollTop +
      target.getBoundingClientRect().top -
      viewport.top -
      (position.offset || 0)
    : position.scrollTop;
  if (Math.abs(top - viewport.scrollTop) < 1) return;
  if (viewport.container) viewport.container.scrollTop = top;
  else window.scrollTo({ top, behavior: "instant" });
}
