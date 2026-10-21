import type { Collaboration } from "./types";

export function collaborationAddress(c: Collaboration) {
  return c.executionId && c.spaceId
    ? `${c.spaceId}/sessions/${c.executionId}`
    : c.id;
}

// Reader IDs are explicit local API addresses; references keep separate IDs.
export function resourceScope(address: string): {
  spaceId: string;
  executionId?: string;
} {
  const [spaceId, executionId] = address.split("/sessions/");
  return { spaceId: spaceId!, ...(executionId ? { executionId } : {}) };
}

export function referenceAddress(ref: {
  spaceId: string;
  executionId?: string;
}) {
  return ref.executionId
    ? `${ref.spaceId}/sessions/${ref.executionId}`
    : ref.spaceId;
}
