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

export type ReadingTab = "materials" | "context";

const ReadingHeader = createContext<{
  toolbar: HTMLDivElement | null;
  active: boolean;
} | null>(null);

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

// Keep both readers mounted and scroll inside a stable viewport. Switching tabs
// restores only the reader's offset, never the surrounding document's position.
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
  context: ReactNode;
}) {
  const id = useId();
  const [toolbar, setToolbar] = useState<HTMLDivElement | null>(null);
  const buttons = useRef<Array<HTMLButtonElement | null>>([]);
  const panes = useRef<Partial<Record<ReadingTab, HTMLDivElement | null>>>({});
  const positions = useRef<Partial<Record<ReadingTab, number>>>({});
  const current = useRef(active);
  const navigation = useRef(navigationKey);
  const tabs = [
    { value: "materials", label: "已发布会话材料", icon: "book" },
    { value: "context", label: "协作上下文", icon: "terminal" },
  ] as const;

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
    if (pane) pane.scrollTop = positions.current[active] ?? 0;
  }, [active, navigationKey]);

  function select(tab: ReadingTab) {
    if (tab === active) return;
    const pane = panes.current[active];
    if (pane) positions.current[active] = pane.scrollTop;
    onChange(tab);
  }

  return (
    <section className="panel collaboration-reading" aria-label="协作阅读">
      <div className="reading-heading">
        <div className="reading-tabs" role="tablist" aria-label="协作阅读内容">
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
                      : (index + 1) % tabs.length;
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
        <div className="reading-toolbar" ref={setToolbar} />
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
          onScroll={(event) => {
            if (active === value)
              positions.current[value] = event.currentTarget.scrollTop;
          }}
        >
          <ReadingHeader.Provider value={{ toolbar, active: active === value }}>
            {value === "materials" ? materials : context}
          </ReadingHeader.Provider>
        </div>
      ))}
    </section>
  );
}
