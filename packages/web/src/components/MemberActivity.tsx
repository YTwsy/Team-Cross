import { useEffect, useRef, useState } from "react";
import { api, errorText } from "../api";
import {
  activityChanged,
  readActivity,
  useActivityResource,
  type ActivityGroup,
  type ActivityItem,
  type ActivityPage,
} from "../activity";
import { t, tr } from "../i18n";
import { relativeTime, type Annotation, type MaterialPage } from "../types";
import type { LibraryReference } from "../library";
import { Discussion } from "./Annotations";
import { MemberFollow, MemberFollowManager } from "./MemberFollow";
import { MarkdownText } from "./MarkdownText";
import { ErrorBox, Icon, Loading, Modal } from "./ui";
import { SpaceAvatar } from "./SpaceIdentity";

function activityLabel(item: ActivityItem) {
  if (item.reasons.includes("mention")) return t("在讨论中 @了你");
  if (item.reasons.includes("direct_reply")) return t("回复了你");
  if (item.kind === "comment") return t("发表了评论");
  if (item.kind === "reply") return t("回复了讨论");
  if (item.kind === "material_published") return t("发布了材料");
  if (item.kind === "material_updated") return t("发布了新版本");
  if (item.kind === "material_withdrawn") return t("撤回了材料");
  if (item.kind === "brief_updated") return t("更新了空间简报");
  if (item.kind.endsWith("failed")) return t("执行失败");
  if (item.kind.endsWith("interrupted") || item.kind.endsWith("cancelled"))
    return t("执行已中断");
  if (item.kind === "turn_completed" && !item.preview) return t("本轮已结束");
  return t("完成了答复");
}
export function ActivityBadge() {
  const data = useActivityResource<{ spaces: ActivityPage[] }>(
    "activity?limit=1",
  );
  const count =
    data.data?.spaces?.reduce((n, p) => n + (p.unread || 0), 0) || 0;
  return count ? (
    <span className="activity-badge" aria-label={tr`${count} 组未读动态`}>
      {count > 99 ? "99+" : count}
    </span>
  ) : null;
}
export function MemberActivity({
  spaceId,
  onLocate,
}: {
  spaceId?: string;
  onLocate?: (ref: LibraryReference) => void;
}) {
  const [expanded, setExpanded] = useState(!spaceId);
  const [scope, setScope] = useState("mine");
  const [category, setCategory] = useState("all");
  const [unread, setUnread] = useState(false);
  const [spaceFilter, setSpaceFilter] = useState("");
  const [selected, setSelected] = useState<ActivityItem>();
  const [manager, setManager] = useState(false);
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState("");
  const [extra, setExtra] = useState<Record<string, ActivityPage>>({});
  const query = `view=${scope}&category=${scope === "space" ? "all" : category}&unread=${scope === "mine" && unread}`;
  const data = useActivityResource<ActivityPage | { spaces: ActivityPage[] }>(
    spaceId
      ? `collaborations/${spaceId}/activity?${query}`
      : `activity?${query}`,
  );
  useEffect(() => setExtra({}), [query, spaceFilter]);
  const pages = data.data
    ? "spaces" in data.data
      ? data.data.spaces
      : [data.data]
    : [];
  const visible = pages.filter(
    (p) => !spaceFilter || p.spaceId === spaceFilter,
  );
  const groups = visible
    .flatMap((page) => {
      const all = [
        ...(page.groups || []),
        ...(extra[page.spaceId]?.groups || []),
      ];
      const seen = new Set<string>();
      return all
        .filter((g) => !seen.has(g.id) && !!seen.add(g.id))
        .map((group) => ({ page, group }));
    })
    .sort(
      (a, b) =>
        b.group.latest.createdAt.localeCompare(a.group.latest.createdAt) ||
        b.group.latest.id.localeCompare(a.group.latest.id),
    );
  const count = visible.reduce((n, p) => n + (p.unread || 0), 0);
  async function allRead() {
    if (busy) return;
    setBusy(true);
    setMessage("");
    const available = visible.filter((p) => p.snapshot);
    const results = await Promise.allSettled(
      available.map((p) => readActivity(p.spaceId, { snapshot: p.snapshot })),
    );
    const failed =
      results.filter((r) => r.status === "rejected" || r.value.pendingSync)
        .length +
      visible.length -
      available.length;
    setMessage(
      failed
        ? tr`已同步 ${visible.length - failed} 个空间，${failed} 个空间待同步`
        : t("已标为已读"),
    );
    setBusy(false);
  }
  async function groupRead(page: ActivityPage, group: ActivityGroup) {
    try {
      const result = await readActivity(page.spaceId, {
        snapshot: page.snapshot,
        group: group.id,
      });
      if (result.pendingSync) setMessage(t("已读，待同步"));
    } catch (e) {
      setMessage(errorText(e));
    }
  }
  async function more(page: ActivityPage) {
    const cursor = extra[page.spaceId]?.nextCursor || page.nextCursor;
    if (!cursor || busy) return;
    setBusy(true);
    try {
      const next = await api<ActivityPage>(
        `collaborations/${page.spaceId}/activity?${query}&cursor=${encodeURIComponent(cursor)}`,
      );
      setExtra((old) => ({
        ...old,
        [page.spaceId]: {
          ...next,
          groups: [...(old[page.spaceId]?.groups || []), ...next.groups],
        },
      }));
    } catch (e) {
      setMessage(errorText(e));
      setExtra({});
      data.reload();
    } finally {
      setBusy(false);
    }
  }
  return (
    <section
      className={`member-activity ${spaceId ? "space-activity" : "global-activity page"}`}
      aria-label={spaceId ? t("最近动态") : t("我的动态")}
    >
      <div className="activity-heading">
        <div>
          {spaceId ? (
            <h3>{t("最近动态")}</h3>
          ) : (
            <h1 tabIndex={-1}>{t("我的动态")}</h1>
          )}
          {count > 0 && <span className="activity-badge">{count}</span>}
        </div>
        <div className="activity-actions">
          {spaceId && (
            <button
              className="text-link"
              onClick={() => setExpanded(!expanded)}
            >
              {expanded ? t("收起动态") : t("查看全部动态")}
              <Icon name="chevron" size={14} />
            </button>
          )}
          <button className="text-link" onClick={() => setManager(true)}>
            {t("管理关注")}
          </button>
        </div>
      </div>
      {!spaceId && (
        <p className="muted">{t("与你有关的提及、回复和关注更新。")}</p>
      )}
      {spaceId && (
        <div className="activity-tabs">
          <button
            className={scope === "mine" ? "active" : ""}
            onClick={() => setScope("mine")}
          >
            {t("与你有关")}
          </button>
          <button
            className={scope === "space" ? "active" : ""}
            onClick={() => setScope("space")}
          >
            {t("空间全部")}
          </button>
        </div>
      )}
      {expanded && (
        <div className="activity-filters">
          {scope === "mine" && (
            <>
              <div className="activity-tabs">
                {[
                  ["all", t("全部")],
                  ["mentions", t("@我")],
                  ["replies", t("回复")],
                  ["following", t("关注")],
                ].map(([value, label]) => (
                  <button
                    key={value}
                    className={category === value ? "active" : ""}
                    onClick={() => setCategory(value!)}
                  >
                    {label}
                  </button>
                ))}
              </div>
              <label>
                <input
                  type="checkbox"
                  checked={unread}
                  onChange={(e) => setUnread(e.target.checked)}
                />
                {t("仅未读")}
              </label>
            </>
          )}
          {!spaceId && (
            <select
              aria-label={t("筛选空间")}
              value={spaceFilter}
              onChange={(e) => setSpaceFilter(e.target.value)}
            >
              <option value="">{t("所有空间")}</option>
              {pages.map((p) => (
                <option key={p.spaceId} value={p.spaceId}>
                  {p.title || p.spaceId}
                </option>
              ))}
            </select>
          )}
          <button
            className="text-link"
            disabled={busy || !visible.some((p) => p.snapshot)}
            onClick={() => void allRead()}
          >
            {spaceId || spaceFilter
              ? t("本空间全部已读")
              : t("所有空间全部已读")}
          </button>
        </div>
      )}
      <ErrorBox message={data.error} />
      {visible
        .filter((p) => p.error)
        .map((p) => (
          <p className="notice" key={p.spaceId}>
            {p.title} · {t("暂未同步")} · {p.error}
          </p>
        ))}
      {data.loading && !data.data && <Loading />}
      <div className="activity-list">
        {(expanded ? groups : groups.slice(0, 3)).map(({ page, group }) => {
          const primary = group.attention || group.latest;
          return (
            <article
              className={`activity-row ${group.unread ? "unread" : ""}`}
              key={`${page.spaceId}:${group.id}`}
            >
              <SpaceAvatar
                name={primary.actor.name || t("成员")}
                agent={primary.actor.kind === "session"}
              />
              <button
                className="activity-open"
                onClick={() => setSelected(primary)}
              >
                <span className="activity-row-title">
                  <strong>{primary.actor.name || t("成员")}</strong>{" "}
                  {activityLabel(primary)}
                  {group.unread > 0 && (
                    <span
                      className="activity-unread-dot"
                      aria-label={t("未读")}
                    />
                  )}
                </span>
                <span className="activity-preview">
                  {primary.preview || t("查看动态详情")}
                </span>
                <small>
                  {!spaceId && `${page.title} · `}
                  {primary.source.materialId && `v${primary.source.version} · `}
                  {relativeTime(primary.createdAt)}
                  {group.unread > 0 && ` · ${tr`${group.unread} 条未读`}`}
                </small>
              </button>
              <div className="activity-row-actions">
                {group.unread > 0 && (
                  <button
                    className="icon-button"
                    title={t("标为已读")}
                    aria-label={t("标为已读")}
                    onClick={() => void groupRead(page, group)}
                  >
                    <Icon name="check" size={16} />
                  </button>
                )}
                {["discussion", "material", "execution"].includes(
                  primary.subject.kind,
                ) && (
                  <MemberFollow
                    spaceId={page.spaceId}
                    subject={primary.subject}
                  />
                )}
              </div>
              {expanded && group.count > 1 && (
                <details className="activity-group-history">
                  <summary>{tr`${group.count} 条动态`}</summary>
                  {group.events
                    .slice()
                    .reverse()
                    .map((item) => (
                      <button key={item.id} onClick={() => setSelected(item)}>
                        {item.unread && "● "}
                        {item.actor.name} ·{" "}
                        {item.preview || activityLabel(item)}
                      </button>
                    ))}
                  {group.count > group.events.length && (
                    <p className="muted small-text">
                      {t("较早内容可在原讨论中查看")}
                    </p>
                  )}
                </details>
              )}
            </article>
          );
        })}
      </div>
      {data.data && !groups.length && (
        <div className="activity-empty">
          <Icon name="comment" size={24} />
          <p>
            {scope === "mine"
              ? t("暂时没有与你有关的新动态")
              : t("空间还没有新动态")}
          </p>
          {spaceId && scope === "mine" && (
            <button className="text-link" onClick={() => setScope("space")}>
              {t("查看空间全部动态")}
            </button>
          )}
        </div>
      )}
      {expanded &&
        visible.map((page) => {
          const last = extra[page.spaceId] || page;
          return last.nextCursor ? (
            <button
              className="text-link"
              key={page.spaceId}
              disabled={busy}
              onClick={() => void more(page)}
            >
              {t("加载更多")} · {page.title}
            </button>
          ) : null;
        })}
      {spaceId && (
        <a className="activity-global-link" href="#/activity">
          {t("查看全局动态")}
          <Icon name="arrow" size={14} />
        </a>
      )}
      {message && (
        <p role="status" className="small-text">
          {message}
        </p>
      )}
      {selected && (
        <Modal title={t("动态详情")} onClose={() => setSelected(undefined)}>
          <ActivitySource
            item={selected}
            onLocate={onLocate}
            onClose={() => setSelected(undefined)}
          />
        </Modal>
      )}
      {manager && (
        <Modal title={t("管理关注")} onClose={() => setManager(false)}>
          {visible.map((p) => (
            <section key={p.spaceId}>
              <h3>{p.title}</h3>
              <MemberFollowManager spaceId={p.spaceId} />
            </section>
          ))}
        </Modal>
      )}
    </section>
  );
}
function ActivitySource({
  item,
  onLocate,
  onClose,
}: {
  item: ActivityItem;
  onLocate?: (ref: LibraryReference) => void;
  onClose: () => void;
}) {
  const source = item.source;
  const address = `${source.spaceId}${source.executionId ? `/sessions/${source.executionId}` : ""}`;
  const [note, setNote] = useState<Annotation>();
  const [segments, setSegments] = useState<MaterialPage["segments"]>();
  const [summary, setSummary] = useState<string>();
  const [error, setError] = useState("");
  const [loaded, setLoaded] = useState(false);
  const [readError, setReadError] = useState("");
  const panel = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const abort = new AbortController();
    let running = false;
    setLoaded(false);
    setError("");
    setReadError("");
    setNote(undefined);
    setSegments(undefined);
    setSummary(undefined);
    async function refresh() {
      if (running || abort.signal.aborted) return;
      running = true;
      try {
        let nextNote: Annotation | undefined,
          nextSegments: MaterialPage["segments"] | undefined,
          nextSummary: string | undefined;
        if (
          source.annotationId ||
          (source.materialId && item.kind !== "material_withdrawn")
        ) {
          const subject = item.subject;
          await api(
            `collaborations/${source.spaceId}/follows?kind=${subject.kind}&subjectId=${encodeURIComponent(subject.id)}&executionId=${subject.executionId || ""}`,
            undefined,
            abort.signal,
          );
        }
        if (source.annotationId) {
          const data = await api<{ annotations: Annotation[] }>(
            `collaborations/${address}/context?kind=annotations&annotationId=${encodeURIComponent(source.annotationId)}`,
            undefined,
            abort.signal,
          );
          const found = data.annotations.find(
            (a) => a.id === source.annotationId,
          );
          if (
            !found ||
            (source.replyId &&
              !found.replies?.some((r) => r.id === source.replyId))
          )
            throw new Error(t("来源已不可用"));
          nextNote = found;
        } else if (source.materialId && source.version) {
          const data = await api<MaterialPage>(
            `collaborations/${source.spaceId}/read-material`,
            { materialId: source.materialId, version: source.version },
            abort.signal,
          );
          nextSegments = data.segments;
        } else if (source.turnId) {
          const data = await api<MaterialPage>(
            `collaborations/${address}/context?kind=history&turnId=${encodeURIComponent(source.turnId)}`,
            undefined,
            abort.signal,
          );
          if (!data.segments?.some((s) => s.turnId === source.turnId))
            throw new Error(t("尚未定位到原始轮次"));
          nextSegments = data.segments;
        } else if (source.requestId) {
          const data = await api<{
            summary?: string;
            error?: string;
            state: string;
          }>(
            `collaborations/${source.spaceId}/workbench/request?requestId=${encodeURIComponent(source.requestId)}`,
            undefined,
            abort.signal,
          );
          nextSummary = data.summary || data.error || data.state;
        } else {
          await api(
            `collaborations/${source.spaceId}`,
            undefined,
            abort.signal,
          );
          nextSummary = item.preview || activityLabel(item);
        }
        if (!abort.signal.aborted) {
          setNote(nextNote);
          setSegments(nextSegments);
          setSummary(nextSummary);
          setError("");
          setLoaded(true);
        }
      } catch (e) {
        if (!abort.signal.aborted) {
          setNote(undefined);
          setSegments(undefined);
          setSummary(undefined);
          setLoaded(false);
          setError(errorText(e));
        }
      } finally {
        running = false;
      }
    }
    void refresh();
    const timer = window.setInterval(() => void refresh(), 4000);
    return () => {
      abort.abort();
      window.clearInterval(timer);
    };
  }, [item.id]);
  useEffect(() => {
    if (!loaded || !item.unread) return;
    const element = source.replyId
      ? Array.from(
          panel.current?.querySelectorAll<HTMLElement>("[data-reply-id]") || [],
        ).find((node) => node.dataset.replyId === source.replyId)
      : panel.current;
    if (!element || typeof IntersectionObserver === "undefined") return;
    if (source.replyId) element.scrollIntoView?.({ block: "center" });
    const observer = new IntersectionObserver(
      (entries) => {
        if (
          entries.some((e) => e.isIntersecting) &&
          document.visibilityState !== "hidden"
        ) {
          observer.disconnect();
          void readActivity(source.spaceId, { eventIds: [item.id] }).catch(
            (e) => setReadError(errorText(e)),
          );
        }
      },
      { threshold: 0.1 },
    );
    observer.observe(element);
    return () => observer.disconnect();
  }, [loaded, item.id]);
  const reference: LibraryReference = source.annotationId
    ? {
        spaceId: source.spaceId,
        executionId: source.executionId,
        kind: "annotation",
        annotationId: source.annotationId,
      }
    : source.materialId
      ? {
          spaceId: source.spaceId,
          kind: "material",
          materialId: source.materialId,
          version: source.version,
        }
      : {
          spaceId: source.spaceId,
          executionId: source.executionId,
          kind: "context",
        };
  function locate() {
    window.location.hash = `/collaborations/${address}?activity=${encodeURIComponent(JSON.stringify({ ...source, navigation: Date.now() }))}`;
    onClose();
  }
  return (
    <div className="activity-source" ref={panel}>
      <ErrorBox message={error} />
      <ErrorBox message={readError} />
      {!loaded && !error && <Loading />}
      {note && (
        <Discussion
          id={address}
          annotation={note}
          disabled={false}
          onSaved={(next) => {
            setNote(next);
            activityChanged();
          }}
          onLocate={() => {
            if (onLocate) {
              onLocate(reference);
              onClose();
            } else locate();
          }}
        />
      )}
      {segments
        ?.filter((s) => !s.collapsed)
        .map((s, i) => (
          <section key={`${s.itemId}:${i}`}>
            <MarkdownText source={s.text} />
          </section>
        ))}
      {summary && <MarkdownText source={summary} />}
      <button className="button small" onClick={locate}>
        {t("打开原文")}
      </button>
    </div>
  );
}
