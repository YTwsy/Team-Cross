import { t } from "../i18n";
import { createContext, useContext, useEffect, useMemo, useState } from "react";
import Markdown, { type Components } from "react-markdown";
import remarkGfm from "remark-gfm";
import remarkMath from "remark-math";
import type { Element, ElementContent, Root, RootContent } from "hast";
import { codeOffsets, textOffsets } from "../reading";
import type { syntaxHighlighter } from "../syntax";
import { ReaderCode, ReaderTable, MathFormula, nodeText } from "./ReaderRich";

type RichOptions = {
  activeRanges?: { start: number; end: number }[];
  source: string;
  streaming?: boolean;
  onAnnotate?: (start: number, end: number, element: HTMLElement) => void;
};
const RichContext = createContext<RichOptions>({ source: "" });
const components: Components = {
  a: ({ href, children }) =>
    /^https?:\/\//i.test(href || "") ? (
      <a href={href} target="_blank" rel="noreferrer noopener">
        {children}
      </a>
    ) : (
      <span>{children}</span>
    ),
  img: ({ alt, src }) => (
    <span className="reader-media-note">
      {t("图片：")}
      {alt || t("未命名")}
      {t("（未加载外部内容）")}
      {/^https?:\/\//i.test(src || "") && (
        <a href={src} target="_blank" rel="noreferrer noopener">
          {t("打开图片来源")}
        </a>
      )}
    </span>
  ),
  pre: function Pre({ children, node }) {
    const options = useContext(RichContext);
    return (
      <ReaderCode
        node={node}
        active={options.activeRanges?.some(
          (h) =>
            h.start < (node?.position?.end.offset ?? 0) &&
            h.end > (node?.position?.start.offset ?? 0),
        )}
        streaming={options.streaming}
        onAnnotate={options.onAnnotate}
      >
        {children}
      </ReaderCode>
    );
  },
  code: function Code({ children, node, className, ...props }) {
    return className?.includes("math-inline") && node ? (
      <MathFormula value={nodeText(node)} />
    ) : (
      <code className={className} {...props}>
        {children}
      </code>
    );
  },
  table: function Table({ children, node }) {
    const { source } = useContext(RichContext);
    return (
      <ReaderTable source={source} node={node}>
        {children}
      </ReaderTable>
    );
  },
};

type Highlight = { start: number; end: number; active?: boolean };
type Colorize = Awaited<ReturnType<typeof syntaxHighlighter>>;
let syntax: Promise<Colorize> | undefined;

function mappedNodes(
  value: string,
  map: number[],
  highlights: Highlight[],
  style?: string,
): ElementContent[] {
  const groups: ElementContent[] = [];
  let start = 0;
  const status = (i: number) =>
    highlights.reduce(
      (state, h) =>
        map[i]! < h.end && map[i + 1]! > h.start
          ? Math.max(state, h.active ? 2 : 1)
          : state,
      0,
    );
  while (start < value.length) {
    const level = status(start);
    let end = start + 1;
    while (end < value.length && status(end) === level) end++;
    const slice = map.slice(start, end + 1);
    const properties: Element["properties"] = {
      "data-source-start": slice[0],
      "data-source-end": slice.at(-1),
    };
    if (slice.some((offset, n) => offset !== slice[0]! + n))
      properties["data-source-map"] = JSON.stringify(slice);
    if (style) properties.style = style;
    const text: Element = {
      type: "element",
      tagName: "span",
      properties,
      children: [{ type: "text", value: value.slice(start, end) }],
    };
    groups.push(
      level
        ? {
            type: "element",
            tagName: "mark",
            properties: {
              "data-annotation-highlight": level === 2 ? "true" : undefined,
            },
            children: [text],
          }
        : text,
    );
    start = end;
  }
  return groups;
}

