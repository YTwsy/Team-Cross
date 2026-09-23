import { t, tr } from "../i18n";
import { useEffect, useRef, useState } from "react";
import { api, errorText, useResource } from "../api";
import {
  type Collaboration,
  type Mode,
  type RuntimeMode,
  type Provider,
  type Preview,
  type ShareTransport,
  type Source,
  projectName,
  relativeTime,
  sourceName,
  runtimeModeName,
  runtimeModeDescription,
} from "../types";
import {
  Empty,
  ErrorBox,
  Icon,
  Loading,
  PageHeading,
  ProviderFilter,
} from "./ui";
export function Create({
  spaceId,
  embedded,
}: { spaceId?: string; embedded?: boolean } = {}) {
  const [provider, setProvider] = useState<Provider>("codex");
  const [step, setStep] = useState(1);
  const [search, setSearch] = useState("");
  const [query, setQuery] = useState("");
  const [source, setSource] = useState<Source>();
  const [mode, setMode] = useState<Mode>("existing");
  const [runtimeMode, setRuntimeMode] = useState<RuntimeMode>("restricted");
  const [shareTransport, setShareTransport] = useState<ShareTransport>("lan");
  const [title, setTitle] = useState("");
  const [preview, setPreview] = useState<Preview>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [previewVersion, setPreviewVersion] = useState(0);
  const [more, setMore] = useState<Source[]>([]);
  const [cursor, setCursor] = useState<string | null>();
  const [loadingMore, setLoadingMore] = useState(false);
  const moreAbort = useRef<AbortController | undefined>(undefined);
  const requestId = useRef(crypto.randomUUID());
  const errorRef = useRef<HTMLDivElement>(null);
  const sourcesPath = `sources?provider=${provider}&search=${encodeURIComponent(query)}`;
  const sources = useResource<{ data: Source[]; nextCursor?: string }>(
    sourcesPath,
  );
  const data = sources.dataPath === sourcesPath ? sources.data : undefined;
  const pending = sources.loading || sources.dataPath !== sourcesPath;
  useEffect(() => {
    setLoadingMore(false);
    return () => moreAbort.current?.abort();
  }, [sourcesPath]);
  useEffect(() => {
    const timer = setTimeout(() => {
      setQuery(search);
      setMore([]);
      setCursor(undefined);
    }, 250);
    return () => clearTimeout(timer);
  }, [search]);
  useEffect(() => {
    if (!source || step !== 2) return;
    const abort = new AbortController();
    setPreview(undefined);
    setError("");
    api<Preview>(
      "preview",
      {
        provider,
        sourceId: source.id,
        ...(spaceId ? { spaceId } : {}),
        workspaceMode: mode,
        runtimeMode,
        requestId: requestId.current,
      },
      abort.signal,
    )
      .then(setPreview)
      .catch((e) => {
        if (!abort.signal.aborted) setError(errorText(e));
      });
    return () => abort.abort();
  }, [source, step, mode, runtimeMode, previewVersion, provider, spaceId]);
  useEffect(() => {
    if (error) errorRef.current?.focus();
  }, [error]);
  async function create() {
    if (!preview || !source) return;
    setBusy(true);
    setError("");
    try {
      const c = await api<Collaboration>("collaborations", {
        provider,
        sourceId: source.id,
        ...(spaceId ? { spaceId } : {}),
        workspaceMode: mode,
        runtimeMode,
        title,
        previewHash: preview.previewHash,
        requestId: requestId.current,
      });
      if (!spaceId) {
        sessionStorage.setItem(`teamcross.transport.${c.id}`, shareTransport);
        try {
          await api(`collaborations/${c.id}/action`, {
            action: "share",
            transport: shareTransport,
            epoch: c.epoch,
          });
        } catch (e) {
          sessionStorage.setItem(`teamcross.create.${c.id}`, errorText(e));
        }
        sessionStorage.setItem(`teamcross.invite.${c.id}`, "1");
      }
      location.hash = `/collaborations/${c.id}`;
    } catch (e) {
      setError(errorText(e));
    } finally {
      setBusy(false);
    }
  }
  const nextCursor = data && (cursor === undefined ? data.nextCursor : cursor);
  return (
    <>
      {!embedded && (
        <>
          <a href="#/" className="back-link">
            <Icon name="back" size={16} />
            {t("协作空间") + " "}
          </a>
          <PageHeading
            title={spaceId ? t("启用共同执行") : t("发起协作")}
            subtitle={
              spaceId
                ? t("在当前空间开始共同执行，保留已有成员、材料和讨论。")
                : t("创建协作空间，并直接开始共同执行。")
            }
          />
        </>
      )}
      {spaceId && (
        <div className="notice">
          <strong>{t("确认新增的共享范围")}</strong>
          <p>
            {t(
              "将创建新的原生 fork，开放其历史、执行目录、文件与改动读取，并按所选权限执行。原来发布的片段范围不会限制这个 fork。原邀请链接和成员保留；在成员列表向需要参与执行的人开放新增访问。",
            ) + " "}
          </p>
        </div>
      )}
      {!(embedded && step === 1) && (
        <ol className="steps">
          <li className={step === 1 ? "active" : "done"}>
            <span>{step === 2 ? <Icon name="check" size={15} /> : "1"}</span>
            {t("选择来源会话") + " "}
          </li>
          <li className={step === 2 ? "active" : ""}>
            <span>2</span>
            {t("确认工作现场") + " "}
          </li>
        </ol>
      )}
      {step === 1 ? (
        <section className="panel source-panel">
          <div className="panel-heading source-heading">
            <div>
              <h2>{t("从哪里继续？")}</h2>
              <span className="muted small-text">
                {t("本机") + " "}
                {provider === "claude" ? "Claude Code" : "Codex"}
                {" " + t("历史") + " "}
              </span>
            </div>
            <ProviderFilter
              value={provider}
              onChange={(value) => {
                setProvider(value);
                setSource(undefined);
                setPreview(undefined);
                setMore([]);
                setCursor(undefined);
                setError("");
                requestId.current = crypto.randomUUID();
              }}
            />
          </div>
          {provider === "claude" ? (
            <p className="source-provider-note">
              {t(
                "使用 Claude 原生 TUI 继续会话，审批与中断在 TUI 中处理。当前支持 Claude Code 2.1.268。",
              ) + " "}
            </p>
          ) : null}
          <label className="search">
            <Icon name="search" size={18} />
            <input
              aria-label={t("搜索来源会话")}
              placeholder={t("搜索会话名称或内容…")}
              value={search}
              onChange={(e) => setSearch(e.target.value)}
            />
            <kbd>⌕</kbd>
          </label>
          <ErrorBox message={sources.error} retry={sources.reload} />
          <div
            className="source-list"
            role="radiogroup"
            aria-label={t("来源会话")}
          >
            {pending && !data ? (
              <Loading text={t("正在读取本机会话…")} />
            ) : !data?.data.length ? (
              <Empty icon="comment" title={t("没有找到来源会话")}>
                <p>{tr`请先在 ${provider === "claude" ? "Claude Code" : "Codex"} 中完成一轮对话，或尝试其他搜索词。`}</p>
              </Empty>
            ) : (
              [...data.data, ...more].map((s) => (
                <button
                  role="radio"
                  aria-checked={source?.id === s.id}
                  className={`source-row ${source?.id === s.id ? "selected" : ""}`}
                  key={s.id}
                  onClick={() => {
                    setSource(s);
                    setTitle(sourceName(s));
                    requestId.current = crypto.randomUUID();
                  }}
                >
                  <span className="radio-dot" />
                  <div>
                    <strong>{sourceName(s)}</strong>
                    <span className="source-description">{s.preview}</span>
                    <span className="row-meta">
                      <Icon name="folder" size={13} />
                      {projectName(s.cwd)}
                      <span>·</span>
                      {relativeTime(s.updatedAt)}
                    </span>
                  </div>
                </button>
              ))
            )}
          </div>
          <div className="source-pager">
            {nextCursor && (
              <button
                className="button small"
                disabled={loadingMore}
                onClick={async () => {
                  const abort = new AbortController();
                  moreAbort.current?.abort();
                  moreAbort.current = abort;
                  setLoadingMore(true);
                  try {
                    const page = await api<{
                      data: Source[];
                      nextCursor?: string;
                    }>(
                      `sources?provider=${provider}&search=${encodeURIComponent(query)}&cursor=${encodeURIComponent(nextCursor)}`,
                      undefined,
                      abort.signal,
                    );
                    if (abort.signal.aborted) return;
                    setMore((s) => [...s, ...page.data]);
                    setCursor(page.nextCursor || null);
                  } catch (e) {
                    if (!abort.signal.aborted) setError(errorText(e));
                  } finally {
                    if (!abort.signal.aborted) setLoadingMore(false);
                  }
                }}
              >
                {t("加载更多") + " "}
              </button>
            )}
          </div>
          <div className="form-footer">
            <span className="muted">
              {t("只读取历史，选择来源不会发送指令。")}
            </span>
            <button
              className="button primary"
              disabled={!source}
              onClick={() => setStep(2)}
            >
              {t("下一步") + " "}
              <Icon name="arrow" size={17} />
            </button>
          </div>
        </section>
      ) : (
        <section className="create-step">
          <div className="source-summary">
            <span className="project-icon">
              <Icon name="comment" />
            </span>
            <div>
              <span className="eyebrow">{t("来源会话")}</span>
              <strong>{source && sourceName(source)}</strong>
            </div>
            <button
              className="text-link"
              disabled={busy}
              onClick={() => setStep(1)}
            >
              {t("重新选择") + " "}
            </button>
          </div>
          <fieldset disabled={busy} className="workspace-field">
            <legend>{t("选择执行目录")}</legend>
            <div className="workspace-grid">
              {(["existing", "worktree"] as Mode[]).map((value) => (
                <label
                  className={`workspace-card ${mode === value ? "selected" : ""}`}
                  key={value}
                >
                  <input
                    type="radio"
                    name="workspaceMode"
                    value={value}
                    checked={mode === value}
                    onChange={() => {
                      setMode(value);
                      requestId.current = crypto.randomUUID();
                    }}
                  />
                  <div className="workspace-top">
                    <span className="entry-icon">
                      <Icon
                        name={value === "existing" ? "folder" : "branch"}
                        size={23}
                      />
                    </span>
                    <span className="radio-dot" />
                  </div>
                  <h3>
                    {value === "existing"
                      ? t("使用原目录")
                      : t("创建新 worktree")}
                  </h3>
                  <strong>
                    {value === "existing"
                      ? t("使用当前现场")
                      : t("从提交开始，独立工作")}
                  </strong>
                  <p>
                    {value === "existing"
                      ? t(
                          "保留当前 Git 分支和所有现有文件。协作中的代码操作直接发生在这个目录。",
                        )
                      : t(
                          "从当前 HEAD 创建新分支和目录。暂存、未暂存、未跟踪与忽略的文件都不会带入。",
                        )}
                  </p>
                </label>
              ))}
            </div>
          </fieldset>
          <fieldset disabled={busy} className="workspace-field">
            <legend>{t("选择协作模式")}</legend>
            <div className="workspace-grid">
              {(["restricted", "trusted"] as RuntimeMode[]).map((value) => (
                <label
                  className={`workspace-card ${runtimeMode === value ? "selected" : ""}`}
                  key={value}
                >
                  <input
                    type="radio"
                    name="runtimeMode"
                    value={value}
                    checked={runtimeMode === value}
                    onChange={() => {
                      setRuntimeMode(value);
                      setPreview(undefined);
                      requestId.current = crypto.randomUUID();
                    }}
                  />
                  <div className="workspace-top">
                    <span className="entry-icon">
                      <Icon name="people" size={23} />
                    </span>
                    <span className="radio-dot" />
                  </div>
                  <h3>{runtimeModeName(value)}</h3>
                  <strong>
                    {value === "trusted"
                      ? t("信任同事使用我的运行环境")
                      : t("默认 · 保留权限限制")}
                  </strong>
                  <p>{runtimeModeDescription(value)}</p>
                </label>
              ))}
            </div>
            <p className="inline-note">
              {t(
                "模式创建后固定，恢复时沿用。信任模式仍遵循原生客户端和系统的授权；原生不可用的能力不会自动启用。",
              ) + " "}
            </p>
          </fieldset>
          {!spaceId && (
            <fieldset disabled={busy} className="workspace-field">
              <legend>{t("选择连接方式")}</legend>
              <div className="workspace-grid">
                {(["lan", "tailcat"] as ShareTransport[]).map((value) => (
                  <label
                    className={`workspace-card ${shareTransport === value ? "selected" : ""}`}
                    key={value}
                  >
                    <input
                      type="radio"
                      name="shareTransport"
                      value={value}
                      checked={shareTransport === value}
                      onChange={() => setShareTransport(value)}
                    />
                    <div className="workspace-top">
                      <span className="entry-icon">
                        <Icon
                          name={value === "lan" ? "link" : "globe"}
                          size={23}
                        />
                      </span>
                      <span className="radio-dot" />
                    </div>
                    <h3>
                      {value === "lan" ? t("局域网") : t("Tailcat 跨网络")}
                    </h3>
                    <strong>
                      {value === "lan"
                        ? t("默认 · 快速直连")
                        : t("实验性 · 无需 Tailscale 账号")}
                    </strong>
                    <p>
                      {value === "lan"
                        ? t(
                            "适合两台 Mac 位于同一局域网，连接不会经过公网中继。",
                          )
                        : t(
                            "通过 Tailcat 建立加密通道；无法点对点直连时可能经过第三方 DERP 中继。",
                          )}
                    </p>
                  </label>
                ))}
              </div>
            </fieldset>
          )}
          <div ref={errorRef} tabIndex={-1}>
            <ErrorBox
              message={error}
              retry={busy ? undefined : () => setPreviewVersion((x) => x + 1)}
            />
          </div>
          {!preview && !error ? (
            <Loading text={t("正在确认会话与 Git 起点…")} />
          ) : (
            preview && (
              <div className="panel starting-point">
                <div className="panel-heading">
                  <h3>{t("确认起点")}</h3>
                  <span className="badge muted">{t("创建时不会发送指令")}</span>
                </div>
                <dl>
                  <dt>{t("来源目录")}</dt>
                  <dd className="path">{preview.workspace.sourceCwd}</dd>
                  <dt>{t("执行目录")}</dt>
                  <dd className="path">
                    {mode === "existing"
                      ? preview.workspace.sourceCwd
                      : preview.targetDirectory ||
                        t("Team Cross 数据目录中的独立 worktree")}
                  </dd>
                  <dt>{t("Git 起点")}</dt>
                  <dd>
                    <code>
                      {preview.workspace.head?.slice(0, 10) || t("尚无提交")}
                    </code>
                    <span className="muted">
                      {" "}
                      · {preview.workspace.branch || t("分离 HEAD")}
                    </span>
                    {mode === "worktree" && (
                      <span className="branch-chip">
                        codex/collab-{requestId.current.slice(0, 8)}
                      </span>
                    )}
                  </dd>
                </dl>
                {mode === "existing" && preview.workspace.dirty && (
                  <p className="inline-note">
                    {t("当前目录有未提交内容，将保留在原处供协作使用。") + " "}
                  </p>
                )}
              </div>
            )
          )}
          {!spaceId && (
            <label className="field">
              {t("协作名称") + " "}
              <input
                value={title}
                maxLength={160}
                disabled={busy}
                onChange={(e) => setTitle(e.target.value)}
                placeholder={t("为这次协作起个名字")}
              />
            </label>
          )}
          <div className="form-footer">
            <button
              className="button"
              disabled={busy}
              onClick={() => setStep(1)}
            >
              <Icon name="back" size={16} />
              {t("上一步") + " "}
            </button>
            <button
              className="button primary"
              disabled={!preview || busy}
              onClick={() => void create()}
            >
              {busy ? (
                <>
                  <span className="spinner" />
                  {!spaceId && shareTransport === "tailcat"
                    ? t("正在建立跨网络邀请…")
                    : t("正在创建协作…")}
                </>
              ) : (
                <>
                  {spaceId ? t("启用共同执行") : t("创建并邀请")}{" "}
                  <Icon name="arrow" size={17} />
                </>
              )}
            </button>
          </div>
        </section>
      )}
    </>
  );
}
