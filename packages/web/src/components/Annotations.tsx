import { AnchoredNote, type AnnotationOrigin } from "./Reading";
import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type KeyboardEvent,
} from "react";
import { api, errorText } from "../api";
import { targetLabel } from "../annotations";
import { relativeTime, type Annotation, type AnnotationTarget } from "../types";
import {
  MaterialReferenceLinks,
  MaterialReferencePicker,
} from "./MaterialReferences";
import type { Material, MaterialReference } from "../types";
import { Copy, ErrorBox, Icon } from "./ui";

export type AnnotationRequest = {
  target?: AnnotationTarget;
  serial: number;
  origin?: AnnotationOrigin;
  annotationId?: string;
};
type Draft = {
  target?: AnnotationTarget;
  text: string;
  materials?: MaterialReference[];
};
const general = "general";

function saveShortcut(
  event: KeyboardEvent<HTMLTextAreaElement>,
  save: () => void,
) {
  if (
    (event.metaKey || event.ctrlKey) &&
    event.key === "Enter" &&
    !event.nativeEvent.isComposing
  ) {
    event.preventDefault();
    save();
  }
}

function Author({ author, createdAt }: { author: string; createdAt: string }) {
  return (
    <div className="annotation-author">
      <span className="avatar small">{author?.[0] || "同"}</span>
      <strong>{author}</strong>
      <time dateTime={createdAt}>{relativeTime(createdAt)}</time>
    </div>
  );
}

function Discussion({
  id,
  annotation,
  disabled,
  onSaved,
  onLocate,
  materials = [],
  onLocateMaterial = () => {},
  origin,
  onClose = () => {},
}: {
  id: string;
  annotation: Annotation;
  disabled: boolean;
  onSaved: (annotation: Annotation) => void;
  onLocate: (annotation: Annotation) => void;
  origin?: AnnotationOrigin;
  onClose?: () => void;
  materials?: Material[];
  onLocateMaterial?: (ref: MaterialReference) => void;
}) {
  const [open, setOpen] = useState(false);
  const [text, setText] = useState("");
  const [refs, setRefs] = useState<MaterialReference[]>([]);
  const [error, setError] = useState("");
  const [pending, setPending] = useState(false);
  const [saved, setSaved] = useState(false);
  const editor = useRef<HTMLTextAreaElement>(null);
  const submitting = useRef(false);
  const attempt = useRef<
    | { text: string; requestId: string; materials?: MaterialReference[] }
    | undefined
  >(undefined);
  useEffect(() => {
    if (open) editor.current?.focus();
  }, [open]);

  async function save() {
    if (submitting.current || disabled || !text.trim()) return;
    submitting.current = true;
    setPending(true);
    setError("");
    if (
      attempt.current?.text !== text ||
      JSON.stringify(attempt.current?.materials || []) !== JSON.stringify(refs)
    )
      attempt.current = {
        text,
        requestId: crypto.randomUUID(),
        ...(refs.length ? { materials: refs } : {}),
      };
    try {
      const updated = await api<Annotation>(
        `collaborations/${id}/annotation-replies`,
        {
          annotationId: annotation.id,
          ...attempt.current,
        },
      );
      onSaved(updated);
      setText("");
      setRefs([]);
      attempt.current = undefined;
      setOpen(false);
      setSaved(true);
    } catch (e) {
      setError(errorText(e));
    } finally {
      submitting.current = false;
      setPending(false);
    }
  }

  const content = (
    <article
      className="annotation-thread"
      aria-label={`批注：${annotation.text}`}
    >
      <Author author={annotation.author} createdAt={annotation.createdAt} />
      {annotation.target ? (
        <button
          className="annotation-source"
          onClick={() => onLocate(annotation)}
          title="查看原位置"
        >
          <span>
            <Icon
              name={annotation.target.kind === "history" ? "comment" : "folder"}
              size={14}
            />
            {targetLabel(annotation.target)}
          </span>
          <blockquote>{annotation.target.quote}</blockquote>
          <span className="annotation-source-action">
            查看原位置 <Icon name="arrow" size={13} />
          </span>
        </button>
      ) : annotation.reference ? (
        <code>{annotation.reference}</code>
      ) : null}
      <div
        className={`annotation-content ${
          annotation.target || annotation.reference ? "with-source" : "general"
        }`}
      >
        <span
          className={
            annotation.target || annotation.reference
              ? "annotation-content-label"
              : "annotation-general"
          }
        >
          {annotation.target || annotation.reference ? "批注意见" : "整体意见"}
        </span>
        <p className="annotation-body">{annotation.text}</p>
      </div>
      <MaterialReferenceLinks
        references={annotation.materials}
        materials={materials}
        onLocate={onLocateMaterial}
      />
      {!!annotation.replies?.length && (
        <div
          className="annotation-replies"
          role="group"
          aria-label={`${annotation.replies.length} 条回复`}
        >
          <span className="annotation-reply-count">
            {annotation.replies.length} 条回复
          </span>
          {annotation.replies.map((reply) => (
            <div className="annotation-reply" key={reply.id}>
              <Author author={reply.author} createdAt={reply.createdAt} />
              <p>{reply.text}</p>
              <MaterialReferenceLinks
                references={reply.materials}
                materials={materials}
                onLocate={onLocateMaterial}
              />
            </div>
          ))}
        </div>
      )}
      <div className="annotation-actions">
        <button
          className="button small"
          aria-expanded={open}
          disabled={disabled && !text}
          onClick={() => {
            setOpen(!open);
            setSaved(false);
          }}
        >
          <Icon name="comment" size={14} />
          {open ? "收起回复" : text ? "继续回复" : "回复"}
        </button>
        <Copy
          className="annotation-copy"
          label="复制处理提示"
          text={`请使用 Team Cross 读取协作 ${id} 的批注 ${annotation.id} 及回复。共享会话使用 read_annotations；个人辅助会话使用 read_context（kind=annotations）。核对 target 与 quote 对应的当前原文，再分析这条意见；需要答复时使用 reply_to_annotation 回复原批注。`}
        />
      </div>
      {saved && (
        <p className="annotation-saved" role="status">
          回复已保存
        </p>
      )}
      {open && (
        <form
          className="annotation-reply-composer"
          aria-label="回复原批注"
          onSubmit={(event) => {
            event.preventDefault();
            void save();
          }}
        >
          <label htmlFor={`reply-${annotation.id}`}>回复这条批注</label>
          <textarea
            id={`reply-${annotation.id}`}
            ref={editor}
            rows={3}
            maxLength={4000}
            value={text}
            disabled={pending}
            placeholder="补充说明，或回应这条意见…"
            onChange={(event) => setText(event.target.value)}
            onKeyDown={(event) => saveShortcut(event, () => void save())}
          />
          <MaterialReferencePicker
            spaceId={id}
            materials={materials}
            value={refs}
            disabled={pending || disabled}
            onChange={setRefs}
          />
          <ErrorBox message={error} />
          {disabled && (
            <p className="muted small-text" role="status">
              连接恢复后可以保存，草稿会保留在当前页面。
            </p>
          )}
          <div className="annotation-composer-footer">
            <span className="muted small-text">⌘ / Ctrl + Enter</span>
            <button
              className="button primary small"
              disabled={pending || disabled || !text.trim()}
            >
              {pending ? "正在保存…" : "保存回复"}
            </button>
          </div>
        </form>
      )}
    </article>
  );
  return origin ? (
    <AnchoredNote origin={origin} onClose={onClose} label="原文讨论">
      {content}
    </AnchoredNote>
  ) : (
    content
  );
}

