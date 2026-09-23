import { t, tr } from "./i18n";
import type { AnnotationTarget } from "./types";

export function targetLabel(target?: AnnotationTarget) {
  if (!target) return t("整体意见");
  if (target.kind === "material") return tr`会话材料 · 版本 ${target.version}`;
  if (target.kind === "history") return t("对话片段");
  const lines =
    target.startLine === target.endLine
      ? tr`第 ${target.startLine} 行`
      : tr`第 ${target.startLine}–${target.endLine} 行`;
  return `${target.path} · ${lines}${target.kind === "changes" ? (target.side === "old" ? t(" · 改动前") : t(" · 改动后")) : ""}`;
}

export type CodeLine = {
  text: string;
  raw: string;
  path?: string;
  number?: number;
  oldNumber?: number;
  newNumber?: number;
  side?: "old" | "new";
  block: number;
};

// Git quotes special filenames with C escapes, including octal UTF-8 bytes.
function gitPath(value: string) {
  let path = value.replace(/\t.*$/, "");
  if (path.startsWith('"')) {
    if (!path.endsWith('"')) return undefined;
    const bytes: number[] = [];
    const encoder = new TextEncoder();
    const escapes: Record<string, string> = {
      a: "\x07",
      b: "\b",
      t: "\t",
      n: "\n",
      v: "\v",
      f: "\f",
      r: "\r",
      '"': '"',
      "\\": "\\",
    };
    const source = path.slice(1, -1);
    for (let i = 0; i < source.length; ) {
      if (source[i] === "\\") {
        const octal = source.slice(i + 1).match(/^[0-7]{3}/)?.[0];
        if (octal) {
          bytes.push(parseInt(octal, 8));
          i += 4;
        } else {
          const escaped = escapes[source[i + 1]!];
          if (escaped === undefined) return undefined;
          bytes.push(...encoder.encode(escaped));
          i += 2;
        }
      } else {
        const char = String.fromCodePoint(source.codePointAt(i)!);
        bytes.push(...encoder.encode(char));
        i += char.length;
      }
    }
    path = new TextDecoder().decode(new Uint8Array(bytes));
  }
  if (path === "/dev/null") return undefined;
  if (!/^[ab]\//.test(path)) return undefined;
  return path.slice(2);
}

export function codeLines(text: string, path: string): CodeLine[] {
  return text.split("\n").map((text, index) => ({
    text,
    raw: text,
    path,
    number: index + 1,
    newNumber: index + 1,
    block: 0,
  }));
}

export function diffLines(diff: string): CodeLine[] {
  let oldPath: string | undefined;
  let newPath: string | undefined;
  let oldLine = 0;
  let newLine = 0;
  let block = 0;
  let inHunk = false;
  return diff.split("\n").map((raw) => {
    const header: CodeLine = { raw, text: raw, block };
    if (raw.startsWith("diff --git ")) {
      oldPath = newPath = undefined;
      inHunk = false;
      block++;
    } else if (!inHunk && raw.startsWith("--- ")) {
      oldPath = gitPath(raw.slice(4));
    } else if (!inHunk && raw.startsWith("+++ ")) {
      newPath = gitPath(raw.slice(4));
    } else if (raw.startsWith("@@ ")) {
      const match = raw.match(/^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@/);
      inHunk = !!match;
      if (match) {
        oldLine = +match[1]!;
        newLine = +match[2]!;
        block++;
      }
    } else if (inHunk && /^[ +\-]/.test(raw)) {
      const side = raw[0] === "-" ? "old" : "new";
      const line: CodeLine = {
        raw,
        text: raw.slice(1),
        side,
        block,
        path: side === "old" ? oldPath : newPath,
        number: side === "old" ? oldLine : newLine,
        oldNumber: raw[0] === "+" ? undefined : oldLine,
        newNumber: raw[0] === "-" ? undefined : newLine,
      };
      if (raw[0] !== "+") oldLine++;
      if (raw[0] !== "-") newLine++;
      return line;
    }
    return header;
  });
}

export function lineTarget(
  lines: CodeLine[],
  start: number,
  end: number,
  meta: Pick<AnnotationTarget, "kind" | "contentHash" | "baseRevision">,
): AnnotationTarget | undefined {
  const selected = lines.slice(start, end + 1);
  const first = selected[0];
  if (!meta.contentHash || !first?.path || !first.number || !selected.length)
    return;
  if (
    selected.some(
      (line, index) =>
        line.path !== first.path ||
        line.side !== first.side ||
        line.block !== first.block ||
        line.number !== first.number! + index,
    )
  )
    return;
  const quote = selected.map((line) => line.text).join("\n");
  if (!quote || Array.from(quote).length > 8000) return;
  return {
    ...meta,
    path: first.path,
    side: first.side,
    startLine: first.number,
    endLine: selected.at(-1)!.number,
    quote,
  };
}

export function codeTargetMatches(
  target: AnnotationTarget,
  lines: CodeLine[],
  contentHash?: string,
) {
  if (!target.contentHash || target.contentHash !== contentHash) return false;
  const start = lines.findIndex(
    (line) =>
      line.path === target.path &&
      line.side === target.side &&
      line.number === target.startLine,
  );
  if (start < 0) return false;
  const match = lineTarget(
    lines,
    start,
    start + target.endLine! - target.startLine!,
    target,
  );
  return match?.quote === target.quote;
}

export function textSelection(element: HTMLElement) {
  const selection = window.getSelection();
  if (!selection || selection.isCollapsed || !selection.rangeCount) return;
  const range = selection.getRangeAt(0);
  if (
    !element.contains(range.startContainer) ||
    !element.contains(range.endContainer)
  )
    return;
  const prefix = range.cloneRange();
  prefix.selectNodeContents(element);
  prefix.setEnd(range.startContainer, range.startOffset);
  const startOffset = prefix.toString().length;
  const quote = range.toString();
  if (!quote.trim() || Array.from(quote).length > 8000) return;
  return { startOffset, endOffset: startOffset + quote.length, quote };
}
