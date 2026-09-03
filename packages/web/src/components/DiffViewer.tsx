import { useEffect, useMemo, useRef, useState } from "react";
import type {
  Annotation,
  CodeAnnotation,
  SealedCodeLine,
  SealedCodeReview,
} from "../types";
import { CommentIcon } from "./Icons";

interface DiffLine extends SealedCodeLine {
  file?: string;
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
  review,
  reference,
}: {
  patch: string;
  annotations: Annotation[];
  onAnnotate?: (anchor: CodeAnnotation) => void;
  review?: SealedCodeReview;
  reference?: CodeAnnotation;
}) {
  const [mode, setMode] = useState<"unified" | "raw">("unified");
  const lines: DiffLine[] = useMemo(
    () => review?.lines ?? parseDiff(patch),
    [patch, review],
  );
  const referenceElement = useRef<HTMLDivElement>(null);
  useEffect(() => {
    referenceElement.current?.scrollIntoView?.({ block: "center" });
  }, [reference, review]);
  const files = useMemo(
    () => [
      ...new Set(
        lines
          .map((line) => line.newPath || line.oldPath || line.file)
          .filter(Boolean),
      ),
    ],
    [lines],
  );
  const patchLabel =
    files.length <= 1
      ? (files[0] ?? "working-tree.patch")
      : `${files.length} files`;
  const commentCounts = useMemo(() => {
    const counts = new Map<string, number>();
    for (const annotation of annotations) {
      if (
        !review ||
        annotation.target?.roundId !== review.roundId ||
        !annotation.target.side ||
        !annotation.file ||
        annotation.line === undefined
      )
        continue;
      const key = `${annotation.target.side}\u0000${annotation.file}\u0000${annotation.line}`;
      counts.set(key, (counts.get(key) ?? 0) + 1);
    }
    return counts;
  }, [annotations, review]);

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
              const anchors: CodeAnnotation[] =
                review && line.kind !== "meta"
                  ? (["old", "new"] as const).flatMap((side) => {
                      const file = side === "old" ? line.oldPath : line.newPath;
                      const number =
                        side === "old" ? line.oldLine : line.newLine;
                      return file && number
                        ? [
                            {
                              file,
                              line: number,
                              target: { roundId: review.roundId, side },
                            },
                          ]
                        : [];
                    })
                  : [];
              const referenced = anchors.some(
                (anchor) =>
                  reference?.target.roundId === anchor.target.roundId &&
                  reference.target.side === anchor.target.side &&
                  reference.file === anchor.file &&
                  reference.line === anchor.line,
              );
              return (
                <div
                  className={`diff-line ${line.kind}${referenced ? " referenced" : ""}`}
                  key={`${index}-${line.text}`}
                  ref={referenced ? referenceElement : undefined}
                  aria-current={referenced ? "location" : undefined}
                >
                  <span className="line-number">{line.oldLine ?? ""}</span>
                  <span className="line-number">{line.newLine ?? ""}</span>
                  <code>{line.text || " "}</code>
                  {onAnnotate && anchors.length ? (
                    <span className="line-annotation-actions">
                      {anchors.map((anchor) => {
                        const count =
                          commentCounts.get(
                            `${anchor.target.side}\u0000${anchor.file}\u0000${anchor.line}`,
                          ) ?? 0;
                        return (
                          <button
                            key={anchor.target.side}
                            aria-label={`Annotate ${anchor.file} ${anchor.target.side} line ${anchor.line}`}
                            className="line-comment"
                            onClick={() => onAnnotate(anchor)}
                            type="button"
                          >
                            <CommentIcon size={14} />
                            {anchor.target.side === "old" ? "旧" : "新"}
                            {count ? <b>{count}</b> : null}
                          </button>
                        );
                      })}
                    </span>
                  ) : null}
                </div>
              );
            })}
          </div>
        )
      ) : (
        <div className="empty-diff">
          <span>✓</span>
          <strong>
            {review
              ? "所选 Round 与 baseline 一致"
              : "Worktree matches the baseline"}
          </strong>
          <p>
            {review
              ? "此不可变快照没有可批注的改动行。"
              : "这是实时 worktree 视图，不是封存 Round，不能创建代码批注。"}
          </p>
        </div>
      )}
    </div>
  );
}
