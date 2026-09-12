import { useEffect, useState } from "react";
import { api, errorText, useResource } from "../api";
import {
  type ClientPlan,
  type Collaboration,
  type Info,
  type Provider,
} from "../types";
import { Copy, ErrorBox, Icon } from "./ui";
import { MCPConnection, mcpStatus } from "./MCPConnection";
export function Clients({
  collaboration,
  initial = "direct",
}: {
  collaboration: Collaboration;
  initial?: "direct" | "assist";
}) {
  const [mode, setMode] = useState<"direct" | "assist">(() =>
    initial === "assist" ||
    localStorage.getItem("teamcross.clientMode") === "assist"
      ? "assist"
      : "direct",
  );
  useEffect(() => {
    localStorage.setItem("teamcross.clientMode", mode);
  }, [mode]);
  const info = useResource<Info>("info", 10000);
  const [preferred, setPreferred] = useState(
    () => localStorage.getItem("teamcross.client.v1") || "tui",
  );
  const [assistProvider, setAssistProvider] = useState<Provider>(() => {
    const saved = localStorage.getItem("teamcross.assistProvider");
    return saved === "claude" || saved === "codex"
      ? saved
      : collaboration.provider === "claude"
        ? "claude"
        : "codex";
  });
  const [connecting, setConnecting] = useState(false);
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const [plan, setPlan] = useState<ClientPlan>();
  const claude =
    (mode === "direct" ? collaboration.provider : assistProvider) === "claude";
  const name = claude ? "Claude Code" : "Codex";
  const mcp = mcpStatus(info.data, assistProvider);
  const clientError = claude ? info.data?.claudeError : info.data?.codexError;
  const directClients = claude
    ? (["tui"] as const)
    : (["tui", "desktop"] as const);
  const mine = collaboration.writer === collaboration.role;
  async function open(client: string, launch: boolean) {
    setBusy(client);
    setError("");
    setPlan(undefined);
    try {
      const result = await api<ClientPlan>(
        `collaborations/${collaboration.id}/${mode === "direct" ? "open" : "assist"}`,
        {
          client,
          launch,
          ...(mode === "assist" ? { provider: assistProvider } : {}),
        },
      );
      setPlan(result);
      if (launch) {
        localStorage.setItem("teamcross.client.v1", client);
        setPreferred(client);
      }
    } catch (e) {
      setError(errorText(e));
    } finally {
      setBusy("");
    }
  }
  return (
    <div className="clients">
      <div className="segmented full" aria-label="参与方式">
        <button
          aria-pressed={mode === "direct"}
          disabled={!!busy || connecting}
          onClick={() => {
            setMode("direct");
            setPlan(undefined);
            setError("");
          }}
        >
          直接操作
        </button>
        <button
          aria-pressed={mode === "assist"}
          disabled={!!busy || connecting}
          onClick={() => {
            setMode("assist");
            setPlan(undefined);
            setError("");
          }}
        >
          用自己的客户端辅助
        </button>
      </div>
      <p className="client-description">
        {mode === "direct"
          ? `连接共享会话，代码与模型调用在 ${collaboration.host} 上执行。`
          : `继续与你自己的 ${name} 对话，通过 Team Cross 工具查看和参与这次协作。个人对话使用本机的模型设置。`}
      </p>
      {mode === "direct" && (!mine || !collaboration.online) && (
        <div className="notice">
          {!collaboration.online
            ? "等待发起者恢复协作运行时后，即可打开客户端。"
            : "当前由另一位参与者输入。交接完成后，即可直接操作；现在仍可使用个人辅助客户端读取上下文。"}
        </div>
      )}
      {mode === "direct" && collaboration.connected && (
        <div className="notice">
          {collaboration.clientState === "session_ready"
            ? "共享会话已打开。"
            : `客户端已连接，等待在 ${claude ? "Claude" : "Codex"} 中打开共享会话。`}
          切换前请关闭当前直接客户端。
        </div>
      )}
      {mode === "assist" && (
        <MCPConnection
          info={info.data}
          provider={assistProvider}
          onProviderChange={(provider) => {
            setAssistProvider(provider);
            localStorage.setItem("teamcross.assistProvider", provider);
            setPlan(undefined);
            setError("");
          }}
          reload={info.reload}
          disabled={!!busy}
          onBusyChange={setConnecting}
        />
      )}
      {collaboration.provider === "claude" && (
        <p className="notice">
          Claude 协作当前通过原生 TUI
          审批、补充和中断。辅助工具可读取上下文、添加批注，并在空闲时发送文本。
        </p>
      )}
      <div className="client-grid">
        {directClients.map((client) => (
          <div className="client-card" key={client}>
            <Icon name={client === "tui" ? "terminal" : "desktop"} size={26} />
            <h3>
              {client === "tui"
                ? claude
                  ? "Claude Code TUI"
                  : "Codex TUI"
                : "Codex Desktop"}
            </h3>
            {preferred === client && (
              <small className="muted">偏好的客户端</small>
            )}
            <p>
              {client === "tui"
                ? "在本机终端中打开"
                : mode === "direct"
                  ? "打开专用于此协作的独立实例"
                  : "打开你平时使用的 Desktop"}
            </p>
            <button
              className="button primary"
              disabled={
                !!busy ||
                connecting ||
                !!clientError ||
                (client === "desktop" && !info.data?.desktopApp) ||
                (mode === "direct" && (!mine || !collaboration.online)) ||
                (mode === "assist" && (!mcp.configured || !!mcp.configError))
              }
              onClick={() => void open(client, true)}
            >
              {busy === client ? "正在打开…" : "打开"}
              <Icon name="arrow" size={16} />
            </button>
            <button
              className="text-link small-text"
              disabled={
                !!busy ||
                connecting ||
                !!clientError ||
                (client === "desktop" && !info.data?.desktopApp) ||
                (mode === "direct" && (!mine || !collaboration.online))
              }
              onClick={() => void open(client, false)}
            >
              查看启动命令
            </button>
          </div>
        ))}
      </div>
      <ErrorBox message={error || clientError || info.error} />
      {(clientError || (!claude && !info.data?.desktopApp)) && (
        <a className="text-link" href="#/settings">
          检查客户端设置
        </a>
      )}
      {plan && (
        <div className="launch-result" role="status">
          {plan.launched && (
            <p>
              <Icon name="check" size={17} />
              {mode === "direct"
                ? "已发送打开请求，连接状态会显示在协作详情中。"
                : `已发送打开请求，请在个人 ${name} 中选择这次协作。`}
            </p>
          )}
          {plan.note && <p className="muted">{plan.note}</p>}
          <details open={!plan.launched}>
            <summary>启动命令</summary>
            <pre>{plan.command}</pre>
            <Copy text={plan.command} />
          </details>
        </div>
      )}
      {mode === "assist" && (
        <div className="assistant-prompt">
          <span className="eyebrow">可以这样告诉自己的 {name}</span>
          <p>
            使用 Team Cross 查看「{collaboration.title}」的上下文和当前状态。
          </p>
          <Copy
            text={`使用 Team Cross 工具查看协作 ${collaboration.id}（${collaboration.title}）的上下文和当前状态。`}
            label={`复制给 ${name}`}
          />
        </div>
      )}
    </div>
  );
}
