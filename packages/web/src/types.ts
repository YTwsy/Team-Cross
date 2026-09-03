export type Role = "owner" | "controller" | "observer";
export type Provider = "mock" | "codex" | "claude";

export interface ReviewSuccessorInput {
  roundId: string;
  repo: string;
  untracked: string[];
  goal: string;
  expectedRevision: number;
}

export interface ReviewSuccessorPreview {
  roundId: string;
  repo: string;
  baseline: string;
  previewHash: string;
  expectedRevision: number;
  snapshotCount: number;
  feedbackCount: number;
  untracked: GitSnapshot["untracked"];
  warning: string;
}

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
  roundId?: string;
  target?: AnnotationTarget;
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
  allowControl?: boolean;
  scope?: ShareScope;
}

export interface AnnotationTarget {
  snapshotId?: string;
  entryId?: string;
  evidenceId?: string;
  roundId?: string;
  side?: "old" | "new";
}

export interface CodeAnnotation {
  file: string;
  line: number;
  target: { roundId: string; side: "old" | "new" };
}

export interface SealedCodeLine {
  kind: "add" | "delete" | "context" | "meta";
  text: string;
  oldPath?: string;
  newPath?: string;
  oldLine?: number;
  newLine?: number;
}

export interface SealedCodeReview {
  roundId: string;
  baseline: string;
  patch: string;
  lines: SealedCodeLine[];
}

export interface ShareScope {
  snapshotId?: string;
  entryIds?: string[];
  evidenceIds?: string[];
  includeCode: boolean;
  includeEvents: boolean;
  nativeLive?: NativeLiveScope;
}

export interface NativeLiveScope {
  followId: string;
  expectedSnapshotId: string;
  entryKinds: SessionEntry["kind"][];
  confirmCurrentAndFuture: true;
}

export interface NativeLiveStatus {
  followId: string;
  state: "active" | "retrying" | "stopped" | "limited";
  latestSnapshotId: string;
  entryKinds: string[];
  reason?: string;
}

export interface NativeOpenResult {
  status: "requested";
  provider: Provider;
  sessionId: string;
  target: "codex-desktop";
  message: string;
}

export interface ShareOptions {
  allowDegraded: boolean;
  allowControl: boolean;
  scope: ShareScope;
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
  readOnly?: boolean;
  sessionSnapshots?: SessionSnapshot[];
  sessionFollows?: SessionFollow[];
  nativeLive?: NativeLiveStatus;
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

export interface SessionRef {
  provider: Provider;
  sessionId: string;
  identityKind: string;
  surface: string;
  title?: string;
  cwd?: string;
  nativeIds?: Record<string, string>;
  providerVersion?: string;
}

export interface SessionCapabilities {
  read: boolean;
  follow: boolean;
  open: boolean;
  resume: boolean;
  takeControl: boolean;
  reason?: string;
}

export interface SessionEntry {
  id: string;
  kind: "message" | "tool" | "notice";
  role?: string;
  text: string;
  sourceId?: string;
  turnId?: string;
}

export interface SessionPreview {
  source: SessionRef;
  capturedAt: string;
  entries: SessionEntry[];
  truncated: boolean;
  warnings: string[];
  capabilities: SessionCapabilities;
}

export interface SessionSnapshot extends SessionPreview {
  id: string;
  threadId: string;
}

export interface SessionFollow {
  id: string;
  threadId: string;
  source: SessionRef;
  sourceSnapshotId: string;
  currentSnapshotId: string;
  state: "active" | "retrying" | "stopped";
  epoch: number;
  updatedAt: string;
  lastPolledAt?: string;
  reason?: string;
  gaps: string[];
}
