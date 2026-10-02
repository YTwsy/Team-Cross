import { currentConversation } from "../currentConversation";
import { formatDate, t, tr } from "../i18n";
import { useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { api, errorText, useResource } from "../api";
import {
  availabilityLabel,
  libraryGroupKey,
  openFullLibrary,
  ResourceActions,
  useLibrary,
  type LibraryBundle,
  type LibraryResource,
} from "../library";
import {
  relativeTime,
  type Annotation,
  type Collaboration,
  type MaterialReference,
} from "../types";
import { Annotations, Discussion, type AnnotationRequest } from "./Annotations";
import { Context } from "./Context";
import { SendToAgent } from "./AgentPairings";
import { MaterialReader, type ReadingMemory } from "./Materials";
import {
  Copy,
  Empty,
  ErrorBox,
  Icon,
  Loading,
  Modal,
  type IconName,
} from "./ui";

const kindIcons: Record<LibraryResource["reference"]["kind"], IconName> = {
  material: "book",
  annotation: "comment",
  context: "terminal",
};
function kindName(kind: LibraryResource["reference"]["kind"]) {
  return kind === "material"
    ? t("材料")
    : kind === "annotation"
      ? t("批注")
      : t("上下文");
}

export function Library({
  quick = false,
  hidden = false,
  quickActions,
  initialKey,
}: {
  quick?: boolean;
  hidden?: boolean;
  quickActions?: ReactNode;
  initialKey?: string;
}) {
  const views = [
    ["recent", t("最近使用"), "refresh"],
    ["participated", t("我参与的"), "people"],
    ["annotated", t("我批注的"), "comment"],
    ["favorites", t("已收藏"), "star"],
  ] as const;
  const lib = useLibrary()!;
  const [view, setView] = useState("recent"),
    [kind, setKind] = useState("all"),
    [search, setSearch] = useState("");
  const [active, setActive] = useState(initialKey),
    [collapsed, setCollapsed] = useState(new Set<string>());
  const searchInput = useRef<HTMLInputElement>(null);
  const memories = useRef(new Map<string, Map<string, ReadingMemory>>());
  const activeMemory = active
    ? memories.current.get(active) || new Map<string, ReadingMemory>()
    : undefined;
  if (active && activeMemory) {
    if (!memories.current.has(active) && memories.current.size >= 12)
      memories.current.delete(memories.current.keys().next().value!);
    memories.current.set(active, activeMemory);
  }
  const resources = lib.data?.resources || [];
  const shown = useMemo(
    () =>
      resources.filter((r) => {
        if (
          (view === "favorites" && !r.favorite) ||
          (view === "annotated" && !r.annotated) ||
          (view === "selected" && !r.selected)
        )
          return false;
        if (kind !== "all" && r.reference.kind !== kind) return false;
        return `${r.title} ${r.spaceTitle} ${r.sessionTitle} ${r.author || ""} ${r.summary || ""}`
          .toLocaleLowerCase()
          .includes(search.trim().toLocaleLowerCase());
      }),
    [resources, view, kind, search],
  );
  const groups = useMemo(() => {
    const result = new Map<string, LibraryResource[]>();
    for (const r of shown) {
      const key = libraryGroupKey(r);
      const group = result.get(key) || [];
      group.push(r);
      result.set(key, group);
    }
    return [...result.entries()];
  }, [shown]);
  const selected = resources.find((r) => r.key === active);
  useEffect(() => {
    if (hidden) return;
    if (quick) searchInput.current?.focus();
    const key = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") {
        e.preventDefault();
        searchInput.current?.focus();
      }
      if (
        e.key === "Escape" &&
        !e.isComposing &&
        !document.querySelector("dialog[open]")
      )
        setActive(undefined);
    };
    window.addEventListener("keydown", key);
    return () => {
      window.removeEventListener("keydown", key);
    };
  }, [quick, hidden]);
  function read(r: LibraryResource) {
    if (quick) {
      openFullLibrary(`/library?item=${r.key}`);
      return;
    }
    setActive(r.key);
    if (r.availability === "available")
      void lib.change({ action: "visit", reference: r.reference });
  }
  return (
    <section
      hidden={hidden}
      className={`library ${quick ? "library-quick" : ""} ${active ? "has-reader" : ""}`}
      aria-label={quick ? t("资源速览") : t("个人资源库")}
    >
      {!quick && (
        <header className="library-heading">
          <div>
            <p className="eyebrow">{t("你的协作足迹")}</p>
            <h1 tabIndex={-1}>{t("资源库")}</h1>
            <p>
              {t("找回参与过的会话材料、批注和上下文，选好后一起带给 Agent。")}
            </p>
          </div>
          <div className="library-header-actions">
            <button
              className="button small"
              onClick={() => lib.refresh({ reorder: true })}
            >
              <Icon name="refresh" size={14} />
              {t("刷新") + " "}
            </button>
          </div>
        </header>
      )}
      <ErrorBox
        message={lib.error}
        retry={() => lib.refresh({ reorder: true })}
      />
      <div className="library-layout">
        <div className={quick ? "quick-toolbar" : "library-navigation"}>
          <nav className="library-views" aria-label={t("资源视图")}>
            {views.map(([id, label, icon]) => (
              <button
                key={id}
                className={view === id ? "active" : ""}
                aria-current={view === id ? "page" : undefined}
                onClick={() => setView(id)}
              >
                <Icon name={icon} size={16} />
                {label}
                <span>
                  {
                    resources.filter((r) =>
                      id === "favorites"
                        ? r.favorite
                        : id === "annotated"
                          ? r.annotated
                          : true,
                    ).length
                  }
                </span>
              </button>
            ))}
            {quick && (
              <button
                className={view === "selected" ? "active" : ""}
                aria-current={view === "selected" ? "page" : undefined}
                onClick={() => setView("selected")}
              >
                <Icon name="check" size={16} />
                {t("当前选择")}
                <span>{lib.data?.selection.length || 0}</span>
              </button>
            )}
            {!quick && (
              <p>{t("材料保留来源与版本。收藏和选择只在本机保存。")}</p>
            )}
          </nav>
          {quick && quickActions}
        </div>
        <div className="library-browse">
          <div className="library-search">
            <Icon name="search" size={17} />
            <input
              ref={searchInput}
              aria-label={t("搜索资源库")}
              placeholder={t("搜索材料、Session、批注…")}
              value={search}
              onChange={(e) => setSearch(e.target.value)}
            />
            {search ? (
              <button
                className="icon-button"
                aria-label={t("清除搜索")}
                onClick={() => setSearch("")}
              >
                <Icon name="close" size={14} />
              </button>
            ) : (
              <kbd>⌘ K</kbd>
            )}
          </div>
          <div className="library-filters" aria-label={t("资源类型")}>
            {[
              ["all", t("全部")],
              ["material", t("材料")],
              ["annotation", t("批注")],
              ["context", t("上下文")],
            ].map(([id, label]) => (
              <button
                key={id}
                aria-pressed={kind === id}
                onClick={() => setKind(id!)}
              >
                {label}
              </button>
            ))}
            <span>
              {shown.length}
              {" " + t("项")}
            </span>
          </div>
          {!lib.data && !lib.error ? (
            <Loading text={t("正在整理协作资源…")} />
          ) : null}
          {lib.data && !resources.length && (
            <Empty icon="book" title={t("让协作内容留在手边")}>
              <p>{t("发起或加入协作后，已发布材料和讨论会自动出现在这里。")}</p>
              <button className="button" onClick={() => openFullLibrary("/")}>
                {t("打开协作空间") + " "}
              </button>
            </Empty>
          )}
          {!!resources.length && !shown.length && (
            <Empty
              icon="search"
              title={
                search
                  ? t("没有找到匹配的内容")
                  : view === "favorites"
                    ? t("还没有收藏")
                    : view === "annotated"
                      ? t("还没有参与批注")
                      : t("这里还没有内容")
              }
            >
              <p>
                {view === "favorites" && !search
                  ? t("点击条目旁的星标，留下以后还会用到的材料。")
                  : t("试试其他关键词或切换筛选；已选内容会继续保留。")}
              </p>
            </Empty>
          )}
          <div className="library-groups">
            {groups.map(([key, items]) => (
              <section className="library-group" key={key}>
                <button
                  className="library-group-heading"
                  aria-expanded={!collapsed.has(key)}
                  onClick={() =>
                    setCollapsed((old) => {
                      const next = new Set(old);
                      if (next.has(key)) next.delete(key);
                      else next.add(key);
                      return next;
                    })
                  }
                >
                  <Icon name="chevron" size={14} />
                  <span>
                    <strong>{items[0]!.sessionTitle}</strong>
                    <small>
                      {items[0]!.spaceTitle}
                      {items[0]!.provider ? ` · ${items[0]!.provider}` : ""}
                    </small>
                  </span>
                  <span className="count">{items.length}</span>
                </button>
                {!collapsed.has(key) &&
                  items.map((r) => (
                    <article
                      className={`library-row ${active === r.key ? "is-reading" : ""} ${r.selected ? "is-selected" : ""}`}
                      key={r.key}
                    >
                      <input
                        type="checkbox"
                        aria-label={tr`选择 ${r.title}`}
                        checked={r.selected}
                        disabled={
                          lib.working ||
                          (r.availability !== "available" && !r.selected)
                        }
                        onChange={(e) =>
                          void lib.change({
                            action: "select",
                            reference: r.reference,
                            key: r.key,
                            enabled: e.target.checked,
                          })
                        }
                      />
                      <span className={`library-kind ${r.reference.kind}`}>
                        <Icon name={kindIcons[r.reference.kind]} size={18} />
                      </span>
                      <button
                        className="library-row-body"
                        onClick={() => read(r)}
                        aria-label={tr`阅读 ${r.title}`}
                      >
                        <strong>{r.title}</strong>
                        <p>{r.summary}</p>
                        <small>
                          <span>{kindName(r.reference.kind)}</span>
                          {r.reference.version && (
                            <span>v{r.reference.version}</span>
                          )}
                          {r.author && <span>{r.author}</span>}
                          {!!r.replyCount && (
                            <span>
                              {r.replyCount}
                              {" " + t("条回复")}
                            </span>
                          )}
                          {r.annotated && (
                            <span className="library-mine">
                              {t("我参与了批注")}
                            </span>
                          )}
                          {r.availability !== "available" ? (
                            <span className="library-unavailable">
                              {availabilityLabel(r)}
                            </span>
                          ) : (
                            <time dateTime={r.updatedAt}>
                              {relativeTime(
                                r.openedAt && !r.openedAt.startsWith("0001")
                                  ? r.openedAt
                                  : r.updatedAt,
                              )}
                            </time>
                          )}
                        </small>
                      </button>
                      <button
                        className={`icon-button library-star ${r.favorite ? "is-favorite" : ""}`}
                        aria-label={`${r.favorite ? t("取消收藏") : t("收藏")} ${r.title}`}
                        aria-pressed={r.favorite}
                        disabled={
                          lib.working ||
                          (r.availability !== "available" && !r.favorite)
                        }
                        onClick={() =>
                          void lib.change({
                            action: "favorite",
                            reference: r.reference,
                            key: r.key,
                            enabled: !r.favorite,
                          })
                        }
                      >
                        <Icon name="star" size={16} />
                      </button>
                    </article>
                  ))}
              </section>
            ))}
          </div>
        </div>
        {!quick && (
          <aside className="library-preview" aria-label={t("资源预览")}>
            {selected ? (
              <>
                <div className="library-preview-heading">
                  <span>
                    {kindName(selected.reference.kind)}
                    {t("预览")}
                  </span>
                  <button
                    className="icon-button"
                    aria-label={t("关闭预览")}
                    onClick={() => setActive(undefined)}
                  >
                    <Icon name="close" size={16} />
                  </button>
                </div>
                <LibraryReader
                  key={selected.key}
                  resource={selected}
                  memory={activeMemory!}
                />
              </>
            ) : (
              <div className="library-preview-empty">
                <Icon name="book" size={30} />
                <h2>{t("在这里继续阅读")}</h2>
                <p>
                  {t("点击标题预览，勾选条目加入选择。") + " "}
                  <br />
                  {t("切换筛选时，选择清单会一直保留。") + " "}
                </p>
              </div>
            )}
          </aside>
        )}
      </div>
    </section>
  );
}

