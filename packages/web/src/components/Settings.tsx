import {
  serviceText,
  t,
  tr,
  type LanguageInfo,
  type LanguageMode,
} from "../i18n";
import { useEffect, useState } from "react";
import { api, errorText, useResource } from "../api";
import { type Info, type Provider } from "../types";
import { ErrorBox, Icon, Loading, PageHeading } from "./ui";
export type Theme = "system" | "light" | "dark";
import { MCPConnection } from "./MCPConnection";
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
  const [binary, setBinary] = useState("");
  const [claudeBinary, setClaudeBinary] = useState("");
  const [desktop, setDesktop] = useState("");
  const [provider, setProvider] = useState<Provider>("codex");
  const [connecting, setConnecting] = useState(false);
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const [saved, setSaved] = useState(false);
  const [languageBusy, setLanguageBusy] = useState(false);
  useEffect(() => {
    if (info.data) {
      setBinary(info.data.binary || "");
      setClaudeBinary(info.data.claudeBinary || "");
      setDesktop(info.data.desktopApp || "");
    }
  }, [info.data]);
  async function save() {
    setBusy("save");
    setError("");
    setSaved(false);
    try {
      await api("settings", { binary, desktopApp: desktop, claudeBinary });
      setSaved(true);
      info.reload();
    } catch (e) {
      setError(errorText(e));
    } finally {
      setBusy("");
    }
  }
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
              <dd>{info.data.installedVersion || t("尚未确认")}</dd>
              <dt>{t("运行中的版本")}</dt>
              <dd>{info.data.version}</dd>
            </dl>
          )}
          {info.data?.installedVersion &&
            info.data.installedVersion !== info.data.version && (
              <p role="status">
                {t(
                  "已安装新版本。结束活动协作并退出 Team Cross 后，重新打开以应用更新。",
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
        <section className="panel settings-section">
          <div className="panel-heading">
            <div>
              <h2>{t("原生客户端")}</h2>
              <p>{t("用于读取本机会话和打开客户端。")}</p>
            </div>
            <span
              className={`badge ${info.data?.codexError ? "warning" : "green"}`}
            >
              <span className="dot" />
              {info.data?.codexError ? t("需要配置") : t("自动检测")}
            </span>
          </div>
          {info.loading ? (
            <Loading />
          ) : (
            <form
              onSubmit={(e) => {
                e.preventDefault();
                void save();
              }}
            >
              <ErrorBox message={info.data?.codexError} />
              <label className="field">
                {t("Codex CLI 路径") + " "}
                <input
                  value={binary}
                  onChange={(e) => setBinary(e.target.value)}
                  placeholder={t("自动检测")}
                />
                <small>
                  {info.data?.codexVersion ||
                    t("支持指定应用内附带的 Codex CLI")}
                </small>
              </label>
              <label className="field">
                {t("Codex Desktop 应用") + " "}
                <input
                  value={desktop}
                  onChange={(e) => setDesktop(e.target.value)}
                  placeholder="/Applications/ChatGPT.app"
                />
                <small>{t("直接操作会使用独立的数据目录启动专用实例。")}</small>
              </label>
              <label className="field">
                {t("Claude Code CLI 路径") + " "}
                <input
                  value={claudeBinary}
                  onChange={(e) => setClaudeBinary(e.target.value)}
                  placeholder={t("自动检测")}
                />
                <small>
                  {info.data?.claudeVersion ||
                    t("实验性接入要求 2.1.268 或更高版本")}
                </small>
              </label>
              {info.data?.claudeError && (
                <p className="small-text muted">
                  {t("Claude Code：")}
                  {serviceText(info.data.claudeError, "请检查原生客户端配置。")}
                </p>
              )}
              <div className="form-footer">
                <span role="status" className="muted">
                  {saved ? t("设置已保存，对新启动的客户端生效。") : ""}
                </span>
                <button className="button" disabled={!!busy || connecting}>
                  {busy === "save" ? t("正在保存…") : t("保存设置")}
                </button>
              </div>
            </form>
          )}
        </section>
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
            disabled={!!busy}
            onBusyChange={setConnecting}
          />
        </section>
      </div>
    </>
  );
}