export function Annotations({
  id,
  annotations,
  request,
  disabled,
  onSaved,
  onLocate,
  materials = [],
  onLocateMaterial = () => {},
}: {
  id: string;
  annotations: Annotation[];
  request?: AnnotationRequest;
  disabled: boolean;
  onSaved: (annotation: Annotation) => void;
  onLocate: (annotation: Annotation) => void;
  materials?: Material[];
  onLocateMaterial?: (ref: MaterialReference) => void;
}) {
  const [active, setActive] = useState(general);
  const [origins, setOrigins] = useState<Record<string, AnnotationOrigin>>({});
  const [inspection, setInspection] = useState<{
    id: string;
    origin: AnnotationOrigin;
  }>();
  const closeInspection = useCallback(() => setInspection(undefined), []);
  const closeEditor = useCallback(() => setActive(general), []);
  const [drafts, setDrafts] = useState<Record<string, Draft>>({});
  const [error, setError] = useState("");
  const [saved, setSaved] = useState(false);
  const [pending, setPending] = useState(false);
  const submitting = useRef(false);
  const editor = useRef<HTMLTextAreaElement>(null);
  const composer = useRef<HTMLFormElement>(null);
  const draft = drafts[active] || { text: "" };
  useEffect(() => {
    if (!request) return;
    if (request.annotationId && request.origin) {
      setInspection({ id: request.annotationId, origin: request.origin });
      return;
    }
    setInspection(undefined);
    const key = request.target ? JSON.stringify(request.target) : general;
    setDrafts((current) =>
      current[key]
        ? current
        : { ...current, [key]: { target: request.target, text: "" } },
    );
    if (request.origin)
      setOrigins((current) => ({ ...current, [key]: request.origin! }));
    setActive(key);
    setError("");
    setSaved(false);
    if (!request.origin)
      composer.current?.scrollIntoView?.({
        block: "nearest",
        behavior: "smooth",
      });
    editor.current?.focus({ preventScroll: true });
  }, [request]);

  const activeOrigin =
    active === general || !origins[active]?.element.isConnected
      ? undefined
      : origins[active];
  useEffect(() => {
    if (activeOrigin) editor.current?.focus({ preventScroll: true });
  }, [activeOrigin]);

  function switchDraft(key: string) {
    setActive(key);
    setError("");
    setSaved(false);
    editor.current?.focus({ preventScroll: true });
  }

  async function save() {
    if (submitting.current || disabled || !draft.text.trim()) return;
    submitting.current = true;
    setPending(true);
    setError("");
    const key = active;
    try {
      const annotation = await api<Annotation>(
        `collaborations/${id}/annotations`,
        draft,
      );
      onSaved(annotation);
      setDrafts((current) => {
        const next = { ...current };
        delete next[key];
        return next;
      });
      setOrigins((current) => {
        const next = { ...current };
        delete next[key];
        return next;
      });
      setActive((current) => (current === key ? general : current));
      setSaved(true);
    } catch (e) {
      setError(errorText(e));
    } finally {
      submitting.current = false;
      setPending(false);
    }
  }

  const otherDrafts = Object.entries(drafts).filter(
    ([key, value]) => key !== active && value.text.trim(),
  );
  const form = (
    <form
      ref={composer}
      className="annotation-composer"
      aria-label="添加批注"
      onSubmit={(event) => {
        event.preventDefault();
        void save();
      }}
    >
      <div className="annotation-composer-heading">
        <span>{draft.target ? "针对所选内容" : "整体意见"}</span>
        {draft.target && (
          <button
            type="button"
            className="text-button"
            disabled={pending}
            onClick={() => switchDraft(general)}
          >
            写整体意见
          </button>
        )}
      </div>
      {draft.target && (
        <div className="annotation-preview">
          <strong>{targetLabel(draft.target)}</strong>
          <blockquote>{draft.target.quote}</blockquote>
        </div>
      )}
      <label htmlFor="annotation-text" className="sr-only">
        你的意见
      </label>
      <textarea
        id="annotation-text"
        ref={editor}
        rows={3}
        maxLength={4000}
        value={draft.text}
        disabled={pending}
        placeholder={
          draft.target ? "这段内容有哪些需要关注？" : "留下对这次协作的意见…"
        }
        onChange={(event) => {
          const text = event.target.value;
          setDrafts((current) => ({
            ...current,
            [active]: { ...draft, text },
          }));
          setSaved(false);
        }}
        onKeyDown={(event) => saveShortcut(event, () => void save())}
      />
      <MaterialReferencePicker
        spaceId={id}
        materials={materials}
        value={draft.materials || []}
        disabled={pending || disabled}
        onChange={(refs) =>
          setDrafts((current) => ({
            ...current,
            [active]: { ...draft, materials: refs },
          }))
        }
      />
      <ErrorBox message={error} />
      {disabled && (
        <p role="status" className="muted small-text">
          连接恢复后可以保存，草稿会保留在当前页面。
        </p>
      )}
      <div className="annotation-composer-footer">
        <span className="muted small-text">⌘ / Ctrl + Enter 保存</span>
        <button
          className="button primary small"
          disabled={pending || disabled || !draft.text.trim()}
        >
          {pending ? "正在保存…" : "保存批注"}
        </button>
      </div>
      {!!otherDrafts.length && (
        <div className="annotation-drafts" aria-label="未保存的草稿">
          {otherDrafts.map(([key, value]) => (
            <button
              key={key}
              type="button"
              className="text-button"
              disabled={pending}
              onClick={() => switchDraft(key)}
            >
              继续草稿 · {value.target ? targetLabel(value.target) : "整体意见"}
            </button>
          ))}
        </div>
      )}
    </form>
  );
  return (
    <section className="panel notes-panel" aria-label="协作批注">
      <div className="panel-heading annotation-panel-heading">
        <h2>
          批注 <span className="count">{annotations.length}</span>
        </h2>
        <p className="annotation-hint">
          选中文字可在原文旁讨论，也可以在这里写整体意见。
        </p>
      </div>
      {activeOrigin ? (
        <>
          <p className="annotation-hint">
            正在原文旁填写，收起后草稿仍会保留。
          </p>
          <AnchoredNote
            origin={activeOrigin}
            onClose={closeEditor}
            label="原文批注"
          >
            {form}
          </AnchoredNote>
        </>
      ) : (
        form
      )}

      {saved && (
        <p className="annotation-saved" role="status">
          <Icon name="check" size={14} />
          已保存，协作者和工具可读取。
        </p>
      )}
      {annotations.length ? (
        <div className="annotations">
          {annotations
            .slice()
            .reverse()
            .map((annotation) => (
              <Discussion
                key={annotation.id}
                origin={
                  inspection?.id === annotation.id
                    ? inspection.origin
                    : undefined
                }
                onClose={closeInspection}
                id={id}
                annotation={annotation}
                disabled={disabled}
                onSaved={onSaved}
                onLocate={onLocate}
                materials={materials}
                onLocateMaterial={onLocateMaterial}
              />
            ))}
        </div>
      ) : (
        <div className="annotation-empty">
          <Icon name="comment" size={24} />
          <p>还没有批注</p>
          <span>留下第一条意见，一起讨论。</span>
        </div>
      )}
      <p className="annotation-hint annotation-boundary">
        在个人或共享客户端里告诉 Agent“读取 Team Cross
        批注”，即可查看意见与回复。保存不会自动开始执行。
      </p>
    </section>
  );
}
