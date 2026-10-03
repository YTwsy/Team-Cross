import { useEffect, useState } from "react";
import { api, errorText, useResource } from "../api";
import { t } from "../i18n";
import { ErrorBox, Loading } from "./ui";

export type PluginConnection = {
  state: string;
  available: boolean;
  autoUpdate: boolean;
  installed: boolean;
  pluginEnabled: boolean;
  reloadRequired: boolean;
  version?: string;
  root?: string;
  dataDir?: string;
  differentData: boolean;
  error?: string;
};

function statusLabel(state: string) {
  switch (state) {
    case "installed":
      return t("插件已安装");
    case "reload_required":
      return t("等待打开新版插件");
    case "update_available":
      return t("插件有可用更新");
    case "unavailable":
      return t("未找到 ChatGPT 桌面应用");
    case "disconnected":
      return t("插件已移除或停用");
    case "error":
      return t("插件连接需要处理");
    default:
      return t("插件尚未安装");
  }
}

export function ChatGPTPluginConnection({
  compact = false,
}: {
  compact?: boolean;
}) {
  const resource = useResource<PluginConnection>("plugin/connection");
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const data = resource.data;
  // Poll only while a new UI is expected, not while a native write is running.
  useEffect(() => {
    if (compact || !data?.reloadRequired || busy) return;
    const timer = window.setTimeout(resource.reload, 15000);
    return () => window.clearTimeout(timer);
  }, [compact, data, busy, resource.reload]);
  async function act(action: "connect" | "disconnect") {
    if (busy) return;
    setBusy(action);
    setError("");
    try {
      await api("plugin/connection", { action });
    } catch (e) {
      setError(errorText(e));
    } finally {
      resource.reload();
      setBusy("");
    }
  }
  if (compact) {
    if (!data?.available || data.autoUpdate || data.error) return null;
    return (
      <div className="notice plugin-onboarding">
        <div>
          <strong>{t("在 ChatGPT 中使用 Team Cross")}</strong>
          <p>
            {t(
              "打开协作界面，在输入框引用材料和批注。首次连接后，插件随 Team Cross 更新。",
            )}
          </p>
        </div>
        <a className="button secondary" href="#/settings">
          {t("设置 ChatGPT 插件")}
        </a>
      </div>
    );
  }
  return (
    <section
      className="panel settings-section plugin-connection"
      aria-label={t("ChatGPT 插件")}
    >
      <div className="panel-heading">
        <div>
          <h2>{t("ChatGPT 插件")}</h2>
          <p>
            {t(
              "在本机 ChatGPT 的 Work/Codex 中打开 Team Cross，并引用材料和批注。",
            )}
          </p>
        </div>
      </div>
      <ErrorBox message={error || resource.error || data?.error} />
      {resource.loading && !data ? (
        <Loading />
      ) : (
        data && (
          <>
            <p role="status">
              <strong>{statusLabel(data.state)}</strong>
            </p>
            <p>
              {data.autoUpdate
                ? t(
                    "自动同步已启用。每次打开 Team Cross App 时，检查并更新此插件。",
                  )
                : t("点击连接后启用自动同步。安装 App 本身不会添加插件。")}
            </p>
          {data.reloadRequired && data.state !== "update_available" && (
              <div className="notice">
                {t(
                  "插件文件已更新。完全退出并重新打开 ChatGPT，再打开 Team Cross 插件。新版界面打开后，此提示会自动消失。",
                )}
              </div>
            )}
            {data.state === "unavailable" && (
              <p>{t("先安装支持本机插件的 ChatGPT 桌面应用，再检查状态。")}</p>
            )}
            {data.state === "disconnected" && (
              <p>
                {t(
                  "自动同步不会重新安装已移除或停用的插件。点击重新连接后恢复。",
                )}
              </p>
            )}
            {data.differentData && (
              <div className="notice">
                {t(
                  "这个插件连接了另一个本机数据目录。更新会保留该目录，不会切换到当前空间列表。",
                )}
              </div>
            )}
            {data.dataDir && (
              <details>
                <summary>{t("安装信息")}</summary>
                <dl>
                  <dt>{t("已安装版本")}</dt>
                  <dd>{data.version || t("尚未确认")}</dd>
                  <dt>{t("插件来源")}</dt>
                  <dd>{data.root}</dd>
                  <dt>{t("绑定的数据目录")}</dt>
                  <dd>{data.dataDir}</dd>
                </dl>
              </details>
            )}
          </>
        )
      )}
      <div className="form-actions">
        <button
          className="button secondary"
          disabled={!!busy || resource.loading}
          onClick={resource.reload}
        >
          {t("检查状态")}
        </button>
        {data?.available && (
          <button
            className="button"
            disabled={!!busy}
            onClick={() => void act("connect")}
          >
            {busy === "connect"
              ? t("正在连接…")
              : data.state === "error"
                ? t("重试连接")
                : data.state === "disconnected"
                  ? t("重新连接")
                  : !data.installed
                    ? t("安装到 ChatGPT")
                    : !data.autoUpdate
                      ? t("连接并启用自动同步")
                      : t("同步插件")}
          </button>
        )}
        {data?.autoUpdate && (
          <button
            className="button ghost"
            disabled={!!busy}
            onClick={() => void act("disconnect")}
          >
            {busy === "disconnect" ? t("正在断开…") : t("断开插件连接")}
          </button>
        )}
      </div>
      <p className="small-text muted">
        {t(
          "断开连接会移除 ChatGPT 中的插件并关闭自动同步。材料、批注和协作数据会保留。",
        )}
      </p>
    </section>
  );
}
