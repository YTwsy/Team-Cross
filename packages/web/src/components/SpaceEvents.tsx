import { useState } from "react";
import { api, errorText, useResource } from "../api";
import { formatDate, t, tr } from "../i18n";
import { Copy, ErrorBox, Icon } from "./ui";

type Connection = {
  id: string;
  spaceId: string;
  name: string;
  state: string;
  expiresAt: string;
};
type Subscription = {
  id: string;
  grantId: string;
  name: string;
  state: string;
  refreshBefore: string;
  verifiedAt: string;
  arguments: { annotationId?: string; requestId?: string };
};
type Event = {
  eventId: string;
  subscriptionId: string;
  state: string;
  receivedAt?: string;
  finishedAt?: string;
  summary?: string;
  timestamp: string;
};
type EventsView = {
  connections: Connection[];
  subscriptions: Subscription[];
  events: Event[];
};

function eventLabel(name: string) {
  if (name === "space.brief.updated") return t("简报更新");
  if (name === "space.discussion.updated") return t("公开讨论更新");
  return t("协作请求结果");
}
function stateLabel(state: string, expires: string) {
  if (state === "revoked") return t("已撤销");
  if (new Date(expires).getTime() <= Date.now()) return t("已到期");
  if (state === "paused") return t("已暂停");
  return t("已启用");
}

