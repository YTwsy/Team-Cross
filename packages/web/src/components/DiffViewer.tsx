import { useMemo, useState } from "react";
import type { Annotation } from "../types";
import { CommentIcon } from "./Icons";

interface DiffLine {
  kind: "add" | "delete" | "context" | "meta";
  text: string;
  file: string;
  oldLine?: number;
  newLine?: number;
}

function unquoteGitPath(value: string): string {
  if (!value.startsWith('"') || !value.endsWith('"')) return value;
  try {
    return JSON.parse(value) as string;
  } catch {
    return value.slice(1, -1);
  }
}

function fileFromDiffHeader(text: string): string | undefined {
  const quotedMarker = '" "b/';
  const quotedIndex = text.lastIndexOf(quotedMarker);
  if (text.startsWith('diff --git "a/') && quotedIndex >= 0) {
    return unquoteGitPath(
      `"${text.slice(quotedIndex + quotedMarker.length)}`,
    ).replace(/^b\//, "");
  }
  const marker = " b/";
  const index = text.lastIndexOf(marker);
  return text.startsWith("diff --git a/") && index >= 0
    ? text.slice(index + marker.length)
    : undefined;
}

function fileFromTargetHeader(text: string): string | undefined {
  if (text.startsWith("+++ b/")) return text.slice("+++ b/".length);
  if (!text.startsWith('+++ "b/')) return undefined;
  return unquoteGitPath(text.slice("+++ ".length)).replace(/^b\//, "");
}

function parseDiff(patch: string): DiffLine[] {
  let oldLine = 0;
  let newLine = 0;
  let currentFile = "working-tree.patch";
  let inHunk = false;
  return patch.split("\n").map((text) => {
    if (text.startsWith("diff --git ")) {
      currentFile = fileFromDiffHeader(text) ?? currentFile;
      oldLine = 0;
      newLine = 0;
      inHunk = false;
      return { kind: "meta", text, file: currentFile };
    }
    const targetFile = fileFromTargetHeader(text);
    if (targetFile) currentFile = targetFile;
    if (text.startsWith("@@")) {
      const match = /@@ -(\d+)(?:,\d+)? \+(\d+)/.exec(text);
      if (match) {
        oldLine = Number(match[1]);
        newLine = Number(match[2]);
      }
      inHunk = Boolean(match);
      return { kind: "meta", text, file: currentFile };
    }
    if (!inHunk || text.startsWith("\\ No newline"))
      return { kind: "meta", text, file: currentFile };
    if (text.startsWith("+"))
      return { kind: "add", text, file: currentFile, newLine: newLine++ };
    if (text.startsWith("-"))
      return { kind: "delete", text, file: currentFile, oldLine: oldLine++ };
    if (text.startsWith(" "))
      return {
        kind: "context",
        text,
        file: currentFile,
        oldLine: oldLine++,
        newLine: newLine++,
      };
    return { kind: "meta", text, file: currentFile };
  });
}

export function DiffViewer({
  patch,
  annotations,
  onAnnotate,
}: {
  patch: string;
  annotations: Annotation[];
  onAnnotate: (file: string, line: number) => void;
}) {
  const [mode, setMode] = useState<"unified" | "raw">("unified");
  const lines = useMemo(() => parseDiff(patch), [patch]);
  const files = useMemo(
    () => [...new Set(lines.map((line) => line.file))],
    [lines],
  );
  const patchLabel =
    files.length <= 1
      ? (files[0] ?? "working-tree.patch")
      : `${files.length} files`;
  const commentCounts = useMemo(() => {
    const counts = new Map<string, number>();
    for (const annotation of annotations) {
      if (!annotation.file || annotation.line === undefined) continue;
      const key = `${annotation.file}\u0000${annotation.line}`;
      counts.set(key, (counts.get(key) ?? 0) + 1);
    }
    return counts;
  }, [annotations]);

  return (
    <div className="diff-panel surface">
      <div className="panel-toolbar">
        <div>
          <span className="file-indicator" /> <strong>{patchLabel}</strong>
        </div>
        <div className="segmented">
          <button
            className={mode === "unified" ? "active" : ""}
            onClick={() => setMode("unified")}
            type="button"
          >
            Unified
          </button>
          <button
            className={mode === "raw" ? "active" : ""}
            onClick={() => setMode("raw")}
            type="button"
          >
            Raw
          </button>
        </div>
      </div>
      {patch ? (
        mode === "raw" ? (
          <pre className="raw-patch">{patch}</pre>
        ) : (
          <div className="diff-lines">
            {lines.map((line, index) => {
              const lineNumber = line.newLine ?? line.oldLine;
              const commentCount =
                lineNumber !== undefined
                  ? (commentCounts.get(`${line.file}\u0000${lineNumber}`) ?? 0)
                  : 0;
              return (
                <div
                  className={`diff-line ${line.kind}`}
                  key={`${index}-${line.text}`}
                >
                  <span className="line-number">{line.oldLine ?? ""}</span>
                  <span className="line-number">{line.newLine ?? ""}</span>
                  <code>{line.text || " "}</code>
                  {lineNumber !== undefined && line.kind !== "meta" ? (
                    <button
                      aria-label={`Annotate ${line.file} line ${lineNumber}`}
                      className="line-comment"
                      onClick={() => onAnnotate(line.file, lineNumber)}
                      type="button"
                    >
                      <CommentIcon size={14} />
                      {commentCount ? <b>{commentCount}</b> : null}
                    </button>
                  ) : null}
                </div>
              );
            })}
          </div>
        )
      ) : (
        <div className="empty-diff">
          <span>✓</span>
          <strong>Worktree matches the baseline</strong>
          <p>Agent changes will appear here as a binary-capable Git patch.</p>
        </div>
      )}
    </div>
  );
}
