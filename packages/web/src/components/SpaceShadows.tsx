import { useEffect, useId, useLayoutEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { api, errorText, useResource } from "../api";
import { formatDate, serviceText, t } from "../i18n";
import { localStorage } from "../storage";
import {
  useRefreshState,
  useRefreshRef,
  useRefreshGuard,
} from "../refreshState";
import type {
  Shadow,
  ShadowApproval,
  ShadowMessage,
  ShadowSettings,
  ShadowModels,
  ShadowView,
} from "../shadows";
import { ErrorBox, Icon, Loading, Modal } from "./ui";
import { MarkdownText } from "./MarkdownText";
import { DefaultShadow } from "./DefaultShadow";
import { ShadowQuestions } from "./ShadowQuestions";
import { ShadowSessions } from "./ShadowSessions";
import { SpaceAvatar } from "./SpaceIdentity";
import { ShadowInspector, type ShadowInspectorTab } from "./ShadowInspector";
import "../shadows.css";

export function SpaceShadows({
  spaceId,
  disabled,
  navigation,
  active = true,
  onActivate,
}: {
  spaceId: string;
  disabled: boolean;
  navigation?: HTMLElement | null;
  active?: boolean;
  onActivate?: () => void;
}) {
  const list = useResource<ShadowView>(
    `collaborations/${spaceId}/shadows/view`,
    2000,
  );
  const refreshKey = `SpaceShadows:v1:${spaceId}`;
  const [selected, select] = useRefreshState(refreshKey + ":selected", "");
  const [expanded, setExpanded] = useRefreshState(
    refreshKey + ":expanded",
    true,
  );
  const [visited, setVisited] = useRefreshState<string[]>(
    refreshKey + ":visited",
    [],
  );
  const groupId = useId();
  const [creating, setCreating] = useRefreshState(
    refreshKey + ":creating",
    false,
  );
  useRefreshGuard(refreshKey, creating);
  const shadows = list.data?.shadows || [];
  const current = shadows.find((s) => s.id === selected) || shadows[0];
  useEffect(() => {
    if (active && current)
      setVisited((ids) =>
        ids.includes(current.id) ? ids : [...ids, current.id],
      );
  }, [active, current?.id]);
  function create() {
    onActivate?.();
    setCreating(true);
  }
  const sidebar = (
    <nav
      className="shadow-navigation execution-navigation"
      aria-label={t("本机数字分身")}
    >
      <div className="shadow-nav-heading">
        <button
          className="shadow-group-toggle"
          aria-expanded={expanded}
          aria-controls={groupId}
          aria-label={expanded ? t("收起数字分身") : t("展开数字分身")}
          title={t("本机数字分身")}
          onClick={() => setExpanded(!expanded)}
        >
          <Icon name="chevron" size={13} />
          <span className="space-nav-label">{t("数字分身")}</span>
          <span className="count">{shadows.length}</span>
        </button>
        <button
          className="icon-button shadow-nav-create"
          disabled={disabled}
          aria-label={t("新增数字分身")}
          title={t("创建我的分身")}
          onClick={create}
        >
          <Icon name="plus" size={15} />
        </button>
      </div>
      <div id={groupId} hidden={!expanded}>
        {shadows.map((s) => (
          <button
            key={s.id}
            className={`execution-nav-item shadow-nav-item ${active && s.id === current?.id ? "active" : ""}`}
            aria-current={active && s.id === current?.id ? "page" : undefined}
            title={s.name}
            aria-label={`${s.name} · ${shadowStatus(s)}`}
            onClick={() => {
              select(s.id);
              onActivate?.();
            }}
          >
            <span className="shadow-nav-avatar">
              <SpaceAvatar name={s.name} agent />
              <span
                className={`dot ${s.online && s.enabled ? "green-dot" : ""}`}
              />
            </span>
            <span className="execution-nav-copy">
              <strong>{s.name}</strong>
              <small>
                {s.owner.name} · {shadowStatus(s)}
              </small>
            </span>
          </button>
        ))}
        {!shadows.length && (
          <p className="small-text muted shadow-nav-empty">
            {list.loading ? t("正在读取分身") : t("还没有数字分身")}
          </p>
        )}
        <button
          className="text-button shadow-nav-add"
          disabled={disabled}
          onClick={create}
          title={t("创建我的分身")}
        >
          <Icon name="plus" size={15} />
          <span>{t("创建我的分身")}</span>
        </button>
      </div>
    </nav>
  );
  return (
    <section className="shadow-workspace">
      {navigation
        ? createPortal(sidebar, navigation)
        : navigation === undefined
          ? sidebar
          : null}
      <header className="shadow-workspace-header">
        <h2>{t("本机数字分身")}</h2>
        {list.data && (
          <details className="shadow-routing-menu">
            <summary>
              <Icon name="settings" size={14} />
              {t("接手我的动态")}
              <Icon name="chevron" size={12} />
            </summary>
            <DefaultShadow
              key={spaceId}
              spaceId={spaceId}
              selfId={list.data.selfId}
              shadows={shadows}
              disabled={disabled}
            />
          </details>
        )}
      </header>
      <ErrorBox message={list.error} />
      {list.loading && !list.data ? (
        <Loading />
      ) : shadows.length ? (
        <>
          {shadows
            .filter(
              (s) => visited.includes(s.id) || (active && s.id === current?.id),
            )
            .map((s) => (
              <div
                className="shadow-chat-slot"
                key={s.id}
                hidden={!active || s.id !== current?.id}
              >
                <ShadowChat
                  spaceId={spaceId}
                  selected={s}
                  selfId={list.data!.selfId}
                  disabled={disabled}
                  active={active && s.id === current?.id}
                />
              </div>
            ))}
        </>
      ) : (
        <div className="panel workbench-empty">
          <h3>{t("让本机 Codex 成为空间里的协作者")}</h3>
          <p>
            {t(
              "分身在创建者的 Mac 上运行。成员看到相同的聊天、记忆和任务进展。",
            )}
          </p>
          <button
            className="button primary"
            disabled={disabled}
            onClick={create}
          >
            <Icon name="plus" />
            {t("开始创建")}
          </button>
        </div>
      )}
      {creating && (
        <CreateShadow
          spaceId={spaceId}
          onClose={() => setCreating(false)}
          onCreated={(id) => {
            select(id);
            setExpanded(true);
            list.reload();
            setCreating(false);
          }}
        />
      )}
    </section>
  );
}

function shadowStatus(s: Shadow) {
  return !s.online
    ? t("离线")
    : !s.enabled
      ? t("已暂停接收")
      : s.busy
        ? t("正在处理")
        : t("在线");
}

function CreateShadow({
  spaceId,
  onClose,
  onCreated,
}: {
  spaceId: string;
  onClose: () => void;
  onCreated: (id: string) => void;
}) {
  const refreshKey = `CreateShadow:v1:${spaceId}`;
  const [name, setName] = useRefreshState(refreshKey + ":name", "");
  const [cwd, setCwd] = useRefreshState(refreshKey + ":cwd", "");
  const [settings, setSettings] = useRefreshState<ShadowSettings>(
    refreshKey + ":settings",
    { model: "", modelProvider: "", reasoningEffort: "" },
  );
  const [catalog, setCatalog] = useState<ShadowModels>();
  const [catalogCwd, setCatalogCwd] = useState("");
  const [loadingModels, setLoadingModels] = useState(false);
  const [modelError, setModelError] = useState("");
  const [reloadModels, setReloadModels] = useState(0);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  // Keep the id on a failed or unknown response. A manual retry cannot create a second shadow.
  const request = useRefreshRef<{
    requestId: string;
    name: string;
    cwd: string;
    settings: ShadowSettings;
  } | null>(refreshKey + ":request", null);
  useRefreshGuard(refreshKey, !!name || !!cwd || !!request.current);
  useEffect(() => {
    if (request.current || !cwd.trim().startsWith("/")) return;
    const abort = new AbortController();
    setLoadingModels(true);
    setModelError("");
    const timer = setTimeout(async () => {
      try {
        const next = await api<ShadowModels>(
          `collaborations/${spaceId}/shadows/models?cwd=${encodeURIComponent(cwd.trim())}`,
          undefined,
          abort.signal,
        );
        if (abort.signal.aborted) return;
        setCatalog(next);
        setCatalogCwd(cwd.trim());
        setSettings((previous) => {
          const model = next.models.find((m) => m.model === previous.model);
          if (
            model &&
            previous.modelProvider === next.defaults.modelProvider &&
            (model.custom ||
              model.supportedReasoningEfforts.some(
                (e) => e.reasoningEffort === previous.reasoningEffort,
              ))
          )
            return previous;
          return next.defaults;
        });
      } catch (e) {
        if (!abort.signal.aborted) setModelError(errorText(e));
      } finally {
        if (!abort.signal.aborted) setLoadingModels(false);
      }
    }, 500);
    return () => {
      clearTimeout(timer);
      abort.abort();
    };
  }, [cwd, spaceId, reloadModels, request, setSettings]);
  const catalogReady =
    !!catalog && catalogCwd === cwd.trim() && !loadingModels && !modelError;
  const selectedModel = catalog?.models.find((m) => m.model === settings.model);
  const locked = busy || !!request.current;
  async function create() {
    if (
      !request.current &&
      (!catalogReady || !settings.model || !settings.reasoningEffort.trim())
    )
      return;
    setBusy(true);
    setError("");
    request.current ||= {
      requestId: crypto.randomUUID(),
      name: name.trim(),
      cwd: cwd.trim(),
      settings: {
        ...settings,
        reasoningEffort: settings.reasoningEffort.trim(),
      },
    };
    try {
      const result = await api<Shadow>(
        `collaborations/${spaceId}/shadows/create`,
        request.current,
      );
      onCreated(result.id);
    } catch (e) {
      setError(errorText(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <Modal title={t("创建本机数字分身")} onClose={onClose}>
      <form
        onSubmit={(e) => {
          e.preventDefault();
          void create();
        }}
      >
        <p>
          {t(
            "使用本机 Codex，继承登录、权限、hooks、插件和 MCP。整个空间的成员都可以向它发消息。",
          )}
        </p>
        <label className="field">
          {t("分身名称")}
          <input
            value={name}
            onChange={(e) => setName(e.target.value)}
            maxLength={80}
            required
            disabled={busy || !!request.current}
          />
        </label>
        <label className="field">
          {t("工作目录")}
          <input
            value={cwd}
            onChange={(e) => setCwd(e.target.value)}
            placeholder="/absolute/path"
            required
            disabled={busy || !!request.current}
          />
        </label>
        {loadingModels && !request.current && (
          <p role="status">{t("正在读取本机模型…")}</p>
        )}
        <ErrorBox message={modelError} />
        {modelError && (
          <button
            type="button"
            className="button"
            disabled={locked}
            onClick={() => setReloadModels((n) => n + 1)}
          >
            {t("重新读取模型")}
          </button>
        )}
        <label className="field">
          {t("模型")}
          <select
            value={settings.model}
            disabled={locked || !catalogReady}
            required
            onChange={(e) => {
              const model = catalog?.models.find(
                (m) => m.model === e.target.value,
              );
              if (model)
                setSettings({
                  ...settings,
                  model: model.model,
                  reasoningEffort: model.defaultReasoningEffort,
                });
            }}
          >
            {!catalog && (
              <option value={settings.model}>
                {settings.model || t("填写工作目录后加载")}
              </option>
            )}
            {catalog?.models.map((model) => (
              <option key={model.model} value={model.model}>
                {model.displayName === model.model
                  ? model.model
                  : `${model.displayName} (${model.model})`}
              </option>
            ))}
          </select>
        </label>
        <label className="field">
          {t("推理强度")}
          {selectedModel?.custom ? (
            <input
              value={settings.reasoningEffort}
              required
              disabled={locked || !catalogReady}
              onChange={(e) =>
                setSettings({ ...settings, reasoningEffort: e.target.value })
              }
            />
          ) : (
            <select
              value={settings.reasoningEffort}
              required
              disabled={locked || !catalogReady}
              onChange={(e) =>
                setSettings({ ...settings, reasoningEffort: e.target.value })
              }
            >
              {!selectedModel && (
                <option value={settings.reasoningEffort}>
                  {settings.reasoningEffort || t("填写工作目录后加载")}
                </option>
              )}
              {selectedModel?.supportedReasoningEfforts.map((effort) => (
                <option
                  key={effort.reasoningEffort}
                  value={effort.reasoningEffort}
                >
                  {effort.reasoningEffort}
                </option>
              ))}
            </select>
          )}
        </label>
        {selectedModel?.custom && (
          <p className="muted">
            {t(
              "此自定义模型未提供能力列表。请填写推理强度，运行时会核对是否接受。",
            )}
          </p>
        )}
        {settings.modelProvider && (
          <p className="muted">Provider: {settings.modelProvider}</p>
        )}
        <p className="muted">
          {t(
            "只在启动时选择一次。后续聊天、换段、后台任务和重启均沿用这组设置。",
          )}
        </p>
        <p className="muted">
          {t(
            "创建独立的共享聊天。工作目录中的原生指令与配置会生效，已有私人对话不会导入。",
          )}
        </p>
        <ErrorBox message={error} />
        <button
          className="button primary"
          disabled={
            busy ||
            !name.trim() ||
            !cwd.trim().startsWith("/") ||
            (!request.current &&
              (!catalogReady ||
                !settings.model ||
                !settings.reasoningEffort.trim()))
          }
        >
          {busy
            ? t("正在创建…")
            : request.current
              ? t("核对并重试创建")
              : t("创建分身")}
        </button>
      </form>
    </Modal>
  );
}

function ShadowChat({
  spaceId,
  selected,
  selfId,
  disabled,
  active,
}: {
  spaceId: string;
  selected: Shadow;
  selfId: string;
  disabled: boolean;
  active: boolean;
}) {
  const refreshKey = `ShadowChat:v1:${spaceId}:${selected.id}`;
  const [offset, setOffset] = useRefreshState(refreshKey + ":offset", 0);
  const data = useResource<ShadowView>(
    active
      ? `collaborations/${spaceId}/shadows/view?shadowId=${selected.id}&offset=${offset}`
      : null,
    1000,
  );
  const view = data.data?.shadows[0] || selected;
  const own = selected.owner.id === selfId;
  const local = useResource<{
    view: Shadow;
    cwd: string;
    approvals: ShadowApproval[];
    diagnosticError?: string;
  }>(own && active ? `shadows/${selected.id}/local` : null, 1000);
  const key = `teamcross-shadow-pending:${spaceId}:${selected.id}`;
  const [pending, setPending] = useState<ShadowMessage[]>(() => {
    try {
      return (
        JSON.parse(localStorage.getItem(key) || "[]") as ShadowMessage[]
      ).map((m) => ({ ...m, state: "sendFailed" }));
    } catch {
      return [];
    }
  });
  const [text, setText] = useRefreshState(refreshKey + ":text", "");
  const [approvalDrafts, setApprovalDrafts] = useRefreshState<
    Record<string, string>
  >(refreshKey + ":approvalDrafts", {});
  useRefreshGuard(
    refreshKey,
    !!text ||
      pending.length > 0 ||
      Object.values(approvalDrafts).some((value) => value !== "{}"),
  );
  useEffect(() => {
    if (!local.data) return;
    const ids = new Set(
      local.data.approvals.map((item) => JSON.stringify(item.id)),
    );
    setApprovalDrafts((previous) => {
      const remaining = Object.entries(previous).filter(([id]) => ids.has(id));
      return remaining.length === Object.keys(previous).length
        ? previous
        : Object.fromEntries(remaining);
    });
  }, [local.data, setApprovalDrafts]);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [details, setDetails] = useRefreshState(refreshKey + ":details", false);
  const [inspectorTab, setInspectorTab] = useRefreshState<ShadowInspectorTab>(
    refreshKey + ":inspectorTab",
    "tasks",
  );
  const [inspectorOpen, setInspectorOpen] = useRefreshState(
    refreshKey + ":inspectorOpen",
    false,
  );
  const [wide, setWide] = useState(
    () => window.matchMedia?.("(min-width: 1101px)")?.matches ?? false,
  );
  const [awayFromEnd, setAwayFromEnd] = useState(false);
  const scroll = useRefreshRef(refreshKey + ":scroll", {
    top: 0,
    follow: true,
  });
  const messagesRef = useRef<HTMLDivElement>(null);
  const layoutRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLTextAreaElement>(null);
  useEffect(() => {
    const query = window.matchMedia?.("(min-width: 1101px)");
    const update = () => {
      setWide(query?.matches ?? false);
      if (query?.matches) setInspectorOpen(false);
    };
    query?.addEventListener("change", update);
    return () => query?.removeEventListener("change", update);
  }, []);
  const attempts = useRef(new Map<string, ReturnType<typeof setTimeout>>());
  useEffect(
    () => () => {
      attempts.current.forEach(clearTimeout);
    },
    [],
  );
  useEffect(() => {
    localStorage.setItem(key, JSON.stringify(pending));
  }, [key, pending]);
  useEffect(() => {
    const acknowledged = new Set(
      (data.data?.shadows[0]?.messages || []).map((m) => m.id),
    );
    setPending((items) => {
      const next = items.filter((m) => !acknowledged.has(m.id));
      return next.length === items.length ? items : next;
    });
  }, [data.data]);
  const messages = [
    ...(view.messages || []),
    ...pending.filter(
      (m) => !view.messages?.some((known) => known.id === m.id),
    ),
  ].sort((a, b) => a.createdAt.localeCompare(b.createdAt));
  useLayoutEffect(() => {
    const el = messagesRef.current;
    if (!active || !el) return;
    el.scrollTop =
      offset === 0 && scroll.current.follow
        ? el.scrollHeight
        : scroll.current.top;
    setAwayFromEnd(el.scrollHeight - el.scrollTop - el.clientHeight >= 100);
  }, [active, messages.length, messages.at(-1)?.text, offset, scroll]);
  useLayoutEffect(() => {
    const el = inputRef.current;
    if (!active || !el) return;
    el.style.height = "auto";
    el.style.height = `${Math.min(144, Math.max(52, el.scrollHeight))}px`;
  }, [text, active]);
  useEffect(() => {
    const el = messagesRef.current;
    if (!active || !el || typeof ResizeObserver === "undefined") return;
    const observer = new ResizeObserver(() => {
      layoutRef.current?.style.setProperty(
        "--shadow-scroll-gutter",
        `${Math.max(0, (el.offsetWidth - el.clientWidth) / 2)}px`,
      );
      if (scroll.current.follow && offset === 0) el.scrollTop = el.scrollHeight;
    });
    observer.observe(el);
    if (el.firstElementChild) observer.observe(el.firstElementChild);
    return () => observer.disconnect();
  }, [active, offset, scroll]);
  function latest() {
    scroll.current = { top: 0, follow: true };
    setOffset(0);
    if (messagesRef.current)
      messagesRef.current.scrollTop = messagesRef.current.scrollHeight;
    setAwayFromEnd(false);
  }
  function submit() {
    if (!text.trim() || busy || disabled || !view.online || !view.enabled)
      return;
    const m: ShadowMessage = {
      id: crypto.randomUUID(),
      actor: { id: selfId, name: t("我") },
      role: "user",
      text: text.trim(),
      createdAt: new Date().toISOString(),
      state: "sending",
    };
    setText("");
    latest();
    void send(m);
  }
  function openInspector(tab: ShadowInspectorTab) {
    setInspectorTab(tab);
    setInspectorOpen(!wide);
  }
  function openDetails() {
    setInspectorOpen(false);
    setDetails(true);
  }
  const inspector = (
    <ShadowInspector
      view={view}
      selfId={selfId}
      tab={inspectorTab}
      onTab={setInspectorTab}
      onDetails={openDetails}
    />
  );
  async function send(message: ShadowMessage) {
    setBusy(true);
    setError("");
    const localMessage = { ...message, state: "sending" };
    setPending((items) => [
      ...items.filter((m) => m.id !== message.id),
      localMessage,
    ]);
    clearTimeout(attempts.current.get(message.id));
    attempts.current.set(
      message.id,
      setTimeout(() => {
        setPending((items) =>
          items.map((m) =>
            m.id === message.id && m.state === "sending"
              ? { ...m, state: "sendFailed" }
              : m,
          ),
        );
        data.reload();
      }, 15000),
    );
    try {
      await api(`collaborations/${spaceId}/shadows/send`, {
        shadowId: selected.id,
        message: { id: message.id, text: message.text },
      });
      data.reload();
    } catch (e) {
      setError(errorText(e));
      setPending((items) =>
        items.map((m) =>
          m.id === message.id ? { ...m, state: "sendFailed" } : m,
        ),
      );
    } finally {
      setBusy(false);
    }
  }
  async function action(action: string, extra: Record<string, unknown> = {}) {
    setBusy(true);
    setError("");
    try {
      await api(`shadows/${selected.id}/action`, { action, ...extra });
      local.reload();
      data.reload();
    } catch (e) {
      setError(errorText(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <div className="shadow-chat-layout" ref={layoutRef}>
      <div className="shadow-chat">
        <div className="shadow-chat-heading">
          <div className="shadow-chat-identity">
            <SpaceAvatar name={view.name} agent />
            <div>
              <strong>{view.name}</strong>
              <span>
                {view.owner.name} ·{" "}
                {view.online
                  ? view.enabled
                    ? t("在线")
                    : t("已暂停接收")
                  : t("离线")}
              </span>
            </div>
          </div>
          <div className="shadow-chat-actions">
            <button
              className="shadow-toolbar-button"
              aria-label={t("查看后台任务")}
              aria-expanded={
                (wide || inspectorOpen) && inspectorTab === "tasks"
              }
              onClick={() => openInspector("tasks")}
            >
              <Icon name="layers" size={16} />
              <span>{t("任务")}</span>
            </button>
            <button
              className="shadow-toolbar-button"
              aria-label={t("查看共享记忆")}
              aria-expanded={
                (wide || inspectorOpen) && inspectorTab === "memory"
              }
              onClick={() => openInspector("memory")}
            >
              <Icon name="pin" size={16} />
              <span>{t("记忆")}</span>
            </button>
            <button
              className="icon-button"
              aria-label={t("会话与任务")}
              title={t("会话与任务")}
              onClick={openDetails}
              aria-expanded={details}
              aria-haspopup="dialog"
            >
              <Icon name="more" size={17} />
            </button>
          </div>
        </div>
        <ErrorBox message={error || data.error || serviceText(view.error)} />
        <div className="shadow-message-area">
          <div
            className="shadow-messages"
            ref={messagesRef}
            role="log"
            aria-label={t("分身共享聊天")}
            onScroll={(e) => {
              const el = e.currentTarget;
              if (!active) return;
              const follow =
                el.scrollHeight - el.scrollTop - el.clientHeight < 100;
              scroll.current = { top: el.scrollTop, follow };
              setAwayFromEnd(!follow);
            }}
          >
            <div className="shadow-reading-column">
              {(data.data?.nextOffset !== undefined || offset > 0) && (
                <div className="workbench-pagination">
                  <button
                    className="button small"
                    disabled={data.data?.nextOffset === undefined}
                    onClick={() => {
                      scroll.current = { top: 0, follow: false };
                      setOffset(data.data!.nextOffset!);
                    }}
                  >
                    {t("更早的消息")}
                  </button>
                  <button
                    className="button small"
                    disabled={!offset}
                    onClick={latest}
                  >
                    {t("回到最新")}
                  </button>
                </div>
              )}
              {!messages.length && (
                <p className="shadow-greeting">
                  {t("直接开始聊。切换话题、追问和长任务都留在这里。")}
                </p>
              )}
              {messages.map((m) => (
                <article
                  key={m.id}
                  className={`shadow-message ${m.role === "assistant" ? "shadow-answer" : "shadow-question"} ${m.actor.id === selfId ? "shadow-own-message" : ""}`}
                >
                  <header>
                    <SpaceAvatar
                      name={m.actor.name}
                      agent={m.role === "assistant"}
                    />
                    <strong>{m.actor.name}</strong>
                    <time>
                      {formatDate(m.createdAt, {
                        month: "numeric",
                        day: "numeric",
                        hour: "2-digit",
                        minute: "2-digit",
                      })}
                    </time>
                  </header>
                  {m.role === "assistant" ? (
                    <MarkdownText source={m.text} />
                  ) : (
                    <p className="shadow-user-text">{m.text}</p>
                  )}
                  {m.role === "user" && (
                    <footer>
                      <span>
                        {m.receivedAt
                          ? t("已送达")
                          : m.state === "sending"
                            ? t("发送中…")
                            : t("未确认送达")}
                      </span>
                      {m.receivedAt && m.state === "submitted" && (
                        <span>{t("处理中")}</span>
                      )}
                      {m.receivedAt && m.state === "unknown" && (
                        <span>{t("处理结果待核对")}</span>
                      )}
                      {m.receivedAt && m.state === "failed" && (
                        <span>{t("处理失败")}</span>
                      )}
                      {!m.receivedAt &&
                        m.actor.id === selfId &&
                        m.state !== "sending" && (
                          <button
                            className="text-button"
                            disabled={busy || disabled}
                            onClick={() => void send(m)}
                          >
                            {t("重发")}
                          </button>
                        )}
                    </footer>
                  )}
                </article>
              ))}
            </div>
          </div>
          {offset === 0 && awayFromEnd && (
            <button className="shadow-jump-latest" onClick={latest}>
              <Icon name="chevron" size={13} />
              {t("回到最新")}
            </button>
          )}
        </div>
        {view.busy && (
          <div className="shadow-thinking" role="status">
            {t("分身正在处理，可以继续留言。")}
          </div>
        )}
        {own && !!local.data?.approvals.length && (
          <div className="shadow-approval-notice">
            <span>
              {t("有本机请求需要处理")} · {local.data.approvals.length}
            </span>
            <button className="text-button" onClick={openDetails}>
              {t("查看请求")}
            </button>
          </div>
        )}
        <div className="shadow-composer-region">
          <form
            className="shadow-composer"
            onSubmit={(e) => {
              e.preventDefault();
              submit();
            }}
          >
            <textarea
              ref={inputRef}
              value={text}
              maxLength={16000}
              rows={2}
              disabled={disabled}
              aria-label={t("给分身发消息")}
              placeholder={t("继续聊，或交给它一个后台任务…")}
              onChange={(e) => setText(e.target.value)}
              onKeyDown={(e) => {
                if (
                  e.key === "Enter" &&
                  !e.shiftKey &&
                  !e.nativeEvent.isComposing &&
                  e.keyCode !== 229
                ) {
                  e.preventDefault();
                  submit();
                }
              }}
            />
            <div>
              <span>
                {!view.online
                  ? t("分身离线时不会自动重发。")
                  : t("记住、纠正或忘掉信息，直接告诉分身。")}
              </span>
              <button
                className="button primary"
                disabled={
                  busy ||
                  disabled ||
                  !text.trim() ||
                  !view.online ||
                  !view.enabled
                }
              >
                <Icon name="arrow" size={17} />
                <span>{t("发送")}</span>
              </button>
            </div>
          </form>
          <p className="shadow-shared-note">
            <span>
              <Icon name="people" size={13} />
              {t("全空间共享这段对话")}
            </span>
            <span>{t("Enter 发送 · Shift+Enter 换行")}</span>
          </p>
        </div>
      </div>
      {wide && (
        <aside className="shadow-inspector" aria-label={t("分身详情")}>
          {inspector}
        </aside>
      )}
      {active && inspectorOpen && !wide && (
        <Modal
          title={t("分身详情")}
          className="shadow-inspector-modal"
          onClose={() => setInspectorOpen(false)}
        >
          {inspector}
        </Modal>
      )}
      {active && details && (
        <Modal
          title={t("会话、任务与记忆")}
          wide
          onClose={() => setDetails(false)}
        >
          <div className="shadow-details">
            <p className="shadow-details-name">
              {view.name} · {view.owner.name}
            </p>
            <ErrorBox message={data.error} />
            {own && !!local.data?.approvals.length && (
              <section className="shadow-approvals">
                <h3>{t("本机原生请求")}</h3>
                <p>
                  {t("只有分身所在 Mac 可以处理这些请求。请核对具体操作。")}
                </p>
                {local.data.approvals.map((approval) => (
                  <NativeRequest
                    key={String(approval.id)}
                    value={approvalDrafts[JSON.stringify(approval.id)] ?? "{}"}
                    onChange={(value) =>
                      setApprovalDrafts((previous) => ({
                        ...previous,
                        [JSON.stringify(approval.id)]: value,
                      }))
                    }
                    request={approval}
                    busy={busy}
                    respond={(result) =>
                      void action("respond", { id: approval.id, result })
                    }
                  />
                ))}
              </section>
            )}
            <ShadowSessions view={view} />
            {own && local.data && (
              <ShadowQuestions
                key={view.id}
                spaceId={spaceId}
                shadowId={view.id}
              />
            )}
            <h3>{t("共享记忆")}</h3>
            <p className="muted">
              {t(
                "个人偏好按成员归属保存。候选记忆尚未确认，忘掉的信息不再作为有效记忆使用。",
              )}
            </p>
            {!view.memories?.length && (
              <p className="muted">{t("还没有记忆")}</p>
            )}
            {view.memories?.map((m) => (
              <div className="shadow-fact" key={m.id}>
                <p>{m.state === "forgotten" ? t("已忘掉") : m.text}</p>
                <span>
                  {m.scope === "space"
                    ? t("空间")
                    : m.subjectId === selfId
                      ? t("我的偏好")
                      : t("成员偏好")}{" "}
                  ·{" "}
                  {m.state === "candidate"
                    ? t("待确认")
                    : m.state === "forgotten"
                      ? t("已失效")
                      : t("有效")}{" "}
                  · v{m.revision}
                </span>
              </div>
            ))}
            <details className="shadow-diagnostics">
              <summary>{t("运行与上下文")}</summary>
              {view.settings && (
                <p>
                  {t("启动配置")} · {view.settings.modelProvider} /{" "}
                  {view.settings.model} / {view.settings.reasoningEffort}
                </p>
              )}
              <p>
                {t("运行时确认")} · Codex ·{" "}
                {view.model || t("等待原生会话确认模型")}
              </p>
              <p>
                {view.modelProvider} {view.reasoningEffort}
              </p>
              {own && (
                <>
                  <p className="shadow-path">{local.data?.cwd}</p>
                  {local.data?.diagnosticError && (
                    <pre className="shadow-path">
                      {local.data.diagnosticError}
                    </pre>
                  )}
                  <p className="muted">
                    {t("暂停接收后，已开始的工作继续完成。")}
                  </p>
                  <button
                    className="button small"
                    disabled={busy || disabled}
                    onClick={() =>
                      void action(view.enabled ? "pause" : "resume")
                    }
                  >
                    {view.enabled ? t("暂停接收") : t("恢复接收")}
                  </button>
                  <button
                    className="button small"
                    disabled={busy || disabled || !view.current}
                    onClick={() => void action("reconcile")}
                  >
                    {t("核对原生状态")}
                  </button>
                </>
              )}
            </details>
          </div>
        </Modal>
      )}
    </div>
  );
}

function NativeRequest({
  value,
  onChange,
  request,
  busy,
  respond,
}: {
  value: string;
  onChange: (value: string) => void;
  request: ShadowApproval;
  busy: boolean;
  respond: (result: unknown) => void;
}) {
  const [error, setError] = useState("");
  const decision =
    request.method === "item/commandExecution/requestApproval" ||
    request.method === "item/fileChange/requestApproval";
  const choices = request.params.availableDecisions as unknown[] | undefined;
  return (
    <div className="shadow-native-request">
      <strong>{request.method}</strong>
      <pre>{JSON.stringify(request.params, null, 2)}</pre>
      {decision ? (
        <div>
          {(!choices || choices.includes("accept")) && (
            <button
              className="button"
              disabled={busy}
              onClick={() => respond({ decision: "accept" })}
            >
              {t("允许这次操作")}
            </button>
          )}
          <button
            className="button"
            disabled={busy}
            onClick={() =>
              respond({
                decision:
                  !choices || choices.includes("decline")
                    ? "decline"
                    : "cancel",
              })
            }
          >
            {t("拒绝")}
          </button>
        </div>
      ) : (
        <>
          <label>
            {t("原生请求回复 JSON")}
            <textarea
              value={value}
              onChange={(e) => onChange(e.target.value)}
            />
          </label>
          <button
            className="button"
            disabled={busy}
            onClick={() => {
              try {
                respond(JSON.parse(value));
                setError("");
              } catch {
                setError(t("请输入有效 JSON"));
              }
            }}
          >
            {t("提交回复")}
          </button>
          <ErrorBox message={error} />
        </>
      )}
    </div>
  );
}
