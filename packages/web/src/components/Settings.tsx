import { useEffect, useState } from "react";
import { api, errorText, useResource } from "../api";
import { type Info } from "../types";
import { Copy, ErrorBox, Icon, Loading, PageHeading } from "./ui";
export type Theme = "system" | "light" | "dark";
export function Settings({
  theme,
  setTheme,
}: {
  theme: Theme;
  setTheme: (theme: Theme) => void;
}) {
  const info = useResource<Info>("info");
  const [binary, setBinary] = useState("");
  const [desktop, setDesktop] = useState("");
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const [saved, setSaved] = useState(false);
  useEffect(() => {
    if (info.data) {
      setBinary(info.data.binary || "");
      setDesktop(info.data.desktopApp || "");
    }
  }, [info.data]);
  async function save() {
    setBusy("save");
    setError("");
    setSaved(false);
    try {
      await api("settings", { binary, desktopApp: desktop });
      setSaved(true);
      info.reload();
    } catch (e) {
      setError(errorText(e));
    } finally {
      setBusy("");
    }
  }
  async function setup() {
    setBusy("mcp");
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
    <>
      <PageHeading
        title="设置与连接"
        subtitle="让 Team Cross 与你日常使用的 Codex 配合。"
      />
      <ErrorBox message={error || info.error} retry={info.reload} />
      <div className="settings-stack">
        <section className="panel settings-section">
          <div>
            <h2>外观</h2>
            <p>选择适合你的工作环境。</p>
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
                    ? "跟随系统"
                    : value === "light"
                      ? "浅色"
                      : "深色"}
                  {theme === value && <Icon name="check" size={15} />}
                </span>
              </button>
            ))}
          </div>
        </section>
        <section className="panel settings-section">
          <div className="panel-heading">
            <div>
              <h2>Codex 客户端</h2>
              <p>用于读取本机会话和打开客户端。</p>
            </div>
            <span
              className={`badge ${info.data?.codexError ? "warning" : "green"}`}
            >
              <span className="dot" />
              {info.data?.codexError ? "需要配置" : "自动检测"}
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
                Codex CLI 路径
                <input
                  value={binary}
                  onChange={(e) => setBinary(e.target.value)}
                  placeholder="自动检测"
                />
                <small>
                  {info.data?.codexVersion || "支持指定应用内附带的 Codex CLI"}
                </small>
              </label>
              <label className="field">
                Codex Desktop 应用
                <input
                  value={desktop}
                  onChange={(e) => setDesktop(e.target.value)}
                  placeholder="/Applications/ChatGPT.app"
                />
                <small>直接操作会使用独立的数据目录启动专用实例。</small>
              </label>
              <div className="form-footer">
                <span role="status" className="muted">
                  {saved ? "设置已保存，对新启动的客户端生效。" : ""}
                </span>
                <button className="button" disabled={!!busy}>
                  {busy === "save" ? "正在保存…" : "保存设置"}
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
              <h2>用自己的 Codex 辅助协作</h2>
              <p>通过本地 MCP 工具接入，TUI 与 Desktop 共用一次配置。</p>
            </div>
          </div>
          <div className="notice">
            接入后，让 Codex
            列出协作并选择目标，即可读取历史、查看文件与改动、参与输入和回应请求。
          </div>
          <button
            className="button primary"
            disabled={!!busy || !info.data || info.data.mcpConfigured}
            onClick={() => void setup()}
          >
            {busy === "mcp"
              ? "正在接入…"
              : info.data?.mcpConfigured
                ? "已接入本机 Codex"
                : "接入本机 Codex"}
            <Icon
              name={info.data?.mcpConfigured ? "check" : "plus"}
              size={17}
            />
          </button>
          <p className="small-text muted">
            配置会写入你当前的 Codex 配置。已打开的客户端需重新加载工具。
          </p>
          <details className="technical">
            <summary>手动配置与诊断</summary>
            {info.data && (
              <>
                <pre>{info.data.mcpCommand}</pre>
                <Copy text={info.data.mcpCommand} label="复制配置命令" />
                <dl>
                  <dt>执行主机</dt>
                  <dd>{info.data.host}</dd>
                  <dt>数据目录</dt>
                  <dd>{info.data.dataDir}</dd>
                  <dt>版本</dt>
                  <dd>{info.data.version}</dd>
                </dl>
              </>
            )}
          </details>
        </section>
      </div>
    </>
  );
}
