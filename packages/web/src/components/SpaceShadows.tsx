import { useEffect, useRef, useState } from "react";
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
import "../shadows.css";

export function SpaceShadows({
  spaceId,
  disabled,
}: {
  spaceId: string;
  disabled: boolean;
}) {
  const list = useResource<ShadowView>(
    `collaborations/${spaceId}/shadows/view`,
    2000,
  );
  const refreshKey = `SpaceShadows:v1:${spaceId}`;
  const [selected, select] = useRefreshState(refreshKey + ":selected", "");
  const [creating, setCreating] = useRefreshState(
    refreshKey + ":creating",
    false,
  );
  useRefreshGuard(refreshKey, creating);
  const current =
    list.data?.shadows.find((s) => s.id === selected) || list.data?.shadows[0];
  return (
    <section className="shadow-workspace">
      <header className="workbench-header">
        <div>
          <h2>{t("本机数字分身")}</h2>
          <p>{t("整个空间共享一段对话，直接提问或接着聊。")}</p>
        </div>
        <button
          className="button"
          disabled={disabled}
          onClick={() => setCreating(true)}
        >
          <Icon name="plus" />
          {t("创建我的分身")}
        </button>
      </header>
      <ErrorBox message={list.error} />
      {list.loading && !list.data ? (
        <Loading />
      ) : list.data?.shadows.length ? (
        <>
          <div className="shadow-picker" aria-label={t("选择分身")}>
            {list.data.shadows.map((s) => (
              <button
                className={`button ${s.id === current?.id ? "primary" : ""}`}
                key={s.id}
                onClick={() => select(s.id)}
              >
                {s.name}
                <span
                  className={`dot ${s.online && s.enabled ? "green-dot" : ""}`}
                />
              </button>
            ))}
          </div>
          {current && (
            <ShadowChat
              key={current.id}
              spaceId={spaceId}
              selected={current}
              selfId={list.data.selfId}
              disabled={disabled}
            />
          )}
        </>
      ) : (
        <div className="panel workbench-empty">
          <h3>{t("让本机 Codex 成为空间里的协作者")}</h3>
          <p>
            {t(
              "分身在创建者的 Mac 上运行。成员看到相同的聊天、记忆和任务进展。",
            )}
          </p>
        </div>
      )}
      {creating && (
        <CreateShadow
          spaceId={spaceId}
          onClose={() => setCreating(false)}
          onCreated={(id) => {
            select(id);
            list.reload();
            setCreating(false);
          }}
        />
      )}
    </section>
  );
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
}: {
  spaceId: string;
  selected: Shadow;
  selfId: string;
  disabled: boolean;
}) {
  const refreshKey = `ShadowChat:v1:${spaceId}:${selected.id}`;
  const [offset, setOffset] = useRefreshState(refreshKey + ":offset", 0);
  const data = useResource<ShadowView>(
    `collaborations/${spaceId}/shadows/view?shadowId=${selected.id}&offset=${offset}`,
    1000,
  );
  const view = data.data?.shadows[0] || selected;
  const own = selected.owner.id === selfId;
  const local = useResource<{
    view: Shadow;
    cwd: string;
    approvals: ShadowApproval[];
    diagnosticError?: string;
  }>(own ? `shadows/${selected.id}/local` : null, 1000);
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
  useRefreshGuard(refreshKey, !!text || pending.length > 0);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [details, setDetails] = useRefreshState(refreshKey + ":details", false);
  const end = useRef<HTMLDivElement>(null);
  const nearEnd = useRef(true);
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
  useEffect(() => {
    if (offset === 0 && nearEnd.current)
      end.current?.scrollIntoView({ block: "nearest" });
  }, [messages.length, offset]);
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
  const segment = view.segments?.find((s) => s.id === view.current);
  return (
    <div className="shadow-chat-layout">
      <div className="shadow-chat panel">
        <div className="shadow-chat-heading">
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
          <button
            className="button small"
            onClick={() => setDetails(!details)}
            aria-expanded={details}
          >
            {t("记忆与任务")}
          </button>
        </div>
        <p className="shadow-shared-note">
          <Icon name="people" size={14} />
          {t("全体成员可读。已送达表示分身所在 Mac 已保存消息。")}
        </p>
        <ErrorBox message={error || data.error || serviceText(view.error)} />
        <div
          className="shadow-messages"
          role="log"
          aria-label={t("分身共享聊天")}
          onScroll={(e) => {
            const el = e.currentTarget;
            nearEnd.current =
              el.scrollHeight - el.scrollTop - el.clientHeight < 100;
          }}
        >
          {(data.data?.nextOffset !== undefined || offset > 0) && (
            <div className="workbench-pagination">
              <button
                className="button small"
                disabled={data.data?.nextOffset === undefined}
                onClick={() => setOffset(data.data!.nextOffset!)}
              >
                {t("更早的消息")}
              </button>
              <button
                className="button small"
                disabled={!offset}
                onClick={() => setOffset(0)}
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
              className={`shadow-message ${m.role === "assistant" ? "shadow-answer" : "shadow-question"}`}
            >
              <header>
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
          <div ref={end} />
        </div>
        {view.busy && (
          <div className="shadow-thinking" role="status">
            {t("分身正在处理，可以继续留言。")}
          </div>
        )}
        <form
          className="shadow-composer"
          onSubmit={(e) => {
            e.preventDefault();
            if (!text.trim() || busy || disabled) return;
            const m: ShadowMessage = {
              id: crypto.randomUUID(),
              actor: { id: selfId, name: t("我") },
              role: "user",
              text: text.trim(),
              createdAt: new Date().toISOString(),
              state: "sending",
            };
            setText("");
            setOffset(0);
            nearEnd.current = true;
            void send(m);
          }}
        >
          <textarea
            value={text}
            maxLength={16000}
            rows={3}
            disabled={disabled}
            aria-label={t("给分身发消息")}
            placeholder={t("继续聊，或交给它一个后台任务…")}
            onChange={(e) => setText(e.target.value)}
          />
          <div>
            <span>
              {!view.online
                ? t("分身离线时不会自动重发。")
                : t("记住、纠正或忘掉信息，直接告诉分身。")}
            </span>
            <button
              className="button primary"
              disabled={busy || disabled || !text.trim()}
            >
              {t("发送")}
            </button>
          </div>
        </form>
      </div>
      {details && (
        <aside className="shadow-details panel">
          <h3>{t("后台任务")}</h3>
          {!view.jobs?.length && <p className="muted">{t("还没有后台任务")}</p>}
          {view.jobs?.map((j) => (
            <div className="shadow-fact" key={j.id}>
              <strong>{j.title}</strong>
              <span>
                {jobLabel(j.state)} · v{j.version}
              </span>
              {j.result && (
                <details>
                  <summary>{t("查看结果")}</summary>
                  <MarkdownText source={j.result} />
                </details>
              )}
              {j.error && <ErrorBox message={serviceText(j.error)} />}
            </div>
          ))}
          <h3>{t("共享记忆")}</h3>
          <p className="muted">
            {t(
              "个人偏好按成员归属保存。候选记忆尚未确认，忘掉的信息不再作为有效记忆使用。",
            )}
          </p>
          {!view.memories?.length && <p className="muted">{t("还没有记忆")}</p>}
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
            <p>
              {segment
                ? `S${segment.number} · ${segment.compactions} ${t("次原生压缩")}`
                : t("首条消息会创建主会话")}
            </p>
            <p>
              {segment?.contextWindow && segment.usageValid
                ? `${segment.inputTokens.toLocaleString()} / ${segment.contextWindow.toLocaleString()} tokens`
                : segment?.contextWindow
                  ? t("等待原生端报告当前上下文用量。")
                  : t("原生端尚未报告上下文窗口，继续使用原生压缩。")}
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
                  onClick={() => void action(view.enabled ? "pause" : "resume")}
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
        </aside>
      )}
      {own && !!local.data?.approvals.length && (
        <section className="shadow-approvals panel">
          <h3>{t("本机原生请求")}</h3>
          <p>{t("只有分身所在 Mac 可以处理这些请求。请核对具体操作。")}</p>
          {local.data.approvals.map((approval) => (
            <NativeRequest
              key={String(approval.id)}
              refreshKey={`${refreshKey}:approval:${JSON.stringify(approval.id)}`}
              request={approval}
              busy={busy}
              respond={(result) =>
                void action("respond", { id: approval.id, result })
              }
            />
          ))}
        </section>
      )}
    </div>
  );
}

function jobLabel(state: string) {
  const labels: Record<string, string> = {
    queued: t("排队中"),
    running: t("执行中"),
    stopping: t("正在停止"),
    completed: t("已完成"),
    failed: t("失败"),
    cancelled: t("已取消"),
    interrupted: t("已中断"),
    unknown: t("待核对"),
  };
  return labels[state] || state;
}
function NativeRequest({
  refreshKey,
  request,
  busy,
  respond,
}: {
  refreshKey: string;
  request: ShadowApproval;
  busy: boolean;
  respond: (result: unknown) => void;
}) {
  const [value, setValue] = useRefreshState(refreshKey + ":value", "{}");
  useRefreshGuard(refreshKey, value !== "{}");
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
              onChange={(e) => setValue(e.target.value)}
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
