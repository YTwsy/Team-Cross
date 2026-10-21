import { t, tr } from "../i18n";
import { useEffect, useRef, useState } from "react";
import { api, errorText } from "../api";
import { type Collaboration, type MaterialReference } from "../types";
import { Annotations, type AnnotationRequest } from "./Annotations";
import { Materials } from "./Materials";
import { Members } from "./Members";
import { Clients } from "./Clients";
import { Create } from "./Create";
import { ExecutionPanel } from "./ExecutionPanel";
import { InvitationPanel } from "./InvitationPanel";
import { SpaceWorkbench } from "./SpaceWorkbench";
import { SpaceAvatar, SpacePeople } from "./SpaceIdentity";
import { Badge, ErrorBox, Icon, Modal, PageHeading } from "./ui";

export function ReadOnlySpace({
  collaboration: c,
  reload,
  update,
  showInvite = false,
  executionId,
}: {
  collaboration: Collaboration;
  reload: () => void;
  update: (value: Collaboration) => void;
  showInvite?: boolean;
  executionId?: string;
}) {
  const [modal, setModal] = useState<
    "invite" | "end" | "assist" | "create" | null
  >(showInvite ? "invite" : null);
  const [visited, setVisited] = useState<string[]>(
    executionId ? [executionId] : [],
  );
  const positions = useRef(new Map<string, number>());
  useEffect(() => {
    if (executionId)
      setVisited((ids) =>
        ids.includes(executionId) ? ids : [...ids, executionId],
      );
    const frame = requestAnimationFrame(() =>
      window.scrollTo({
        top: positions.current.get(executionId || "materials") || 0,
        behavior: "instant",
      }),
    );
    const savePosition = () =>
      positions.current.set(executionId || "materials", window.scrollY);
    window.addEventListener("scroll", savePosition, { passive: true });
    return () => {
      cancelAnimationFrame(frame);
      window.removeEventListener("scroll", savePosition);
    };
  }, [executionId]);
  function selectExecution(next?: string) {
    positions.current.set(executionId || "materials", window.scrollY);
    window.location.hash = `/collaborations/${c.id}${next ? `/sessions/${next}` : ""}`;
  }
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [request, setRequest] = useState<AnnotationRequest>();
  const [location, setLocation] = useState<AnnotationRequest>();
  const owner = c.role === "owner";
  const closed = ["ended", "left", "expired"].includes(c.state);
  const disabled = closed || (!owner && c.reachable === false);
  function locateMaterial(ref: MaterialReference) {
    selectExecution();
    setLocation({
      target: {
        kind: "material",
        materialId: ref.materialId,
        version: ref.version,
        turnId: ref.turnId,
        quote: "",
      },
      serial: Date.now(),
    });
  }
  async function manage(path: string, body: object) {
    setBusy(true);
    setError("");
    try {
      const result = await api<Collaboration>(
        `collaborations/${c.id}/${path}`,
        body,
      );
      update(result);
      if (path === "action") setModal(null);
      reload();
    } catch (e) {
      setError(errorText(e));
    } finally {
      setBusy(false);
    }
  }
  if (closed && !owner) {
    return (
      <>
        <a href="#/" className="back-link">
          {t("返回协作空间") + " "}
        </a>
        <PageHeading title={c.title} subtitle={tr`托管主机：${c.host}`}>
          <Badge collaboration={c} />
        </PageHeading>
        <section className="panel readonly-intro">
          <h2>
            {c.state === "left" ? t("你已离开这个空间") : t("这次共享已结束")}
          </h2>
          <p>
            {t(
              "已发布材料与讨论仍保存在托管主机。继续参与时，请使用当前有效的空间邀请链接。",
            ) + " "}
          </p>
          <a className="button primary" href="#/join">
            {t("使用邀请链接加入") + " "}
          </a>
        </section>
      </>
    );
  }
  return (
    <>
      <header className="space-room-bar">
        <a
          href="#/"
          className="space-room-symbol"
          aria-label={t("返回协作空间")}
        >
          <Icon name="people" size={17} />
        </a>
        <div className="space-room-title">
          <h1 tabIndex={-1}>{c.title}</h1>
          <span>{t("材料、讨论与协作会话")}</span>
        </div>
        {closed && <Badge collaboration={c} />}
        <details className="sharing-inspector">
          <summary aria-label={t("查看共享状态详情")}>
            <Icon name="link" size={16} />
            {c.sharing ? t("共享中") : t("共享已关闭")}
          </summary>
          <div className="sharing-inspector-popover">
            <h3>{t("共享状态")}</h3>
            <p>
              {c.sharingPreparing
                ? t(
                    "正在连接 Tailcat DERP 并生成临时跨网络地址；可以取消或等待完成。",
                  )
                : c.participantJoined && c.sharing
                  ? tr`${owner ? t("同事") : t("你")}已加入，访问持续有效，直到主动离开或结束共享。`
                  : c.invitationState === "expired"
                    ? t("邀请尚未使用且已到期，可以重新邀请同事。")
                    : t(
                        "空间成员可阅读全部已发布材料和协作会话；输入权按会话单独交接。",
                      )}
            </p>
            {owner && c.sharingPreparing && (
              <button
                className="text-button"
                disabled={busy}
                onClick={() => void manage("action", { action: "end" })}
              >
                {t("取消生成邀请")}
              </button>
            )}
          </div>
        </details>
        <SpacePeople collaboration={c} />
        {owner && (
          <button
            className="button small"
            disabled={busy}
            onClick={() => setModal("invite")}
          >
            <Icon name="people" size={16} />
            {closed ? t("重新开放空间") : t("邀请成员")}
          </button>
        )}
      </header>
      <ErrorBox message={error || (!closed ? c.error : undefined)} />
      {closed && (
        <p className="notice">
          {t("空间已关闭。材料和讨论保留，重新开放后可以继续邀请同事。") + " "}
        </p>
      )}
      <SpaceWorkbench
        key={c.id}
        collaboration={c}
        readingSession={c.sessions?.find(
          (session) => session.id === executionId,
        )}
        contextNavigation={
          <nav className="execution-navigation" aria-label={t("协作会话")}>
            <div className="execution-nav-heading">
              <strong>{t("协作会话")}</strong>
              <span>{c.sessions?.length || 0}</span>
            </div>
            {(c.sessions || []).map((session) => (
              <button
                key={session.id}
                className={`execution-nav-item ${executionId === session.id ? "active" : ""}`}
                aria-current={executionId === session.id ? "page" : undefined}
                aria-label={`${session.title} ${session.provider === "claude" ? "Claude Code" : "Codex"} · ${session.busy ? t("正在执行") : session.paused ? t("已暂停") : session.online ? t("等待输入") : t("运行时未连接")}`}
                title={session.title}
                onClick={() => selectExecution(session.id)}
              >
                <SpaceAvatar
                  name={session.provider === "claude" ? "Claude Code" : "Codex"}
                  agent
                />
                <span className="execution-nav-copy">
                  <strong>{session.title}</strong>
                  <small>{`${session.provider === "claude" ? "Claude Code" : "Codex"} · ${session.busy ? t("正在执行") : session.paused ? t("已暂停") : session.online ? t("等待输入") : t("运行时未连接")}`}</small>
                </span>
              </button>
            ))}
            {!c.sessions?.length && (
              <p className="small-text muted">{t("还没有协作会话")}</p>
            )}
            {owner && !closed && (
              <button
                className="text-button"
                aria-label={t("新增协作会话")}
                title={t("新增协作会话")}
                onClick={() => setModal("create")}
              >
                <Icon name="plus" size={15} />
                <span>{t("新增协作会话")}</span>
              </button>
            )}
          </nav>
        }
        overview={
          <>
            <section className="panel readonly-intro">
              <div>
                <h2>{t("分享与审阅")}</h2>
                <p>
                  {t(
                    "空间保存已发布材料和讨论。每个人可以用自己的 Agent 分析，再把新的调查附回来。",
                  ) + " "}
                </p>
              </div>
              <div className="material-actions">
                {owner ? (
                  <>
                    {!closed && (
                      <button
                        className="button"
                        disabled={busy}
                        onClick={() => setModal("end")}
                      >
                        {t("关闭空间") + " "}
                      </button>
                    )}
                  </>
                ) : (
                  !closed && (
                    <button
                      className="button"
                      disabled={busy}
                      onClick={() => void manage("action", { action: "leave" })}
                    >
                      {t("离开空间") + " "}
                    </button>
                  )
                )}
                <button
                  className="button"
                  disabled={disabled}
                  onClick={() => setModal("assist")}
                >
                  {t("使用自己的 Agent 辅助") + " "}
                </button>
              </div>
            </section>
            <Members
              collaboration={c}
              busy={busy}
              closed={closed}
              onAction={() => {}}
              onRemove={(memberId) =>
                void manage("remove-member", { memberId })
              }
              onAssist={() => setModal("assist")}
            />
            <section className="panel optional-execution">
              <h2>{t("协作会话")}</h2>
              <p>
                {t(
                  "一个空间可以从多个来源继续执行。所有成员共同阅读，每个会话独立管理输入归属和运行状态。",
                )}
              </p>
              {owner && !closed && (
                <button className="button" onClick={() => setModal("create")}>
                  {t("新增协作会话")}
                </button>
              )}
              <p className="small-text muted">
                {t(
                  "空间成员可阅读全部已发布材料和协作会话；输入权按会话单独交接。",
                )}
              </p>
            </section>
          </>
        }
        comments={
          <Annotations
            id={c.id}
            annotations={c.annotations || []}
            materials={c.materials}
            request={request}
            disabled={disabled}
            onSaved={reload}
            onLocate={(annotation) => {
              selectExecution();
              setLocation({ target: annotation.target, serial: Date.now() });
            }}
            onLocateMaterial={locateMaterial}
          />
        }
        commentRequest={request}
        onLocate={(ref) => {
          if (ref.kind === "context" && ref.executionId) {
            selectExecution(ref.executionId);
            return;
          }
          selectExecution();
          if (ref.kind === "material")
            setLocation({
              target: {
                kind: "material",
                materialId: ref.materialId,
                version: ref.version,
                quote: "",
              },
              serial: Date.now(),
            });
          else {
            const note = c.annotations?.find((a) => a.id === ref.annotationId);
            setRequest({ annotationId: ref.annotationId, serial: Date.now() });
            if (note?.target)
              setLocation({ target: note.target, serial: Date.now() });
          }
        }}
      >
        <div hidden={!!executionId}>
          <Materials
            collaboration={c}
            onActivate={() => selectExecution()}
            reload={reload}
            onAnnotate={(target, origin) =>
              setRequest({ target, origin, serial: Date.now() })
            }
            onDiscuss={(annotationId, origin) =>
              setRequest({ annotationId, origin, serial: Date.now() })
            }
            location={location}
          />
        </div>
        {visited.map((id) => {
          const session = c.sessions?.find((s) => s.id === id);
          return session ? (
            <div key={id} hidden={executionId !== id}>
              <ExecutionPanel
                space={c}
                session={session}
                active={executionId === id}
                reload={reload}
                onLocateMaterial={locateMaterial}
              />
            </div>
          ) : null;
        })}
        {executionId && !c.sessions?.some((s) => s.id === executionId) && (
          <p className="notice">{t("此协作会话不存在或已不可访问。")}</p>
        )}
      </SpaceWorkbench>
      {modal && (
        <Modal
          title={
            modal === "create"
              ? t("新增协作会话")
              : modal === "invite"
                ? t("邀请成员")
                : modal === "end"
                  ? t("关闭这个空间？")
                  : t("使用个人 Agent 辅助")
          }
          onClose={() => setModal(null)}
        >
          {modal === "create" ? (
            <Create
              embedded
              spaceId={c.id}
              onCreated={(session) => {
                update({
                  ...c,
                  sessions: [
                    ...(c.sessions || []).filter((s) => s.id !== session.id),
                    session,
                  ],
                });
                setModal(null);
                reload();
                selectExecution(session.executionId || session.id);
              }}
            />
          ) : modal === "invite" ? (
            <InvitationPanel
              collaboration={c}
              onUpdated={(value) => {
                update(value);
                reload();
              }}
            />
          ) : modal === "end" ? (
            <>
              <p>
                {t(
                  "关闭空间与所有会话的远端访问，保留材料、讨论、原生历史与工作目录。已接收的执行完成后释放运行时。",
                ) + " "}
              </p>
              <button
                className="button danger-button"
                disabled={busy}
                onClick={() => void manage("action", { action: "end" })}
              >
                {t("确认关闭空间") + " "}
              </button>
            </>
          ) : (
            <Clients collaboration={c} initial="assist" />
          )}
        </Modal>
      )}
    </>
  );
}
