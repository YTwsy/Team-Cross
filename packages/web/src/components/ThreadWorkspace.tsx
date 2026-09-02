import { useEffect, useRef, useState } from "react";
import { api, subscribeEvents } from "../api";
import type {
  AppInfo,
  InputQuestion,
  PendingInput,
  Provider,
  Role,
  ThreadDetail,
  TimelineEvent,
} from "../types";
import { AgentIcon, CommentIcon, GitIcon } from "./Icons";
import { Composer } from "./Composer";
import { DiffViewer } from "./DiffViewer";
import { EvidencePanel } from "./EvidencePanel";
import { ImportSessionModal } from "./ImportSessionModal";
import { SharePanel } from "./SharePanel";
import { Timeline } from "./Timeline";

type Tab = "timeline" | "diff" | "evidence" | "annotations";

const snapshotRefreshDelay = 100;
const leaseRenewInterval = 20_000;

function mergeEvents(...groups: TimelineEvent[][]): TimelineEvent[] {
  const events = new Map<number, TimelineEvent>();
  for (const group of groups) {
    for (const event of group) events.set(event.seq, event);
  }
  return [...events.values()].sort((left, right) => left.seq - right.seq);
}

function reconcileThread(
  current: ThreadDetail | undefined,
  snapshot: ThreadDetail,
): ThreadDetail {
  if (!current || current.id !== snapshot.id) return snapshot;
  const events = mergeEvents(snapshot.events, current.events);
  if (snapshot.revision < current.revision) return { ...current, events };
  return {
    ...snapshot,
    revision: Math.max(snapshot.revision, current.revision),
    events,
  };
}

function revisionFromEvents(current: number, events: TimelineEvent[]): number {
  return events.reduce((revision, event) => {
    const value = event.payload.revision;
    const parsed =
      typeof value === "number"
        ? value
        : typeof value === "string"
          ? Number(value)
          : Number.NaN;
    return Number.isFinite(parsed) ? Math.max(revision, parsed) : revision;
  }, current);
}

function effectiveRoleFor(
  info: AppInfo,
  thread: ThreadDetail | undefined,
  now: number,
): Role {
  if (info.role === "owner") return "owner";
  const expiresAt = Date.parse(thread?.controlLease?.expiresAt ?? "");
  return info.participantId &&
    thread?.controlLease?.participantId === info.participantId &&
    Number.isFinite(expiresAt) &&
    expiresAt > now
    ? "controller"
    : "observer";
}

function inputQuestion(
  value: unknown,
  index: number,
): InputQuestion | undefined {
  if (typeof value !== "object" || value === null || Array.isArray(value))
    return undefined;
  const item = value as Record<string, unknown>;
  const question = item.question ?? item.prompt ?? item.label;
  if (typeof question !== "string" || !question.trim()) return undefined;
  const options = Array.isArray(item.options)
    ? item.options.flatMap((option) => {
        if (typeof option === "string") return [{ label: option }];
        if (
          typeof option !== "object" ||
          option === null ||
          Array.isArray(option)
        )
          return [];
        const entry = option as Record<string, unknown>;
        return typeof entry.label === "string"
          ? [
              {
                label: entry.label,
                ...(typeof entry.description === "string"
                  ? { description: entry.description }
                  : {}),
              },
            ]
          : [];
      })
    : undefined;
  return {
    id: typeof item.id === "string" ? item.id : `question-${index}`,
    question,
    ...(typeof item.header === "string" ? { header: item.header } : {}),
    ...(options?.length ? { options } : {}),
  };
}

