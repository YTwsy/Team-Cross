import { useEffect, useRef, useState } from "react";
import { api, errorText } from "../api";
import { targetLabel } from "../annotations";
import { relativeTime, type Annotation, type AnnotationTarget } from "../types";
import { Copy, ErrorBox, Icon, Modal } from "./ui";

export type AnnotationRequest = { target?: AnnotationTarget; serial: number };

export function Annotations({
  id,
  annotations,
  request,
  disabled,
  onSaved,
  onLocate,
}: {
  id: string;
  annotations: Annotation[];
  request?: AnnotationRequest;
  disabled: boolean;
  onSaved: (annotation: Annotation) => void;
  onLocate: (annotation: Annotation) => void;
}) {
  const [open, setOpen] = useState(false);
  const [target, setTarget] = useState<AnnotationTarget>();
  const [text, setText] = useState("");
  const [error, setError] = useState("");
  const [saved, setSaved] = useState(false);
  const [pending, setPending] = useState(false);
  const submitting = useRef(false);
  const editor = useRef<HTMLTextAreaElement>(null);
  useEffect(() => {
    if (open && !pending) editor.current?.focus();
  }, [open, pending]);
  useEffect(() => {
    if (!request) return;
    setTarget(request.target);
    setError("");
    setOpen(true);
  }, [request]);

  async function save() {
    if (submitting.current || disabled || !text.trim()) return;
    submitting.current = true;
    setPending(true);
    setError("");
    try {
      const annotation = await api<Annotation>(
        `collaborations/${id}/annotations`,
        { text, target },
      );
      onSaved(annotation);
      setOpen(false);
      setText("");
      setTarget(undefined);
      setSaved(true);
    } catch (e) {
      setError(errorText(e));
    } finally {
      submitting.current = false;
      setPending(false);
    }
  }

  return (
    <>
      <section className="panel notes-panel" aria-label="协作批注">
        <div className="panel-heading">
          <h2>
            批注 <span className="count">{annotations.length}</span>
          </h2>
          <button
            className="button small"
            disabled={disabled}
            onClick={() => {
              setError("");
              setOpen(true);
            }}
          >
            <Icon name="plus" size={15} />
            {text ? "继续草稿" : "整体意见"}
          </button>
        </div>
        <p className="annotation-hint">
          选中对话文字或代码行，就能针对原文留下意见。
        </p>
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
                <article key={annotation.id}>
                  <div className="annotation-author">
                    <span className="avatar small">
                      {annotation.author?.[0] || "同"}
                    </span>
                    <strong>{annotation.author}</strong>
                    <time dateTime={annotation.createdAt}>
                      {relativeTime(annotation.createdAt)}
                    </time>
                  </div>
                  {annotation.target ? (
                    <button
                      className="annotation-source"
                      onClick={() => onLocate(annotation)}
                      title="查看原位置"
                    >
                      <span>
                        <Icon
                          name={
                            annotation.target.kind === "history"
                              ? "comment"
                              : "folder"
                          }
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
                  ) : (
                    <span className="annotation-general">整体意见</span>
                  )}
                  <p>{annotation.text}</p>
                  <Copy
                    className="annotation-copy"
                    label="复制给辅助客户端"
                    text={`使用 Team Cross 读取协作 ${id} 的批注（read_context，kind=annotations），查看批注 ${annotation.id}。请结合其 target 定位与 quote 原文片段分析这条意见，先核对当前内容是否变化。`}
                  />
                </article>
              ))}
          </div>
        ) : (
          <div className="annotation-empty">
            <Icon name="comment" size={24} />
            <p>还没有批注</p>
            <span>可以标记具体片段，也可以添加整体意见。</span>
          </div>
        )}
        <p className="annotation-hint annotation-boundary">
          批注会保存到协作，可通过 Team Cross 工具读取，或复制给辅助客户端。
        </p>
      </section>
      {open && (
        <Modal
          title={text && !target ? "添加整体意见" : "添加批注"}
          onClose={() => {
            if (!submitting.current) setOpen(false);
          }}
        >
          <form
            className="annotation-composer"
            onSubmit={(event) => {
              event.preventDefault();
              void save();
            }}
          >
            {target ? (
              <div className="annotation-preview">
                <strong>{targetLabel(target)}</strong>
                <blockquote>{target.quote}</blockquote>
              </div>
            ) : (
              <p className="muted small-text">这条意见适用于整个协作。</p>
            )}
            <label htmlFor="annotation-text">你的意见</label>
            <textarea
              id="annotation-text"
              ref={editor}
              rows={5}
              maxLength={4000}
              value={text}
              disabled={pending}
              placeholder="希望调整什么，或者哪里值得关注？"
              onChange={(event) => setText(event.target.value)}
              onKeyDown={(event) => {
                if (
                  (event.metaKey || event.ctrlKey) &&
                  event.key === "Enter" &&
                  !event.nativeEvent.isComposing
                ) {
                  event.preventDefault();
                  void save();
                }
              }}
            />
            <ErrorBox message={error} />
            {disabled && (
              <p role="status" className="muted small-text">
                连接恢复后可以保存，草稿会保留在当前页面。
              </p>
            )}
            <div className="annotation-composer-footer">
              <span className="muted small-text">
                ⌘ / Ctrl + Enter 保存
                <br />
                草稿仅保留在当前页面
              </span>
              <button
                type="button"
                className="button"
                disabled={pending}
                onClick={() => setOpen(false)}
              >
                暂存草稿
              </button>
              <button
                className="button primary"
                disabled={pending || disabled || !text.trim()}
              >
                {pending ? "正在保存…" : "保存批注"}
              </button>
            </div>
          </form>
        </Modal>
      )}
    </>
  );
}
