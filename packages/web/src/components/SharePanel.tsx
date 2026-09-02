import { useEffect, useState } from "react";
import type { Participant, Share } from "../types";
import { CopyIcon, ShareIcon } from "./Icons";

export function SharePanel({
  share,
  participants,
  canManage,
  onCreate,
  onRevoke,
  onRevokeControl,
}: {
  share?: Share;
  participants: Participant[];
  canManage: boolean;
  onCreate: (degraded: boolean) => Promise<void>;
  onRevoke: () => Promise<void>;
  onRevokeControl: () => Promise<void>;
}) {
  const [busy, setBusy] = useState(false);
  const [copied, setCopied] = useState(false);
  const [degraded, setDegraded] = useState(false);
  const remoteController = participants.find(
    (participant) => participant.role === "controller",
  );

  useEffect(() => {
    if (!copied) return;
    const timer = window.setTimeout(() => setCopied(false), 1400);
    return () => window.clearTimeout(timer);
  }, [copied]);

  async function run(action: () => Promise<void>) {
    setBusy(true);
    try {
      await action();
    } finally {
      setBusy(false);
    }
  }

  async function copyInvite() {
    if (!share) return;
    await navigator.clipboard.writeText(share.invite);
    setCopied(true);
  }

  return (
    <section className="side-section">
      <div className="side-section-heading">
        <span>
          <ShareIcon size={16} /> Share
        </span>
        {share ? (
          <span className={`share-state ${share.status}`}>{share.status}</span>
        ) : null}
      </div>
      {share ? (
        <div className="share-card">
          <label>One-time invitation</label>
          <button className="invite-value" onClick={copyInvite} type="button">
            <code>{share.invite.slice(0, 30)}…</code>
            <span>{copied ? "Copied" : <CopyIcon size={15} />}</span>
          </button>
          <div className="transport-path">
            {["LAN", "Tailnet", "Tailcat"].map((name, index) => (
              <span
                className={
                  share.transports
                    .map((item) => item.toLowerCase())
                    .includes(name.toLowerCase())
                    ? "ready"
                    : "off"
                }
                key={name}
              >
                {index ? <i>→</i> : null}
                <b>{name}</b>
              </span>
            ))}
          </div>
          <small>
            Expires{" "}
            {new Date(share.expiresAt).toLocaleTimeString([], {
              hour: "2-digit",
              minute: "2-digit",
            })}
          </small>
          {canManage ? (
            <button
              className="text-button danger"
              disabled={busy}
              onClick={() => run(onRevoke)}
              type="button"
            >
              Revoke share
            </button>
          ) : null}
        </div>
      ) : canManage ? (
        <div className="share-empty">
          <p>Invite collaborators to observe, annotate, or request control.</p>
          <label className="inline-checkbox">
            <input
              checked={degraded}
              onChange={(event) => setDegraded(event.target.checked)}
              type="checkbox"
            />
            Allow LAN/Tailnet-only if Tailcat fails
          </label>
          <button
            className="button secondary full"
            disabled={busy}
            onClick={() => run(() => onCreate(degraded))}
            type="button"
          >
            {busy ? "Warming Tailcat…" : "Create share"}
          </button>
        </div>
      ) : (
        <p className="side-muted">This thread is not currently shared.</p>
      )}

      <div className="participant-list">
        <div className="participant-list-heading">
          Participants · {participants.length}
          {canManage && remoteController ? (
            <button
              className="text-button danger"
              disabled={busy}
              onClick={() => run(onRevokeControl)}
              type="button"
            >
              Reclaim control
            </button>
          ) : null}
        </div>
        {participants.map((participant) => (
          <div className="participant" key={participant.id}>
            <span className="participant-avatar">
              {participant.name.slice(0, 1).toUpperCase()}
            </span>
            <span>
              <strong>{participant.name}</strong>
              <small>{participant.transport}</small>
            </span>
            <em>{participant.role}</em>
          </div>
        ))}
        {participants.length === 0 ? (
          <p className="side-muted">Only you are here.</p>
        ) : null}
      </div>
    </section>
  );
}
