import type { BriefItem, SpaceBrief } from "./workbench";

const sections = ["decisions", "questions", "progress"] as const;
type Section = (typeof sections)[number];
type Entry = { section: Section; item: BriefItem };
export type BriefMergeConflict =
  | { kind: "topic"; key: string; local: string; remote: string }
  | { kind: "item"; key: string; local?: Entry; remote?: Entry };

const itemKey = (section: Section, item: BriefItem, index: number) =>
  item.itemId || `${section}:${index}`;

function entries(brief: SpaceBrief) {
  const result = new Map<string, Entry>();
  for (const section of sections)
    (brief[section] || []).forEach((item, index) =>
      result.set(itemKey(section, item, index), { section, item }),
    );
  return result;
}

function content(entry?: Entry) {
  if (!entry) return "";
  const { item, section } = entry;
  return JSON.stringify({
    section,
    text: item.text,
    status: item.status,
    requestId: item.requestId,
    sources: (item.sources || []).map((source) =>
      Object.fromEntries(
        Object.entries(source).sort(([a], [b]) => a.localeCompare(b)),
      ),
    ),
  });
}

// Preserve independent edits and additions. Conflicting edits stay outside the
// live draft until the editor has resolved every choice in the merge dialog.
export function mergeBrief(
  base: SpaceBrief | undefined,
  local: SpaceBrief,
  remote: SpaceBrief,
) {
  const brief: SpaceBrief = {
    ...structuredClone(remote),
    decisions: [],
    questions: [],
    progress: [],
  };
  const conflicts: BriefMergeConflict[] = [];
  if (local.topic !== remote.topic) {
    if (base && remote.topic === base.topic) brief.topic = local.topic;
    else if (!base || local.topic !== base.topic)
      conflicts.push({
        kind: "topic",
        key: "$topic",
        local: local.topic,
        remote: remote.topic,
      });
  }
  const before = base ? entries(base) : undefined;
  const mine = entries(local),
    theirs = entries(remote);
  for (const key of new Set([...theirs.keys(), ...mine.keys()])) {
    const old = before?.get(key),
      a = mine.get(key),
      b = theirs.get(key);
    let chosen = b;
    if (content(a) !== content(b)) {
      if (before && content(b) === content(old)) chosen = a;
      else if (!before || content(a) !== content(old))
        conflicts.push({ kind: "item", key, local: a, remote: b });
    }
    if (chosen) brief[chosen.section]!.push(structuredClone(chosen.item));
  }
  return { brief, conflicts };
}

export function resolveBriefMerge(
  draft: SpaceBrief,
  conflict: BriefMergeConflict,
  choice: "local" | "remote",
): SpaceBrief {
  const brief = structuredClone(draft);
  if (conflict.kind === "topic") {
    brief.topic = conflict[choice];
    return brief;
  }
  const chosen = conflict[choice];
  let inserted = false;
  for (const section of sections) {
    brief[section] = (brief[section] || []).flatMap((item, index) => {
      if (itemKey(section, item, index) !== conflict.key) return [item];
      if (chosen?.section !== section) return [];
      inserted = true;
      return [structuredClone(chosen.item)];
    });
  }
  if (chosen && !inserted)
    brief[chosen.section]!.push(structuredClone(chosen.item));
  return brief;
}
