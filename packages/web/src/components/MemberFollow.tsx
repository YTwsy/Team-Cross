import { useEffect, useRef, useState } from "react";
import { api, errorText } from "../api";
import {
  activityChanged,
  sameSubject,
  useActivityResource,
  type ActivitySubject,
  type Follow,
} from "../activity";
import { t, tr } from "../i18n";
import { ErrorBox, Icon } from "./ui";

export function MemberFollow({
  spaceId,
  subject,
  disabled = false,
}: {
  spaceId: string;
  subject: ActivitySubject;
  disabled?: boolean;
}) {
  const resource = useActivityResource<Follow[]>(
    `collaborations/${spaceId}/follows?kind=${subject.kind}&subjectId=${encodeURIComponent(subject.id)}&executionId=${subject.executionId || ""}`,
  );
  const current = (Array.isArray(resource.data) ? resource.data : []).find(
    (p) => sameSubject(p.subject, subject),
  );
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const attempt = useRef<
    { mode: string; revision: number; requestId: string } | undefined
  >(undefined);
  const menu = useRef<HTMLDetailsElement>(null);
  const [menuPosition, setMenuPosition] = useState<{
    left: number;
    top: number;
  }>();
  function positionMenu() {
    const node = menu.current;
    if (!node?.open) return;
    const popup = node.querySelector<HTMLElement>(".member-follow-menu");
    if (!popup) return;
    const rect = node.getBoundingClientRect(),
      width = popup.offsetWidth,
      height = popup.offsetHeight;
    setMenuPosition({
      left: Math.max(
        8,
        Math.min(rect.right - width, window.innerWidth - width - 8),
      ),
      top: Math.max(
        8,
        Math.min(rect.bottom + 4, window.innerHeight - height - 8),
      ),
    });
  }
  useEffect(() => {
    window.addEventListener("resize", positionMenu);
    window.addEventListener("scroll", positionMenu, true);
    return () => {
      window.removeEventListener("resize", positionMenu);
      window.removeEventListener("scroll", positionMenu, true);
    };
  }, []);
  const effective = current?.effectiveMode || "direct_only";
  async function setMode(mode: string) {
    if (busy) return;
    setBusy(true);
    setError("");
    if (menu.current) menu.current.open = false;
    const revision = current?.revision || 0;
    if (attempt.current?.mode !== mode || attempt.current.revision !== revision)
      attempt.current = { mode, revision, requestId: crypto.randomUUID() };
    try {
      await api(`collaborations/${spaceId}/follow-preferences`, {
        subject,
        ...attempt.current,
      });
      attempt.current = undefined;
      activityChanged();
    } catch (e) {
      setError(errorText(e));
      resource.reload();
    } finally {
      setBusy(false);
    }
  }
  return (
    <span className="member-follow">
      <button
        type="button"
        className={`button small ${effective === "following" ? "is-following" : ""}`}
        disabled={busy || disabled || !resource.data}
        onClick={() =>
          void setMode(
            current?.mode === "muted"
              ? "unmute"
              : effective === "following"
                ? "direct_only"
                : "following",
          )
        }
        title={
          current?.origin === "publication" ||
          current?.origin === "participation"
            ? t("自动关注；可随时取消")
            : undefined
        }
      >
        <Icon name={effective === "muted" ? "clock" : "star"} size={14} />
        {effective === "muted"
          ? t("已静音")
          : effective === "following"
            ? t("已关注")
            : t("关注")}
      </button>
      <details
        ref={menu}
        onToggle={positionMenu}
        onKeyDown={(e) => {
          if (e.key === "Escape" && menu.current) menu.current.open = false;
        }}
        onBlur={(e) => {
          if (!e.currentTarget.contains(e.relatedTarget as Node | null))
            e.currentTarget.open = false;
        }}
      >
        <summary aria-label={t("关注设置")}>
          <Icon name="chevron" size={12} />
        </summary>
        <div className="member-follow-menu" style={menuPosition}>
          <p>
            {t("取消关注后仍接收直接 @ 和回复；静音会停止此范围内的新提醒。")}
          </p>
          {current && current.mode !== effective && (
            <p>{t("父对象设置正在生效")}</p>
          )}
          <button
            type="button"
            disabled={busy || disabled}
            onClick={() => void setMode("following")}
          >
            {t("关注")}
          </button>
          <button
            type="button"
            disabled={busy || disabled}
            onClick={() => void setMode("direct_only")}
          >
            {t("取消关注")}
          </button>
          <button
            type="button"
            disabled={busy || disabled}
            onClick={() =>
              void setMode(current?.mode === "muted" ? "unmute" : "muted")
            }
          >
            {current?.mode === "muted" ? t("解除静音") : t("静音此对象")}
          </button>
        </div>
      </details>
      {error && <ErrorBox message={error} />}
    </span>
  );
}
type FollowPage = {
  items: Follow[];
  total: number;
  following: number;
  nextCursor?: string;
};
export function MemberFollowManager({ spaceId }: { spaceId: string }) {
  const data = useActivityResource<FollowPage>(
    `collaborations/${spaceId}/follows`,
  );
  const [extra, setExtra] = useState<FollowPage>();
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  useEffect(
    () => setExtra(undefined),
    [spaceId, JSON.stringify(data.data?.items)],
  );
  const items = [...(data.data?.items || []), ...(extra?.items || [])];
  const cursor = extra ? extra.nextCursor : data.data?.nextCursor;
  async function more() {
    if (!cursor || busy) return;
    setBusy(true);
    setError("");
    try {
      const page = await api<FollowPage>(
        `collaborations/${spaceId}/follows?cursor=${encodeURIComponent(cursor)}`,
      );
      setExtra((old) => ({
        ...page,
        items: [...(old?.items || []), ...page.items],
      }));
    } catch (e) {
      setError(errorText(e));
      setExtra(undefined);
      data.reload();
    } finally {
      setBusy(false);
    }
  }
  return (
    <div className="member-follow-list">
      <ErrorBox message={data.error || error} />
      {items
        .filter((p) => p.mode !== "direct_only")
        .map((p) => (
          <div key={JSON.stringify(p.subject)}>
            <div>
              <strong>{p.title}</strong>
              <small>
                {p.subject.kind === "material"
                  ? t("材料")
                  : p.subject.kind === "discussion"
                    ? t("讨论")
                    : t("协作会话")}{" "}
                · {p.origin === "manual" ? t("手动关注") : t("自动关注")}
              </small>
            </div>
            <MemberFollow
              spaceId={spaceId}
              subject={p.subject}
              disabled={!p.available}
            />
          </div>
        ))}
      {data.data && !items.some((p) => p.mode !== "direct_only") && !cursor && (
        <p className="muted">{t("还没有关注的对象")}</p>
      )}
      {cursor && (
        <button
          className="text-link"
          disabled={busy}
          onClick={() => void more()}
        >
          {t("加载更多")}
        </button>
      )}
      <p className="muted small-text">{tr`${data.data?.following || 0} 个关注对象`}</p>
    </div>
  );
}
