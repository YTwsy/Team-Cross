import type { SessionSnapshot, ThreadDetail } from "../types";

export const snapshot: SessionSnapshot = {
  id: "snapshot-1",
  threadId: "thread-1",
  source: {
    provider: "codex",
    sessionId: "native-1",
    identityKind: "thread.id",
    surface: "cli",
    title: "Fix parser",
    providerVersion: "test-version",
  },
  capturedAt: "2026-09-03T00:00:00Z",
  truncated: false,
  warnings: [],
  entries: [
    { id: "entry-1", kind: "message", role: "user", text: "Visible request" },
    { id: "entry-2", kind: "tool", role: "tool", text: "Private tool output" },
  ],
  capabilities: {
    read: true,
    follow: false,
    open: false,
    resume: false,
    takeControl: false,
    reason: "Native writer fencing is not verified",
  },
};

export const reviewThread: ThreadDetail = {
  id: "thread-1",
  title: "Review thread",
  repo: "",
  branch: "",
  status: "read_only",
  updatedAt: "2026-09-03T00:00:00Z",
  revision: 4,
  worktree: "",
  goal: "Review context",
  progress: "",
  blocker: "",
  tried: "",
  questions: "",
  readOnly: true,
  git: {
    head: "",
    branch: "",
    unborn: true,
    status: "",
    stagedPatch: "",
    unstagedPatch: "",
    untracked: [],
  },
  rounds: [],
  events: [],
  annotations: [],
  evidence: [],
  participants: [],
  sessionSnapshots: [snapshot],
};

export const executableThread: ThreadDetail = {
  ...reviewThread,
  repo: "/repo",
  worktree: "/isolated/thread-1",
  readOnly: false,
  git: { ...reviewThread.git, head: "abc123", unborn: false },
  rounds: [
    {
      id: "round-0",
      sequence: 0,
      summary: "initial snapshot",
      createdAt: "2026-09-03T00:00:00Z",
    },
    {
      id: "round-1",
      sequence: 1,
      summary: "latest snapshot",
      createdAt: "2026-09-03T00:05:00Z",
    },
  ],
};
