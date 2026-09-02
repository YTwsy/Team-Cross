import type { AppInfo } from "../types";
import { CrossIcon, PlusIcon, PulseIcon, ThreadsIcon } from "./Icons";

interface AppShellProps {
  active: "threads" | "doctor";
  info?: AppInfo;
  onNavigate: (page: "threads" | "doctor") => void;
  onNewThread: () => void;
  children: React.ReactNode;
}

export function AppShell({
  active,
  info,
  onNavigate,
  onNewThread,
  children,
}: AppShellProps) {
  const transport = info?.selectedTransport;
  return (
    <div className="app-shell">
      <aside className="sidebar">
        <button
          className="brand"
          onClick={() => onNavigate("threads")}
          type="button"
        >
          <span className="brand-mark">
            <CrossIcon size={17} />
          </span>
          <span>
            <strong>Team Cross</strong>
            <small>Local collaboration</small>
          </span>
        </button>

        <nav aria-label="Primary navigation" className="sidebar-nav">
          <button
            className={active === "threads" ? "active" : ""}
            onClick={() => onNavigate("threads")}
            type="button"
          >
            <ThreadsIcon /> Threads
          </button>
          <button
            className={active === "doctor" ? "active" : ""}
            onClick={() => onNavigate("doctor")}
            type="button"
          >
            <PulseIcon /> Setup & Doctor
          </button>
        </nav>

        {info?.mode === "host" ? (
          <button
            className="new-thread-button"
            onClick={onNewThread}
            type="button"
          >
            <PlusIcon size={17} /> New thread
          </button>
        ) : null}

        <div className="sidebar-footer">
          <div className="host-status">
            <span className="live-dot" />
            <span>
              <strong>
                {info?.mode === "join" ? "Connected" : "Host online"}
              </strong>
              <small>{transport ? `via ${transport}` : "Loopback only"}</small>
            </span>
          </div>
          <div className="version">v{info?.version ?? "0.1.0"} prototype</div>
        </div>
      </aside>
      <main className="main-area">{children}</main>
    </div>
  );
}
