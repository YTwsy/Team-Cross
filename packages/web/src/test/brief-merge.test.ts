import { expect, it } from "vitest";
import { mergeBrief, resolveBriefMerge } from "../brief-merge";
import type { SpaceBrief } from "../workbench";

function fixture(): SpaceBrief {
  return {
    revision: 1,
    topic: "Original topic",
    decisions: [
      {
        itemId: "decision",
        text: "Original",
        status: "proposed",
        sources: [{ spaceId: "space", materialId: "material", version: 1 }],
      },
    ],
    questions: [
      { itemId: "question", text: "Still open", status: "open", sources: [] },
    ],
    progress: [],
    updatedAt: "old",
    updatedBy: { kind: "human", memberId: "member", name: "Member" },
  };
}

it("preserves independent edits, deletions and additions without a choice", () => {
  const base = fixture(),
    mine = fixture(),
    remote = fixture();
  mine.decisions[0]!.status = "confirmed";
  mine.questions = [];
  remote.revision = 2;
  remote.topic = "New topic";
  remote.decisions[0]!.updatedAt = "metadata-only";
  remote.progress = [
    {
      itemId: "progress",
      text: "New work",
      status: "in_progress",
      sources: [],
    },
  ];
  const merged = mergeBrief(base, mine, remote);
  expect(merged.conflicts).toEqual([]);
  expect(merged.brief).toMatchObject({
    revision: 2,
    topic: "New topic",
    questions: [],
  });
  expect(merged.brief.decisions[0]!.status).toBe("confirmed");
  expect(merged.brief.progress).toEqual(remote.progress);
});

it("asks about deletion versus changed evidence and preserves a chosen remote item", () => {
  const base = fixture(),
    mine = fixture(),
    remote = fixture();
  mine.decisions = [];
  remote.revision = 2;
  remote.decisions[0]!.sources[0]!.version = 2;
  const merged = mergeBrief(base, mine, remote);
  expect(merged.conflicts).toHaveLength(1);
  expect(
    resolveBriefMerge(merged.brief, merged.conflicts[0]!, "remote").decisions,
  ).toEqual(remote.decisions);
  expect(
    resolveBriefMerge(merged.brief, merged.conflicts[0]!, "local").decisions,
  ).toEqual([]);
  expect(base.decisions[0]!.sources[0]!.version).toBe(1);
});

it("requires choices when the original snapshot has expired and supports moved items", () => {
  const mine = fixture(),
    remote = fixture();
  remote.revision = 300;
  remote.topic = "New topic";
  remote.progress = [{ ...remote.decisions[0]!, status: "completed" }];
  remote.decisions = [];
  const merged = mergeBrief(undefined, mine, remote);
  expect(merged.conflicts.map((item) => item.kind)).toEqual(["topic", "item"]);
  const chosen = resolveBriefMerge(merged.brief, merged.conflicts[1]!, "local");
  expect(chosen.decisions).toEqual(mine.decisions);
  expect(chosen.progress).toEqual([]);
  expect(chosen.revision).toBe(300);
});
