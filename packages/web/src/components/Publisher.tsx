import { useEffect, useRef, useState } from "react";
import { api, errorText, useResource } from "../api";
import {
  sourceName,
  type Collaboration,
  type Material,
  type MaterialTurn,
  type Provider,
  type PublicationDraft,
  type PublicationResult,
  type Source,
} from "../types";
import { ErrorBox, Loading, PageHeading } from "./ui";
import { Create } from "./Create";

const turnLabel = (t: MaterialTurn, index: number) =>
  `${index + 1}. ${(t.items.find((i) => i.type === "userMessage")?.text || t.items[0]?.text || "对话").slice(0, 90)}`;
export const itemLabel = (type: string) =>
  ({
    userMessage: "用户提问",
    agentMessage: "助手回复",
    toolCall: "工具调用",
    toolResult: "工具输出",
    commandExecution: "命令与结果",
    fileChange: "文件改动",
    unavailable: "未能导出的内容",
  })[type] || "已保存的工具过程";

export function Publisher({
  spaceId,
  material,
  onPublished,
}: {
  spaceId?: string;
  material?: Material;
  onPublished: (result: PublicationResult, id: string) => void;
}) {
  const latest = material?.versions.at(-1);
  const [provider, setProvider] = useState<Provider>(
    latest?.provider || "codex",
  );
  const [search, setSearch] = useState("");
  const [query, setQuery] = useState("");
  const [cursor, setCursor] = useState("");
  const [sourceId, setSourceId] = useState(latest?.sourceId || "");
  const [draft, setDraft] = useState<PublicationDraft>();
  const [preview, setPreview] = useState<PublicationDraft>();
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
  async function freeze() {
    setBusy(true);
    setError("");
    setStatusNote("");
    try {
      const d = await api<PublicationDraft>("publications/source", {
        provider,
        sourceId,
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
  }
  async function review() {
    if (!draft) return;
    setBusy(true);
    setError("");
    try {
      const p = await api<PublicationDraft>("publications/preview", {
        draftId: draft.id,
        title,
        startTurnId: start,
        endTurnId: end,
        readingStartId: reading,
      });
      if (!mounted.current) return;
      setPreview(p);
      attempt.current = crypto.randomUUID();
      setUncertain(false);
    } catch (e) {
      if (mounted.current) setError(errorText(e));
    } finally {
      if (mounted.current) setBusy(false);
    }
  }
  async function publish() {
    if (!preview || busy) return;
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
  return (
    <section className="publication-composer" aria-label="发布会话材料">
      <p className="muted">
        选择你本机的一份调查。只有确认的历史范围会发布到空间，供现在和以后获准加入的成员阅读。
      </p>
      {!draft ? (
        <>
          {latest ? (
            <p>
              更新「{latest.title}」，来源为 {latest.provider} ·{" "}
              {latest.sourceId}。
            </p>
          ) : (
            <>
              <label className="field">
                来源客户端
                <select
                  value={provider}
                  disabled={busy}
                  onChange={(e) => {
                    setProvider(e.target.value as Provider);
                    setSourceId("");
                    setCursor("");
                  }}
                >
                  <option value="codex">Codex</option>
                  <option value="claude">Claude Code · 实验性</option>
                </select>
              </label>
              <label className="field">
                搜索本机会话
                <input
                  value={search}
                  onChange={(e) => setSearch(e.target.value)}
                  placeholder="名称或内容"
                />
              </label>
              <ErrorBox message={sources.error} retry={sources.reload} />
              {sources.loading ? (
                <Loading text="正在读取本机历史…" />
              ) : (
                <div
                  className="publication-sources"
                  role="radiogroup"
                  aria-label="要发布的会话"
                >
                  {rows?.data.map((s) => (
                    <button
                      className={`source-row ${sourceId === s.id ? "selected" : ""}`}
                      key={s.id}
                      role="radio"
                      aria-checked={sourceId === s.id}
                      disabled={busy}
                      onClick={() => setSourceId(s.id)}
                    >
                      <span className="radio-dot" />
                      <div>
                        <strong>{sourceName(s)}</strong>
                        <span className="source-description">{s.preview}</span>
                      </div>
                    </button>
                  ))}
                  {!rows?.data.length && (
                    <p className="muted">没有找到来源会话。</p>
                  )}
                </div>
              )}
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
          <p className="inline-note">
            历史已固定在 {new Date(draft.frozenAt).toLocaleString("zh-CN")}
            。源会话的新对话不会自动加入本次发布。
          </p>
          <fieldset disabled={busy || uncertain} className="publication-range">
            <label className="field">
              材料名称
              <input
                maxLength={160}
                value={title}
                onChange={(e) => {
                  setTitle(e.target.value);
                  changeScope();
                }}
              />
            </label>
            <button
              className="button small"
              type="button"
              onClick={() => {
                setStart(draft.startTurnId);
                setEnd(draft.endTurnId);
                setReading("");
                changeScope();
              }}
            >
              选择全部已结束对话
            </button>
            <div className="publication-range-grid">
              <label className="field">
                从这次提问开始
                <select
                  value={start}
                  onChange={(e) => {
                    setStart(e.target.value);
                    setReading("");
                    changeScope();
                  }}
                >
                  <option value="" disabled>
                    选择起点…
                  </option>
                  {draft.turns.map((t, i) => (
                    <option key={t.id} value={t.id}>
                      {turnLabel(t, i)}
                    </option>
                  ))}
                </select>
              </label>
              <label className="field">
                公开到这里为止
                <select
                  value={end}
                  onChange={(e) => {
                    setEnd(e.target.value);
                    setReading("");
                    changeScope();
                  }}
                >
                  <option value="" disabled>
                    选择终点…
                  </option>
                  {draft.turns.map((t, i) => (
                    <option key={t.id} value={t.id} disabled={i < startIndex}>
                      {turnLabel(t, i)}
                    </option>
                  ))}
                </select>
              </label>
            </div>
            <label className="field">
              建议先读哪里
              <select
                value={reading}
                onChange={(e) => {
                  setReading(e.target.value);
                  changeScope();
                }}
              >
                <option value="">从公开范围开头阅读</option>
                {draft.turns.slice(startIndex, endIndex + 1).map((t, i) => (
                  <option key={t.id} value={t.id}>
                    {turnLabel(t, startIndex + i)}
                  </option>
                ))}
              </select>
            </label>
            <p className="small-text muted">
              每次提问、可见回复及已保存的工具过程一起选中。建议阅读起点只影响导航，范围内其他内容仍然可以读取。
            </p>
            <button
              className="button"
              disabled={
                startIndex < 0 ||
                endIndex < 0 ||
                startIndex > endIndex ||
                !title.trim()
              }
              onClick={() => void review()}
            >
              预览实际公开内容
            </button>
          </fieldset>
          {preview && (
            <div className="publication-review">
              <h3>
                即将公开：{preview.title} · {preview.turns.length} 轮
              </h3>
              <p>
                下方包含全部发布正文；折叠的内容也会公开。工具参数和输出请一并核对，未导出的附件或内容会注明。
              </p>
              <div className="publication-transcript">
                {preview.turns.map((t, i) => (
                  <details key={t.id} open={i === 0}>
                    <summary>
                      {turnLabel(t, i)} ·{" "}
                      {t.status === "completed" ? "已完成" : "已结束但未完成"}
                    </summary>
                    {t.items.map((item) => (
                      <div className="material-message" key={item.id}>
                        <strong>{itemLabel(item.type)}</strong>
                        {item.notice && (
                          <p className="inline-note">{item.notice}</p>
                        )}
                        <pre>{item.text}</pre>
                      </div>
                    ))}
                  </details>
                ))}
              </div>
              <p className="small-text muted">
                发布后保留固定版本。更新和扩大公开范围都需要再次发布。
              </p>
              <button
                className="button primary"
                disabled={busy}
                onClick={() => void publish()}
              >
                {busy
                  ? "正在保存…"
                  : material
                    ? `发布版本 ${latest!.version + 1}`
                    : spaceId
                      ? "发布到空间"
                      : "创建只读空间并发布"}
              </button>
            </div>
          )}
          {!uncertain && (
            <button
              className="text-link"
              disabled={busy}
              onClick={() => {
                setDraft(undefined);
                setPreview(undefined);
              }}
            >
              重新选择来源
            </button>
          )}
        </>
      )}
      <ErrorBox message={error} />
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

export function StartSpace() {
  const [mode, setMode] = useState<"readonly" | "execution">("readonly");
  return (
    <>
      <div className="segmented full space-mode" aria-label="协作形态">
        <button
          aria-pressed={mode === "readonly"}
          onClick={() => setMode("readonly")}
        >
          分享与审阅
        </button>
        <button
          aria-pressed={mode === "execution"}
          onClick={() => setMode("execution")}
        >
          共同继续执行
        </button>
      </div>
      {mode === "execution" ? (
        <Create />
      ) : (
        <>
          <PageHeading
            title="分享一次调查"
            subtitle="从一份会话材料开始，邀请同事阅读、批注和带回他们的分析。"
          />
          <div className="panel publication-start">
            <Publisher
              onPublished={(_, id) => {
                sessionStorage.setItem(`teamcross.invite.${id}`, "1");
                location.hash = `/collaborations/${id}`;
              }}
            />
          </div>
        </>
      )}
    </>
  );
}
