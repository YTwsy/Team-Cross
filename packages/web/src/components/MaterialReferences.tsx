import { useId, useRef, useState } from "react";
import { api, errorText } from "../api";
import {
  relativeTime,
  type Material,
  type MaterialPage,
  type MaterialReference,
} from "../types";
import { ErrorBox, Icon } from "./ui";

export function MaterialReferenceLinks({
  references = [],
  materials = [],
  onLocate,
}: {
  references?: MaterialReference[];
  materials?: Material[];
  onLocate: (ref: MaterialReference) => void;
}) {
  if (!references.length) return null;
  return (
    <div className="material-reference-links">
      {references.map((r, i) => {
        const m = materials.find((m) => m.id === r.materialId),
          v = m?.versions.find((v) => v.version === r.version);
        return (
          <button
            type="button"
            className="material-reference-chip"
            key={`${r.materialId}-${r.version}-${i}`}
            onClick={() => onLocate(r)}
          >
            <Icon name="link" size={13} />
            {v?.title || "会话材料"} · v{r.version}
            {m?.withdrawnAt
              ? " · 已撤回"
              : m && r.version !== m.versions.at(-1)?.version
                ? " · 有新版本"
                : ""}
          </button>
        );
      })}
    </div>
  );
}

export function MaterialReferencePicker({
  materials = [],
  value,
  onChange,
  disabled,
  spaceId,
}: {
  materials?: Material[];
  value: MaterialReference[];
  onChange: (refs: MaterialReference[]) => void;
  disabled: boolean;
  spaceId?: string;
}) {
  const [open, setOpen] = useState(false),
    [query, setQuery] = useState("");
  const [preview, setPreview] = useState<{
    ref: MaterialReference;
    title: string;
    text?: string;
    error?: string;
  }>();
  const request = useRef(0),
    search = useRef<HTMLInputElement>(null),
    trigger = useRef<HTMLButtonElement>(null),
    id = useId();
  const available = materials.filter((m) => !m.withdrawnAt);
  const choices = available.filter((m) =>
    `${m.author} ${m.versions.map((v) => v.title).join(" ")}`
      .toLocaleLowerCase()
      .includes(query.trim().toLocaleLowerCase()),
  );
  function add(ref: MaterialReference) {
    if (disabled || value.length >= 16) return;
    if (
      !value.some(
        (r) => r.materialId === ref.materialId && r.version === ref.version,
      )
    )
      onChange([...value, ref]);
    setOpen(false);
    trigger.current?.focus();
  }
  async function read(ref: MaterialReference, title: string) {
    const serial = ++request.current;
    setPreview({ ref, title });
    try {
      const page = await api<MaterialPage>(
        `collaborations/${spaceId}/read-material`,
        ref,
      );
      if (serial === request.current)
        setPreview({
          ref,
          title,
          text: page.segments
            .map((s) => s.text)
            .join("\n\n")
            .slice(0, 2000),
        });
    } catch (e) {
      if (serial === request.current)
        setPreview({ ref, title, error: errorText(e) });
    }
  }
  return (
    <div
      className="material-reference-picker"
      onKeyDown={(e) => {
        if (e.key === "Escape" && open) {
          e.stopPropagation();
          setOpen(false);
          trigger.current?.focus();
        }
      }}
    >
      {!!value.length && (
        <div className="material-reference-tags">
          {value.map((ref, i) => {
            const m = materials.find((m) => m.id === ref.materialId),
              v = m?.versions.find((v) => v.version === ref.version);
            return (
              <span
                className="material-reference-chip"
                key={`${ref.materialId}:${ref.version}`}
              >
                <Icon name="link" size={12} />
                <span>
                  {v?.title || "会话材料"} · v{ref.version}
                  {m?.withdrawnAt && " · 已撤回"}
                </span>
                <button
                  type="button"
                  className="icon-button"
                  aria-label={`移除引用 ${v?.title || "会话材料"} v${ref.version}`}
                  disabled={disabled}
                  onClick={() => onChange(value.filter((_, n) => n !== i))}
                >
                  <Icon name="close" size={12} />
                </button>
              </span>
            );
          })}
        </div>
      )}
      <button
        ref={trigger}
        type="button"
        className="text-button reference-trigger"
        aria-expanded={open}
        aria-controls={id}
        disabled={disabled || value.length >= 16}
        onClick={() => {
          setOpen(!open);
          setPreview(undefined);
          request.current++;
        }}
      >
        <Icon name="link" size={14} />
        引用材料{value.length ? `（${value.length}）` : ""}
      </button>
      {open && (
        <div
          className="reference-menu"
          id={id}
          role="group"
          aria-label="选择引用材料"
        >
          <div className="reference-menu-heading">
            <strong>引用已发布材料</strong>
            <button
              type="button"
              className="icon-button"
              aria-label="关闭材料选择"
              onClick={() => {
                setOpen(false);
                trigger.current?.focus();
              }}
            >
              <Icon name="close" size={14} />
            </button>
          </div>
          <label className="sr-only" htmlFor={`${id}-search`}>
            搜索材料标题或作者
          </label>
          <input
            ref={search}
            id={`${id}-search`}
            autoFocus
            type="search"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="搜索标题或作者"
          />
          {choices.map((m) => {
            const versions = m.versions
                .slice()
                .sort((a, b) => b.version - a.version),
              latest = versions[0]!;
            const row = (v: typeof latest) => (
              <div className="reference-choice" key={v.version}>
                <button
                  type="button"
                  className="reference-choice-main"
                  disabled={value.some(
                    (r) => r.materialId === m.id && r.version === v.version,
                  )}
                  onClick={() => add({ materialId: m.id, version: v.version })}
                >
                  <strong>
                    {v.title} · v{v.version}
                  </strong>
                  <span>
                    {m.author} · {v.turnCount} 轮 · {relativeTime(v.createdAt)}
                    {v === latest ? " · 最新" : ""}
                  </span>
                </button>
                {spaceId && (
                  <button
                    type="button"
                    className="text-button"
                    aria-label={`预览 ${v.title} v${v.version}`}
                    onClick={() =>
                      void read(
                        { materialId: m.id, version: v.version },
                        v.title,
                      )
                    }
                  >
                    预览
                  </button>
                )}
              </div>
            );
            return (
              <div className="reference-material" key={m.id}>
                {row(latest)}
                {versions.length > 1 && (
                  <details>
                    <summary>选择历史版本（{versions.length - 1}）</summary>
                    {versions.slice(1).map(row)}
                  </details>
                )}
              </div>
            );
          })}
          {!choices.length && (
            <p className="small-text muted">
              {available.length
                ? "没有找到匹配的材料。"
                : "还没有已发布材料。先在材料区发布会话，再回到这份草稿引用。"}
            </p>
          )}
          {preview && (
            <section className="reference-preview" aria-label="材料预览">
              <strong>
                {preview.title} · v{preview.ref.version}
              </strong>
              <ErrorBox message={preview.error} />
              {preview.text !== undefined ? (
                <pre>{preview.text || "这页没有文字内容。"}</pre>
              ) : (
                !preview.error && <p role="status">正在读取预览…</p>
              )}
              <p className="small-text muted">
                仅预览首屏片段，完整内容可在材料区阅读。
              </p>
            </section>
          )}
          <p className="small-text muted">
            引用保留所选版本；新增发布不会替换已有引用。
          </p>
        </div>
      )}
    </div>
  );
}
