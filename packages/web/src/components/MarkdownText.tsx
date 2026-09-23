import { t } from "../i18n";
import { useEffect, useMemo, useState } from "react";
import Markdown, { type Components } from "react-markdown";
import remarkGfm from "remark-gfm";
import type { Element, ElementContent, Root, RootContent } from "hast";
import { codeOffsets, textOffsets } from "../reading";
import type { syntaxHighlighter } from "../syntax";
import { Copy } from "./ui";

function nodeText(node: RootContent): string {
  return node.type === "text"
    ? node.value
    : node.type === "element"
      ? node.children.map(nodeText).join("")
      : "";
}

const components: Components = {
  a: ({ href, children }) =>
    /^https?:\/\//i.test(href || "") ? (
      <a href={href} target="_blank" rel="noreferrer noopener">
        {children}
      </a>
    ) : (
      <span>{children}</span>
    ),
  img: ({ alt }) => (
    <span className="reader-media-note">
      {t("图片：")}
      {alt || t("未命名")}
      {t("（未加载外部内容）") + " "}
    </span>
  ),
  pre: ({ children, node }) => {
    const original = node ? nodeText(node) : "";
    return (
      <div className="reader-code">
        <div className="reader-code-tools">
          <span>{t("代码")}</span>
          <Copy label={t("复制代码")} text={original} />
        </div>
        <pre>{children}</pre>
      </div>
    );
  },
  table: ({ children }) => (
    <div className="reader-table">
      <table>{children}</table>
    </div>
  ),
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
}: {
  source: string;
  highlights?: Highlight[];
  raw?: boolean;
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
              const map = codeOffsets(
                source.slice(start, end),
                value,
                start,
                block,
              );
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
    <div className="reader-markdown">
      <Markdown
        remarkPlugins={[remarkGfm]}
        rehypePlugins={[plugin]}
        skipHtml
        components={components}
      >
        {source}
      </Markdown>
    </div>
  );
}
