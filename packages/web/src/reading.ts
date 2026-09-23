import { t } from "./i18n";
import { decodeString } from "micromark-util-decode-string";
import type { AnnotationTarget, MaterialPage } from "./types";

export const itemLabel = (type: string) =>
  ({
    userMessage: t("用户提问"),
    agentMessage: t("助手回复"),
    toolCall: t("工具调用"),
    toolResult: t("工具输出"),
    commandExecution: t("命令与结果"),
    fileChange: t("文件改动"),
    unavailable: t("未能导出的内容"),
  })[type] || t("已保存的工具过程");

export function readingTitle(source: string, limit = 32) {
  const line = source.trim().split(/\r?\n/)[0] || t("对话");
  const title = line
    .replace(/^\s{0,3}(?:#{1,6}|>)\s*/, "")
    .replace(/\[([^\]]+)\]\([^)]*\)/g, "$1")
    .replace(/[*`_]/g, "");
  const characters = Array.from(title);
  return (
    characters.slice(0, limit).join("") + (characters.length > limit ? "…" : "")
  );
}

// Every boundary is an offset in the original UTF-16 string, never rendered text.
export function textOffsets(
  raw: string,
  value: string,
  start: number,
  literal = false,
) {
  const offsets = [start];
  let decoded = "";
  for (let i = 0; i < raw.length; ) {
    const token =
      (!literal &&
        /^(?:\\[!-/:-@\[-`{-~]|&(?:#[xX][\da-fA-F]+|#\d+|[a-zA-Z][\da-zA-Z]*);)/.exec(
          raw.slice(i),
        )?.[0]) ||
      /^\r\n?/.exec(raw.slice(i))?.[0] ||
      raw[i]!;
    const text =
      token[0] === "\r" ? "\n" : literal ? token : decodeString(token);
    decoded += text;
    for (let n = 1; n <= text.length; n++)
      offsets.push(start + i + (n === text.length ? token.length : 0));
    i += token.length;
  }
  return decoded === value ? offsets : undefined;
}

export function codeOffsets(
  raw: string,
  value: string,
  start: number,
  block: boolean,
) {
  if (!block) {
    const delimiter = /^`+/.exec(raw)?.[0];
    if (!delimiter || !raw.endsWith(delimiter)) return;
    const text = raw.slice(delimiter.length, -delimiter.length);
    let normalized = text.replace(/\r\n?|\n/g, " ");
    let map = textOffsets(
      text,
      text.replace(/\r\n?/g, "\n"),
      start + delimiter.length,
      true,
    );
    if (
      normalized.startsWith(" ") &&
      normalized.endsWith(" ") &&
      /[^ ]/.test(normalized)
    ) {
      normalized = normalized.slice(1, -1);
      map = map?.slice(1, -1);
    }
    return normalized === value ? map : undefined;
  }
  const fence = /^ {0,3}(`{3,}|~{3,})[^\n]*(?:\n|$)/.exec(raw);
  const bodyStart = fence ? fence[0].length : 0;
  const sourceLines = raw.slice(bodyStart).split("\n");
  const lines = value.split("\n");
  const offsets: number[] = [];
  let at = start + bodyStart;
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i]!;
    if (i === lines.length - 1 && line === "") break;
    const source = sourceLines[i];
    if (source === undefined) return;
    const clean = source.replace(/\r$/, "");
    // Only parser indentation may be omitted. Never search for an arbitrary match.
    const removed = clean.length - line.length;
    if (
      removed < 0 ||
      !/^[ \t]*$/.test(clean.slice(0, removed)) ||
      clean.slice(removed) !== line
    )
      return;
    for (let n = 0; n < line.length; n++) offsets.push(at + removed + n);
    offsets.push(at + clean.length);
    at += source.length + 1;
  }
  if (offsets.length === value.length)
    offsets.push(Math.min(start + raw.length, at));
  return offsets.length === value.length + 1 ? offsets : undefined;
}

function endpoint(node: Node, offset: number, end: boolean) {
  // A Range may end at an element boundary rather than inside a text node.
  if (node.nodeType === Node.ELEMENT_NODE) {
    const children = node.childNodes;
    const child = end ? children[offset - 1] : children[offset];
    if (!child) return;
    node = child;
    while (node.nodeType !== Node.TEXT_NODE && node.childNodes.length)
      node = end ? node.lastChild! : node.firstChild!;
    offset = end ? node.textContent?.length || 0 : 0;
  }
  const mapped = node.parentElement?.closest<HTMLElement>(
    "[data-source-start]",
  );
  if (!mapped) return;
  const prefix = document.createRange();
  prefix.selectNodeContents(mapped);
  prefix.setEnd(node, offset);
  const index = prefix.toString().length;
  const map: number[] | undefined = mapped.dataset.sourceMap
    ? JSON.parse(mapped.dataset.sourceMap)
    : undefined;
  return map ? map[index] : Number(mapped.dataset.sourceStart) + index;
}

