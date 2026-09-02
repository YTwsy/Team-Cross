import type {
  AppInfo,
  CapturePreview,
  StoredSession,
  ThreadDetail,
  ThreadSummary,
  TimelineEvent,
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

function normalizeThread(value: ThreadDetail): ThreadDetail {
  const git = value.git ?? ({} as ThreadDetail["git"]);
  const share = value.share
    ? { ...value.share, transports: collection(value.share.transports) }
    : undefined;
  return {
    ...value,
    git: { ...git, untracked: collection(git.untracked) },
    rounds: collection(value.rounds),
    events: collection(value.events).map(normalizeEvent),
    annotations: collection(value.annotations),
    evidence: collection(value.evidence),
    participants: collection(value.participants),
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
  const body = contentType.includes("application/json")
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
  createShare: (threadId: string, degraded = false) =>
    request<ThreadDetail>(`/api/v1/threads/${threadId}/shares`, {
      method: "POST",
      body: JSON.stringify({ ttlSeconds: 3600, allowDegraded: degraded }),
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
