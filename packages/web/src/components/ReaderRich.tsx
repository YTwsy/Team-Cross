import { useEffect, useId, useRef, useState, type ReactNode } from "react";
import DOMPurify from "dompurify";
import katex from "katex";
import type { Element, RootContent } from "hast";
import type { Mounted } from "pele/lazy";
import { t } from "../i18n";
import { Copy, Icon, Modal } from "./ui";
import { MediaZoom } from "./ReaderMedia";

export function nodeText(node: RootContent): string {
  return node.type === "text"
    ? node.value
    : node.type === "element"
      ? node.children.map(nodeText).join("")
      : "";
}

function sanitizeDiagram(source: string): Node {
  const fragment = DOMPurify.sanitize(source, {
    USE_PROFILES: { svg: true, svgFilters: true },
    FORBID_TAGS: ["foreignObject", "image", "a", "style"],
    FORBID_ATTR: ["href", "xlink:href"],
    RETURN_DOM_FRAGMENT: true,
  });
  fragment.querySelectorAll("[style]").forEach((el) => {
    if (/url\s*\(|@import/i.test(el.getAttribute("style") || ""))
      el.removeAttribute("style");
  });
  return fragment;
}

export function Diagram({
  source,
  onError,
}: {
  source: string;
  onError: (error: string) => void;
}) {
  const root = useRef<HTMLDivElement>(null);
  const id = useId().replace(/[^\w-]/g, "");
  useEffect(() => {
    let live = true,
      mounted: Mounted | undefined;
    void import("pele/lazy")
      .then(async (pele) => {
        const type = pele.detectType(source);
        if (!type || !(await pele.load(type)))
          throw new Error(t("此图形暂不支持，请查看源码。"));
        if (!live || !root.current) return;
        mounted = pele.mount(root.current, source, {
          idPrefix: `tc-${id}-`,
          autoDirection: false,
          zoom: false,
          links: false,
          images: false,
          limit: 50000,
          maxEdges: 500,
          sanitize: sanitizeDiagram,
          onError: () => {
            if (live) onError(t("图形无法渲染，已保留源码。"));
          },
        });
      })
      .catch(() => {
        if (live) onError(t("图形无法渲染，已保留源码。"));
      });
    return () => {
      live = false;
      mounted?.destroy();
    };
  }, [source, id]);
  return (
    <div
      ref={root}
      className="reader-diagram pele"
      role="img"
      aria-label={t("Mermaid 图形")}
      data-source-atomic
    />
  );
}

export function MathFormula({
  value,
  display = false,
}: {
  value: string;
  display?: boolean;
}) {
  try {
    const html = katex.renderToString(value, {
      displayMode: display,
      output: "mathml",
      throwOnError: true,
      trust: false,
      strict: "ignore",
      maxExpand: 1000,
      maxSize: 20,
    });
    return (
      <span
        className={`reader-math${display ? " reader-math-display" : ""}`}
        data-source-atomic
        title={value}
        dangerouslySetInnerHTML={{ __html: html }}
      />
    );
  } catch {
    return (
      <code
        className="reader-math-error"
        title={t("公式无法渲染，已保留源码。")}
      >
        {value}
      </code>
    );
  }
}

export function ReaderCode({
  children,
  node,
  streaming,
  onAnnotate,
  active = false,
}: {
  children: ReactNode;
  node?: Element;
  streaming?: boolean;
  active?: boolean;
  onAnnotate?: (start: number, end: number, element: HTMLElement) => void;
}) {
  const code = node?.children.find(
    (n): n is Element => n.type === "element" && n.tagName === "code",
  );
  const classes = String(code?.properties.className || "");
  const language = /language-([\w+-]+)/.exec(classes)?.[1] || "";
  const original = node ? nodeText(node) : "";
  const mermaid = language === "mermaid";
  const math = classes.includes("math-display");
  const [raw, setRaw] = useState(false),
    [wrap, setWrap] = useState(false),
    [expanded, setExpanded] = useState(false),
    [dialog, setDialog] = useState(false),
    [error, setError] = useState("");
  const root = useRef<HTMLDivElement>(null);
  useEffect(() => setError(""), [original]);
  const sourceStart = node?.position?.start.offset,
    sourceEnd = node?.position?.end.offset;
  const graphical = (mermaid || math) && !raw && !streaming && !error;
  useEffect(() => {
    if (active) {
      setExpanded(true);
      if (mermaid || math) setRaw(true);
    }
  }, [active, mermaid, math]);
  const long = original.split("\n").length > 20 || original.length > 1800;
  return (
    <div
      ref={root}
      className={`reader-code${wrap ? " reader-code-wrap" : ""}${expanded || active ? " reader-code-expanded" : ""}`}
    >
      <div className="reader-code-tools">
        <span className="reader-code-language">{language || t("代码")}</span>
        <div className="reader-code-actions">
          {(mermaid || math) && (
            <button
              type="button"
              onClick={() => setRaw(!raw)}
              disabled={streaming}
              aria-pressed={raw}
              aria-label={raw ? t("显示图形") : t("显示源码")}
              title={raw ? t("显示图形") : t("显示源码")}
            >
              <Icon name={raw ? "diagram" : "code"} size={15} />
            </button>
          )}
          {graphical && mermaid && (
            <button
              type="button"
              onClick={() => setDialog(true)}
              aria-label={t("展开图形")}
              title={t("展开图形")}
            >
              <Icon name="expand" size={15} />
            </button>
          )}
          {!graphical && (
            <button
              type="button"
              aria-pressed={wrap}
              onClick={() => setWrap(!wrap)}
              aria-label={t("自动换行")}
              title={t("自动换行")}
            >
              <Icon name="wrap" size={15} />
            </button>
          )}
          {!graphical && long && (
            <button
              type="button"
              aria-expanded={expanded}
              onClick={() => setExpanded(!expanded)}
              aria-label={expanded ? t("收起代码") : t("展开代码")}
              title={expanded ? t("收起代码") : t("展开代码")}
            >
              <Icon name={expanded ? "collapse" : "expand"} size={15} />
            </button>
          )}
          {onAnnotate &&
            sourceStart !== undefined &&
            sourceEnd !== undefined &&
            sourceEnd - sourceStart <= 8000 && (
              <button
                type="button"
                onClick={() =>
                  onAnnotate(sourceStart, sourceEnd, root.current!)
                }
                aria-label={t("批注此块")}
                title={t("批注此块")}
              >
                <Icon name="comment" size={15} />
              </button>
            )}
          <Copy
            label={t("复制代码")}
            text={original.replace(/\n$/, "")}
            iconOnly
          />
        </div>
      </div>
      {error && (
        <div className="reader-render-error" role="status">
          {error}
          <button className="text-button" onClick={() => setError("")}>
            {t("重试")}
          </button>
        </div>
      )}
      {streaming && (mermaid || math) && (
        <span className="small-text muted">{t("生成结束后显示图形")}</span>
      )}
      {graphical ? (
        mermaid ? (
          <Diagram source={original} onError={setError} />
        ) : (
          <MathFormula value={original} display />
        )
      ) : (
        <pre tabIndex={0}>{children}</pre>
      )}
      {dialog && (
        <Modal title={t("Mermaid 图形")} wide onClose={() => setDialog(false)}>
          <MediaZoom>
            <Diagram source={original} onError={setError} />
          </MediaZoom>
        </Modal>
      )}
    </div>
  );
}

export function ReaderTable({
  node,
  source,
  children,
}: {
  node?: Element;
  source: string;
  children: ReactNode;
}) {
  const [wrap, setWrap] = useState(false);
  const rows: string[][] = [];
  const walk = (n: RootContent) => {
    if (n.type !== "element") return;
    if (n.tagName === "tr")
      rows.push(
        n.children
          .filter(
            (c): c is Element =>
              c.type === "element" && ["th", "td"].includes(c.tagName),
          )
          .map(nodeText),
      );
    else n.children.forEach(walk);
  };
  if (node) walk(node);
  const csv = rows
    .map((row) =>
      row.map((cell) => `"${cell.replaceAll('"', '""')}"`).join(","),
    )
    .join("\r\n");
  const markdown = source.slice(
    node?.position?.start.offset,
    node?.position?.end.offset,
  );
  return (
    <div className={`reader-table-block${wrap ? " reader-table-wrap" : ""}`}>
      <div className="reader-rich-tools">
        <button
          type="button"
          className="reader-icon-button"
          aria-pressed={wrap}
          aria-label={t("自动换行")}
          title={t("自动换行")}
          onClick={() => setWrap(!wrap)}
        >
          <Icon name="wrap" size={15} />
        </button>
        <Copy label={t("复制 Markdown")} text={markdown} iconOnly icon="code" />
        <Copy label={t("复制 CSV")} text={csv} iconOnly icon="table" />
      </div>
      <div
        className="reader-table"
        tabIndex={0}
        role="region"
        aria-label={t("表格横向滚动")}
      >
        <table>{children}</table>
      </div>
    </div>
  );
}
