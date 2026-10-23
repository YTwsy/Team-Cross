import { useEffect, useState } from "react";
import { api, useResource } from "./api";
import type { MemberMention } from "./types";

export type ActivitySubject = {
  kind: "material" | "discussion" | "execution";
  id: string;
  executionId?: string;
};
export type ActivitySource = {
  spaceId: string;
  executionId?: string;
  materialId?: string;
  version?: number;
  annotationId?: string;
  replyId?: string;
  turnId?: string;
  itemIds?: string[];
  requestId?: string;
};
export type ActivityItem = {
  id: string;
  seq: number;
  kind: string;
  source: ActivitySource;
  subject: ActivitySubject;
  actor: { kind: string; name: string; memberId?: string };
  createdAt: string;
  preview: string;
  reasons: string[];
  unread: boolean;
};
export type ActivityGroup = {
  id: string;
  latest: ActivityItem;
  attention?: ActivityItem;
  events: ActivityItem[];
  count: number;
  unread: number;
};
export type ActivityPage = {
  spaceId: string;
  title: string;
  selfId: string;
  groups: ActivityGroup[];
  unread: number;
  snapshot: string;
  nextCursor?: string;
  revision: number;
  readRevision: number;
  error?: string;
  pendingSync?: boolean;
  syncedAt: string;
};
export type Follow = {
  subject: ActivitySubject;
  mode: "following" | "direct_only" | "muted";
  previousMode?: string;
  origin: string;
  revision: number;
  effectiveMode: string;
  inheritedFrom?: string;
  available: boolean;
  title: string;
};
const eventName = "teamcross:member-activity";
export function activityChanged() {
  window.dispatchEvent(new Event(eventName));
}
export function useActivityResource<T>(path: string | null) {
  const [revision, setRevision] = useState(0);
  useEffect(() => {
    const refresh = () => setRevision((v) => v + 1);
    window.addEventListener(eventName, refresh);
    return () => window.removeEventListener(eventName, refresh);
  }, []);
  return useResource<T>(path, 4000, revision);
}
export const sameSubject = (a: ActivitySubject, b: ActivitySubject) =>
  a.kind === b.kind && a.id === b.id && a.executionId === b.executionId;
export async function readActivity(spaceId: string, body: object) {
  const result = await api<ActivityPage>(
    `collaborations/${spaceId}/activity/read`,
    body,
  );
  activityChanged();
  return result;
}
// Offsets are JS/UTF-16 offsets. Ordinary pasted @ text never acquires a member identity.
export function editedMentions(
  before: string,
  after: string,
  mentions: MemberMention[],
): MemberMention[] {
  let start = 0;
  while (
    start < before.length &&
    start < after.length &&
    before[start] === after[start]
  )
    start++;
  let end = before.length,
    nextEnd = after.length;
  while (
    end > start &&
    nextEnd > start &&
    before[end - 1] === after[nextEnd - 1]
  ) {
    end--;
    nextEnd--;
  }
  const delta = nextEnd - end;
  return mentions
    .flatMap((m) => {
      if (m.endOffset <= start) return [m];
      if (m.startOffset >= end)
        return [
          {
            ...m,
            startOffset: m.startOffset + delta,
            endOffset: m.endOffset + delta,
          },
        ];
      return [];
    })
    .filter((m) => after.slice(m.startOffset, m.endOffset) === `@${m.label}`);
}
