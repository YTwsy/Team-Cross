import {
  createContext,
  useContext,
  useId,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { api, errorText, useResource } from "../api";
import { formatDate, serviceText, t, tr } from "../i18n";
import type { LibraryReference } from "../library";
import type { Collaboration } from "../types";
import type {
  BriefItem,
  SpaceAssistant,
  SpaceBrief,
  SpaceReceiver,
  SpaceRequest,
  WorkbenchView,
} from "../workbench";
import {
  AgentRequestStatus,
  PairConversation,
  SendToAgent,
  type AgentPairing,
} from "./AgentPairings";
import { Copy, ErrorBox, Loading, Modal } from "./ui";

type Tab = "reading" | "requests" | "brief" | "sessions";
type Send = {
  references: LibraryReference[];
  targetId?: string;
  parentRequestId?: string;
};
const WorkbenchContext = createContext<{
  view?: WorkbenchView;
  send: (value: Send) => void;
  show: (tab: Tab) => void;
  disabled: boolean;
} | null>(null);

export function SpaceRequestButton({
  references,
  disabled = false,
}: {
  references: LibraryReference[];
  disabled?: boolean;
}) {
  const space = useContext(WorkbenchContext);
  if (!space) return null;
  return (
    <button
      className="button small"
      disabled={disabled || space.disabled}
      onClick={() => space.send({ references })}
    >
      {t("发起空间协作")}
    </button>
  );
}

export function WorkbenchReadingShortcut() {
  const space = useContext(WorkbenchContext);
  if (!space) return null;
  const latest = space.view?.requests[0];
  return (
    <section className="panel workbench-shortcut">
      <div className="panel-heading">
        <h2>{t("空间协作")}</h2>
        <button className="text-link" onClick={() => space.show("requests")}>
          {t("查看进展")}
        </button>
      </div>
      <p className="muted">
        {t("围绕当前材料发起请求，成员和接收会话在空间中接力。")}
      </p>
      {latest && (
        <div className="workbench-latest">
          <p>{latest.instruction}</p>
          <AgentRequestStatus request={latest} />
        </div>
      )}
      <button
        className="button"
        disabled={space.disabled}
        onClick={() => space.send({ references: [] })}
      >
        {t("发起协作请求")}
      </button>
    </section>
  );
}

export function assistantLabel(a: SpaceAssistant) {
  return a.state === "ready"
    ? t("已接手")
    : a.state === "initializing"
      ? t("等待读取与接手确认")
      : a.state === "paused"
        ? t("已暂停")
        : a.state === "failed"
          ? t("接手未完成")
          : t("未启用");
}

export function SpaceWorkbench({
  collaboration: c,
  children,
  onLocate,
}: {
  collaboration: Collaboration;
  children: ReactNode;
  onLocate: (ref: LibraryReference) => void;
}) {
  const [tab, setTab] = useState<Tab>("reading");
  const [offset, setOffset] = useState(0);
  const data = useResource<WorkbenchView>(
    `collaborations/${c.id}/workbench/view?offset=${offset}`,
    2500,
  );
  const [send, setSend] = useState<Send>();
  const [assistant, setAssistant] = useState(false);
  const [selectedRequest, setSelectedRequest] = useState("");
  const detail = useResource<SpaceRequest>(
    selectedRequest
      ? `collaborations/${c.id}/workbench/request?requestId=${selectedRequest}`
      : null,
    2500,
  );
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const tabs = useRef<(HTMLButtonElement | null)[]>([]);
  const heading = useRef<HTMLDivElement>(null);
  const uid = useId();
  const owner = c.role === "owner";
  const disabled =
    ["ended", "left", "expired"].includes(c.state) ||
    (!owner && c.reachable === false);
  const show = (next: Tab) => {
    setTab(next);
    if (next !== "reading")
      heading.current?.scrollIntoView?.({ block: "start" });
  };
  const locate = (ref: LibraryReference) => {
    setSelectedRequest("");
    setTab("reading");
    onLocate(ref);
  };
  async function act(op: string, body: object) {
    if (busy) return;
    setBusy(true);
    setError("");
    try {
      await api(`collaborations/${c.id}/workbench/${op}`, body);
      data.reload();
    } catch (e) {
      setError(errorText(e));
    } finally {
      setBusy(false);
    }
  }
  const nav: { value: Tab; label: string }[] = [
    { value: "reading", label: t("阅读与讨论") },
    { value: "requests", label: t("协作请求") },
    { value: "brief", label: t("空间简报") },
    { value: "sessions", label: t("参与会话") },
  ];
  const content = data.data?.assistant ? data.data : undefined;
  const card = (request: SpaceRequest) => (
    <RequestCard
      key={request.id}
      request={request}
      view={content!}
      c={c}
      onLocate={locate}
      onParent={setSelectedRequest}
      onContinue={() => {
        setSelectedRequest("");
        setSend({
          references: request.references,
          parentRequestId: request.id,
        });
      }}
      onCancel={() => void act("cancel", { requestId: request.id })}
      disabled={disabled || busy}
    />
  );
  return (
    <WorkbenchContext.Provider
      value={{ view: content, send: setSend, show, disabled }}
    >
      <div className="workbench-heading" ref={heading}>
        <div>
          <span className="eyebrow">{t("空间工作台")}</span>
          <p className="muted">
            {t("共享材料、明确请求，保留每次接力的进展。")}
          </p>
        </div>
        <div className="workbench-heading-actions">
          <button
            className="button"
            onClick={() => setAssistant(true)}
            disabled={!content}
          >
            {t("专用会话")} ·{" "}
            {content ? assistantLabel(content.assistant) : t("读取中…")}
          </button>
          <button
            className="button primary"
            disabled={disabled}
            onClick={() => setSend({ references: [] })}
          >
            {t("发起协作请求")}
          </button>
        </div>
      </div>
      <div
        className="workbench-tabs"
        role="tablist"
        aria-label={t("空间工作台")}
      >
        {nav.map((item, index) => (
          <button
            key={item.value}
            ref={(el) => {
              tabs.current[index] = el;
            }}
            id={`${uid}-${item.value}-tab`}
            role="tab"
            aria-selected={tab === item.value}
            aria-controls={`${uid}-${item.value}-panel`}
            tabIndex={tab === item.value ? 0 : -1}
            onClick={() => setTab(item.value)}
            onKeyDown={(e) => {
              if (!["ArrowLeft", "ArrowRight", "Home", "End"].includes(e.key))
                return;
              e.preventDefault();
              const next =
                e.key === "Home"
                  ? 0
                  : e.key === "End"
                    ? nav.length - 1
                    : (index + (e.key === "ArrowLeft" ? -1 : 1) + nav.length) %
                      nav.length;
              setTab(nav[next]!.value);
              tabs.current[next]?.focus();
            }}
          >
            {item.label}
            {item.value === "requests" && content && (
              <span className="count">{content.total}</span>
            )}
          </button>
        ))}
      </div>
      <ErrorBox message={error || data.error} retry={data.reload} />
      <div
        id={`${uid}-reading-panel`}
        role="tabpanel"
        aria-labelledby={`${uid}-reading-tab`}
        hidden={tab !== "reading"}
      >
        {children}
      </div>
      <div
        id={`${uid}-requests-panel`}
        role="tabpanel"
        aria-labelledby={`${uid}-requests-tab`}
        hidden={tab !== "requests"}
        className="workbench-pane"
      >
        {content ? (
          <>
            <div className="workbench-section-heading">
              <div>
                <h2>{t("协作请求")}</h2>
                <p className="muted">
                  {t("提交、读取和完成分别记录；结果摘要由接收会话报告。")}
                </p>
              </div>
              <span>{tr`共 ${content.total} 条`}</span>
            </div>
            {content.requests.length ? (
              <div className="workbench-requests">
                {content.requests.map(card)}
              </div>
            ) : (
              <div className="panel workbench-empty">
                <h3>{t("从一条明确的请求开始")}</h3>
                <p>
                  {t(
                    "选择已发布材料或批注，交给空间中的一个接收会话。也可以直接提出新的问题。",
                  )}
                </p>
                <button className="button" onClick={() => setTab("sessions")}>
                  {t("关联接收会话")}
                </button>
              </div>
            )}
            <div className="workbench-pagination">
              <button
                className="button small"
                disabled={offset === 0}
                onClick={() => setOffset(Math.max(0, offset - 40))}
              >
                {t("上一页")}
              </button>
              <span>{tr`${Math.floor(offset / 40) + 1} / ${Math.max(1, Math.ceil(content.total / 40))}`}</span>
              <button
                className="button small"
                disabled={content.nextOffset === undefined}
                onClick={() => setOffset(content.nextOffset!)}
              >
                {t("下一页")}
              </button>
            </div>
          </>
        ) : tab === "requests" ? (
          <Loading />
        ) : null}
      </div>
      <div
        id={`${uid}-brief-panel`}
        role="tabpanel"
        aria-labelledby={`${uid}-brief-tab`}
        hidden={tab !== "brief"}
        className="workbench-pane"
      >
        {content && (
          <BriefPanel
            key={c.id}
            c={c}
            brief={content.brief}
            disabled={disabled}
            onSaved={data.reload}
            onLocate={locate}
          />
        )}
      </div>
      <div
        id={`${uid}-sessions-panel`}
        role="tabpanel"
        aria-labelledby={`${uid}-sessions-tab`}
        hidden={tab !== "sessions"}
        className="workbench-pane"
      >
        {content && (
          <SpaceSessions
            c={c}
            view={content}
            disabled={disabled}
            onChanged={data.reload}
            onSend={(targetId) => setSend({ references: [], targetId })}
            onRemove={(targetId) => void act("remove", { targetId })}
          />
        )}
      </div>
      {send && (
        <Modal title={t("发起空间协作")} onClose={() => setSend(undefined)}>
          <SendToAgent
            references={send.references}
            spaceId={c.id}
            initialTargetId={send.targetId}
            parentRequestId={send.parentRequestId}
            onSent={data.reload}
          />
        </Modal>
      )}
      {selectedRequest && (
        <Modal title={t("关联请求")} onClose={() => setSelectedRequest("")}>
          <ErrorBox message={detail.error} />
          {detail.data && content ? card(detail.data) : <Loading />}
        </Modal>
      )}
      {assistant && content && (
        <Modal title={t("可选的专用会话")} onClose={() => setAssistant(false)}>
          <AssistantSetup
            c={c}
            view={content}
            disabled={disabled}
            onChanged={data.reload}
            onSessions={() => {
              setAssistant(false);
              setTab("sessions");
            }}
            onRequest={(id) => {
              setAssistant(false);
              setSelectedRequest(id);
            }}
          />
        </Modal>
      )}
    </WorkbenchContext.Provider>
  );
}

function referenceName(c: Collaboration, ref: LibraryReference) {
  if (ref.kind === "annotation")
    return (
      c.annotations
        ?.find((a) => a.id === ref.annotationId)
        ?.text.slice(0, 60) || t("原批注")
    );
  const material = c.materials?.find((m) => m.id === ref.materialId);
  return `${material?.versions.find((v) => v.version === ref.version)?.title || t("材料")} · ${t("版本")} ${ref.version}`;
}

function References({
  c,
  refs,
  onLocate,
}: {
  c: Collaboration;
  refs: LibraryReference[];
  onLocate: (ref: LibraryReference) => void;
}) {
  return (
    <div className="workbench-references">
      {refs.map((ref, i) => (
        <button className="text-link" key={i} onClick={() => onLocate(ref)}>
          {referenceName(c, ref)}
        </button>
      ))}
    </div>
  );
}

function RequestCard({
  request: r,
  view,
  c,
  onLocate,
  onParent,
  onContinue,
  onCancel,
  disabled,
}: {
  request: SpaceRequest;
  view: WorkbenchView;
  c: Collaboration;
  onLocate: (ref: LibraryReference) => void;
  onParent: (id: string) => void;
  onContinue: () => void;
  onCancel: () => void;
  disabled: boolean;
}) {
  const target = view.targets.find((t) => t.id === r.targetId);
  return (
    <article className="panel workbench-request">
      <div className="workbench-request-meta">
        <span>
          {r.actor.name}
          {r.actor.kind === "session" ? ` · ${r.actor.provider}` : ""} →{" "}
          {target?.name || t("已移除的会话")}
        </span>
        <time dateTime={r.createdAt}>
          {formatDate(r.createdAt, {
            month: "short",
            day: "numeric",
            hour: "2-digit",
            minute: "2-digit",
          })}
        </time>
      </div>
      {r.bootstrap && (
        <span className="workbench-tag">{t("专用会话接手")}</span>
      )}
      <h3>{r.instruction}</h3>
      <References c={c} refs={r.references || []} onLocate={onLocate} />
      {r.parentRequestId && (
        <button
          className="text-link"
          onClick={() => onParent(r.parentRequestId!)}
        >
          {t("查看关联请求")}
        </button>
      )}
      <AgentRequestStatus request={r} />
      <div className="workbench-request-meta">
        <span>
          {r.receivedAt
            ? tr`已读取：${formatDate(r.receivedAt)}`
            : t("尚无读取回执")}
        </span>
        {r.finishedAt && (
          <span>{tr`已报告结果：${formatDate(r.finishedAt)}`}</span>
        )}
      </div>
      {r.bootstrap && (
        <details>
          <summary>{tr`启动简报 · 版本 ${r.bootstrap.brief.revision}`}</summary>
          <p>{r.bootstrap.brief.topic || t("议题尚未填写")}</p>
          <p className="small-text muted">{r.bootstrap.rules}</p>
        </details>
      )}
      <div className="workbench-card-actions">
        <button
          className="button small"
          disabled={disabled}
          onClick={onContinue}
        >
          {t("基于此请求继续协作")}
        </button>
        {r.state === "queued" &&
          (view.selfId === "owner" || view.selfId === r.actor.memberId) && (
            <button
              className="text-link"
              disabled={disabled}
              onClick={onCancel}
            >
              {t("取消待投递请求")}
            </button>
          )}
      </div>
    </article>
  );
}

function BriefPanel({
  c,
  brief,
  disabled,
  onSaved,
  onLocate,
}: {
  c: Collaboration;
  brief: SpaceBrief;
  disabled: boolean;
  onSaved: () => void;
  onLocate: (ref: LibraryReference) => void;
}) {
  const [draft, setDraft] = useState<SpaceBrief>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const options: LibraryReference[] = (c.materials || [])
    .filter((m) => !m.withdrawnAt)
    .flatMap((m) =>
      m.versions.map((v) => ({
        spaceId: c.id,
        kind: "material" as const,
        materialId: m.id,
        version: v.version,
      })),
    );
  options.push(
    ...(c.annotations || [])
      .filter((a) => !a.target || a.target.kind === "material")
      .map((a) => ({
        spaceId: c.id,
        kind: "annotation" as const,
        annotationId: a.id,
      })),
  );
  async function save() {
    if (!draft || busy) return;
    setBusy(true);
    setError("");
    try {
      await api(`collaborations/${c.id}/workbench/brief`, {
        baseRevision: draft.revision,
        brief: draft,
      });
      setDraft(undefined);
      onSaved();
    } catch (e) {
      setError(errorText(e));
    } finally {
      setBusy(false);
    }
  }
  const items = (kind: "decisions" | "questions", label: string) => (
    <section className="workbench-brief-group">
      <h3>{label}</h3>
      {(draft || brief)[kind]?.map((item, i) => (
        <div className="workbench-brief-item" key={i}>
          {draft ? (
            <>
              <label>
                {tr`${label} ${i + 1}`}
                <textarea
                  value={item.text}
                  maxLength={1000}
                  rows={2}
                  onChange={(e) =>
                    setDraft({
                      ...draft,
                      [kind]: draft[kind].map((v, index) =>
                        index === i ? { ...v, text: e.target.value } : v,
                      ),
                    })
                  }
                />
              </label>
              <label>
                {t("关联已发布来源")}
                <select
                  multiple
                  value={item.sources?.map((ref) => JSON.stringify(ref)) || []}
                  onChange={(e) => {
                    const sources = Array.from(e.target.selectedOptions).map(
                      (o) => JSON.parse(o.value) as LibraryReference,
                    );
                    setDraft({
                      ...draft,
                      [kind]: draft[kind].map((v, index) =>
                        index === i ? { ...v, sources } : v,
                      ),
                    });
                  }}
                >
                  {options.map((ref) => (
                    <option
                      key={JSON.stringify(ref)}
                      value={JSON.stringify(ref)}
                    >
                      {referenceName(c, ref)}
                    </option>
                  ))}
                </select>
              </label>
              <button
                className="text-link"
                onClick={() =>
                  setDraft({
                    ...draft,
                    [kind]: draft[kind].filter((_, index) => index !== i),
                  })
                }
              >
                {t("移除条目")}
              </button>
            </>
          ) : (
            <>
              <p>{item.text}</p>
              <References c={c} refs={item.sources || []} onLocate={onLocate} />
            </>
          )}
        </div>
      ))}
      {!(draft || brief)[kind]?.length && (
        <p className="muted">{t("尚未记录")}</p>
      )}
      {draft && (
        <button
          className="button small"
          disabled={draft[kind].length >= 32}
          onClick={() =>
            setDraft({
              ...draft,
              [kind]: [...draft[kind], { text: "", sources: [] } as BriefItem],
            })
          }
        >{tr`添加${label}`}</button>
      )}
    </section>
  );
  return (
    <section className="panel workbench-brief">
      <div className="workbench-section-heading">
        <div>
          <h2>{t("空间简报")}</h2>
          <p className="muted">{tr`版本 ${brief.revision} · 由成员确认，所有参与会话均可读取。`}</p>
        </div>
        {!draft && (
          <button
            className="button"
            disabled={disabled}
            onClick={() => {
              setDraft(structuredClone(brief));
              setError("");
            }}
          >
            {t("编辑简报")}
          </button>
        )}
      </div>
      <p>
        {t(
          "用简短的议题、已确认决定和待解决事项，让后来的成员与会话知道从哪里接手。",
        )}
      </p>
      {draft ? (
        <label>
          {t("当前议题")}
          <textarea
            value={draft.topic}
            rows={3}
            maxLength={2000}
            onChange={(e) => setDraft({ ...draft, topic: e.target.value })}
          />
        </label>
      ) : (
        <div className="workbench-brief-topic">
          <h3>{t("当前议题")}</h3>
          <p>{brief.topic || t("议题尚未填写")}</p>
        </div>
      )}
      {items("decisions", t("已确认决定"))}
      {items("questions", t("待解决事项"))}
      <ErrorBox message={error} />
      {draft && (
        <>
          <p className="muted">
            {t("保存表示你已核对这些内容。会话建议需要由成员确认后纳入。")}
          </p>
          {brief.revision !== draft.revision && (
            <p className="notice">
              {t(
                "其他成员已更新简报。请保留草稿内容，取消编辑后读取最新版本再合并。",
              )}
            </p>
          )}
          <div className="workbench-card-actions">
            <button
              className="button primary"
              disabled={busy || disabled || brief.revision !== draft.revision}
              onClick={() => void save()}
            >
              {t("保存确认后的简报")}
            </button>
            <button
              className="button"
              disabled={busy}
              onClick={() => setDraft(undefined)}
            >
              {t("取消编辑")}
            </button>
          </div>
        </>
      )}
    </section>
  );
}

function SpaceSessions({
  c,
  view,
  disabled,
  onChanged,
  onSend,
  onRemove,
}: {
  c: Collaboration;
  view: WorkbenchView;
  disabled: boolean;
  onChanged: () => void;
  onSend: (id: string) => void;
  onRemove: (id: string) => void;
}) {
  const pairs = useResource<AgentPairing[]>("agent-pairings", 3000);
  const receivers = useResource<SpaceReceiver[]>(
    `space-receivers?spaceId=${c.id}`,
    2500,
  );
  const [selected, setSelected] = useState("");
  const [pairing, setPairing] = useState(false);
  const [name, setName] = useState(t("空间协作助手"));
  const [creatingID, setCreatingID] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  async function associate(pairingId: string) {
    setBusy(true);
    setError("");
    try {
      await api(`collaborations/${c.id}/workbench/register`, { pairingId });
      setPairing(false);
      onChanged();
    } catch (e) {
      setError(errorText(e));
    } finally {
      setBusy(false);
    }
  }
  async function create() {
    if (busy) return;
    const requestId = creatingID || crypto.randomUUID();
    setCreatingID(requestId);
    setBusy(true);
    setError("");
    try {
      await api(`collaborations/${c.id}/workbench/create-receiver`, {
        requestId,
        name: name.trim(),
      });
      receivers.reload();
      pairs.reload();
      onChanged();
      setCreatingID("");
    } catch (e) {
      setError(errorText(e));
      receivers.reload();
    } finally {
      setBusy(false);
    }
  }
  return (
    <div className="workbench-sessions">
      <section className="panel">
        <h2>{t("参与会话")}</h2>
        <p className="muted">
          {t("仅显示成员明确关联的接收会话。原生历史和私人配对列表不会公开。")}
        </p>
        <div className="workbench-targets">
          {view.targets
            .filter((target) => !target.removed)
            .map((target) => {
              const assistantBlocked =
                view.assistant.targetId === target.id &&
                !["ready", "disabled"].includes(view.assistant.state);
              return (
                <article className="workbench-target" key={target.id}>
                  <div>
                    <h3>{target.name}</h3>
                    <p className="muted">
                      {target.member} · {target.provider}
                      {target.execution ? ` · ${t("共同执行")}` : ""}
                    </p>
                    <p
                      className={
                        target.available && !assistantBlocked
                          ? "workbench-online"
                          : "muted"
                      }
                    >
                      {assistantBlocked
                        ? assistantLabel(view.assistant)
                        : target.available
                          ? t("可以接收")
                          : serviceText(
                              target.reason,
                              "接收会话暂不可用，请核对客户端与连接状态。",
                            )}
                    </p>
                  </div>
                  <div className="workbench-card-actions">
                    <button
                      className="button small"
                      disabled={
                        disabled || !target.available || assistantBlocked
                      }
                      onClick={() => onSend(target.id)}
                    >
                      {t("发起协作请求")}
                    </button>
                    {(view.selfId === "owner" ||
                      view.selfId === target.memberId) && (
                      <button
                        className="text-link"
                        disabled={disabled}
                        onClick={() => onRemove(target.id)}
                      >
                        {t("解除关联")}
                      </button>
                    )}
                  </div>
                </article>
              );
            })}
        </div>
        {!view.targets.some((t) => !t.removed) && (
          <p className="workbench-empty">
            {t("还没有接收会话。先关联一个已配对会话，或创建独立接收会话。")}
          </p>
        )}
      </section>
      <section className="panel workbench-associate">
        <h3>{t("关联我的接收会话")}</h3>
        <p>{t("关联后，空间成员可向它发送明确请求。接收会话仍由你掌握。")}</p>
        <label>
          {t("本机已配对会话")}
          <select
            value={selected}
            disabled={disabled || busy}
            onChange={(e) => setSelected(e.target.value)}
          >
            <option value="">{t("选择接收会话")}</option>
            {pairs.data
              ?.filter(
                (p) =>
                  p.state === "paired" && (!p.spaceId || p.spaceId === c.id),
              )
              .map((p) => (
                <option value={p.id} key={p.id}>
                  {p.name}
                </option>
              ))}
          </select>
        </label>
        <div className="workbench-card-actions">
          <button
            className="button"
            disabled={disabled || busy || !selected}
            onClick={() => void associate(selected)}
          >
            {t("关联到本空间")}
          </button>
          <button
            className="text-link"
            disabled={disabled || busy}
            onClick={() => setPairing(!pairing)}
          >
            {t("配对新会话")}
          </button>
        </div>
        {pairing && <PairConversation onPaired={(id) => void associate(id)} />}
      </section>
      <section className="panel workbench-associate">
        <h3>{t("创建独立接收会话")}</h3>
        <p>
          {t(
            "新建一个只绑定本空间的原生 Codex 会话，使用独立工作目录。创建后可接收协作请求，也可被选为空间专用会话。",
          )}
        </p>
        <p className="muted small-text">
          {t("继承本机原生模型设置，默认受限权限；需要原生确认时由你处理。")}
        </p>
        <label>
          {t("接收会话名称")}
          <input
            value={name}
            maxLength={80}
            disabled={busy || !!creatingID}
            onChange={(e) => setName(e.target.value)}
          />
        </label>
        <button
          className="button"
          disabled={disabled || busy || !name.trim()}
          onClick={() => void create()}
        >
          {busy
            ? t("正在创建…")
            : creatingID
              ? t("核对同一次创建")
              : t("创建并关联接收会话")}
        </button>
      </section>
      <ErrorBox message={error || pairs.error || receivers.error} />
      {receivers.data?.map((receiver) => (
        <ReceiverCard
          key={receiver.id}
          receiver={receiver}
          onChanged={receivers.reload}
          disabled={disabled}
        />
      ))}
    </div>
  );
}

function ReceiverCard({
  receiver: r,
  onChanged,
  disabled,
}: {
  receiver: SpaceReceiver;
  onChanged: () => void;
  disabled: boolean;
}) {
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const events = useResource<{
    approvals: { id: unknown; method: string; params: { message?: string } }[];
  }>(r.approvals ? `space-receivers/${r.id}/events` : null, 2000);
  async function call(action: string, body: object) {
    setBusy(true);
    setError("");
    try {
      await api(`space-receivers/${r.id}/${action}`, body);
      onChanged();
      events.reload();
    } catch (e) {
      setError(errorText(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <section className="panel workbench-receiver">
      <div className="workbench-section-heading">
        <h3>{r.name}</h3>
        <span>
          {r.online ? (r.busy ? t("正在处理") : t("已连接")) : t("未连接")}
        </span>
      </div>
      <p className="small-text muted">
        {t("仅你可见的原生会话控制")} · {r.model || t("模型尚未确认")}
        {r.reasoningEffort ? ` · ${r.reasoningEffort}` : ""}
      </p>
      {!r.online && r.state === "ready" && (
        <button
          className="button"
          disabled={disabled || busy}
          onClick={() => void call("action", { action: "start" })}
        >
          {t("恢复接收会话")}
        </button>
      )}
      {r.command && (
        <details>
          <summary>{t("在原生终端中打开")}</summary>
          <p>{t("在本机终端运行此命令，查看会话、调整模型或处理原生交互。")}</p>
          <Copy text={r.command} />
        </details>
      )}
      {events.data?.approvals.map((a) => (
        <div className="notice" key={JSON.stringify(a.id)}>
          <p>
            {a.params.message || t("原生会话正在等待确认，请在终端中处理。")}
          </p>
          {a.method === "mcpServer/elicitation/request" && (
            <div className="workbench-card-actions">
              <button
                className="button"
                disabled={busy || disabled}
                onClick={() =>
                  void call("respond", {
                    id: a.id,
                    result: { action: "accept", content: {} },
                  })
                }
              >
                {t("允许这次工具调用")}
              </button>
              <button
                className="button"
                disabled={busy}
                onClick={() =>
                  void call("respond", {
                    id: a.id,
                    result: { action: "decline" },
                  })
                }
              >
                {t("拒绝这次工具调用")}
              </button>
            </div>
          )}
        </div>
      ))}
      <ErrorBox message={error || serviceText(r.error)} />
    </section>
  );
}

function AssistantSetup({
  c,
  view,
  disabled,
  onChanged,
  onSessions,
  onRequest,
}: {
  c: Collaboration;
  view: WorkbenchView;
  disabled: boolean;
  onChanged: () => void;
  onSessions: () => void;
  onRequest: (id: string) => void;
}) {
  const [target, setTarget] = useState(view.assistant.targetId || "");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const assistant = view.assistant;
  const selected = view.targets.find((t) => t.id === target);
  async function change(state: string) {
    if (busy) return;
    setBusy(true);
    setError("");
    try {
      await api(`collaborations/${c.id}/workbench/assistant`, {
        state,
        baseEpoch: assistant.epoch,
        targetId: target,
        requestId: crypto.randomUUID(),
      });
      onChanged();
    } catch (e) {
      setError(errorText(e));
      onChanged();
    } finally {
      setBusy(false);
    }
  }
  return (
    <div className="workbench-assistant">
      <p>
        {t(
          "每个空间最多一个。专用会话帮助理解简报、组织材料和发起已授权的协作；其他会话仍可直接互相协作。",
        )}
      </p>
      <p className="notice">
        {assistantLabel(assistant)}
        {assistant.targetId &&
          ` · ${view.targets.find((t) => t.id === assistant.targetId)?.name || t("已移除的会话")}`}
      </p>
      <div className="workbench-bootstrap-preview">
        <h3>{t("启动时会交给它的信息")}</h3>
        <ul>
          <li>{tr`当前议题与已确认简报 · 版本 ${view.brief.revision}`}</li>
          <li>{t("已发布材料目录与固定版本，不自动导入私人历史")}</li>
          <li>{t("明确关联的接收会话、待处理请求和当前进展")}</li>
          <li>{t("按需介入的规则：先读取并确认接手，再响应明确请求")}</li>
        </ul>
        <p>
          {view.brief.topic || t("议题尚未填写；它会把缺失信息标记为未知。")}
        </p>
      </div>
      {c.role === "owner" ? (
        <>
          <label>
            {t("专用接收会话")}
            <select
              value={target}
              disabled={busy || disabled}
              onChange={(e) => setTarget(e.target.value)}
            >
              <option value="">{t("选择接收会话")}</option>
              {view.targets
                .filter((t) => !t.removed && !t.execution)
                .map((t) => (
                  <option key={t.id} value={t.id}>
                    {t.name} · {t.member}
                  </option>
                ))}
            </select>
          </label>
          <button className="text-link" onClick={onSessions}>
            {t("创建或关联独立接收会话")}
          </button>
          <div className="workbench-card-actions">
            <button
              className="button primary"
              disabled={
                disabled ||
                busy ||
                !selected?.available ||
                (assistant.state === "initializing" &&
                  target === assistant.targetId)
              }
              onClick={() => void change("initializing")}
            >
              {assistant.state === "disabled"
                ? t("启用并发送启动简报")
                : target !== assistant.targetId
                  ? t("更换并请求接手")
                  : t("重新读取简报并接手")}
            </button>
            {assistant.state !== "disabled" && (
              <>
                <button
                  className="button"
                  disabled={disabled || busy || assistant.state === "paused"}
                  onClick={() => void change("paused")}
                >
                  {t("暂停介入")}
                </button>
                <button
                  className="text-link"
                  disabled={disabled || busy}
                  onClick={() => void change("disabled")}
                >
                  {t("停用专用会话")}
                </button>
              </>
            )}
          </div>
        </>
      ) : (
        <p>{t("由空间发起者启用、暂停或更换专用会话。")}</p>
      )}
      {assistant.bootstrapId && (
        <button
          className="text-link"
          onClick={() => onRequest(assistant.bootstrapId!)}
        >
          {t("查看接手请求与回执")}
        </button>
      )}
      <p className="small-text muted">
        {t(
          "暂停或停用后保留原生会话与已发生的结果；正在执行的输入需在原生会话中处理。",
        )}
      </p>
      <ErrorBox message={error} />
    </div>
  );
}