export function mappedSelection(element: HTMLElement, source: string) {
  const selection = window.getSelection();
  if (!selection || selection.isCollapsed || !selection.rangeCount) return;
  const range = selection.getRangeAt(0);
  if (
    !element.contains(range.startContainer) ||
    !element.contains(range.endContainer)
  )
    return;
  const startOffset = endpoint(range.startContainer, range.startOffset, false);
  const endOffset = endpoint(range.endContainer, range.endOffset, true);
  if (
    startOffset === undefined ||
    endOffset === undefined ||
    endOffset <= startOffset
  )
    return;
  const quote = source.slice(startOffset, endOffset);
  if (!quote.trim() || Array.from(quote).length > 8000) return;
  return { startOffset, endOffset, quote };
}

export function sameMessage(a: AnnotationTarget, b: AnnotationTarget) {
  return (
    a.kind === b.kind &&
    a.materialId === b.materialId &&
    a.version === b.version &&
    a.sessionId === b.sessionId &&
    a.turnId === b.turnId &&
    a.itemId === b.itemId
  );
}

export function joinMaterialSegments(segments: MaterialPage["segments"]) {
  const result: MaterialPage["segments"] = [];
  for (const segment of segments) {
    const previous = result.at(-1);
    if (
      previous &&
      previous.turnId === segment.turnId &&
      previous.itemId === segment.itemId &&
      previous.endOffset === segment.startOffset
    ) {
      const wasCollapsed = !!previous.collapsed;
      previous.text += segment.text;
      previous.endOffset = segment.endOffset;
      // An item-range response carries the item's original notice, while the
      // stream preview also appends a temporary "collapsed" explanation. Once
      // more of that item is loaded, replace the preview notice so the expanded
      // UI does not keep claiming the visible body is only the folded prefix.
      if (wasCollapsed) {
        previous.notice = segment.notice;
        previous.readHint = segment.readHint;
      } else previous.notice ||= segment.notice;
      previous.length = Math.max(previous.length, segment.length);
      previous.collapsed = previous.endOffset < previous.length;
      previous.sourceLength ||= segment.sourceLength;
      previous.omittedLength ||= segment.omittedLength;
    } else result.push({ ...segment });
  }
  return result;
}

export function mergeMaterialSegments(
  existing: MaterialPage["segments"],
  incoming: MaterialPage["segments"],
) {
  const result = [...existing];
  for (const segment of incoming) {
    let insertAt = result.findIndex(
      (current) =>
        current.turnId === segment.turnId &&
        current.itemId === segment.itemId &&
        current.startOffset > segment.startOffset,
    );
    if (insertAt < 0) {
      const last = result.findLastIndex(
        (current) =>
          current.turnId === segment.turnId &&
          current.itemId === segment.itemId,
      );
      insertAt = last < 0 ? result.length : last + 1;
    }
    if (
      !result.some(
        (current) =>
          current.turnId === segment.turnId &&
          current.itemId === segment.itemId &&
          current.startOffset === segment.startOffset &&
          current.endOffset === segment.endOffset,
      )
    )
      result.splice(insertAt, 0, segment);
  }
  return coalesceAdjacentSegments(result);
}

function coalesceAdjacentSegments(segments: MaterialPage["segments"]) {
  const out: MaterialPage["segments"] = [];
  for (const segment of segments) {
    const prev = out[out.length - 1];
    if (
      prev &&
      prev.turnId === segment.turnId &&
      prev.itemId === segment.itemId &&
      prev.endOffset === segment.startOffset
    ) {
      const wasCollapsed = !!prev.collapsed;
      prev.text += segment.text;
      prev.endOffset = segment.endOffset;
      if (wasCollapsed) {
        prev.notice = segment.notice;
        prev.readHint = segment.readHint;
      } else prev.notice ||= segment.notice;
      prev.length = Math.max(prev.length, segment.length);
      prev.collapsed = prev.endOffset < prev.length;
      prev.sourceLength ||= segment.sourceLength;
      prev.omittedLength ||= segment.omittedLength;
    } else {
      out.push({ ...segment });
    }
  }
  return out;
}
