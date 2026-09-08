import { useState } from "react";
import { api, errorText, useResource } from "../api";
import { type ClientPlan, type Collaboration, type Info } from "../types";
import { Copy, ErrorBox, Icon, Loading } from "./ui";
export function Clients({
  collaboration,
  initial = "direct",
}: {
  collaboration: Collaboration;
  initial?: "direct" | "assist";
}) {
  const [mode, setMode] = useState(initial);
  const info = useResource<Info>("info");
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const [plan, setPlan] = useState<ClientPlan>();
  const mine = collaboration.writer === collaboration.role;
  async function open(client: string, launch: boolean) {
    setBusy(client);
    setError("");
    setPlan(undefined);
    try {
      setPlan(
        await api<ClientPlan>(
          `collaborations/${collaboration.id}/${mode === "direct" ? "open" : "assist"}`,
          { client, launch },
        ),
      );
    } catch (e) {
      setError(errorText(e));
    } finally {
      setBusy("");
    }
  }
  async function setup() {
    setBusy("setup");
    setError("");
    try {
      await api("mcp/setup", {});
      info.reload();
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
          onClick={() => {
            setMode("assist");
            setPlan(undefined);
            setError("");
          }}
        >
          用自己的 Codex 辅助
        </button>
      </div>
      <p className="client-description">
        {mode === "direct"
          ? `连接共享会话，代码与模型调用在 ${collaboration.host} 上执行。`
          : "继续与你自己的 Codex 对话，通过 Team Cross 工具查看和参与这次协作。"}
      </p>
      {mode === "direct" && (!mine || !collaboration.online) && (
        <div className="notice">
          {!collaboration.online
            ? "等待发起者恢复协作运行时后，即可打开客户端。"
            : "当前由另一位参与者输入。交接完成后，即可直接操作；现在仍可使用自己的 Codex 读取上下文。"}
        </div>
      )}
      {mode === "direct" && collaboration.connected && (
        <div className="notice">
          已有一个直接操作客户端连接。切换前请关闭该客户端；随后打开新客户端会继续同一会话。
        </div>
      )}
      {mode === "assist" && (
        <div className="mcp-guide">
          <div className="guide-number">1</div>
          <div>
            <h3>
              {info.data?.mcpConfigured
                ? "本机 MCP 已接入"
                : "一次接入，之后直接选择协作"}
            </h3>
            <p>
              接入后，普通 TUI 和 Desktop 都能使用 Team Cross
              工具。已打开的会话可能需要重新打开以加载工具。
            </p>
            {info.loading ? (
              <Loading />
            ) : info.data?.mcpConfigured ? (
              <span className="badge green">
                <Icon name="check" size={14} />
                已配置
              </span>
            ) : (
              <button
                className="button small"
                disabled={!!busy || !!info.error}
                onClick={() => void setup()}
              >
                {busy === "setup" ? "正在配置…" : "接入本机 Codex"}
              </button>
            )}
            <ErrorBox message={info.error} retry={info.reload} />
          </div>
        </div>
      )}
      <div className="client-grid">
        {(["tui", "desktop"] as const).map((client) => (
          <div className="client-card" key={client}>
            <Icon name={client === "tui" ? "terminal" : "desktop"} size={26} />
            <h3>{client === "tui" ? "Codex TUI" : "Codex Desktop"}</h3>
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
                (mode === "direct" && (!mine || !collaboration.online)) ||
                (mode === "assist" && !info.data?.mcpConfigured)
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
                (mode === "direct" && (!mine || !collaboration.online))
              }
              onClick={() => void open(client, false)}
            >
              查看启动命令
            </button>
          </div>
        ))}
      </div>
      <ErrorBox message={error} />
      {plan && (
        <div className="launch-result" role="status">
          {plan.launched && (
            <p>
              <Icon name="check" size={17} />
              已发送打开请求，连接状态会显示在协作详情中。
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
          <span className="eyebrow">可以这样告诉自己的 Codex</span>
          <p>
            使用 Team Cross 查看「{collaboration.title}」的上下文和当前状态。
          </p>
          <Copy
            text={`使用 Team Cross 工具查看协作 ${collaboration.id}（${collaboration.title}）的上下文和当前状态。`}
            label="复制给 Codex"
          />
        </div>
      )}
    </div>
  );
}
