export type Role = "owner" | "controller" | "observer";
export type Provider = "mock" | "codex" | "claude";

export interface DoctorCheck {
  key: string;
  label: string;
  status: "ok" | "warning" | "error";
  detail: string;
  optional?: boolean;
}

export interface AppInfo {
  version: string;
  mode: "host" | "join";
  role: Role;
  repo?: string;
  selectedTransport?: "lan" | "tailscale" | "tailcat";
  participantId?: string;
  doctor: DoctorCheck[];
}

export interface GitSnapshot {
  head: string;
  branch: string;
  unborn: boolean;
  status: string;
  stagedPatch: string;
  unstagedPatch: string;
  finalPatch?: string;
  untracked: Array<{
    path: string;
    size: number;
    captured: boolean;
    reason?: string;
  }>;
}

export interface TimelineEvent {
  seq: number;
  type: string;
  createdAt: string;
  actor?: string;
  payload: Record<string, unknown>;
}

export interface Annotation {
  id: string;
  author: string;
  body: string;
  file?: string;
  line?: number;
  createdAt: string;
}

export interface Evidence {
  id: string;
  kind: string;
  name: string;
  size: number;
  mimeType?: string;
  source?: string;
  createdAt: string;
}

export interface InputQuestion {
  id?: string;
  question: string;
  header?: string;
  options?: Array<{
    label: string;
    description?: string;
  }>;
}

export interface PendingInput {
  id: string;
  questions: InputQuestion[];
  blocking: boolean;
}

export interface AgentRun {
  id: string;
  provider: Provider;
  status: "idle" | "running" | "waiting" | "closed" | "error";
  sessionId?: string;
  turnId?: string;
  networkEnabled: boolean;
}

export interface Participant {
  id: string;
  name: string;
  role: Role;
  transport: string;
  lastSeenAt: string;
}

export interface Share {
  id: string;
  invite: string;
  expiresAt: string;
  status: "warming" | "active" | "degraded" | "revoked";
  transports: string[];
}

export interface Round {
  id: string;
  sequence: number;
  summary: string;
  createdAt: string;
  provider?: Provider;
}

export interface ThreadSummary {
  id: string;
  title: string;
  repo: string;
  branch: string;
  status: string;
  updatedAt: string;
  provider?: Provider;
}

export interface ThreadDetail extends ThreadSummary {
  revision: number;
  worktree: string;
  goal: string;
  progress: string;
  blocker: string;
  tried: string;
  questions: string;
  git: GitSnapshot;
  rounds: Round[];
  events: TimelineEvent[];
  annotations: Annotation[];
  evidence: Evidence[];
  agentRun?: AgentRun;
  participants: Participant[];
  share?: Share;
  controlLease?: {
    participantId: string;
    epoch: number;
    expiresAt: string;
  };
}

export interface CapturePreview {
  repo: string;
  branch: string;
  head: string;
  unborn: boolean;
  status: string;
  untracked: Array<{ path: string; size: number }>;
}

export interface StoredSession {
  id: string;
  provider: Provider;
  title: string;
  updatedAt: string;
  cwd?: string;
}
