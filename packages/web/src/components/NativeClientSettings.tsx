import { useEffect, useRef, useState } from "react";
import { api, errorText } from "../api";
import { serviceText, t } from "../i18n";
import { type Info } from "../types";
import { ErrorBox, Loading } from "./ui";

export function NativeClientSettings({
  info,
  loading,
  reload,
  onChecked,
  disabled,
  onBusyChange,
}: {
  info?: Info;
  loading: boolean;
  reload: () => void;
  onChecked: (info: Info) => void;
  disabled: boolean;
  onBusyChange: (busy: boolean) => void;
}) {
  const initialized = useRef(false);
  const [binary, setBinary] = useState("");
  const [desktop, setDesktop] = useState("");
  const [claudeBinary, setClaudeBinary] = useState("");
  const [manual, setManual] = useState(false);
  const [busy, setBusy] = useState(false);
  const [checking, setChecking] = useState(false);
  const [saved, setSaved] = useState(false);
  const [error, setError] = useState("");
  useEffect(() => {
    if (info && !initialized.current) {
      const settings = info.settings;
      setBinary(settings?.binary || "");
      setManual(!!settings?.binary);
      setDesktop(settings?.desktopApp || "");
      setClaudeBinary(settings?.claudeBinary || "");
      initialized.current = true;
    }
  }, [info]);
  const installation = info?.codexInstallation;
  const source = installation?.source;
  const sourceLabel =
    source === "custom"
      ? t("手动指定")
      : source === "environment"
        ? t("环境变量")
        : source === "app"
          ? t("应用内 CLI")
          : source === "shell"
            ? t("登录 Shell")
            : source === "common"
              ? t("常见安装位置")
              : source === "path"
                ? "PATH"
                : t("尚未检测到");
  const dirty =
    (manual ? binary : "") !== (info?.settings?.binary || "") ||
    desktop !== (info?.settings?.desktopApp || "") ||
    claudeBinary !== (info?.settings?.claudeBinary || "");
  async function save() {
    if (busy || checking || disabled || loading) return;
    if (!info?.settings) return;
    if (manual && !binary.trim()) {
      setError(t("请输入 Codex CLI 路径，或选择自动发现"));
      return;
    }
    setBusy(true);
    onBusyChange(true);
    setError("");
    setSaved(false);
    try {
      await api("settings", {
        binary: manual ? binary.trim() : "",
        desktopApp: desktop.trim(),
        claudeBinary: claudeBinary.trim(),
      });
      initialized.current = false;
      setSaved(true);
      reload();
    } catch (e) {
      setError(errorText(e));
    } finally {
      setBusy(false);
      onBusyChange(false);
    }
  }
  async function check() {
    if (busy || checking || disabled || loading) return;
    setChecking(true);
    onBusyChange(true);
    setError("");
    try {
      onChecked(await api<Info>("info?refreshClients=1"));
    } catch (e) {
      setError(errorText(e));
    } finally {
      setChecking(false);
      onBusyChange(false);
    }
  }
  function selectAutomatic() {
    setManual(false);
    setBinary("");
    setError("");
    setSaved(false);
  }
  return (
    <section className="panel settings-section">
      <div className="panel-heading">
        <div>
          <h2>{t("原生客户端")}</h2>
          <p>{t("用于读取本机会话和打开客户端。")}</p>
        </div>
        <button
          type="button"
          className="button small"
          disabled={busy || checking || disabled || loading}
          onClick={() => void check()}
        >
          {loading || checking ? t("正在检测…") : t("重新检测")}
        </button>
      </div>
      {!info && loading ? (
        <Loading />
      ) : (
        <form
          onSubmit={(e) => {
            e.preventDefault();
            void save();
          }}
        >
          <div className="client-detection">
            <strong>Codex CLI</strong>
            <span
              className={`badge ${info?.codexError ? "warning" : info?.codexVersion ? "green" : ""}`}
            >
              {info?.codexError
                ? t("需要配置")
                : info?.codexVersion
                  ? t("CLI 可运行")
                  : t("尚未检测到")}
            </span>
            {info?.codexVersion && (
              <span className="muted">{info.codexVersion}</span>
            )}
          </div>
          <ErrorBox
            message={
              error ||
              (info && !info.settings
                ? t("客户端设置尚未加载，请重新打开 Team Cross 后再检测。")
                : info?.codexError)
            }
          />
          {info?.codexError && info.codexRecovery && (
            <p>{serviceText(info.codexRecovery)}</p>
          )}
          {installation?.recoveredFrom && (
            <div className="notice" role="status">
              {t(
                "应用内 CLI 的位置已变化，已从同一应用重新发现。恢复自动发现并保存后，将不再固定旧路径。",
              )}
            </div>
          )}
          <div
            className="client-source-options"
            role="group"
            aria-label={t("Codex CLI 来源")}
          >
            <button
              type="button"
              className={`button small ${!manual ? "primary" : ""}`}
              aria-pressed={!manual}
              disabled={busy || disabled}
              onClick={selectAutomatic}
            >
              {t("自动发现")}
            </button>
            <button
              type="button"
              className={`button small ${manual ? "primary" : ""}`}
              aria-pressed={manual}
              disabled={busy || disabled}
              onClick={() => {
                setManual(true);
                setSaved(false);
              }}
            >
              {t("手动指定")}
            </button>
            {manual && (info?.codexError || installation?.recoveredFrom) && (
              <button
                type="button"
                className="button small"
                disabled={busy || disabled}
                onClick={selectAutomatic}
              >
                {t("恢复自动发现")}
              </button>
            )}
          </div>
          <p>
            {t("自动发现会使用已选择应用的内置 CLI、PATH 或本机常见安装位置。")}
          </p>
          {manual && (
            <label className="field">
              {t("Codex CLI 路径") + " "}
              <input
                value={binary}
                disabled={busy || disabled}
                onChange={(e) => {
                  setBinary(e.target.value);
                  setSaved(false);
                }}
                placeholder={t("可执行文件的完整路径或命令名")}
              />
            </label>
          )}
          <details className="client-installation">
            <summary>{t("检测详情")}</summary>
            <dl>
              <dt>{t("发现来源")}</dt>
              <dd>{sourceLabel}</dd>
              <dt>{t("当前 CLI")}</dt>
              <dd>{info?.binary || t("尚未检测到")}</dd>
              <dt>{t("Codex Desktop 应用")}</dt>
              <dd>
                {installation?.desktopApp ||
                  info?.desktopApp ||
                  t("尚未检测到")}
              </dd>
            </dl>
          </details>
          <label className="field">
            {t("Codex Desktop 应用") + " "}
            <input
              value={desktop}
              disabled={busy || disabled}
              onChange={(e) => {
                setDesktop(e.target.value);
                setSaved(false);
              }}
              placeholder={info?.desktopApp || t("自动检测")}
            />
            <small>
              {t("留空自动发现；直接操作会使用独立的数据目录启动专用实例。")}
            </small>
          </label>
          <label className="field">
            {t("Claude Code CLI 路径") + " "}
            <input
              value={claudeBinary}
              disabled={busy || disabled}
              onChange={(e) => {
                setClaudeBinary(e.target.value);
                setSaved(false);
              }}
              placeholder={info?.claudeBinary || t("自动检测")}
            />
            <small>
              {info?.claudeVersion || t("实验性接入要求 2.1.268 或更高版本")}
            </small>
          </label>
          {info?.claudeError && (
            <p className="small-text muted">
              {t("Claude Code：")}
              {serviceText(info.claudeError, "请检查原生客户端配置。")}
            </p>
          )}
          <div className="form-footer">
            <span role="status" className="muted">
              {saved
                ? t("设置已保存，对新启动的客户端生效。")
                : dirty
                  ? t("设置有改动，保存后生效。")
                  : ""}
            </span>
            <button
              className="button"
              disabled={
                busy || checking || loading || disabled || !info?.settings
              }
            >
              {busy ? t("正在保存…") : t("保存设置")}
            </button>
          </div>
        </form>
      )}
    </section>
  );
}