export function MarkdownText({
  source,
  highlights = [],
  raw = false,
  streaming = false,
  onAnnotate,
}: {
  source: string;
  highlights?: Highlight[];
  raw?: boolean;
  streaming?: boolean;
  onAnnotate?: RichOptions["onAnnotate"];
}) {
  const [colorize, setColorize] = useState<Colorize>();
  useEffect(() => {
    if (raw || !/```|~~~/.test(source)) return;
    let live = true;
    syntax ||= import("../syntax").then((m) => m.syntaxHighlighter());
    void syntax
      .then((fn) => {
        if (live) setColorize(() => fn);
      })
      .catch(() => {});
    return () => {
      live = false;
    };
  }, [raw, source]);
  const plugin = useMemo(
    () => () => (tree: Root) => {
      function walk(parent: Root | Element) {
        const children: RootContent[] = [];
        for (const node of parent.children) {
          // remark-math puts the source position on the enclosing pre, while
          // its generated code child has no position of its own.
          if (
            node.type === "element" &&
            node.tagName === "code" &&
            String(node.properties.className || "").includes("language-math") &&
            !node.position &&
            parent.type === "element" &&
            parent.tagName === "pre"
          ) {
            node.position = parent.position;
          }
          const start = node.position?.start.offset,
            end = node.position?.end.offset;
          if (
            node.type === "text" &&
            start !== undefined &&
            end !== undefined
          ) {
            const map = textOffsets(
              source.slice(start, end),
              node.value,
              start,
            );
            if (map) {
              children.push(...mappedNodes(node.value, map, highlights));
              continue;
            }
          }
          if (node.type === "element") {
            if (
              node.tagName === "code" &&
              start !== undefined &&
              end !== undefined
            ) {
              const value = node.children
                .map((n) => (n.type === "text" ? n.value : ""))
                .join("");
              const block =
                parent.type === "element" && parent.tagName === "pre";
              const math = String(node.properties.className || "").includes(
                "language-math",
              );
              let map = math
                ? undefined
                : codeOffsets(source.slice(start, end), value, start, block);
              if (math) {
                const rawMath = source.slice(start, end);
                const opening = /^\$+[ \t]*(?:\r?\n)?/.exec(rawMath);
                if (opening) {
                  const at = opening[0].length;
                  // Match only the parser-defined body boundary, never search
                  // elsewhere in the message for a repeated formula.
                  const text = value.replace(/\n$/, "");
                  map = textOffsets(
                    rawMath.slice(at, at + text.length),
                    text,
                    start + at,
                    true,
                  );
                  if (map && value.endsWith("\n"))
                    map.push(start + at + text.length);
                }
              }
              if (map) {
                const language = String(
                  node.properties.className || "",
                ).replace(/^language-/, "");
                const tokens = block && colorize?.(value, language);
                if (
                  tokens &&
                  tokens
                    .map((line) => line.map((t) => t.content).join(""))
                    .join("\n") === value
                ) {
                  let offset = 0;
                  node.children = tokens.flatMap((line, index) => {
                    const spans = line.flatMap((token) => {
                      const rendered = mappedNodes(
                        token.content,
                        map.slice(offset, offset + token.content.length + 1),
                        highlights,
                        `color:${token.htmlStyle?.color || token.color};--syntax-dark:${token.htmlStyle?.["--shiki-dark"] || token.color}`,
                      );
                      offset += token.content.length;
                      return rendered;
                    });
                    if (index < tokens.length - 1) {
                      spans.push(
                        ...mappedNodes(
                          "\n",
                          map.slice(offset, offset + 2),
                          highlights,
                        ),
                      );
                      offset++;
                    }
                    return spans;
                  });
                } else node.children = mappedNodes(value, map, highlights);
              }
              children.push(node);
              continue;
            }
            walk(node);
          }
          children.push(node);
        }
        parent.children = children as ElementContent[];
      }
      walk(tree);
    },
    [source, highlights, colorize],
  );
  if (raw) {
    const boundaries = [
      ...new Set([
        0,
        source.length,
        ...highlights.flatMap((h) => [h.start, h.end]),
      ]),
    ]
      .filter((n) => n >= 0 && n <= source.length)
      .sort((a, b) => a - b);
    return (
      <pre className="reader-raw">
        {boundaries.slice(0, -1).map((start, i) => {
          const end = boundaries[i + 1]!,
            h = highlights.find((h) => start < h.end && end > h.start);
          const span = (
            <span data-source-start={start} data-source-end={end}>
              {source.slice(start, end)}
            </span>
          );
          return h ? (
            <mark key={start} data-annotation-highlight={h.active || undefined}>
              {span}
            </mark>
          ) : (
            <span key={start}>{span}</span>
          );
        })}
      </pre>
    );
  }
  return (
    <RichContext.Provider
      value={{
        source,
        streaming,
        onAnnotate,
        activeRanges: highlights.filter((h) => h.active),
      }}
    >
      <div className="reader-markdown">
        <Markdown
          remarkPlugins={[remarkGfm, remarkMath]}
          rehypePlugins={[plugin]}
          skipHtml
          components={components}
        >
          {source}
        </Markdown>
      </div>
    </RichContext.Provider>
  );
}