export function SpaceEvents({
  spaceId,
  disabled,
}: {
  spaceId: string;
  disabled: boolean;
}) {
  const [open, setOpen] = useState(false);
  const data = useResource<EventsView>(open ? "event-access" : null, 3000);
  const [name, setName] = useState("ChatGPT Work Cloud");
  const [credential, setCredential] = useState("");
  const [requestId, setRequestId] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const connections =
    data.data?.connections.filter((c) => c.spaceId === spaceId) || [];
  const ids = new Set(connections.map((c) => c.id));
  const subscriptions =
    data.data?.subscriptions.filter((s) => ids.has(s.grantId)) || [];
  const events =
    data.data?.events
      .filter((e) => subscriptions.some((s) => s.id === e.subscriptionId))
      .slice(0, 10) || [];
  async function change(kind: string, id: string, state: string) {
    if (busy) return;
    setBusy(true);
    setError("");
    try {
      await api(`event-access/${kind}/${id}`, { state });
      data.reload();
    } catch (e) {
      setError(errorText(e));
    } finally {
      setBusy(false);
    }
  }
  const actions = (
    kind: string,
    entry: { id: string; state: string },
    expires: string,
  ) =>
    entry.state !== "revoked" && (
      <div className="workbench-card-actions">
        <button
          className="button small"
          disabled={
            busy || disabled || new Date(expires).getTime() <= Date.now()
          }
          onClick={() =>
            void change(
              kind,
              entry.id,
              entry.state === "paused" ? "active" : "paused",
            )
          }
        >
          {entry.state === "paused" ? t("恢复接收") : t("暂停接收")}
        </button>
        <button
          className="text-link"
          disabled={busy}
          onClick={() => void change(kind, entry.id, "revoked")}
        >
          {t("撤销")}
        </button>
      </div>
    );
  return (
    <details
      className="panel workbench-disclosure workbench-events"
      onToggle={(event) => setOpen(event.currentTarget.open)}
    >
      <summary>
        <span className="workbench-symbol neutral">
          <Icon name="globe" />
        </span>
        <span className="workbench-disclosure-title">
          <strong>{t("ChatGPT 云端通知")}</strong>
          <span>{t("按需订阅简报、讨论与请求的变化")}</span>
        </span>
        <Icon name="chevron" />
      </summary>
      <div className="workbench-events-body">
        <p className="muted small-text">
          {t(
            "在 ChatGPT Work Cloud 中明确选择要关注的变化和处理方式后，才会开始订阅。空间引用和会话连接不会自动订阅。",
          )}
        </p>
        <p className="muted small-text">
          {t(
            "需要先配置可访问的事件服务地址。连接凭据只允许读取这个空间的简报、公开讨论、材料与请求，并记录事件处理回执，七天后到期。",
          )}
        </p>
        <form
          className="workbench-events-form"
          onSubmit={async (event) => {
            event.preventDefault();
            if (busy || disabled) return;
            const id = requestId || crypto.randomUUID();
            setRequestId(id);
            setBusy(true);
            setError("");
            try {
              const result = await api<{ credential: string }>("event-access", {
                requestId: id,
                spaceId,
                name,
              });
              setCredential(result.credential);
              data.reload();
            } catch (e) {
              setError(errorText(e));
              data.reload();
            } finally {
              setBusy(false);
            }
          }}
        >
          <label>
            {t("连接名称")}
            <input
              value={name}
              maxLength={80}
              disabled={busy || !!credential}
              onChange={(e) => setName(e.target.value)}
            />
          </label>
          {!credential && (
            <button
              className="button"
              disabled={busy || disabled || !name.trim() || !!requestId}
            >
              {t("创建空间连接凭据")}
            </button>
          )}
        </form>
        {!!error && !!requestId && !credential && (
          <div className="notice">
            <p>
              {t(
                "创建结果尚未确认。请先核对下方连接并撤销可能已创建的连接，再创建新凭据。",
              )}
            </p>
            <button
              className="text-link"
              disabled={busy}
              onClick={() => {
                setRequestId("");
                setError("");
              }}
            >
              {t("已核对连接，准备创建新凭据")}
            </button>
          </div>
        )}
        {credential && (
          <div className="notice">
            <p>{t("将这份凭据用于刚才选定的云端连接。它仅在此处显示一次。")}</p>
            <Copy text={credential} label={t("复制连接凭据")} />
            <button
              className="text-link"
              onClick={() => {
                setCredential("");
                setRequestId("");
              }}
            >
              {t("已保存，隐藏凭据")}
            </button>
          </div>
        )}
        <ErrorBox message={error || data.error} />
        {connections.map((connection) => (
          <section className="workbench-event-connection" key={connection.id}>
            <strong>{connection.name}</strong>
            <p className="small-text muted">
              {stateLabel(connection.state, connection.expiresAt)} ·{" "}
              {tr`有效期至 ${formatDate(connection.expiresAt)}`}
            </p>
            {actions("connection", connection, connection.expiresAt)}
            {subscriptions
              .filter((s) => s.grantId === connection.id)
              .map((sub) => (
                <div className="workbench-event-subscription" key={sub.id}>
                  <p>
                    {eventLabel(sub.name)} ·{" "}
                    {stateLabel(sub.state, sub.refreshBefore)}
                  </p>
                  <p className="small-text muted">
                    {sub.arguments.annotationId
                      ? t("仅所选讨论")
                      : sub.arguments.requestId
                        ? t("仅所选请求")
                        : t("本空间的对应变化")}{" "}
                    · {t("回调已核验")}
                  </p>
                  {actions("subscription", sub, sub.refreshBefore)}
                </div>
              ))}
          </section>
        ))}
        {open && !subscriptions.length && (
          <p className="muted small-text workbench-events-empty">
            {t("尚无订阅。创建连接凭据后，还需在 ChatGPT 中明确发起关注。")}
          </p>
        )}
        {events.length > 0 && (
          <section>
            <h4>{t("最近的事件回执")}</h4>
            {events.map((event) => (
              <div className="workbench-brief-item" key={event.eventId}>
                <span>
                  {event.finishedAt
                    ? event.state === "completed"
                      ? t("已报告处理完成")
                      : t("已报告处理失败")
                    : event.receivedAt
                      ? t("会话已读取事件")
                      : event.state === "delivered"
                        ? t("宿主已接收，尚无读取回执")
                        : event.state === "pending"
                          ? t("等待投递")
                          : t("投递已停止，请核对连接")}
                </span>
                {event.summary && <p>{event.summary}</p>}
              </div>
            ))}
          </section>
        )}
      </div>
    </details>
  );
}
