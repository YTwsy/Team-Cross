import { useEffect, useState } from "react";
import { api, errorText } from "../api";
import { activityChanged, useActivityResource } from "../activity";
import { useRefreshGuard, useRefreshState } from "../refreshState";
import { t } from "../i18n";
import type { Shadow } from "../shadows";
import { ErrorBox } from "./ui";

type RouteView = {
  route: { shadowId?: string; revision: number; enabled: boolean };
  runtime?: { shadowId: string; name: string; state: string };
  localShadows: number;
};
type RouteChange = {
  shadowId: string;
  baseRevision: number;
  requestId: string;
};

export function DefaultShadow({
  spaceId,
  shadows,
  selfId,
  disabled,
}: {
  spaceId: string;
  shadows: Shadow[];
  selfId: string;
  disabled: boolean;
}) {
  const path = `collaborations/${spaceId}/attention/route`;
  const data = useActivityResource<RouteView>(path);
  const key = `DefaultShadow:v1:${spaceId}`;
  const [pending, setPending] = useRefreshState<RouteChange | null>(
    key + ":request",
    null,
  );
  useRefreshGuard(key, !!pending);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const own = shadows.filter((s) => s.owner.id === selfId);
  const route = data.data?.route;
  useEffect(() => {
    if (busy || !pending || !route || route.revision <= pending.baseRevision)
      return;
    setPending(null);
    setError(
      route.shadowId === pending.shadowId
        ? ""
        : t("接手设置已变化，请重新选择分身。"),
    );
  }, [busy, pending, route?.revision, route?.shadowId]);
  async function choose(shadowId: string) {
    if (busy || !route || !shadowId) return;
    const change = pending || {
      shadowId,
      baseRevision: route.revision,
      requestId: crypto.randomUUID(),
    };
    setPending(change);
    setBusy(true);
    setError("");
    try {
      await api<RouteView>(path, change);
      setPending(null);
      activityChanged();
    } catch (e) {
      setError(errorText(e));
    } finally {
      setBusy(false);
      data.reload();
    }
  }
  let status = t("正在读取接手设置");
  if (route) {
    if (!route.shadowId)
      status = own.length
        ? t("正在接通默认分身")
        : t("创建自己的分身后，会自动接手与你有关的动态。");
    else if (!route.enabled || data.data?.runtime?.state === "paused")
      status = t("自动处理已暂停，已接收的事项会保留。");
    else if (!data.data?.runtime || data.data.runtime.state === "offline")
      status = t("默认分身暂不可用，待处理动态会保留。");
    else if (data.data.runtime.state === "error")
      status = t("分身运行需要核对，已接收的事项会保留。");
    else status = t("@你、回复及关注更新会自动交给这个分身。");
  }
  return (
    <div className="default-shadow">
      <div>
        <label>
          <strong>{t("接手我的动态")}</strong>
          {own.length > 0 && (
            <select
              aria-label={t("默认分身")}
              value={pending?.shadowId || route?.shadowId || ""}
              disabled={disabled || busy || !!pending || !route}
              onChange={(e) => void choose(e.target.value)}
            >
              {!route?.shadowId && (
                <option value="">{t("正在接通默认分身")}</option>
              )}
              {own.map((s) => (
                <option key={s.id} value={s.id}>
                  {s.name}
                </option>
              ))}
            </select>
          )}
        </label>
        <p className="muted small-text">{status}</p>
        {own.length > 1 && (
          <p className="muted small-text">
            {t(
              "切换后，新的动态交给所选分身；已经开始的任务继续由原分身处理。",
            )}
          </p>
        )}
      </div>
      {pending && !busy && (
        <button
          className="button small"
          disabled={disabled}
          onClick={() => void choose(pending.shadowId)}
        >
          {t("重试设置")}
        </button>
      )}
      <ErrorBox message={error || data.error} />
    </div>
  );
}
