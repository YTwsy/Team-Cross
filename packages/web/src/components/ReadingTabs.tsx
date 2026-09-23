import { t } from "../i18n";
import {
  createContext,
  useContext,
  useId,
  useLayoutEffect,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { createPortal } from "react-dom";
import { Icon } from "./ui";
import {
  captureReadingPosition,
  readingViewport,
  restoreReadingPosition,
  type ReadingPosition,
} from "../reading-position";

export type ReadingTab = "materials" | "context";

const ReadingHeader = createContext<{
  toolbar: HTMLDivElement | null;
  tools: HTMLDivElement | null;
  active: boolean;
} | null>(null);

export function ReadingPanelTools({ children }: { children: ReactNode }) {
  const shared = useContext(ReadingHeader);
  if (!shared) return children;
  return shared.tools
    ? createPortal(
        <div hidden={!shared.active} className="reading-tools-content">
          {children}
        </div>,
        shared.tools,
      )
    : null;
}

// Standalone readers keep their heading; embedded readers share one toolbar.
export function ReadingPanelHeading({
  title,
  children,
}: {
  title: ReactNode;
  children: ReactNode;
}) {
  const shared = useContext(ReadingHeader);
  if (shared)
    return shared.toolbar
      ? createPortal(
          <div className="reading-heading-actions" hidden={!shared.active}>
            {children}
          </div>,
          shared.toolbar,
        )
      : null;
  return (
    <div className="panel-heading">
      <h2>{title}</h2>
      {children}
    </div>
  );
}

// Keep both readers mounted, with the document as the single reading scroller.
export function ReadingTabs({
  active,
  onChange,
  materialCount,
  navigationKey,
  materials,
  context,
}: {
  active: ReadingTab;
  onChange: (tab: ReadingTab) => void;
  materialCount: number;
  navigationKey?: number;
  materials: ReactNode;
  context?: ReactNode;
}) {
  const id = useId();
  const [toolbar, setToolbar] = useState<HTMLDivElement | null>(null);
  const [tools, setTools] = useState<HTMLDivElement | null>(null);
  const section = useRef<HTMLElement>(null);
  const heading = useRef<HTMLDivElement>(null);
  const buttons = useRef<Array<HTMLButtonElement | null>>([]);
  const panes = useRef<Partial<Record<ReadingTab, HTMLDivElement | null>>>({});
  const positions = useRef<Partial<Record<ReadingTab, ReadingPosition>>>({});
  const wasReading = useRef(false);
  const current = useRef(active);
  const navigation = useRef(navigationKey);
  const tabs = [
    { value: "materials", label: t("已发布会话材料"), icon: "book" },
    { value: "context", label: t("协作上下文"), icon: "terminal" },
  ].filter(
    (tab) => tab.value === "materials" || context !== undefined,
  ) as Array<{ value: ReadingTab; label: string; icon: "book" | "terminal" }>;

  useLayoutEffect(() => {
    const root = section.current,
      bar = heading.current;
    if (!root || !bar) return;
    const measure = () =>
      root.style.setProperty(
        "--reading-header-height",
        `${bar.getBoundingClientRect().height}px`,
      );
    measure();
    const observer =
      typeof ResizeObserver !== "undefined"
        ? new ResizeObserver(measure)
        : undefined;
    observer?.observe(bar);
    return () => observer?.disconnect();
  });

  useLayoutEffect(() => {
    const changed = current.current !== active;
    const locating = navigation.current !== navigationKey;
    current.current = active;
    navigation.current = navigationKey;
    if (!changed) return;
    window.getSelection()?.removeAllRanges();
    // Annotation navigation owns the destination; do not restore an old offset.
    if (locating) return;
    const pane = panes.current[active];
    if (pane && positions.current[active])
      restoreReadingPosition(pane, positions.current[active]);
    else if (pane && wasReading.current) {
      const viewport = readingViewport(pane);
      const top =
        window.scrollY +
        section.current!.getBoundingClientRect().top -
        viewport.stickyTop;
      window.scrollTo({ top, behavior: "instant" });
    }
  }, [active, navigationKey]);

  function select(tab: ReadingTab) {
    if (tab === active) return;
    const pane = panes.current[active];
    if (pane) {
      positions.current[active] = captureReadingPosition(pane);
      wasReading.current =
        section.current!.getBoundingClientRect().top <=
        readingViewport(pane).stickyTop;
    }
    onChange(tab);
  }

  return (
    <section
      ref={section}
      className="panel collaboration-reading"
      aria-label={t("协作阅读")}
    >
      <div className="reading-heading" ref={heading}>
        <div className="reading-heading-row">
          {tabs.length === 1 ? (
            <h2 id={`${id}-materials-tab`}>
              {t("已发布会话材料") + " "}
              <span className="count">{materialCount}</span>
            </h2>
          ) : (
            <div
              className="reading-tabs"
              role="tablist"
              aria-label={t("协作阅读内容")}
            >
              {tabs.map(({ value, label, icon }, index) => (
                <button
                  key={value}
                  ref={(button) => {
                    buttons.current[index] = button;
                  }}
                  id={`${id}-${value}-tab`}
                  role="tab"
                  aria-selected={active === value}
                  aria-controls={`${id}-${value}-panel`}
                  tabIndex={active === value ? 0 : -1}
                  onClick={() => select(value)}
                  onKeyDown={(event) => {
                    if (
                      !["ArrowLeft", "ArrowRight", "Home", "End"].includes(
                        event.key,
                      )
                    )
                      return;
                    event.preventDefault();
                    const next =
                      event.key === "Home"
                        ? 0
                        : event.key === "End"
                          ? tabs.length - 1
                          : (index +
                              (event.key === "ArrowLeft" ? -1 : 1) +
                              tabs.length) %
                            tabs.length;
                    select(tabs[next]!.value);
                    buttons.current[next]?.focus({ preventScroll: true });
                  }}
                >
                  <Icon name={icon} size={17} />
                  {label}
                  {value === "materials" && (
                    <span className="count">{materialCount}</span>
                  )}
                </button>
              ))}
            </div>
          )}
          <div className="reading-toolbar" ref={setToolbar} />
        </div>
        <div className="reading-tools" ref={setTools} />
      </div>
      {tabs.map(({ value }) => (
        <div
          key={value}
          id={`${id}-${value}-panel`}
          className="reading-pane"
          role="tabpanel"
          aria-labelledby={`${id}-${value}-tab`}
          hidden={active !== value}
          tabIndex={0}
          ref={(pane) => {
            panes.current[value] = pane;
          }}
        >
          <ReadingHeader.Provider
            value={{ toolbar, tools, active: active === value }}
          >
            {value === "materials" ? materials : context}
          </ReadingHeader.Provider>
        </div>
      ))}
    </section>
  );
}