function LibraryReader({
  resource: r,
  memory,
}: {
  resource: LibraryResource;
  memory: Map<string, ReadingMemory>;
}) {
  const lib = useLibrary()!;
  const c = useResource<Collaboration>(
    r.availability === "available"
      ? `collaborations/${r.reference.spaceId}`
      : null,
    5000,
  );
  const [request, setRequest] = useState<AnnotationRequest>();
  const [location, setLocation] = useState<AnnotationRequest | undefined>(
    r.reference.target ? { target: r.reference.target, serial: 0 } : undefined,
  );
  const [reading, setReading] = useState<MaterialReference | undefined>(
    r.reference.kind === "material"
      ? { materialId: r.reference.materialId!, version: r.reference.version! }
      : undefined,
  );
  const [annotation, setAnnotation] = useState<Annotation>(),
    [error, setError] = useState("");
  const [revision, setRevision] = useState(0);
  useEffect(() => {
    if (r.reference.kind !== "annotation" || r.availability !== "available")
      return;
    const abort = new AbortController();
    void api<{ content: Annotation }>("library/read", r.reference, abort.signal)
      .then((res) => setAnnotation(res.content))
      .catch((e) => {
        if (!abort.signal.aborted) setError(errorText(e));
      });
    return () => abort.abort();
  }, [r.key, r.availability, revision]);
  // Clearing memory on loss of access prevents returning to a cached body.
  useEffect(() => {
    if (r.availability !== "available") {
      memory.clear();
      setAnnotation(undefined);
    }
  }, [r.availability]);
  function reload() {
    c.reload();
    setRevision((v) => v + 1);
    lib.refresh();
  }
  function locate(ref: MaterialReference) {
    setReading(ref);
    setLocation(undefined);
  }
  function locateAnnotation(a: Annotation) {
    if (a.target?.kind === "material")
      setReading({
        materialId: a.target.materialId!,
        version: a.target.version!,
        turnId: a.target.turnId,
      });
    setLocation({ target: a.target, serial: Date.now() });
  }
  const space = c.data;
  const accessible =
    r.availability === "available" &&
    (space?.role === "owner" ||
      (space?.reachable !== false &&
        !["ended", "left", "expired", "joining"].includes(space?.state || "")));
  const discussionDisabled =
    !space || ["ended", "left", "expired"].includes(space.state);
  const material = space?.materials?.find((m) => m.id === reading?.materialId);
  const discussion =
    space?.annotations?.find((a) => a.id === r.reference.annotationId) ||
    annotation;
  return (
    <div className="library-reader-content">
      <p className="eyebrow">{r.spaceTitle}</p>
      <h2>{r.title}</h2>
      <div className="library-reader-links">
        <a
          className="text-link"
          href={`#/collaborations/${r.reference.spaceId}`}
        >
          {t("打开所在空间") + " "}
          <Icon name="arrow" size={14} />
        </a>
        <ResourceActions
          reference={r.reference}
          disabled={!accessible}
          favorite
        />
      </div>
      {!accessible ? (
        <div className="notice">
          <strong>{availabilityLabel(r) || t("当前无法读取")}</strong>
          <p>
            {t(
              "保留来源入口；正文需要当前有效的访问权限。可以重新连接后再读取。",
            ) + " "}
          </p>
          <button className="button small" onClick={() => lib.refresh()}>
            {t("重新检查") + " "}
          </button>
        </div>
      ) : (
        <>
          <ErrorBox message={c.error || error} retry={reload} />
          {!space && !c.error && <Loading text={t("正在核对当前访问范围…")} />}
          {space &&
            r.reference.kind === "annotation" &&
            !discussion &&
            !error && <Loading text={t("正在读取批注及回复…")} />}
          {space && discussion && (
            <Discussion
              id={space.id}
              annotation={discussion}
              disabled={discussionDisabled}
              onSaved={reload}
              onLocate={locateAnnotation}
              materials={space.materials}
              onLocateMaterial={locate}
            />
          )}
          {space &&
            reading &&
            (material?.withdrawnAt ? (
              <p className="notice">
                {t("这份材料已撤回，原文不再可读；批注保留当时的引用。") + " "}
              </p>
            ) : material ? (
              <MaterialReader
                key={`${reading.materialId}:${reading.version}:${reading.turnId || ""}:${location?.serial || 0}`}
                spaceId={space.id}
                material={material}
                reference={reading}
                memory={memory}
                onVersion={(version) =>
                  setReading({ materialId: material.id, version })
                }
                target={location?.target}
                onAnnotate={(target, origin) =>
                  setRequest({ target, origin, serial: Date.now() })
                }
                onDiscuss={(annotationId, origin) =>
                  setRequest({ annotationId, origin, serial: Date.now() })
                }
                annotations={space.annotations}
                disabled={discussionDisabled}
              />
            ) : (
              <p className="notice">{t("当前空间没有这份材料。")}</p>
            ))}
          {space &&
            !reading &&
            (r.reference.kind === "context" ||
              (location?.target && location.target.kind !== "material")) && (
              <Context
                id={space.id}
                sessionId={space.sessionId}
                sequence={space.sequence}
                online={space.online}
                closed={discussionDisabled}
                canAnnotate={!discussionDisabled}
                onAnnotate={(target, origin) =>
                  setRequest({ target, origin, serial: Date.now() })
                }
                location={location}
                annotations={space.annotations}
                onDiscuss={(annotationId, origin) =>
                  setRequest({ annotationId, origin, serial: Date.now() })
                }
                agentName={
                  space.provider === "claude" ? "Claude Code" : "Codex"
                }
              />
            )}
          {space && (request || r.reference.kind !== "annotation") && (
            <div className="library-discussions">
              <Annotations
                id={space.id}
                annotations={
                  space.annotations?.filter((a) =>
                    reading
                      ? a.target?.materialId === reading.materialId &&
                        a.target.version === reading.version
                      : a.target?.kind !== "material",
                  ) || []
                }
                materials={space.materials}
                request={request}
                disabled={discussionDisabled}
                onSaved={reload}
                onLocate={locateAnnotation}
                onLocateMaterial={locate}
              />
            </div>
          )}
        </>
      )}
    </div>
  );
}

