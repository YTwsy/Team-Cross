import type {
  AppInfo,
  CapturePreview,
  StoredSession,
  ThreadDetail,
  ThreadSummary,
  TimelineEvent,
  AnnotationTarget,
  SessionPreview,
  ShareOptions,
  SessionFollow,
  SessionSnapshot,
  NativeOpenResult,
  SealedCodeReview,
  ReviewSuccessorInput,
  ReviewSuccessorPreview,
} from "./types";

function collection<T>(value: T[] | null | undefined): T[] {
  return Array.isArray(value) ? value : [];
}

function record(value: unknown): Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : {};
}

function normalizeEvent(event: TimelineEvent): TimelineEvent {
  return { ...event, payload: record(event.payload) };
}

function normalizeSessionFollow(value: SessionFollow): SessionFollow {
  return { ...value, gaps: collection(value.gaps) };
}

function normalizeSessionSnapshot(value: SessionSnapshot): SessionSnapshot {
  return {
    ...value,
    entries: collection(value.entries),
    warnings: collection(value.warnings),
  };
}

function normalizeThread(value: ThreadDetail): ThreadDetail {
  const git = value.git ?? ({} as ThreadDetail["git"]);
  const share = value.share
    ? {
        ...value.share,
        transports: collection(value.share.transports),
        scope: value.share.scope
          ? {
              ...value.share.scope,
              nativeLive: value.share.scope.nativeLive
                ? {
                    ...value.share.scope.nativeLive,
                    entryKinds: collection(
                      value.share.scope.nativeLive.entryKinds,
                    ),
                  }
                : undefined,
            }
          : undefined,
      }
    : undefined;
  return {
    ...value,
    git: { ...git, untracked: collection(git.untracked) },
    rounds: collection(value.rounds),
    events: collection(value.events).map(normalizeEvent),
    annotations: collection(value.annotations),
    evidence: collection(value.evidence),
    sessionSnapshots: collection(value.sessionSnapshots).map(
      normalizeSessionSnapshot,
    ),
    sessionFollows: collection(value.sessionFollows).map(
      normalizeSessionFollow,
    ),
    participants: collection(value.participants),
    nativeLive: value.nativeLive
      ? {
          ...value.nativeLive,
          entryKinds: collection(value.nativeLive.entryKinds),
        }
      : undefined,
    share,
  };
}

