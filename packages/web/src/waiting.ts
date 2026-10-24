import type { ActivityItem, ActivitySource } from "./activity";

export type QuestionHandling = {
  id: string;
  source: ActivitySource;
  handler: "owner" | "shadow";
  revision: number;
  question: string;
  state: string;
  canHandoff?: boolean;
};
export type WaitingItem = {
  id: string;
  kind: string;
  direction: string;
  state: string;
  source: ActivitySource;
  question: string;
  tasks: {
    shadowId: string;
    jobId: string;
    title: string;
    state: string;
    dependencyId: string;
    condition: string;
  }[];
  totalTasks: number;
  handling?: QuestionHandling;
  error?: string;
  updatedAt: string;
};
export type WaitingPage = {
  items: WaitingItem[];
  total: number;
  revision: string;
  nextCursor?: string;
};
export type MemberAttention = {
  waiting: WaitingPage;
  localShadows: number;
  route: { shadowId: string; enabled: boolean; revision: number };
};

// Presentation adapter for the existing exact-source reader. It is never
// stored as an activity, given a space sequence, or marked read.
export function waitingSourceItem(item: WaitingItem): ActivityItem {
  return {
    id: `waiting:${item.id}`,
    seq: 0,
    kind: item.kind,
    source: item.source,
    subject: {
      kind: "discussion",
      id: item.source.annotationId || "",
      executionId: item.source.executionId,
    },
    actor: { kind: "", name: "" },
    createdAt: item.updatedAt,
    preview: item.question,
    reasons: [],
    unread: false,
    preserveReadState: true,
  };
}