function pendingInputFromEvents(
  events: TimelineEvent[],
): PendingInput | undefined {
  const resolved = new Set<string>();
  const completedTurns = new Set<string>();
  for (let index = events.length - 1; index >= 0; index -= 1) {
    const event = events[index];
    if (!event) continue;
    const turnId = event.payload.turnId;
    if (
      (event.type === "turn.completed" ||
        event.type === "run.closed" ||
        event.type === "run.error") &&
      typeof turnId === "string" &&
      turnId
    ) {
      completedTurns.add(turnId);
    }
    const id = event.payload.inputRequestId;
    if (typeof id !== "string" || !id) continue;
    if (event.type === "input.resolved") {
      resolved.add(id);
      continue;
    }
    if (
      event.type !== "input.requested" ||
      resolved.has(id) ||
      (typeof turnId === "string" && completedTurns.has(turnId))
    )
      continue;
    const questions = Array.isArray(event.payload.questions)
      ? event.payload.questions.flatMap((question, questionIndex) => {
          const parsed = inputQuestion(question, questionIndex);
          return parsed ? [parsed] : [];
        })
      : [];
    return {
      id,
      questions:
        questions.length > 0
          ? questions
          : [
              {
                question:
                  typeof event.payload.prompt === "string"
                    ? event.payload.prompt
                    : "The Agent is waiting for additional input.",
              },
            ],
      blocking: event.payload.blocking === true,
    };
  }
  return undefined;
}

