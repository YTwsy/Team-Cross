import { t } from "./i18n";
import type { ActivitySource } from "./activity";
import type { LibraryReference } from "./library";

export type SpaceTarget = {
  inFlow?: boolean;
  id: string;
  memberId: string;
  member: string;
  name: string;
  provider: string;
  executionId?: string;
  removed?: boolean;
  available: boolean;
  reason?: string;
};
export type SpaceActor = {
  memberId: string;
  name: string;
  kind: "human" | "session" | "shadow";
  shadowId?: string;
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
};
export type BriefItem = {
  itemId?: string;
  text: string;
  status?: string;
  updatedBy?: SpaceActor;
  updatedAt?: string;
  sources: ActivitySource[];
  requestId?: string;
};
export type SpaceBrief = {
  revision: number;
  topic: string;
  decisions: BriefItem[];
  questions: BriefItem[];
  progress?: BriefItem[];
  updatedBy: SpaceActor;
  updatedAt: string;
};
export type WorkbenchView = {
  inFlow?: Record<string, boolean>;
  notes?: SpaceNote[];
  spaceId: string;
  selfId: string;
  targets: SpaceTarget[];
  requests: SpaceRequest[];
  total: number;
  nextOffset?: number;
  brief: SpaceBrief;
};
export type SpaceNote = {
  id: string;
  targetId: string;
  actor: SpaceActor;
  text: string;
  createdAt: string;
  deliveredAt?: string;
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

export function briefStatusLabel(status?: string) {
  return (
    (
      {
        proposed: t("建议"),
        confirmed: t("已确认"),
        disputed: t("有争议"),
        open: t("待解决"),
        resolved: t("已解决"),
        in_progress: t("进行中"),
        completed: t("已完成"),
        blocked: t("受阻"),
      } as Record<string, string>
    )[status || ""] || t("待核对")
  );
}
