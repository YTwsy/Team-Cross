import { t, tr } from "../i18n";
import { useState } from "react";
import { api, errorText } from "../api";
import type { Info, MCPClientStatus, Provider } from "../types";
import { Copy, ErrorBox, Icon } from "./ui";

export function mcpStatus(
  info: Info | undefined,
  provider: Provider,
): MCPClientStatus {
  return (
    info?.mcpClients?.[provider] ??
    (provider === "codex"
      ? {
          configured: !!info?.mcpConfigured,
          command: info?.mcpCommand || "",
          observedAt: info?.mcpObservedAt,
        }
      : { configured: false, command: "" })
  );
}

export function MCPConnection({
  info,
  provider,
  onProviderChange,
  reload,
  disabled,
  onBusyChange,
}: {
  info?: Info;
  provider: Provider;
  onProviderChange: (provider: Provider) => void;
  reload: () => void;
  disabled?: boolean;
  onBusyChange?: (busy: boolean) => void;
}) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const status = mcpStatus(info, provider);
  const name = provider === "claude" ? "Claude Code" : "Codex";
  const clientError =
    provider === "claude" ? info?.claudeError : info?.codexError;
  async function check(setup: boolean) {
    setBusy(true);
    onBusyChange?.(true);
    setError("");
    try {
      if (setup) await api("mcp/setup", { provider });
      const result = await api<{ ready: boolean }>("mcp/probe", {});
      if (!result.ready)
        setError(t("Team Cross 工具协议检查未通过，请重新检查。"));
    } catch (e) {
      setError(errorText(e));
    } finally {
      reload();
      setBusy(false);
      onBusyChange?.(false);
    }
  }
  return (
    <div className="mcp-connection">
      <div className="segmented" aria-label={t("个人辅助客户端")}>
        {(["codex", "claude"] as const).map((value) => (
          <button
            key={value}
            disabled={busy || disabled}
            aria-pressed={provider === value}
            onClick={() => {
              setError("");
              onProviderChange(value);
            }}
          >
            {value === "claude" ? "Claude Code" : "Codex"}
          </button>
        ))}
      </div>
      <p>
        {provider === "claude"
          ? t(
              "接入个人 Claude Code TUI。配置保存在本机个人范围，已打开的客户端请在 /mcp 中重新连接或重新打开。",
            )
          : t(
              "接入个人 Codex，TUI 与 Desktop 共用配置。已打开的客户端需重新加载工具。",
            )}
      </p>
      <button
        className="button small"
        disabled={
          busy ||
          disabled ||
          !info ||
          status.configured ||
          !!clientError ||
          !!status.configError
        }
        onClick={() => void check(true)}
      >
        <Icon name={status.configured ? "check" : "plus"} size={15} />
        {busy
          ? t("正在检查…")
          : status.configured
            ? tr`${name} MCP 配置已保存`
            : tr`接入本机 ${name}`}
      </button>
      <p className="small-text muted">
        {tr`Team Cross 工具协议：${info?.mcpProbed ? t("通过") : t("尚未检查")}。`}
        <br />
        {tr`${name} 客户端：${
          status.observedAt && !status.observedAt.startsWith("0001")
            ? t("本次运行已收到工具调用")
            : t("等待实际工具调用；请让客户端使用 Team Cross 列出协作")
        }。`}
      </p>
      <button
        className="text-link"
        disabled={busy || disabled}
        onClick={() => void check(false)}
      >
        {t("检查工具连接") + " "}
      </button>
      <ErrorBox message={error || status.configError} />
      <details className="technical">
        <summary>{t("手动配置与诊断")}</summary>
        {status.command && (
          <>
            <pre>{status.command}</pre>
            <Copy text={status.command} label={t("复制配置命令")} />
          </>
        )}
        <p className="small-text muted">
          {t(
            "配置检测对应 Team Cross 当前本地目录。其他项目中的同名配置、禁用设置或组织策略可能影响加载，以客户端实际调用为准。",
          ) + " "}
        </p>
      </details>
    </div>
  );
}
