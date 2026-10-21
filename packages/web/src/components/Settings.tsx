import { t, tr, type LanguageInfo, type LanguageMode } from "../i18n";
import { useState } from "react";
import { api, errorText, useResource } from "../api";
import { type Info, type Provider } from "../types";
import { ErrorBox, Icon, PageHeading } from "./ui";
export type Theme = "system" | "light" | "dark";
import { MCPConnection } from "./MCPConnection";
import { NativeClientSettings } from "./NativeClientSettings";
import { AgentPairings } from "./AgentPairings";
import { ChatGPTPluginConnection } from "./ChatGPTPluginConnection";
export function Settings({
  theme,
  setTheme,
  uiLanguage,
  onLanguageChange,
}: {
  theme: Theme;
  setTheme: (theme: Theme) => void;
  uiLanguage: LanguageInfo;
  onLanguageChange: (language: LanguageInfo) => void;
}) {
  const info = useResource<Info>("info");
  const [provider, setProvider] = useState<Provider>("codex");
  const [connecting, setConnecting] = useState(false);
  const [nativeBusy, setNativeBusy] = useState(false);
  const [error, setError] = useState("");
  const [languageBusy, setLanguageBusy] = useState(false);
  async function chooseLanguage(mode: LanguageMode) {
    if (languageBusy || mode === uiLanguage.mode) return;
    setLanguageBusy(true);
    setError("");
    try {
      const updated = await api<LanguageInfo>("ui-language", { mode });
      onLanguageChange(updated);
    } catch (e) {
      setError(errorText(e));
    } finally {
      setLanguageBusy(false);
    }
  }
  return (
    <>
      <PageHeading
        title={t("设置与连接")}
        subtitle={t("配置原生客户端与协作工具。")}
      />
      <ErrorBox message={error || info.error} retry={info.reload} />
      <div className="settings-stack">
        <ChatGPTPluginConnection />
        <AgentPairings />
        <section className="panel settings-section">
          <div>
            <h2>{t("界面语言")}</h2>
            <p>
              {t(
                "默认按这台 Mac 的首选系统语言显示：中文使用简体中文，其他语言使用英文。手动选择会同步菜单栏和 WebGUI。",
              )}
            </p>
          </div>
          <div
            className="theme-options"
            role="group"
            aria-label={t("界面语言")}
          >
            {(["auto", "zh-CN", "en"] as LanguageMode[]).map((mode) => (
              <button
                key={mode}
                className={`theme-card ${uiLanguage.mode === mode ? "selected" : ""}`}
                aria-pressed={uiLanguage.mode === mode}
                disabled={languageBusy}
                onClick={() => void chooseLanguage(mode)}
              >
                <span>
                  {mode === "auto"
                    ? t("跟随系统")
                    : mode === "zh-CN"
                      ? t("简体中文")
                      : "English"}
                </span>
                {uiLanguage.mode === mode && <Icon name="check" size={15} />}
              </button>
            ))}
          </div>
        </section>
        <section className="panel settings-section">
          <div>
            <h2>{t("命令行工具")}</h2>
            <p>{t("在终端运行 teamcross，也可以打开协作空间。")}</p>
          </div>
          {info.data?.cli && (
            <dl>
              <dt>{t("命令位置")}</dt>
              <dd>{info.data.cli.command || t("尚未在命令路径中找到")}</dd>
              <dt>{t("安装来源")}</dt>
              <dd>
                {info.data.cli.source === "formula"
                  ? "Homebrew Formula"
                  : info.data.cli.source === "app"
                    ? "Team Cross App"
                    : info.data.cli.source === "standalone"
                      ? t("独立命令")
                      : t("尚未检测到")}
              </dd>
              <dt>{t("已安装版本")}</dt>
              <dd>
                {info.data.installedVersion || t("尚未确认")}
                {info.data.installedCommit && info.data.installedCommit !== "unknown" && (
                  <> · <code>{info.data.installedCommit.slice(0, 12)}</code></>
                )}
              </dd>
              <dt>{t("运行中的版本")}</dt>
              <dd>
                {info.data.version}
                {info.data.commit && info.data.commit !== "unknown" && (
                  <> · <code>{info.data.commit.slice(0, 12)}</code></>
                )}
              </dd>
            </dl>
          )}
          {(info.data?.updatePending ||
            (info.data?.installedVersion && info.data.installedVersion !== info.data.version)) && (
            <p role="status">
              {t(
                "已安装新构建，当前服务仍使用原构建。活动结束后，下次打开 Team Cross 或使用插件时自动应用更新。",
              ) + " "}
            </p>
          )}
          <p>
            {t(
              "通过 DMG 安装后，可从菜单栏的“命令行工具…”安装或移除命令入口。Homebrew 安装由对应渠道管理。",
            ) + " "}
          </p>
          {info.data?.cli?.conflict && (
            <p>{tr`检测到其他安装的命令：${info.data.cli.conflict}。切换前请通过原安装渠道处理。`}</p>
          )}
        </section>
        <section className="panel settings-section">
          <div>
            <h2>{t("外观")}</h2>
            <p>{t("选择适合你的工作环境。")}</p>
          </div>
          <div className="theme-options">
            {(["system", "light", "dark"] as Theme[]).map((value) => (
              <button
                key={value}
                className={`theme-card ${theme === value ? "selected" : ""}`}
                aria-pressed={theme === value}
                onClick={() => setTheme(value)}
              >
                <span className={`theme-preview ${value}`}>
                  <i />
                  <i />
                  <i />
                </span>
                <span>
                  {value === "system"
                    ? t("跟随系统")
                    : value === "light"
                      ? t("浅色")
                      : t("深色")}
                  {theme === value && <Icon name="check" size={15} />}
                </span>
              </button>
            ))}
          </div>
        </section>
        <NativeClientSettings
          info={info.data}
          loading={info.loading}
          reload={info.reload}
          onChecked={info.setData}
          disabled={connecting}
          onBusyChange={setNativeBusy}
        />
        <section className="panel settings-section">
          <div className="mcp-heading">
            <span className="entry-icon">
              <Icon name="people" size={23} />
            </span>
            <div>
              <h2>{t("用自己的客户端辅助协作")}</h2>
              <p>{t("为个人 Codex 或 Claude Code 安装 Team Cross 工具。")}</p>
            </div>
          </div>
          <div className="notice">
            {t(
              "让个人客户端列出协作并选择目标，即可读取上下文、留下批注，并在持有输入权时使用目标协作支持的操作。",
            ) + " "}
          </div>
          <MCPConnection
            info={info.data}
            provider={provider}
            onProviderChange={setProvider}
            reload={info.reload}
            disabled={nativeBusy}
            onBusyChange={setConnecting}
          />
        </section>
      </div>
    </>
  );
}
