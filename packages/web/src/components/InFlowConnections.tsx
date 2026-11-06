import { useState } from "react";
import { api, errorText, useResource } from "../api";
import {
  hasConversationPanel,
  type CurrentConnection,
} from "../currentConversation";
import { t } from "../i18n";
import { useRefreshGuard, useRefreshState } from "../refreshState";
import { sourceName, type Provider, type Source } from "../types";
import { ErrorBox, Loading, Modal, ProviderFilter } from "./ui";

export function InFlowConnections({
  spaceId,
  disabled,
  onChanged,
}: {
  spaceId: string;
  disabled: boolean;
  onChanged: () => void;
}) {
  const [open, setOpen] = useRefreshState(
    `InFlowConnections:${spaceId}:open`,
    false,
  );
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const path = hasConversationPanel()
    ? `plugin/current-connection?spaceId=${encodeURIComponent(spaceId)}`
    : null;
  const connection = useResource<CurrentConnection>(path, 3000);
  const current =
    connection.dataPath === path && !connection.error
      ? connection.data
      : undefined;
  async function enableCurrent() {
    if (busy || !current?.known) return;
    setBusy(true);
    setError("");
    try {
      await api("plugin/current-inflow", { spaceId });
      connection.reload();
      onChanged();
    } catch (e) {
      setError(errorText(e));
      connection.reload();
    } finally {
      setBusy(false);
    }
  }
  return (
    <section className="panel inflow-connections">
      <h3>{t("接收空间更新")}</h3>
      <p className="muted small-text">
        {t(
          "选择自己的会话，下次交互时附带简报、定向留言和本人请求结果。无需在对话中配对。",
        )}
      </p>
      <div className="workbench-card-actions">
        {path && (
          <button
            className="button primary"
            disabled={disabled || busy || !current?.known || !!current.inFlow}
            onClick={enableCurrent}
          >
            {current?.inFlow
              ? t("当前会话已开启顺流感知")
              : busy
                ? t("正在开启…")
                : t("让当前会话接收空间更新")}
          </button>
        )}
        <button
          className="button"
          disabled={disabled || busy}
          onClick={() => {
            setOpen(true);
          }}
        >
          {t("选择本机会话")}
        </button>
      </div>
      {path && current && !current.known && (
        <p className="muted small-text">
          {t("宿主未提供当前会话身份，请从本机会话列表选择。")}
        </p>
      )}
      {current?.inFlow && (
        <p className="notice" role="status">
          {t("顺流感知已开启，更新将在该会话下一次正常交互时附带。")}
        </p>
      )}
      <ErrorBox message={error || connection.error} />
      <details className="inflow-setup-help">
        <summary>{t("自己或其他协作者如何配置？")}</summary>
        <ol>
          <li>
            {t(
              "每位成员在自己的 Team Cross 中加入同一个空间，再选择自己的本机会话。",
            )}
          </li>
          <li>
            {t(
              "在本机设置中接入 Team Cross 插件或个人 MCP；会话正常调用 Team Cross 工具时即可附带更新。",
            )}
          </li>
          <li>
            {t(
              "如需在发送普通消息时接收更新，可在原生客户端启用并信任 Team Cross 的 UserPromptSubmit Hook。",
            )}
          </li>
        </ol>
        <p className="muted small-text">
          {t(
            "每人只管理自己的会话。空间和本机 Core 需要可连接；开启后仍需可用的 MCP 交互或 Hook，不会唤醒空闲会话。",
          )}
        </p>
        <a className="text-link" href="#/settings">
          {t("打开本机设置")}
        </a>
      </details>
      {open && (
        <LocalInFlowConnection
          key={spaceId}
          spaceId={spaceId}
          disabled={disabled}
          onClose={() => setOpen(false)}
          onConnected={() => {
            setOpen(false);
            connection.reload();
            onChanged();
          }}
        />
      )}
    </section>
  );
}