export class APIError extends Error {
  constructor(
    message: string,
    readonly status: number,
    readonly body?: unknown,
  ) {
    super(message);
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(path, {
    ...init,
    headers: {
      "Content-Type": "application/json",
      ...init?.headers,
    },
  });
  const contentType = response.headers.get("content-type") ?? "";
  const body =
    contentType.includes("application/json") || contentType.includes("+json")
      ? await response.json()
      : await response.text();
  if (!response.ok) {
    const message =
      typeof body === "object" && body && "error" in body
        ? String(body.error)
        : `Request failed (${response.status})`;
    throw new APIError(message, response.status, body);
  }
  return body as T;
}

export const api = {
  roundCode: (threadId: string, roundId: string) =>
    request<SealedCodeReview>(
      `/api/v1/threads/${encodeURIComponent(threadId)}/rounds/${encodeURIComponent(roundId)}/code`,
    ).then((value) => ({ ...value, lines: collection(value.lines) })),
  info: () =>
    request<AppInfo>("/api/v1/info").then((value) => ({
      ...value,
      doctor: collection(value.doctor),
    })),
  threads: () =>
    request<ThreadSummary[] | null>("/api/v1/threads").then(collection),
  thread: (id: string) =>
    request<ThreadDetail>(`/api/v1/threads/${id}`).then(normalizeThread),
  preview: (repo: string) =>
    request<CapturePreview>("/api/v1/capture/preview", {
      method: "POST",
      body: JSON.stringify({ repo }),
    }).then((value) => ({ ...value, untracked: collection(value.untracked) })),
  createThread: (input: Record<string, unknown>) =>
    request<ThreadDetail>("/api/v1/threads", {
      method: "POST",
      body: JSON.stringify(input),
    }).then(normalizeThread),
  addAnnotation: (
    threadId: string,
    input: {
      body: string;
      file?: string;
      line?: number;
      roundId?: string;
      target?: AnnotationTarget;
      expectedRevision: number;
      leaseEpoch?: number;
    },
  ) =>
    request<ThreadDetail>(`/api/v1/threads/${threadId}/annotations`, {
      method: "POST",
      body: JSON.stringify({ ...input, commandId: crypto.randomUUID() }),
    }).then(normalizeThread),
  attachEvidence: (threadId: string, input: object) =>
    request<ThreadDetail>(`/api/v1/threads/${threadId}/evidence`, {
      method: "POST",
      body: JSON.stringify(input),
    }).then(normalizeThread),
  evidenceContent: (threadId: string, evidenceId: string, download = false) =>
    `/api/v1/threads/${encodeURIComponent(threadId)}/evidence/${encodeURIComponent(evidenceId)}${download ? "?download=1" : ""}`,
  createShare: (threadId: string, options: ShareOptions) =>
    request<ThreadDetail>(`/api/v1/threads/${threadId}/shares`, {
      method: "POST",
      body: JSON.stringify({ ttlSeconds: 3600, ...options }),
    }).then(normalizeThread),
  revokeShare: (threadId: string) =>
    request<ThreadDetail>(`/api/v1/threads/${threadId}/shares/current`, {
      method: "DELETE",
    }).then(normalizeThread),
  control: (
    threadId: string,
    action: "request" | "renew" | "release",
    revision: number,
    leaseEpoch = 0,
  ) =>
    request<ThreadDetail>(`/api/v1/threads/${threadId}/control`, {
      method: "POST",
      body: JSON.stringify({
        action,
        expectedRevision: revision,
        leaseEpoch,
        commandId: crypto.randomUUID(),
      }),
    }).then(normalizeThread),
  revokeControl: (threadId: string, revision: number) =>
    request<ThreadDetail>(`/api/v1/threads/${threadId}/control`, {
      method: "POST",
      body: JSON.stringify({ action: "revoke", expectedRevision: revision }),
    }).then(normalizeThread),
  send: (threadId: string, text: string, revision: number, leaseEpoch = 0) =>
    request<ThreadDetail>(`/api/v1/threads/${threadId}/agent/send`, {
      method: "POST",
      body: JSON.stringify({
        text,
        expectedRevision: revision,
        leaseEpoch,
        commandId: crypto.randomUUID(),
      }),
    }).then(normalizeThread),
  steer: (threadId: string, text: string, revision: number, leaseEpoch = 0) =>
    request<ThreadDetail>(`/api/v1/threads/${threadId}/agent/steer`, {
      method: "POST",
      body: JSON.stringify({
        text,
        expectedRevision: revision,
        leaseEpoch,
        commandId: crypto.randomUUID(),
      }),
    }).then(normalizeThread),
  interrupt: (threadId: string, revision: number, leaseEpoch = 0) =>
    request<ThreadDetail>(`/api/v1/threads/${threadId}/agent/interrupt`, {
      method: "POST",
      body: JSON.stringify({
        expectedRevision: revision,
        leaseEpoch,
        commandId: crypto.randomUUID(),
      }),
    }).then(normalizeThread),
  respondInput: (
    threadId: string,
    inputRequestId: string,
    response: unknown,
    revision: number,
    leaseEpoch = 0,
  ) =>
    request<ThreadDetail>(`/api/v1/threads/${threadId}/agent/input`, {
      method: "POST",
      body: JSON.stringify({
        inputRequestId,
        response,
        expectedRevision: revision,
        leaseEpoch,
        commandId: crypto.randomUUID(),
      }),
    }).then(normalizeThread),
  switchAgent: (threadId: string, provider: string, networkEnabled: boolean) =>
    request<ThreadDetail>(`/api/v1/threads/${threadId}/agent/switch`, {
      method: "POST",
      body: JSON.stringify({ provider, networkEnabled }),
    }).then(normalizeThread),
  storedSessions: () =>
    request<StoredSession[] | null>("/api/v1/sessions/stored").then(collection),
  sessionPreview: (provider: string, sessionId: string) =>
    request<SessionPreview>("/api/v1/sessions/preview", {
      method: "POST",
      body: JSON.stringify({ provider, sessionId }),
    }).then((value) => ({
      ...value,
      entries: collection(value.entries),
      warnings: collection(value.warnings),
    })),
  createThreadFromSession: (
    provider: string,
    sessionId: string,
    title?: string,
  ) =>
    request<ThreadDetail>("/api/v1/threads/from-session", {
      method: "POST",
      body: JSON.stringify({ provider, sessionId, title }),
    }).then(normalizeThread),
  feedback: (threadId: string) =>
    request<string>(`/api/v1/threads/${threadId}/feedback`),
  previewReviewSuccessor: (threadId: string, input: ReviewSuccessorInput) =>
    request<ReviewSuccessorPreview>(`/api/v1/threads/${encodeURIComponent(threadId)}/successor/preview`, {
      method: "POST",
      body: JSON.stringify(input),
    }),
  createReviewSuccessor: (threadId: string, input: ReviewSuccessorInput & { previewHash: string; confirmSeparateBaseline: true }) =>
    request<ThreadDetail>(`/api/v1/threads/${encodeURIComponent(threadId)}/successors`, {
      method: "POST",
      body: JSON.stringify(input),
    }).then(normalizeThread),
  sessionSnapshot: (threadId: string, snapshotId: string) =>
    request<SessionSnapshot>(
      `/api/v1/threads/${encodeURIComponent(threadId)}/sessions/snapshots/${encodeURIComponent(snapshotId)}`,
    ).then(normalizeSessionSnapshot),
  openNativeSession: (threadId: string, snapshotId: string) =>
    request<NativeOpenResult>(
      `/api/v1/threads/${encodeURIComponent(threadId)}/sessions/open`,
      {
        method: "POST",
        body: JSON.stringify({ snapshotId, confirmOpen: true }),
      },
    ),
  startSessionFollow: (threadId: string, snapshotId: string) =>
    request<SessionFollow>(
      `/api/v1/threads/${encodeURIComponent(threadId)}/follows`,
      {
        method: "POST",
        body: JSON.stringify({ snapshotId, confirmReadOnly: true }),
      },
    ).then(normalizeSessionFollow),
  stopSessionFollow: (threadId: string, followId: string) =>
    request<SessionFollow>(
      `/api/v1/threads/${encodeURIComponent(threadId)}/follows/${encodeURIComponent(followId)}`,
      {
        method: "DELETE",
      },
    ).then(normalizeSessionFollow),
  continueFromRound: (
    threadId: string,
    input: {
      roundId: string;
      provider: string;
      prompt: string;
      networkEnabled: boolean;
      expectedRevision: number;
      fork: boolean;
    },
  ) =>
    request<ThreadDetail>(`/api/v1/threads/${threadId}/continue`, {
      method: "POST",
      body: JSON.stringify(input),
    }).then(normalizeThread),
  forkThread: (threadId: string, roundId: string) =>
    request<ThreadDetail>(`/api/v1/threads/${threadId}/fork`, {
      method: "POST",
      body: JSON.stringify({ roundId }),
    }).then(normalizeThread),
  exportBundle: (
    threadId: string,
    input: {
      roundId: string;
      evidenceIds: string[];
      snapshotIds: string[];
      confirmExport: true;
    },
  ) =>
    request<unknown>(`/api/v1/threads/${threadId}/bundles`, {
      method: "POST",
      body: JSON.stringify(input),
    }),
  importBundle: (bundle: unknown) =>
    request<ThreadDetail>("/api/v1/bundles/import", {
      method: "POST",
      body: JSON.stringify(bundle),
    }).then(normalizeThread),
  importSession: (threadId: string, provider: string, sessionId: string) =>
    request<ThreadDetail>(`/api/v1/threads/${threadId}/sessions/import`, {
      method: "POST",
      body: JSON.stringify({ provider, sessionId }),
    }).then(normalizeThread),
};

export function subscribeEvents(
  threadId: string,
  after: number,
  onEvents: (events: TimelineEvent[]) => void,
  onConnection: (connected: boolean) => void,
): () => void {
  const source = new EventSource(
    `/api/v1/threads/${threadId}/events?after=${after}`,
  );
  source.onopen = () => onConnection(true);
  source.onerror = () => onConnection(false);
  source.onmessage = (message) => {
    try {
      const value = JSON.parse(message.data) as TimelineEvent | TimelineEvent[];
      const events = Array.isArray(value) ? value : [value];
      if (
        events.some(
          (event) =>
            !event ||
            typeof event !== "object" ||
            typeof event.seq !== "number" ||
            typeof event.type !== "string",
        )
      ) {
        throw new TypeError("Invalid timeline event");
      }
      onConnection(true);
      onEvents(events.map(normalizeEvent));
    } catch {
      onConnection(false);
    }
  };
  return () => source.close();
}