export function SelectionTray({ quick = false }: { quick?: boolean }) {
  const lib = useLibrary()!;
  const [expanded, setExpanded] = useState(false),
    [mode, setMode] = useState<"read" | "send">();
  const [snapshot, setSnapshot] = useState<LibraryResource[]>([]);
  const [entrySerial, setEntrySerial] = useState(0);
  const tray = useRef<HTMLElement>(null);
  const items = (lib.data?.selection || [])
    .map((key) => lib.data!.resources.find((r) => r.key === key))
    .filter((r): r is LibraryResource => !!r);
  const oneSpace =
    items.length > 0 &&
    items.every((r) => r.reference.spaceId === items[0]!.reference.spaceId);
  const canRead =
    items.length > 0 && items.every((r) => r.availability === "available");
  const selectionChanged =
    mode === "read" &&
    items.map((r) => r.key).join(",") !== snapshot.map((r) => r.key).join(",");
  const hasTray = items.length > 0 || mode === "read";
  useEffect(() => {
    if (quick && (expanded || mode === "read"))
      tray.current?.scrollIntoView?.({ block: "nearest" });
  }, [quick, expanded, mode]);
  useEffect(() => {
    document.body.classList.toggle("has-selection-tray", hasTray);
    const updateHeight = () =>
      document.body.style.setProperty(
        "--selection-tray-height",
        `${tray.current?.getBoundingClientRect().height || 68}px`,
      );
    updateHeight();
    const observer =
      typeof ResizeObserver !== "undefined"
        ? new ResizeObserver(updateHeight)
        : undefined;
    if (tray.current) observer?.observe(tray.current);
    return () => {
      observer?.disconnect();
      document.body.classList.remove("has-selection-tray");
      document.body.style.removeProperty("--selection-tray-height");
    };
  }, [hasTray]);
  if (!items.length && !mode) return null;
  return (
    <>
      {hasTray && (
        <section
          ref={tray}
          className={`selection-tray ${quick ? "selection-tray-quick" : ""}`}
          aria-label={t("当前选择清单")}
        >
          {expanded && !!items.length && (
            <div className="selection-items">
              <div className="panel-heading">
                <strong>{t("当前选择")}</strong>
                <button
                  className="text-link"
                  disabled={lib.working}
                  onClick={() => void lib.change({ action: "clear" })}
                >
                  {t("清空选择") + " "}
                </button>
              </div>
              {items.map((r) => (
                <div className="selection-item" key={r.key}>
                  <Icon name={kindIcons[r.reference.kind]} size={16} />
                  <div>
                    <strong>{r.title}</strong>
                    <small>
                      {r.spaceTitle} ·{" "}
                      {r.reference.version
                        ? tr`版本 ${r.reference.version}`
                        : kindName(r.reference.kind)}
                      {availabilityLabel(r) && ` · ${availabilityLabel(r)}`}
                    </small>
                  </div>
                  <button
                    className="icon-button"
                    aria-label={tr`移除 ${r.title}`}
                    disabled={lib.working}
                    onClick={() =>
                      void lib.change({
                        action: "select",
                        key: r.key,
                        enabled: false,
                      })
                    }
                  >
                    <Icon name="close" size={14} />
                  </button>
                </div>
              ))}
            </div>
          )}
          {mode === "read" && (
            <SelectionEntry
              key={entrySerial}
              resources={snapshot}
              changed={selectionChanged}
              onClose={() => setMode(undefined)}
            />
          )}
          <div className="selection-tray-bar">
            <button
              className="selection-count"
              aria-expanded={expanded}
              onClick={() => setExpanded(!expanded)}
            >
              <span>{items.length}</span>
              <strong>{t("已选内容")}</strong>
              <Icon name="chevron" size={14} />
            </button>
            {!quick && (
              <span className="selection-hint">
                {items.length === 0
                  ? t("可继续选择下一组内容")
                  : canRead
                    ? oneSpace
                      ? t("材料、批注和上下文一起使用")
                      : t("来自多个空间，可供个人 Agent 读取")
                    : t("包含暂不可读的内容，请先移除或重新连接")}
              </span>
            )}
            <div className="selection-buttons">
              <button
                className="button primary"
                disabled={!canRead}
                onClick={() => {
                  setSnapshot(items);
                  setMode("send");
                }}
              >
                {t("交给 Agent")}
              </button>
              <button
                className="button"
                disabled={!canRead}
                onClick={() => {
                  setSnapshot(items);
                  setEntrySerial((v) => v + 1);
                  setExpanded(false);
                  setMode("read");
                }}
              >
                <Icon name="link" size={15} />
                {mode === "read" ? t("重新生成") : t("生成读取入口")}
              </button>
            </div>
          </div>
        </section>
      )}
      {mode === "send" && (
        <Modal title={t("交给 Agent")} onClose={() => setMode(undefined)}>
          <SendToAgent references={snapshot.map((r) => r.reference)} />
        </Modal>
      )}
    </>
  );
}
function SelectionEntry({
  resources,
  changed,
  onClose,
}: {
  resources: LibraryResource[];
  changed: boolean;
  onClose: () => void;
}) {
  const [bundle, setBundle] = useState<LibraryBundle>(),
    [error, setError] = useState("");
  const request = useRef(crypto.randomUUID());
  const [revision, setRevision] = useState(0);
  useEffect(() => {
    const abort = new AbortController();
    setError("");
    void api<LibraryBundle>(
      "library/bundles",
      {
        references: resources.map((r) => r.reference),
        requestId: request.current,
      },
      abort.signal,
    )
      .then((value) => {
        if (!abort.signal.aborted) setBundle(value);
      })
      .catch((e) => {
        if (!abort.signal.aborted) setError(errorText(e));
      });
    return () => abort.abort();
  }, [resources, revision]);
  const prompt = bundle
    ? tr`请使用 Team Cross 的 read_selection 读取 ${bundle.code}，核对原文后分析。`
    : "";
  return (
    <section className="selection-entry" aria-label={t("个人 Agent 读取入口")}>
      <div className="selection-entry-heading">
        <span>
          {t("个人 Agent 读取入口") + " "}
          <small>
            {t("· 已固定") + " "}
            {resources.length}
            {" " + t("项")}
          </small>
        </span>
        <button
          className="icon-button"
          aria-label={t("收起读取入口")}
          onClick={onClose}
        >
          <Icon name="close" size={14} />
        </button>
      </div>
      <ErrorBox message={error} retry={() => setRevision((v) => v + 1)} />
      {!bundle && !error && <Loading text={t("正在生成读取入口…")} />}
      {bundle && (
        <>
          <div className="selection-entry-result" aria-live="polite">
            <code className="selection-code">{bundle.code}</code>
            <span>{t("复制后，粘贴到本机个人 Agent")}</span>
            <Copy label={t("复制读取提示")} text={prompt} />
            {currentConversation() && (
              <ConversationHandoff
                key={bundle.code}
                bundle={bundle}
                prompt={prompt}
              />
            )}
          </div>
          {changed && (
            <p className="selection-entry-changed" role="status">
              {t("选择已变化，重新生成可更新入口。") + " "}
            </p>
          )}
          <details className="selection-entry-details">
            <summary>{t("查看提示与所选内容")}</summary>
            <textarea
              aria-label={t("个人 Agent 读取提示")}
              readOnly
              value={prompt}
              rows={2}
            />
            <ul>
              {resources.map((r) => (
                <li key={r.key}>
                  {r.title}
                  {r.reference.version
                    ? tr` · 版本 ${r.reference.version}`
                    : ""}
                  <small> {r.spaceTitle}</small>
                </li>
              ))}
            </ul>
            <p className="small-text muted">{tr`有效至 ${formatDate(bundle.expiresAt)}；此后修改选择不会改变这个入口，每次读取仍会检查访问权限。`}</p>
            <a
              className="text-link"
              href="#/settings"
              onClick={(e) => {
                if (window.webkit?.messageHandlers?.teamcross) {
                  e.preventDefault();
                  openFullLibrary("/settings");
                }
              }}
            >
              {t("在设置与连接中接入个人 Agent") + " "}
            </a>
          </details>
        </>
      )}
    </section>
  );
}

// The existing reading entry also works in a host conversation. All selection,
// version binding and reading UI above remain shared with the browser WebGUI.
function ConversationHandoff({
  bundle,
  prompt,
}: {
  bundle: LibraryBundle;
  prompt: string;
}) {
  const host = currentConversation()!;
  const [state, setState] = useState(() => host.status(bundle.code));
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const sending = useRef(false);
  async function send() {
    if (sending.current || state) return;
    sending.current = true;
    setBusy(true);
    setError("");
    try {
      await host.send(bundle, prompt);
    } catch (error) {
      setError(errorText(error));
    } finally {
      setState(host.status(bundle.code));
      setBusy(false);
      sending.current = false;
    }
  }
  return (
    <div>
      <button
        className="button"
        disabled={busy || !!state}
        onClick={() => void send()}
      >
        {t("带回当前对话")}
      </button>
      <ErrorBox message={error} />
      {state && (
        <p className="small-text" role="status">
          {state === "sent"
            ? t("消息已交给当前会话，请在会话中查看处理结果。")
            : t("消息结果尚未确认，请查看当前会话；不会自动重发。")}
        </p>
      )}
    </div>
  );
}
