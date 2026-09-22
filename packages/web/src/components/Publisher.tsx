import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { api, errorText, useResource } from "../api";
import {
  sourceName,
  type Collaboration,
  type Material,
  type Provider,
  type PublicationDraftSummary,
  type PublicationResult,
  type Source,
  projectName,
  relativeTime,
} from "../types";
import { ErrorBox, Icon, Loading, PageHeading, ProviderFilter } from "./ui";
import { Create } from "./Create";
import { PublicationReader } from "./PublicationReader";
export { itemLabel } from "../reading";

export function Publisher({
  spaceId,
  material,
  embedded,
  onPublished,
  onStageChange,
}: {
  spaceId?: string;
  material?: Material;
  embedded?: boolean;
  onPublished: (result: PublicationResult, id: string) => void;
  onStageChange?: (stage: "source" | "range" | "review") => void;
}) {
  const latest = material?.versions.at(-1);
  const [provider, setProvider] = useState<Provider>(
    latest?.provider || "codex",
  );
  const [search, setSearch] = useState("");
  const [query, setQuery] = useState("");
  const [cursor, setCursor] = useState("");
  const [sourceId, setSourceId] = useState(latest?.sourceId || "");
  const [draft, setDraft] = useState<PublicationDraftSummary>();
  const [preview, setPreview] = useState<PublicationDraftSummary>();
  const [reviewing, setReviewing] = useState(false);
  const [previewReady, setPreviewReady] = useState(false);
  const [source, setSource] = useState<Source>();
  const [title, setTitle] = useState(latest?.title || "");
  const [start, setStart] = useState("");
  const [end, setEnd] = useState("");
  const [reading, setReading] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [uncertain, setUncertain] = useState(false);
  const [statusNote, setStatusNote] = useState("");
  const attempt = useRef(crypto.randomUUID());
  const spaceRequest = useRef(spaceId || crypto.randomUUID());
  const createdSpace = useRef(spaceId);
  const mounted = useRef(true);
  const composer = useRef<HTMLElement>(null);
  const [controls, setControls] = useState<HTMLDivElement | null>(null);
  const [readerTools, setReaderTools] = useState<HTMLDivElement | null>(null);
  const editorPosition = useRef(0);
  const restoreEditor = useRef(false);
  const stage = !draft ? "source" : reviewing ? "review" : "range";
  useEffect(() => onStageChange?.(stage), [stage, onStageChange]);
  useLayoutEffect(() => {
    const element = composer.current;
    if (!element || !controls) return;
    // Wrapped actions change height with the viewport, title and review stage.
    // Keep directory positioning and turn navigation clear of the sticky bar.
    const updateHeight = () =>
      element.style.setProperty(
        "--publication-controls-height",
        `${controls.getBoundingClientRect().height}px`,
      );
    updateHeight();
    const observer =
      typeof ResizeObserver !== "undefined"
        ? new ResizeObserver(updateHeight)
        : undefined;
    observer?.observe(controls);
    return () => {
      observer?.disconnect();
      element.style.removeProperty("--publication-controls-height");
    };
  }, [controls]);
  useLayoutEffect(() => {
    if (restoreEditor.current) {
      const dialog = composer.current?.closest("dialog");
      if (dialog) dialog.scrollTop = editorPosition.current;
      else window.scrollTo({ top: editorPosition.current });
      restoreEditor.current = false;
    }
  }, [reviewing]);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);
  useEffect(() => {
    const timer = setTimeout(() => {
      setQuery(search);
      setCursor("");
    }, 250);
    return () => clearTimeout(timer);
  }, [search]);
  const path = `sources?provider=${provider}&search=${encodeURIComponent(query)}&cursor=${encodeURIComponent(cursor)}`;
  const sources = useResource<{ data: Source[]; nextCursor?: string }>(
    latest ? null : path,
  );
  const rows = sources.dataPath === path ? sources.data : undefined;
  const pending = sources.loading || (!latest && sources.dataPath !== path);
  async function freeze() {
    setBusy(true);
    setError("");
    setStatusNote("");
    try {
      const d = await api<PublicationDraftSummary>("publications/source", {
        provider,
        sourceId,
        compact: true,
      });
      if (!mounted.current) return;
      setDraft(d);
      setTitle(latest?.title || d.title);
      const contains = (id: string | undefined) =>
        !!id && d.turns.some((t) => t.id === id);
      setStart(
        latest
          ? contains(latest.startTurnId)
            ? latest.startTurnId
            : ""
          : d.startTurnId,
      );
      setEnd(
        latest
          ? contains(latest.endTurnId)
            ? latest.endTurnId
            : ""
          : d.endTurnId,
      );
      setReading(
        contains(latest?.readingStartId) ? latest!.readingStartId! : "",
      );
      if (
        latest &&
        (!contains(latest.startTurnId) || !contains(latest.endTurnId))
      )
        setError(
          "这次历史中没有找到上次公开范围的边界，请重新选择。不会自动扩大公开范围。",
        );
      setPreview(undefined);
      setReviewing(false);
      requestAnimationFrame(() =>
        composer.current?.scrollIntoView?.({ block: "start" }),
      );
    } catch (e) {
      if (mounted.current) setError(errorText(e));
    } finally {
      if (mounted.current) setBusy(false);
    }
  }
  function changeScope() {
    setPreview(undefined);
    setUncertain(false);
    setStatusNote("");
    setError("");
  }
  async function review() {
    if (!draft) return;
    setBusy(true);
    setError("");
    try {
      const p = await api<PublicationDraftSummary>("publications/preview", {
        draftId: draft.id,
        title,
        startTurnId: start,
        endTurnId: end,
        readingStartId: reading,
        compact: true,
      });
      if (!mounted.current) return;
      setPreview(p);
      setPreviewReady(false);
      if (!reviewing) {
        editorPosition.current =
          composer.current?.closest("dialog")?.scrollTop ?? window.scrollY;
        setReviewing(true);
        requestAnimationFrame(() =>
          composer.current?.scrollIntoView?.({ block: "start" }),
        );
      }
      attempt.current = crypto.randomUUID();
      setUncertain(false);
    } catch (e) {
      if (mounted.current) setError(errorText(e));
    } finally {
      if (mounted.current) setBusy(false);
    }
  }
  async function publish() {
    if (!preview || busy || !previewCurrent || !previewReady) return;
    setBusy(true);
    setError("");
    try {
      if (!createdSpace.current) {
        const c = await api<Collaboration>("spaces", {
          title: preview.title,
          requestId: spaceRequest.current,
        });
        createdSpace.current = c.id;
      }
      const result = await api<PublicationResult>(
        `collaborations/${createdSpace.current}/materials`,
        {
          previewId: preview.id,
          previewHash: preview.hash,
          requestId: attempt.current,
          ...(material
            ? { materialId: material.id, baseVersion: latest!.version }
            : {}),
        },
      );
      if (mounted.current) onPublished(result, createdSpace.current!);
    } catch (e) {
      if (mounted.current) {
        setError(errorText(e));
        setUncertain(true);
      }
    } finally {
      if (mounted.current) setBusy(false);
    }
  }
  async function checkStatus() {
    setBusy(true);
    setError("");
    try {
      const result = await api<PublicationResult>(
        `collaborations/${createdSpace.current || spaceRequest.current}/publication-status`,
        { requestId: attempt.current },
      );
      if (!mounted.current) return;
      if (result.state === "published")
        onPublished(result, createdSpace.current || spaceRequest.current);
      else
        setStatusNote(
          "尚未查到已保存结果。网络中的原请求可能仍在进行；需要重试时保持当前预览与请求标识。",
        );
    } catch (e) {
      if (mounted.current) setError(errorText(e));
    } finally {
      if (mounted.current) setBusy(false);
    }
  }
  const startIndex = draft?.turns.findIndex((t) => t.id === start) ?? 0;
  const endIndex = draft?.turns.findIndex((t) => t.id === end) ?? 0;
  const validRange = startIndex >= 0 && endIndex >= startIndex;
  const count = validRange ? endIndex - startIndex + 1 : 0;
  const previewCurrent =
    !!preview &&
    preview.title === title.trim() &&
    preview.startTurnId === start &&
    preview.endTurnId === end &&
    (preview.readingStartId || "") === reading;
  const readingIndex = draft?.turns.findIndex((t) => t.id === reading) ?? -1;
  const selectedNotices = validRange
    ? draft?.turns
        .slice(startIndex, endIndex + 1)
        .reduce((n, t) => n + (t.noticeCount || 0), 0) || 0
    : 0;
  function setRange(first: string, last: string) {
    if (!draft) return;
    const from = draft.turns.findIndex((t) => t.id === first);
    const to = draft.turns.findIndex((t) => t.id === last);
    const previousReading = draft.turns.findIndex((t) => t.id === reading);
    setStart(first);
    setEnd(last);
    changeScope();
    if (reading && (previousReading < from || previousReading > to)) {
      setReading("");
      setStatusNote("原建议阅读起点已移出范围，改为从分享范围开头阅读。");
    }
  }
  function returnToEditor() {
    restoreEditor.current = true;
    setReviewing(false);
  }
  return (
    <section
      ref={composer}
      className="publication-composer"
      aria-label="发布会话材料"
    >
      {!embedded && (
        <p className="muted">
          选择你本机的一份调查。只有确认的历史范围会发布到空间，供现在和以后获准加入的成员阅读。
        </p>
      )}
      {!draft ? (
        <>
          {latest ? (
            <p>
              更新「{latest.title}」，来源为 {latest.provider} ·{" "}
              {latest.sourceId}。
            </p>
          ) : (
            <>
              <div className="panel-heading source-heading">
                <div>
                  <h2>选择本机会话</h2>
                  <span className="muted small-text">
                    本机 {provider === "claude" ? "Claude Code" : "Codex"} 历史
                  </span>
                </div>
                <ProviderFilter
                  value={provider}
                  disabled={busy}
                  onChange={(value) => {
                    setProvider(value);
                    setSourceId("");
                    setSource(undefined);
                    setCursor("");
                  }}
                />
              </div>
              {provider === "claude" ? (
                <p className="source-provider-note">
                  Claude Code 历史接入仍是实验性能力。只读取已保存的会话。
                </p>
              ) : null}
              <label className="search">
                <Icon name="search" size={18} />
                <input
                  aria-label="搜索本机会话"
                  placeholder="搜索会话名称或内容…"
                  value={search}
                  onChange={(e) => setSearch(e.target.value)}
                />
              </label>
              <ErrorBox message={sources.error} retry={sources.reload} />
              <div
                className="publication-sources"
                role="radiogroup"
                aria-label="要发布的会话"
              >
                {pending ? (
                  <Loading text="正在读取本机历史…" />
                ) : (
                  <>
                    {rows?.data.map((s) => (
                      <button
                        className={`source-row ${sourceId === s.id ? "selected" : ""}`}
                        key={s.id}
                        role="radio"
                        aria-checked={sourceId === s.id}
                        disabled={busy}
                        onClick={() => {
                          setSourceId(s.id);
                          setSource(s);
                        }}
                      >
                        <span className="radio-dot" />
                        <div>
                          <strong>{sourceName(s)}</strong>
                          <span className="source-description">
                            {s.preview}
                          </span>
                          <span className="row-meta">
                            {projectName(s.cwd)} · {relativeTime(s.updatedAt)}
                          </span>
                        </div>
                      </button>
                    ))}
                    {!rows?.data.length && (
                      <p className="muted">没有找到来源会话。</p>
                    )}
                  </>
                )}
              </div>
              <div className="material-actions">
                {cursor && (
                  <button
                    className="button small"
                    onClick={() => setCursor("")}
                  >
                    回到首批
                  </button>
                )}
                {rows?.nextCursor && (
                  <button
                    className="button small"
                    onClick={() => setCursor(rows.nextCursor!)}
                  >
                    下一批会话
                  </button>
                )}
              </div>
            </>
          )}
          <button
            className="button primary"
            disabled={busy || !sourceId}
            onClick={() => void freeze()}
          >
            {busy ? "正在冻结历史…" : "选择公开范围"}
          </button>
        </>
      ) : (
        <>
          <div className="publication-source-bar">
            <div>
              <span className="eyebrow">
                {reviewing ? "确认分享内容" : "选择分享范围"}
              </span>
              <h2>{draft.title}</h2>
              <p className="small-text muted">
                {provider === "claude" ? "Claude Code" : "Codex"}
                {source?.cwd ? ` · ${projectName(source.cwd)}` : ""} ·{" "}
                {draft.turns.length} 轮已结束对话
              </p>
            </div>
            <button
              className="text-link"
              disabled={busy || uncertain}
              onClick={() => {
                setDraft(undefined);
                setPreview(undefined);
                setReviewing(false);
                setError("");
              }}
            >
              {latest ? "重新读取来源" : "重新选择来源"}
            </button>
          </div>
          <p className="publication-snapshot small-text muted">
            固定于 {new Date(draft.frozenAt).toLocaleString("zh-CN")} ·
            后续对话不会自动加入
          </p>
          <div
            ref={setControls}
            className="publication-controls"
            role="region"
            aria-label="分享范围与操作"
          >
            <div className="publication-scope-bar">
              <div
                className="publication-scope-description"
                role="status"
                aria-live="polite"
              >
                <strong>
                  {validRange
                    ? `已选第 ${startIndex + 1}–${endIndex + 1} 轮，共 ${count} 轮`
                    : "请重新选择起点和终点"}
                </strong>
                <span className="small-text muted">
                  {readingIndex >= 0
                    ? `建议从第 ${readingIndex + 1} 轮读起 · 分享范围不变`
                    : "默认从分享范围开头阅读"}
                </span>
              </div>
              <div className="publication-scope-actions">
                {!reviewing && (
                  <div className="publication-presets">
                    <button
                      className="button small"
                      disabled={busy || uncertain}
                      aria-pressed={
                        start === draft.startTurnId && end === draft.endTurnId
                      }
                      onClick={() =>
                        setRange(draft.startTurnId, draft.endTurnId)
                      }
                    >
                      全部已结束对话
                    </button>
                    {draft.turns.length > 3 && (
                      <button
                        className="button small"
                        disabled={busy || uncertain}
                        aria-pressed={
                          start === draft.turns.at(-3)!.id &&
                          end === draft.endTurnId
                        }
                        onClick={() =>
                          setRange(draft.turns.at(-3)!.id, draft.endTurnId)
                        }
                      >
                        最近 3 轮
                      </button>
                    )}
                  </div>
                )}
                <div className="publication-actions">
                  {reviewing && (
                    <button
                      className="button"
                      disabled={busy || uncertain}
                      onClick={returnToEditor}
                    >
                      返回调整范围
                    </button>
                  )}
                  <button
                    className="button primary"
                    disabled={
                      busy ||
                      !validRange ||
                      !title.trim() ||
                      (reviewing && previewCurrent && !previewReady)
                    }
                    onClick={() =>
                      void (reviewing && previewCurrent ? publish() : review())
                    }
                  >
                    {busy
                      ? "正在处理…"
                      : !reviewing
                        ? "预览分享内容"
                        : !previewCurrent
                          ? "更新分享预览"
                          : material
                            ? `发布版本 ${latest!.version + 1}`
                            : spaceId
                              ? "发布到空间"
                              : "创建只读空间并发布"}
                    {!reviewing && <Icon name="arrow" size={16} />}
                  </button>
                </div>
              </div>
            </div>
            <div className="publication-reading-tools" ref={setReaderTools} />
            {selectedNotices > 0 && (
              <details className="publication-export-notice">
                <summary>{selectedNotices} 处导出说明</summary>
                <p>
                  图片等附件或不支持的消息内容可能未包含在分享中。请查看目录中标有“导出说明”的轮次，正文会注明具体原因。
                  这里按说明条目计数，不是缺少的轮数；工具过程仅折叠显示，仍会分享。
                </p>
              </details>
            )}
          </div>
          {!reviewing && <ErrorBox message={error} />}
          {!reviewing && statusNote && (
            <p className="small-text muted" role="status">
              {statusNote}
            </p>
          )}
          <div hidden={reviewing}>
            <PublicationReader
              key={draft.id}
              draft={draft}
              toolbarTarget={reviewing ? null : readerTools}
              disabled={busy || uncertain}
              scope={{
                start,
                end,
                reading,
                onStart: (id) => {
                  const index = draft.turns.findIndex((t) => t.id === id);
                  setRange(id, endIndex >= 0 && endIndex < index ? id : end);
                },
                onEnd: (id) => {
                  const index = draft.turns.findIndex((t) => t.id === id);
                  setRange(startIndex > index ? id : start, id);
                },
                onReading: (id) => {
                  setReading(id);
                  changeScope();
                },
              }}
            />
          </div>
          {reviewing && preview && (
            <div className="publication-confirmation">
              <label className="field">
                分享标题
                <input
                  maxLength={160}
                  disabled={busy || uncertain}
                  value={title}
                  onChange={(e) => {
                    setTitle(e.target.value);
                    setError("");
                  }}
                />
              </label>
              {!previewCurrent && (
                <p className="small-text muted" role="status">
                  标题已修改，更新分享预览后即可发布。
                </p>
              )}
              <PublicationReader
                draft={preview}
                toolbarTarget={readerTools}
                originalTurns={draft.turns}
                onReadyChange={setPreviewReady}
              />
            </div>
          )}
        </>
      )}
      {(!draft || reviewing) && <ErrorBox message={error} />}
      {uncertain && (
        <div className="notice">
          <p>
            结果尚未确认。发布请求：<code>{attempt.current}</code>
          </p>
          <button
            className="button small"
            disabled={busy}
            onClick={() => void checkStatus()}
          >
            查询发布结果
          </button>
          <p>{statusNote}</p>
        </div>
      )}
    </section>
  );
}