function LocalInFlowConnection({
  spaceId,
  disabled,
  onClose,
  onConnected,
}: {
  spaceId: string;
  disabled: boolean;
  onClose: () => void;
  onConnected: () => void;
}) {
  const key = `LocalInFlowConnection:${spaceId}`;
  const [provider, setProvider] = useRefreshState<Provider>(
    key + ":provider",
    "codex",
  );
  const [search, setSearch] = useRefreshState(key + ":search", "");
  const [cursor, setCursor] = useState("");
  const [selected, setSelected] = useRefreshState(key + ":source", "");
  const [name, setName] = useRefreshState(key + ":name", "");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const path = `sources?provider=${provider}&search=${encodeURIComponent(search)}&cursor=${encodeURIComponent(cursor)}`;
  const sources = useResource<{ data: Source[]; nextCursor?: string }>(path);
  const rows =
    sources.dataPath === path && !sources.error ? sources.data : undefined;
  const source = rows?.data.find((s) => s.id === selected);
  useRefreshGuard(key, true);
  async function connect() {
    if (busy || !source) return;
    setBusy(true);
    setError("");
    try {
      await api(`collaborations/${spaceId}/workbench/inflow-connect`, {
        provider,
        sourceId: source.id,
        ...(name.trim() ? { name: name.trim() } : {}),
      });
      onConnected();
    } catch (e) {
      setError(errorText(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <Modal
      title={t("选择会话接收空间更新")}
      onClose={() => {
        if (!busy) onClose();
      }}
    >
      <p className="muted small-text">
        {t("仅公开所选会话的名称与接收状态，私人历史保留在本机。")}
      </p>
      <fieldset disabled={busy || disabled} className="inflow-source-fields">
        <ProviderFilter
          value={provider}
          onChange={(p) => {
            setProvider(p);
            setCursor("");
            setSelected("");
          }}
        />
        <label>
          {t("搜索来源会话")}
          <input
            value={search}
            onChange={(e) => {
              setSearch(e.target.value);
              setCursor("");
              setSelected("");
            }}
          />
        </label>
        <ErrorBox message={sources.error} retry={sources.reload} />
        <div
          className="source-list"
          role="radiogroup"
          aria-label={t("本机会话")}
        >
          {sources.loading && !rows ? (
            <Loading text={t("正在读取本机会话…")} />
          ) : rows && !rows.data.length ? (
            <p className="muted">{t("没有找到来源会话")}</p>
          ) : (
            rows?.data.map((s) => (
              <button
                type="button"
                role="radio"
                aria-checked={selected === s.id}
                className={`source-row ${selected === s.id ? "selected" : ""}`}
                key={s.id}
                onClick={() => setSelected(s.id)}
              >
                <span className="radio-dot" />
                <div>
                  <strong>{sourceName(s)}</strong>
                  <span className="row-meta">{s.cwd}</span>
                  <span className="row-meta">{s.id.slice(0, 8)}</span>
                </div>
              </button>
            ))
          )}
        </div>
        <div className="source-pager">
          {!!cursor && (
            <button
              className="button small"
              onClick={() => {
                setCursor("");
                setSelected("");
              }}
            >
              {t("返回首页")}
            </button>
          )}
          {!!rows?.nextCursor && (
            <button
              className="button small"
              onClick={() => {
                setCursor(rows.nextCursor!);
                setSelected("");
              }}
            >
              {t("下一页")}
            </button>
          )}
        </div>
        <details>
          <summary>{t("自定义会话名称（可选）")}</summary>
          <input
            aria-label={t("接收会话名称")}
            value={name}
            maxLength={80}
            onChange={(e) => setName(e.target.value)}
          />
        </details>
      </fieldset>
      <ErrorBox message={error} />
      <p className="muted small-text">
        {t(
          "开启后，请在该会话正常使用已接入的 Team Cross 工具，或通过已信任的 Hook 接收更新。",
        )}
      </p>
      <button
        className="button primary"
        disabled={disabled || busy || !source}
        onClick={connect}
      >
        {busy ? t("正在开启…") : t("开启顺流感知")}
      </button>
    </Modal>
  );
}
