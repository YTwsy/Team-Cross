import { useEffect, useRef, useState } from "react";
import { api, errorText } from "../api";
import { t, tr } from "../i18n";
import { sameReference, type LibraryReference } from "../library";
import type { Collaboration, MaterialPage } from "../types";
import { MarkdownText } from "./MarkdownText";
import { ErrorBox, Icon, Modal } from "./ui";

export function spaceReferenceTitle(c: Collaboration, ref: LibraryReference) {
  if (ref.kind === "annotation")
    return (
      c.annotations?.find((a) => a.id === ref.annotationId)?.text || t("原批注")
    );
  const material = c.materials?.find((m) => m.id === ref.materialId);
  return `${material?.versions.find((v) => v.version === ref.version)?.title || t("会话材料")} · v${ref.version}`;
}

// Opening and previewing never change the composer's fixed references.
export function SpaceReferencePicker({
  collaboration: c,
  value,
  onChange,
  disabled,
  compact = false,
}: {
  collaboration: Collaboration;
  value: LibraryReference[];
  onChange: (value: LibraryReference[]) => void;
  disabled: boolean;
  compact?: boolean;
}) {
  const [open, setOpen] = useState(false);
  const [draft, setDraft] = useState<LibraryReference[]>([]);
  const [kind, setKind] = useState<"material" | "annotation">("material");
  const [query, setQuery] = useState("");
  const [preview, setPreview] = useState<LibraryReference>();
  const [text, setText] = useState<string>();
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);
  const request = useRef(0);
  const controller = useRef<AbortController | undefined>(undefined);
  const trigger = useRef<HTMLButtonElement>(null);
  useEffect(
    () => () => {
      controller.current?.abort();
      request.current++;
    },
    [],
  );
  const materials = (c.materials || []).filter(
    (m) => !m.withdrawnAt && m.versions.length,
  );
  const notes = (c.annotations || []).filter(
    (a) => !a.target || a.target.kind === "material",
  );
  const material = materials.find((m) => m.id === preview?.materialId);
  const note = notes.find((a) => a.id === preview?.annotationId);
  function inspect(ref: LibraryReference) {
    controller.current?.abort();
    request.current++;
    setPreview(ref);
    setText(undefined);
    setError("");
    setLoading(false);
  }
  function close() {
    controller.current?.abort();
    request.current++;
    setOpen(false);
    trigger.current?.focus();
  }
  function select(ref: LibraryReference) {
    if (disabled) return;
    const other = draft.filter(
      (r) =>
        !(
          r.kind === ref.kind &&
          (ref.kind === "material"
            ? r.materialId === ref.materialId
            : r.annotationId === ref.annotationId)
        ),
    );
    if (other.length < 16) setDraft([...other, ref]);
  }
  async function read() {
    if (!preview || preview.kind !== "material") return;
    const serial = ++request.current;
    controller.current?.abort();
    controller.current = new AbortController();
    setLoading(true);
    setError("");
    try {
      const page = await api<MaterialPage>(
        `collaborations/${c.id}/read-material`,
        { materialId: preview.materialId, version: preview.version },
        controller.current.signal,
      );
      if (serial === request.current)
        setText(
          page.segments
            .map((s) => s.text)
            .join("\n\n")
            .slice(0, 2400),
        );
    } catch (e) {
      if (serial === request.current && !controller.current?.signal.aborted)
        setError(errorText(e));
    } finally {
      if (serial === request.current) setLoading(false);
    }
  }
  const matches = (text: string) =>
    text.toLocaleLowerCase().includes(query.trim().toLocaleLowerCase());
  const refs: { ref: LibraryReference; title: string; detail: string }[] =
    kind === "material"
      ? materials
          .filter((m) =>
            matches(`${m.author} ${m.versions.map((v) => v.title).join(" ")}`),
          )
          .map((m) => {
            const selected = draft.find(
              (r) => r.kind === "material" && r.materialId === m.id,
            );
            const version =
              m.versions.find((v) => v.version === selected?.version) ||
              m.versions.at(-1)!;
            return {
              ref: selected || {
                spaceId: c.id,
                kind: "material" as const,
                materialId: m.id,
                version: version.version,
              },
              title: version.title,
              detail: `${m.author} · ${version.provider}`,
            };
          })
      : notes
          .filter((a) => matches(`${a.author} ${a.text}`))
          .map((a) => ({
            ref: {
              spaceId: c.id,
              kind: "annotation" as const,
              annotationId: a.id,
            },
            title: a.text,
            detail: a.author,
          }));
  return (
    <div
      className={`space-reference-picker ${compact ? "space-reference-compact" : ""}`}
    >
      <div className="space-reference-heading">
        <strong>
          {t("引用内容")} <span className="count">{value.length}</span>
        </strong>
        <button
          type="button"
          ref={trigger}
          className="text-button"
          disabled={disabled}
          onClick={() => {
            setDraft(value.map((r) => ({ ...r })));
            setQuery("");
            setKind("material");
            inspect(
              value.find((r) => r.kind === "material") ||
                (materials[0]
                  ? {
                      spaceId: c.id,
                      kind: "material",
                      materialId: materials[0].id,
                      version: materials[0].versions.at(-1)!.version,
                    }
                  : { spaceId: c.id, kind: "material" }),
            );
            setOpen(true);
          }}
        >
          <Icon name={compact ? "attach" : "plus"} size={14} />
          {t("添加引用")}
        </button>
      </div>
      <div className="space-reference-chips">
        {value.map((ref) => (
          <div className="space-reference-chip" key={JSON.stringify(ref)}>
            <Icon
              name={ref.kind === "annotation" ? "comment" : "book"}
              size={15}
            />
            <span title={spaceReferenceTitle(c, ref)}>
              {spaceReferenceTitle(c, ref)}
            </span>
            <button
              type="button"
              className="icon-button"
              aria-label={tr`移除引用：${spaceReferenceTitle(c, ref)}`}
              disabled={disabled}
              onClick={() =>
                onChange(value.filter((r) => !sameReference(r, ref)))
              }
            >
              <Icon name="close" size={13} />
            </button>
          </div>
        ))}
        {!value.length && (
          <p className="muted small-text">
            {t("可同时引用已发布材料的固定版本和公开批注。")}
          </p>
        )}
      </div>
      {open && (
        <Modal title={t("添加引用")} onClose={close} wide>
          <div className="space-reference-dialog">
            <label className="space-material-search">
              <Icon name="search" size={16} />
              <input
                type="search"
                autoFocus
                aria-label={t("搜索引用")}
                placeholder={t("搜索标题、作者或批注内容")}
                value={query}
                onChange={(e) => setQuery(e.target.value)}
              />
            </label>
            <div
              className="space-reference-kinds"
              role="group"
              aria-label={t("引用类型")}
            >
              <button
                className="button small"
                aria-pressed={kind === "material"}
                onClick={() => setKind("material")}
              >
                {t("材料")} {materials.length}
              </button>
              <button
                className="button small"
                aria-pressed={kind === "annotation"}
                onClick={() => setKind("annotation")}
              >
                {t("批注")} {notes.length}
              </button>
            </div>
            <div className="space-reference-grid">
              <div className="space-reference-choices">
                {refs.map(({ ref, title, detail }) => {
                  const checked = draft.some((r) => sameReference(r, ref));
                  return (
                    <div
                      className="space-reference-choice"
                      key={ref.materialId || ref.annotationId}
                    >
                      <input
                        type="checkbox"
                        aria-label={tr`选择 ${title}`}
                        checked={checked}
                        disabled={disabled || (!checked && draft.length >= 16)}
                        onChange={(e) =>
                          e.target.checked
                            ? select(ref)
                            : setDraft(
                                draft.filter((r) => !sameReference(r, ref)),
                              )
                        }
                      />
                      <button
                        type="button"
                        onClick={() => inspect(ref)}
                        aria-pressed={!!preview && sameReference(preview, ref)}
                      >
                        <strong>{title}</strong>
                        <small>
                          {detail}
                          {ref.kind === "material" ? ` · v${ref.version}` : ""}
                        </small>
                        {checked && (
                          <small className="space-fixed-reference">
                            {ref.kind === "material"
                              ? tr`已选 v${ref.version} · 固定引用`
                              : t("已选择")}
                          </small>
                        )}
                      </button>
                    </div>
                  );
                })}
                {!refs.length && (
                  <p className="muted">{t("没有找到匹配的引用。")}</p>
                )}
              </div>
              <section
                className="space-reference-preview"
                aria-label={t("引用内容预览")}
              >
                {preview && (material || note) ? (
                  <>
                    <strong>
                      {material?.versions.find(
                        (v) => v.version === preview.version,
                      )?.title || note?.text}
                    </strong>
                    {material && (
                      <label>
                        {t("预览版本")}
                        <select
                          aria-label={t("引用预览版本")}
                          value={preview.version}
                          onChange={(e) =>
                            inspect({
                              ...preview,
                              version: Number(e.target.value),
                            })
                          }
                        >
                          {[...material.versions].reverse().map((v) => (
                            <option key={v.version} value={v.version}>
                              v{v.version} · {v.title}
                            </option>
                          ))}
                        </select>
                      </label>
                    )}
                    {note && (
                      <blockquote>{note.target?.quote || note.text}</blockquote>
                    )}
                    <ErrorBox message={error} />
                    {text !== undefined && (
                      <div className="space-reference-excerpt">
                        <MarkdownText
                          source={text || t("这页没有文字内容。")}
                        />
                      </div>
                    )}
                    {material && (
                      <button
                        type="button"
                        className="text-link"
                        disabled={loading}
                        onClick={() => void read()}
                      >
                        {loading ? t("正在读取预览…") : t("预览所选版本")}
                      </button>
                    )}
                    <p className="muted small-text">
                      {t("预览不改变已选引用；选择后会保留这个固定版本。")}
                    </p>
                    <button
                      type="button"
                      className="button small"
                      disabled={
                        disabled ||
                        draft.some((r) => sameReference(r, preview)) ||
                        (draft.length >= 16 &&
                          !draft.some(
                            (r) => r.materialId === preview.materialId,
                          ))
                      }
                      onClick={() => select(preview)}
                    >
                      {draft.some((r) => sameReference(r, preview))
                        ? t("已选此版本")
                        : material
                          ? tr`选择 v${preview.version}`
                          : t("选择这条批注")}
                    </button>
                  </>
                ) : (
                  <p className="muted">{t("点击左侧引用查看内容和版本。")}</p>
                )}
              </section>
            </div>
            <div className="space-reference-chips" aria-live="polite">
              {draft.map((ref) => (
                <div className="space-reference-chip" key={JSON.stringify(ref)}>
                  <span>{spaceReferenceTitle(c, ref)}</span>
                  <button
                    type="button"
                    className="icon-button"
                    aria-label={tr`取消选择：${spaceReferenceTitle(c, ref)}`}
                    onClick={() =>
                      setDraft(draft.filter((r) => !sameReference(r, ref)))
                    }
                  >
                    <Icon name="close" size={13} />
                  </button>
                </div>
              ))}
            </div>
            <div className="modal-actions">
              <button type="button" className="button" onClick={close}>
                {t("取消")}
              </button>
              <button
                type="button"
                className="button primary"
                disabled={disabled}
                onClick={() => {
                  onChange(draft);
                  close();
                }}
              >{tr`添加 ${draft.length} 项引用`}</button>
            </div>
          </div>
        </Modal>
      )}
    </div>
  );
}
