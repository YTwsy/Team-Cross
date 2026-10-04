import type { LibraryReference } from "./library";

export type SpaceTarget = {
  id: string;
  memberId: string;
  member: string;
  name: string;
  provider: string;
  execution?: boolean;
  removed?: boolean;
  available: boolean;
  reason?: string;
};
export type SpaceActor = {
  memberId: string;
  name: string;
  kind: "human" | "session";
  provider?: string;
  session?: string;
};
export type SpaceRequest = {
  id: string;
  targetId: string;
  actor: SpaceActor;
  parentRequestId?: string;
  briefRevision?: number;
  context?: {
    brief: SpaceBrief;
    previousBriefRevision?: number;
    parent?: {
      requestId: string;
      state: string;
      instruction: string;
      summary?: string;
    };
  };
  references: LibraryReference[];
  instruction: string;
  intent: string;
  state:
    | "queued"
    | "submitted"
    | "received"
    | "completed"
    | "failed"
    | "unknown"
    | "cancelled";
  summary?: string;
  error?: string;
  createdAt: string;
  updatedAt: string;
  receivedAt?: string;
  finishedAt?: string;
  bootstrap?: {
    brief: SpaceBrief;
    epoch: number;
    title: string;
    rules: string;
  };
};
export type BriefItem = {
  text: string;
  sources: LibraryReference[];
  requestId?: string;
};
export type SpaceBrief = {
  revision: number;
  topic: string;
  decisions: BriefItem[];
  questions: BriefItem[];
  updatedBy: SpaceActor;
  updatedAt: string;
};
export type SpaceAssistant = {
  targetId?: string;
  epoch: number;
  state: "disabled" | "initializing" | "ready" | "paused" | "failed";
  bootstrapId?: string;
  revision: number;
};
export type WorkbenchView = {
  spaceId: string;
  selfId: string;
  targets: SpaceTarget[];
  requests: SpaceRequest[];
  total: number;
  nextOffset?: number;
  brief: SpaceBrief;
  assistant: SpaceAssistant;
};
export type SpaceReceiver = {
  id: string;
  spaceId: string;
  name: string;
  state: string;
  error?: string;
  sessionId: string;
  binary?: string;
  clientError?: string;
  canRetryCreation?: boolean;
  clientRecovered?: boolean;
  online: boolean;
  busy: boolean;
  approvals: number;
  activeTurnId?: string;
  receivingPaused?: boolean;
  releasePending?: boolean;
  model?: string;
  modelProvider?: string;
  reasoningEffort?: string;
  command?: string;
  pairingId: string;
};