export function ThreadWorkspace({
  id,
  info,
  onBack,
}: {
  id: string;
  info: AppInfo;
  onBack: () => void;
}) {
  const [thread, setThread] = useState<ThreadDetail>();
  const [tab, setTab] = useState<Tab>("timeline");
  const [connected, setConnected] = useState(false);
  const [error, setError] = useState("");
  const [annotationTarget, setAnnotationTarget] = useState<{
    file?: string;
    line?: number;
  }>();
  const [annotationBody, setAnnotationBody] = useState("");
  const [importOpen, setImportOpen] = useState(false);
  const [switching, setSwitching] = useState(false);
  const [networkEnabled, setNetworkEnabled] = useState(false);
  const [leaseClock, setLeaseClock] = useState(() => Date.now());
  const threadRef = useRef<ThreadDetail | undefined>(undefined);

  useEffect(() => {
    threadRef.current = thread;
  }, [thread]);

  useEffect(() => {
    let active = true;
    setThread(undefined);
    setConnected(false);
    setError("");
    void api
      .thread(id)
      .then((value) => {
        if (!active) return;
        setThread(value);
        setNetworkEnabled(value.agentRun?.networkEnabled ?? false);
      })
      .catch((reason: unknown) => {
        if (active)
          setError(
            reason instanceof Error ? reason.message : "Unable to open thread",
          );
      });
    return () => {
      active = false;
    };
  }, [id]);

  const loadedThreadId = thread?.id;
  useEffect(() => {
    if (!loadedThreadId || loadedThreadId !== id) return;

    let active = true;
    let refreshTimer: number | undefined;
    let refreshRunning = false;
    let refreshAgain = false;

    const refreshSnapshot = async () => {
      if (!active) return;
      if (refreshRunning) {
        refreshAgain = true;
        return;
      }
      refreshRunning = true;
      try {
        const snapshot = await api.thread(id);
        if (!active) return;
        setThread((current) =>
          current?.id === id ? reconcileThread(current, snapshot) : current,
        );
      } catch (reason) {
        if (active)
          setError(
            reason instanceof Error
              ? reason.message
              : "Unable to refresh thread",
          );
      } finally {
        refreshRunning = false;
        if (active && refreshAgain) {
          refreshAgain = false;
          refreshTimer = window.setTimeout(() => {
            refreshTimer = undefined;
            void refreshSnapshot();
          }, snapshotRefreshDelay);
        }
      }
    };

    const scheduleSnapshotRefresh = () => {
      if (!active) return;
      if (refreshRunning) {
        refreshAgain = true;
        return;
      }
      if (refreshTimer !== undefined) return;
      refreshTimer = window.setTimeout(() => {
        refreshTimer = undefined;
        void refreshSnapshot();
      }, snapshotRefreshDelay);
    };

    const after = threadRef.current?.events.at(-1)?.seq ?? 0;
    const unsubscribe = subscribeEvents(
      id,
      after,
      (events) => {
        if (!active || events.length === 0) return;
        setThread((current) =>
          current?.id === id
            ? {
                ...current,
                events: mergeEvents(current.events, events),
                revision: revisionFromEvents(current.revision, events),
              }
            : current,
        );
        scheduleSnapshotRefresh();
      },
      (value) => {
        if (active) setConnected(value);
      },
    );

    return () => {
      active = false;
      if (refreshTimer !== undefined) window.clearTimeout(refreshTimer);
      unsubscribe();
    };
  }, [id, loadedThreadId]);

  const leaseExpiry = thread?.controlLease?.expiresAt;
  useEffect(() => {
    if (info.role === "owner" || !leaseExpiry) return;
    const delay = Date.parse(leaseExpiry) - Date.now();
    if (!Number.isFinite(delay) || delay <= 0) {
      setLeaseClock(Date.now());
      return;
    }
    const timer = window.setTimeout(
      () => setLeaseClock(Date.now()),
      Math.min(delay + 1, 2_147_483_647),
    );
    return () => window.clearTimeout(timer);
  }, [info.role, leaseExpiry]);

  const effectiveRole = effectiveRoleFor(info, thread, leaseClock);
  const leaseEpoch = thread?.controlLease?.epoch ?? 0;
  const controllerParticipantId =
    effectiveRole === "controller"
      ? thread?.controlLease?.participantId
      : undefined;
  useEffect(() => {
    if (effectiveRole !== "controller" || !controllerParticipantId) return;

    let active = true;
    let inFlight = false;
    const renew = async () => {
      if (!active || inFlight) return;
      const current = threadRef.current;
      if (
        !current ||
        current.id !== id ||
        current.controlLease?.participantId !== controllerParticipantId
      )
        return;
      inFlight = true;
      try {
        const value = await api.control(
          current.id,
          "renew",
          current.revision,
          current.controlLease.epoch,
        );
        if (active) {
          setThread((current) => reconcileThread(current, value));
          setError("");
        }
      } catch (reason) {
        if (!active) return;
        setError(
          reason instanceof Error ? reason.message : "Unable to renew control",
        );
        try {
          const snapshot = await api.thread(id);
          if (active)
            setThread((current) => reconcileThread(current, snapshot));
        } catch {
          // Keep the renewal error visible; SSE reconnect or the next action can recover.
        }
      } finally {
        inFlight = false;
      }
    };

    const timer = window.setInterval(() => {
      void renew();
    }, leaseRenewInterval);
    return () => {
      active = false;
      window.clearInterval(timer);
    };
  }, [controllerParticipantId, effectiveRole, id]);

  async function refresh(action: () => Promise<ThreadDetail>) {
    try {
      const value = await action();
      setThread((current) => reconcileThread(current, value));
      setError("");
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "Request failed");
      throw reason;
    }
  }

  if (!thread)
    return (
      <section className="workspace-loading">
        <span className="spinner" />
        <p>{error || "Opening isolated workspace…"}</p>
        <button className="text-button" onClick={onBack} type="button">
          Back to threads
        </button>
      </section>
    );

  const currentThread = thread;
  const canManage = info.role === "owner";
  const patch =
    thread.git.finalPatch ??
    `${thread.git.stagedPatch}${thread.git.unstagedPatch}`;
  const pendingInput = pendingInputFromEvents(thread.events);

  async function addAnnotation() {
    if (!annotationBody.trim()) return;
    await refresh(() =>
      api.addAnnotation(currentThread.id, {
        body: annotationBody.trim(),
        ...annotationTarget,
        expectedRevision: currentThread.revision,
        leaseEpoch,
      }),
    );
    setAnnotationBody("");
    setAnnotationTarget(undefined);
  }

  async function switchAgent(provider: Provider) {
    setSwitching(true);
    try {
      await refresh(() =>
        api.switchAgent(currentThread.id, provider, networkEnabled),
      );
    } finally {
      setSwitching(false);
    }
  }

  return (
    <div className="thread-workspace">
      <header className="workspace-header">
        <button className="back-button" onClick={onBack} type="button">
          ‹
        </button>
        <div className="thread-heading">
          <span className="status-chip active">
            <i />
            {thread.status}
          </span>
          <h1>{thread.title}</h1>
          <span className="thread-meta">
            <GitIcon size={14} />
            {thread.branch || "unborn"} ·{" "}
            {thread.git.head ? thread.git.head.slice(0, 8) : "no commit"}
          </span>
        </div>
        <div className="workspace-actions">
          <span
            className={`connection-indicator ${connected ? "connected" : "reconnecting"}`}
          >
            <i />
            {connected ? "Live" : "Reconnecting"}
          </span>
          <a
            className="button ghost compact"
            href={`/api/v1/threads/${thread.id}/patch`}
            download
          >
            Export patch
          </a>
        </div>
      </header>

      {error ? (
        <div className="workspace-error" role="alert">
          {error}
          <button onClick={() => setError("")} type="button">
            ×
          </button>
        </div>
      ) : null}

      <div className="workspace-body">
        <div className="workspace-main">
          <nav className="tabs" aria-label="Thread views">
            {(["timeline", "diff", "evidence", "annotations"] as const).map(
              (value) => (
                <button
                  className={tab === value ? "active" : ""}
                  key={value}
                  onClick={() => setTab(value)}
                  type="button"
                >
                  {value}
                  {value === "annotations" && thread.annotations.length ? (
                    <b>{thread.annotations.length}</b>
                  ) : null}
                </button>
              ),
            )}
          </nav>
          <div className="workspace-content">
            {tab === "timeline" ? (
              <Timeline events={thread.events} rounds={thread.rounds} />
            ) : null}
            {tab === "diff" ? (
              <DiffViewer
                annotations={thread.annotations}
                onAnnotate={(file, line) => {
                  setAnnotationTarget({ file, line });
                  setTab("annotations");
                }}
                patch={patch}
              />
            ) : null}
            {tab === "evidence" ? (
              <EvidencePanel
                canManage={canManage}
                evidence={thread.evidence}
                onAttach={(input) =>
                  refresh(() => api.attachEvidence(thread.id, input))
                }
                threadId={thread.id}
              />
            ) : null}
            {tab === "annotations" ? (
              <div className="annotation-view">
                <div className="annotation-list">
                  {thread.annotations.map((item) => (
                    <article className="annotation-card surface" key={item.id}>
                      <span className="event-avatar user">
                        {item.author.slice(0, 1).toUpperCase()}
                      </span>
                      <div>
                        <header>
                          <strong>{item.author}</strong>
                          <time>
                            {new Date(item.createdAt).toLocaleString()}
                          </time>
                        </header>
                        {item.file ? (
                          <code>
                            {item.file}
                            {item.line ? `:${item.line}` : ""}
                          </code>
                        ) : null}
                        <p>{item.body}</p>
                      </div>
                    </article>
                  ))}
                  {thread.annotations.length === 0 ? (
                    <div className="tab-empty">
                      <CommentIcon size={24} />
                      <h2>No annotations yet</h2>
                      <p>
                        Leave a general note, or annotate a line from the Diff
                        tab.
                      </p>
                    </div>
                  ) : null}
                </div>
                <div className="annotation-compose surface">
                  <label>
                    {annotationTarget?.file
                      ? `Comment on ${annotationTarget.file}:${annotationTarget.line}`
                      : "General annotation"}
                  </label>
                  <textarea
                    placeholder="Leave a concrete observation or question…"
                    value={annotationBody}
                    onChange={(event) => setAnnotationBody(event.target.value)}
                  />
                  <div>
                    {annotationTarget ? (
                      <button
                        className="text-button"
                        onClick={() => setAnnotationTarget(undefined)}
                        type="button"
                      >
                        Clear line target
                      </button>
                    ) : (
                      <span />
                    )}
                    <button
                      className="button secondary compact"
                      disabled={!annotationBody.trim()}
                      onClick={addAnnotation}
                      type="button"
                    >
                      Add annotation
                    </button>
                  </div>
                </div>
              </div>
            ) : null}
          </div>
          <Composer
            pendingInput={pendingInput}
            role={effectiveRole}
            run={thread.agentRun}
            onInterrupt={() =>
              refresh(() =>
                api.interrupt(thread.id, thread.revision, leaseEpoch),
              )
            }
            onRequestControl={() =>
              refresh(() => api.control(thread.id, "request", thread.revision))
            }
            onRespondInput={(inputRequestId, response) =>
              refresh(() =>
                api.respondInput(
                  thread.id,
                  inputRequestId,
                  response,
                  thread.revision,
                  leaseEpoch,
                ),
              )
            }
            onSend={(text) =>
              refresh(() =>
                api.send(thread.id, text, thread.revision, leaseEpoch),
              )
            }
            onSteer={(text) =>
              refresh(() =>
                api.steer(thread.id, text, thread.revision, leaseEpoch),
              )
            }
          />
        </div>

        <aside className="context-panel">
          <section className="side-section handoff-summary">
            <div className="side-section-heading">
              <span>Handoff context</span>
            </div>
            <dl>
              <dt>Goal</dt>
              <dd>{thread.goal}</dd>
              <dt>Progress</dt>
              <dd>{thread.progress || "Not recorded"}</dd>
              <dt>Blocker</dt>
              <dd>{thread.blocker || "None recorded"}</dd>
              <dt>Question</dt>
              <dd>{thread.questions || "Open-ended review"}</dd>
            </dl>
          </section>
          <section className="side-section agent-section">
            <div className="side-section-heading">
              <span>
                <AgentIcon size={16} /> Agent
              </span>
              {thread.agentRun ? (
                <span className={`agent-state ${thread.agentRun.status}`}>
                  {thread.agentRun.status}
                </span>
              ) : null}
            </div>
            {thread.git.unborn ? (
              <p className="side-muted">
                Create the first commit before starting a managed Agent.
              </p>
            ) : canManage ? (
              <>
                <div className="provider-buttons">
                  {(["codex", "claude", "mock"] as Provider[]).map(
                    (provider) => (
                      <button
                        className={
                          thread.agentRun?.provider === provider ? "active" : ""
                        }
                        disabled={
                          switching || thread.agentRun?.status === "running"
                        }
                        key={provider}
                        onClick={() => switchAgent(provider)}
                        type="button"
                      >
                        <span className={`provider-logo ${provider}`}>
                          {provider.slice(0, 1).toUpperCase()}
                        </span>
                        {provider}
                      </button>
                    ),
                  )}
                </div>
                <label className="network-toggle">
                  <span>
                    <strong>Tool network</strong>
                    <small>Disabled by default</small>
                  </span>
                  <input
                    checked={networkEnabled}
                    onChange={(event) =>
                      setNetworkEnabled(event.target.checked)
                    }
                    type="checkbox"
                  />
                  <i />
                </label>
                <button
                  className="text-button"
                  onClick={() => setImportOpen(true)}
                  type="button"
                >
                  Import old Session as context
                </button>
              </>
            ) : (
              <div className="current-agent">
                <span
                  className={`provider-logo ${thread.agentRun?.provider ?? "mock"}`}
                >
                  {thread.agentRun?.provider.slice(0, 1).toUpperCase()}
                </span>
                <span>
                  <strong>{thread.agentRun?.provider ?? "No Agent"}</strong>
                  <small>Runs on host account</small>
                </span>
              </div>
            )}
          </section>
          <SharePanel
            canManage={canManage}
            participants={thread.participants}
            share={thread.share}
            onCreate={(degraded) =>
              refresh(() => api.createShare(thread.id, degraded))
            }
            onRevokeControl={() =>
              refresh(() => api.revokeControl(thread.id, thread.revision))
            }
            onRevoke={() => refresh(() => api.revokeShare(thread.id))}
          />
          <section className="side-section worktree-section">
            <div className="side-section-heading">
              <span>Isolation</span>
            </div>
            <code>{thread.worktree}</code>
            <p>Original repository is never automatically modified.</p>
          </section>
        </aside>
      </div>
      <ImportSessionModal
        open={importOpen}
        onClose={() => setImportOpen(false)}
        onImport={(provider, sessionId) =>
          refresh(() => api.importSession(thread.id, provider, sessionId))
        }
      />
    </div>
  );
}
