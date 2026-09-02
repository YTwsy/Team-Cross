import { useEffect, useState } from "react";
import { api } from "../api";
import type { StoredSession } from "../types";

export function ImportSessionModal({
  open,
  onClose,
  onImport,
}: {
  open: boolean;
  onClose: () => void;
  onImport: (provider: string, sessionId: string) => Promise<void>;
}) {
  const [sessions, setSessions] = useState<StoredSession[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    if (!open) return;
    let active = true;
    setSessions([]);
    setError("");
    void api
      .storedSessions()
      .then((value) => {
        if (active) setSessions(value);
      })
      .catch((reason: unknown) => {
        if (active)
          setError(
            reason instanceof Error
              ? reason.message
              : "Unable to list stored Sessions",
          );
      });
    return () => {
      active = false;
    };
  }, [open]);

  if (!open) return null;
  return (
    <div
      className="modal-backdrop"
      role="presentation"
      onMouseDown={(event) => {
        if (event.target === event.currentTarget) onClose();
      }}
    >
      <section
        aria-labelledby="import-title"
        aria-modal="true"
        className="modal surface"
        role="dialog"
      >
        <header>
          <div>
            <p className="eyebrow">Read-only context</p>
            <h2 id="import-title">Import an old Session</h2>
          </div>
          <button
            aria-label="Close"
            className="modal-close"
            onClick={onClose}
            type="button"
          >
            ×
          </button>
        </header>
        <p>
          The original transcript is captured as sourced evidence. Team Cross
          creates a new managed Session and never resumes the external native
          ID.
        </p>
        <div className="session-list">
          {sessions.map((session) => (
            <button
              disabled={busy}
              key={`${session.provider}-${session.id}`}
              onClick={async () => {
                setBusy(true);
                try {
                  await onImport(session.provider, session.id);
                  onClose();
                } catch (reason) {
                  setError(
                    reason instanceof Error ? reason.message : "Import failed",
                  );
                } finally {
                  setBusy(false);
                }
              }}
              type="button"
            >
              <span className={`provider-logo ${session.provider}`}>
                {session.provider.slice(0, 1).toUpperCase()}
              </span>
              <span>
                <strong>{session.title}</strong>
                <small>
                  {session.provider} · {session.cwd ?? "stored session"}
                </small>
              </span>
              <time>{new Date(session.updatedAt).toLocaleDateString()}</time>
            </button>
          ))}
          {sessions.length === 0 && !error ? (
            <div className="empty-inline">
              No compatible stored Sessions found.
            </div>
          ) : null}
        </div>
        {error ? (
          <p className="form-error" role="alert">
            {error}
          </p>
        ) : null}
        <footer>
          <button className="button ghost" onClick={onClose} type="button">
            Cancel
          </button>
        </footer>
      </section>
    </div>
  );
}
