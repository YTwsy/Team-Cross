import type { Collaboration } from "../types";

// Migrate the existing reader fixtures into the actual space/session response
// shape. Tests keep their original history and lifecycle data under one child.
export function spaceWithExecution(value: Collaboration): Collaboration {
  const executionId = `execution-${value.id}`;
  return {
    ...value,
    hasExecution: false,
    annotations: [],
    sessionCount: 1,
    sessions: [
      {
        ...value,
        id: executionId,
        spaceId: value.id,
        executionId,
        hasExecution: true,

        paused:
          value.paused ?? (value.runtimeState === "released" && !value.sharing),
        members: value.members?.map((member) => ({
          ...member,
        })),
        annotations: value.annotations.map((note) => ({
          ...note,
          executionId,
        })),
      },
    ],
  };
}