function keepIntentScroll(e: { preventDefault: () => void }) {
  e.preventDefault();
}

export function StartSpace() {
  const [mode, setMode] = useState<"readonly" | "execution">("readonly");
  const [publicationStage, setPublicationStage] = useState<
    "source" | "range" | "review"
  >("source");
  const compact = mode === "readonly" && publicationStage !== "source";
  return (
    <div className={`start-space ${compact ? "is-publishing" : ""}`}>
      <a href="#/" className="back-link">
        <Icon name="back" size={16} />
        协作空间
      </a>
      <PageHeading
        title="发起协作"
        subtitle="从一份本机会话开始。分享讨论只发布选定历史；一起执行会立刻创建原生 fork。"
      />
      {compact && (
        <div className="publication-intent">
          <span>
            <Icon name="comment" size={16} />
            先分享讨论
          </span>
          <span className="muted small-text">
            来源已选择 ·{" "}
            {publicationStage === "review" ? "确认分享" : "选择范围"}
          </span>
        </div>
      )}
      <fieldset className="workspace-field start-intent" hidden={compact}>
        <legend>这次要做什么</legend>
        <div className="workspace-grid">
          <label
            className={`workspace-card ${mode === "readonly" ? "selected" : ""}`}
            onMouseDown={keepIntentScroll}
          >
            <input
              type="radio"
              name="spaceMode"
              value="readonly"
              checked={mode === "readonly"}
              onChange={() => setMode("readonly")}
            />
            <div className="workspace-top">
              <span className="entry-icon">
                <Icon name="comment" size={23} />
              </span>
              <span className="radio-dot" />
            </div>
            <h3>先分享讨论</h3>
            <strong>默认 · 不创建 fork</strong>
            <p>
              发布选定历史，邀请阅读和批注。不要求 Git
              工作区，也不开放执行目录。
            </p>
          </label>
          <label
            className={`workspace-card ${mode === "execution" ? "selected" : ""}`}
            onMouseDown={keepIntentScroll}
          >
            <input
              type="radio"
              name="spaceMode"
              value="execution"
              checked={mode === "execution"}
              onChange={() => setMode("execution")}
            />
            <div className="workspace-top">
              <span className="entry-icon">
                <Icon name="terminal" size={23} />
              </span>
              <span className="radio-dot" />
            </div>
            <h3>直接一起执行</h3>
            <strong>立刻创建原生会话</strong>
            <p>
              在本机 fork 来源会话，选择目录与权限后共同继续。空间仍可发布材料。
            </p>
          </label>
        </div>
      </fieldset>
      <div className="start-body">
        <div
          className={`start-pane ${mode === "readonly" ? "is-active" : ""}`}
          aria-hidden={mode !== "readonly"}
          inert={mode !== "readonly"}
        >
          <div className="panel publication-start">
            <Publisher
              embedded
              onStageChange={setPublicationStage}
              onPublished={(_, id) => {
                sessionStorage.setItem(`teamcross.invite.${id}`, "1");
                location.hash = `/collaborations/${id}`;
              }}
            />
          </div>
        </div>
        <div
          className={`start-pane ${mode === "execution" ? "is-active" : ""}`}
          aria-hidden={mode !== "execution"}
          inert={mode !== "execution"}
        >
          <Create embedded />
        </div>
      </div>
    </div>
  );
}
