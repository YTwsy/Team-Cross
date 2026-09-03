import { useEffect, useRef, useState } from "react";
import { api } from "../api";
import type { NativeOpenResult, SessionSnapshot } from "../types";

export function NativeSessionOpen({
  threadId,
  snapshot,
  canManage,
}: {
  threadId: string;
  snapshot: SessionSnapshot;
  canManage: boolean;
}) {
  const [confirmed, setConfirmed] = useState(false);
  const [busy, setBusy] = useState(false);
  const [result, setResult] = useState<NativeOpenResult>();
  const [error, setError] = useState("");
  const active = useRef(true);
  useEffect(() => {
    active.current = true;
    return () => {
      active.current = false;
    };
  }, []);
  const allowed =
    canManage &&
    snapshot.source.provider === "codex" &&
    snapshot.capabilities?.open === true;
  const reason = !canManage
    ? "只有主机 Owner 可以请求打开原生会话。"
    : snapshot.source.provider !== "codex"
      ? "当前只支持能力已验证的 Codex Desktop 目标。"
      : snapshot.capabilities?.reason || "此来源的准确打开能力尚未验证。";
  async function open() {
    if (!allowed || !confirmed || busy) return;
    setBusy(true);
    setError("");
    setResult(undefined);
    try {
      const value = await api.openNativeSession(threadId, snapshot.id);
      if (active.current) {
        if (
          value.status !== "requested" ||
          value.provider !== "codex" ||
          value.target !== "codex-desktop" ||
          value.sessionId !== snapshot.source.sessionId
        )
          throw new Error(
            "打开请求返回的原生身份与所选 Session 不一致；请检查主机状态。",
          );
        setResult(value);
        setConfirmed(false);
      }
    } catch (error) {
      if (active.current)
        setError(error instanceof Error ? error.message : "打开请求失败");
    } finally {
      if (active.current) setBusy(false);
    }
  }
  return (
    <section className="native-session-open surface" aria-label="打开原生会话">
      <strong>返回熟悉的原生 UI</strong>
      <p>
        {snapshot.source.provider === "codex"
          ? "目标：Codex Desktop"
          : "此 Provider 暂无已验证的打开目标"}{" "}
        · 原生 ID <code>{snapshot.source.sessionId}</code>
        。Team Cross 仅请求打开，不发送 Resume
        或工作指令；这不证明原生界面已正确定位。
      </p>
      {!allowed ? <p className="side-muted">{reason}</p> : null}
      {canManage ? (
        <label className="inline-checkbox">
          <input
            type="checkbox"
            disabled={!allowed || busy}
            checked={confirmed}
            onChange={(event) => setConfirmed(event.target.checked)}
          />
          确认在 Codex Desktop 打开上述原生 Session
        </label>
      ) : null}
      <button
        type="button"
        className="button secondary compact"
        disabled={!allowed || !confirmed || busy}
        onClick={() => void open()}
      >
        {busy ? "请求中…" : "请求在 Codex Desktop 打开"}
      </button>
      {result ? (
        <p role="status">
          系统已接收打开请求；尚未验证 Desktop 已定位正确 Session。
          {result.message}
        </p>
      ) : null}
      {error ? (
        <p className="form-error" role="alert">
          {error}
        </p>
      ) : null}
    </section>
  );
}
