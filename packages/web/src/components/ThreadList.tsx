import type { ThreadSummary } from "../types";
import { AgentIcon, ChevronIcon, GitIcon, PlusIcon } from "./Icons";

interface ThreadListProps {
  loading: boolean;
  threads: ThreadSummary[];
  canCreate: boolean;
  onOpen: (id: string) => void;
  onNew: () => void;
}

const formatter = new Intl.RelativeTimeFormat("en", { numeric: "auto" });

function relativeTime(value: string): string {
  const delta = new Date(value).getTime() - Date.now();
  const minutes = Math.round(delta / 60_000);
  if (Math.abs(minutes) < 60) return formatter.format(minutes, "minute");
  const hours = Math.round(minutes / 60);
  if (Math.abs(hours) < 24) return formatter.format(hours, "hour");
  return formatter.format(Math.round(hours / 24), "day");
}

export function ThreadList({
  loading,
  threads,
  canCreate,
  onOpen,
  onNew,
}: ThreadListProps) {
  return (
    <section className="page">
      <header className="page-header">
        <div>
          <p className="eyebrow">Workspace</p>
          <h1>Development threads</h1>
          <p>
            Each thread owns an isolated worktree, evidence trail, and Agent
            history.
          </p>
        </div>
        {canCreate ? (
          <button className="button primary" onClick={onNew} type="button">
            <PlusIcon /> Capture thread
          </button>
        ) : null}
      </header>

      <div className="thread-stats">
        <div>
          <strong>{threads.length}</strong>
          <span>threads</span>
        </div>
        <div>
          <strong>
            {threads.filter((thread) => thread.status === "running").length}
          </strong>
          <span>active Agents</span>
        </div>
        <div>
          <strong>
            {threads.filter((thread) => thread.status === "shared").length}
          </strong>
          <span>shared</span>
        </div>
      </div>

      <div className="thread-grid">
        {threads.map((thread) => (
          <button
            className="thread-card surface"
            key={thread.id}
            onClick={() => onOpen(thread.id)}
            type="button"
          >
            <span className="thread-card-top">
              <span className={`status-chip ${thread.status}`}>
                <i />
                {thread.status}
              </span>
              <small>{relativeTime(thread.updatedAt)}</small>
            </span>
            <strong>{thread.title}</strong>
            <span className="repo-line">
              <GitIcon size={15} /> {thread.repo}
            </span>
            <span className="thread-card-bottom">
              <span className="branch-pill">{thread.branch || "unborn"}</span>
              {thread.provider ? (
                <span className="provider-line">
                  <AgentIcon size={15} /> {thread.provider}
                </span>
              ) : (
                <span>No Agent</span>
              )}
              <ChevronIcon className="chevron" />
            </span>
          </button>
        ))}
      </div>

      {!loading && threads.length === 0 ? (
        <div className="empty-state surface">
          <span className="empty-mark">
            <GitIcon size={28} />
          </span>
          <h2>No handoff threads yet</h2>
          <p>
            Capture the current repository state without touching your original
            workspace.
          </p>
          {canCreate ? (
            <button className="button primary" onClick={onNew} type="button">
              Create the first thread
            </button>
          ) : null}
        </div>
      ) : null}
      {loading ? <div className="loading-block">Loading threads…</div> : null}
    </section>
  );
}
