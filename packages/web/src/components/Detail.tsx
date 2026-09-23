import {
  transportName,
  runtimeModeName,
  runtimeModeDescription,
} from "../types";
import { useEffect, useRef, useState } from "react";
import { APIError, api, errorText, useResource } from "../api";
import {
  type ClientPlan,
  type Collaboration,
  type ShareTransport,
  projectName,
} from "../types";
import { Clients } from "./Clients";
import { Materials } from "./Materials";
import { InvitationPanel } from "./InvitationPanel";
import { ReadOnlySpace } from "./ReadOnlySpace";
import { Members } from "./Members";
import { Context } from "./Context";
import { ReadingTabs, type ReadingTab } from "./ReadingTabs";
import { Annotations, type AnnotationRequest } from "./Annotations";
import {
  Badge,
  Empty,
  ErrorBox,
  Icon,
  Loading,
  Modal,
  PageHeading,
} from "./ui";

function TechnicalInformation({
  collaboration: c,
  agentName,
}: {
  collaboration: Collaboration;
  agentName: string;
}) {
  return (
    <div className="context-technical">
      <div className="context-technical-heading">
        <span className="entry-icon">
          <Icon name="terminal" size={20} />
        </span>
        <div>
          <h3>运行与会话</h3>
          <p>当前协作记录与运行时最近确认的信息。</p>
        </div>
      </div>
      <dl>
        <div>
          <dt>协作模式</dt>
          <dd>{runtimeModeName(c.runtimeMode)} · 创建后固定</dd>
        </div>
        <div>
          <dt>原生客户端</dt>
          <dd>
            {agentName}
            {c.provider === "claude" ? " · 实验性" : ""}
          </dd>
        </div>
        <div>
          <dt>协作 ID</dt>
          <dd>{c.id}</dd>
        </div>
        <div>
          <dt>来源会话</dt>
          <dd>{c.sourceId}</dd>
        </div>
        <div>
          <dt>协作会话</dt>
          <dd>{c.sessionId}</dd>
        </div>
        <div>
          <dt>Git 分支</dt>
          <dd>{c.branch}</dd>
        </div>
        <div>
          <dt>基线提交</dt>
          <dd>{c.head}</dd>
        </div>
        <div>
          <dt>{c.online ? "当前模型" : "最近确认的模型"}</dt>
          <dd>{c.model || `等待 ${agentName} 确认`}</dd>
        </div>
        <div>
          <dt>推理强度</dt>
          <dd>{c.reasoningEffort || `${agentName} 默认`}</dd>
        </div>
      </dl>
    </div>
  );
}

export function Detail({ id }: { id: string }) {
  const resource = useResource<Collaboration>(`collaborations/${id}`, 2500);
  const c = resource.data;
  const [modal, setModal] = useState<
    "clients" | "assist" | "invite" | "end" | null
  >(() => (sessionStorage.getItem(`teamcross.invite.${id}`) ? "invite" : null));
  const [busy, setBusy] = useState("");
  const [shareTransport, setShareTransport] = useState<ShareTransport>(() =>
    sessionStorage.getItem(`teamcross.transport.${id}`) === "tailcat"
      ? "tailcat"
      : "lan",
  );
  const [personalOpenNote, setPersonalOpenNote] = useState("");
  const [error, setError] = useState(
    () => sessionStorage.getItem(`teamcross.create.${id}`) || "",
  );
  const actionSerial = useRef(0);
  useEffect(() => {
    sessionStorage.removeItem(`teamcross.create.${id}`);
    sessionStorage.removeItem(`teamcross.invite.${id}`);
    sessionStorage.removeItem(`teamcross.transport.${id}`);
    setPersonalOpenNote("");
  }, [id]);
  useEffect(() => {
    if (c?.transport) setShareTransport(c.transport);
  }, [c?.transport]);
  const [annotationRequest, setAnnotationRequest] =
    useState<AnnotationRequest>();
  const [annotationLocation, setAnnotationLocation] =
    useState<AnnotationRequest>();
  const [readingTab, setReadingTab] = useState<ReadingTab>();
  function locateAnnotation(location: AnnotationRequest) {
    setReadingTab(
      location.target?.kind === "material" ? "materials" : "context",
    );
    setAnnotationLocation(location);
  }
  async function action(
    value: string,
    transport?: ShareTransport,
    memberId?: string,
  ) {
    if (!c) return;
    const serial = ++actionSerial.current;
    setBusy(value);
    setError("");
    try {
      await api(`collaborations/${id}/action`, {
        action: value,
        ...(transport ? { transport } : {}),
        epoch: c.epoch,
        ...(memberId ? { memberId } : {}),
      });
      if (serial !== actionSerial.current) return;
      resource.reload();
      if (value === "share") setModal("invite");
      if (value === "end") setModal(null);
      if (value === "start" && c.runtimeState === "released")
        setModal("clients");
    } catch (e) {
      if (
        serial === actionSerial.current &&
        !(e instanceof APIError && e.code === "sharing_cancelled")
      )
        setError(errorText(e));
    } finally {
      if (serial === actionSerial.current) setBusy("");
    }
  }
  async function manage(path: string, body: object) {
    setBusy(path);
    setError("");
    try {
      await api(`collaborations/${id}/${path}`, body);
      resource.reload();
    } catch (e) {
      setError(errorText(e));
    } finally {
      setBusy("");
    }
  }
  async function openPersonalCodex() {
    setBusy("personal-desktop");
    setError("");
    setPersonalOpenNote("");
    try {
      const result = await api<ClientPlan>(
        `collaborations/${id}/personal-desktop`,
        { launch: true },
      );
      setPersonalOpenNote(result.note || "已请求个人 Codex 打开此协作会话。");
    } catch (e) {
      setError(errorText(e));
    } finally {
      setBusy("");
    }
  }
  if (!c)
    return (
      <>
        <a href="#/" className="back-link">
          <Icon name="back" size={16} />
          协作空间
        </a>
        <ErrorBox message={resource.error} retry={resource.reload} />
        {resource.loading && <Loading />}
      </>
    );
  if (c.hasExecution === false)
    return (
      <ReadOnlySpace
        collaboration={c}
        reload={resource.reload}
        update={resource.setData}
        showInvite={modal === "invite"}
      />
    );
  const agentName = c.provider === "claude" ? "Claude Code" : "Codex";
  const waiting = c.approvals > 0 || !!c.nativeWaiting;
  const owner = c.role === "owner";
  const mine = (c.selfId || c.role) === c.writer;
  const writerName =
    c.writer === "owner"
      ? "发起者"
      : c.members?.find((m) => m.id === c.writer)?.name || "同事";
  const closed = ["ended", "left", "expired"].includes(c.state);
  const canOpenPersonalCodex =
    owner &&
    c.provider !== "claude" &&
    !!c.sessionId &&
    c.sessionId !== c.sourceId;
  const canContinueInPersonalCodex =
    !c.sharing && c.runtimeState === "released";
  const connectionName = transportName(c.transport);
  const sharingState = c.sharingPreparing
    ? `正在建立${connectionName}通道`
    : c.sharing
      ? `${connectionName}共享中`
      : "共享已关闭";
  const sharingDescription = c.sharingPreparing
    ? c.transport === "tailcat"
      ? "正在连接 Tailcat DERP 并生成临时跨网络地址；可以取消或等待完成。"
      : "正在生成局域网地址与临时 TLS 邀请。"
    : c.participantJoined && c.sharing
      ? `${owner ? "同事" : "你"}已加入，访问持续有效，直到主动离开或结束共享。`
      : c.sharing && c.expiresAt
        ? `首次加入期限：${new Date(c.expiresAt).toLocaleTimeString("zh-CN", { hour: "2-digit", minute: "2-digit" })}。加入后不受此期限影响。`
        : c.sharing && c.invitationState === "expired"
          ? "邀请尚未使用且已到期，可以重新邀请同事。"
          : c.releasePending
            ? "当前执行和审批完成、专用客户端关闭后，会自动释放会话。"
            : "会话与代码保留在执行主机上，可随时重新打开。";
  return (
    <>
      <PageHeading
        eyebrow={
          <nav className="detail-breadcrumb" aria-label="当前位置">
            <a href="#/" className="back-link">
              <Icon name="back" size={16} />
              协作空间
            </a>
            <span aria-hidden="true">/</span>
            <span>{`${projectName(c.repo)} / ${owner ? "我发起的" : "我加入的"}`}</span>
          </nav>
        }
        title={c.title}
      >
        <Badge collaboration={c} />
        <details className="runtime-mode-details">
          <summary aria-label="查看协作模式说明">
            {runtimeModeName(c.runtimeMode)}
            <Icon name="chevron" size={14} />
          </summary>
          <div className="runtime-mode-popover" role="note">
            <strong>{runtimeModeName(c.runtimeMode)} · 创建后固定</strong>
            <p>{runtimeModeDescription(c.runtimeMode)}</p>
          </div>
        </details>
      </PageHeading>
      <ErrorBox
        message={error || resource.error || (!closed ? c.error : undefined)}
        retry={resource.reload}
      />
      <div className="execution-strip">
        <div>
          <Icon name="desktop" />
          <span>
            <small>执行主机</small>
            <strong title={c.host}>{c.host}</strong>
          </span>
        </div>
        <div>
          <Icon name={c.workspaceMode === "worktree" ? "branch" : "folder"} />
          <span>
            <small>
              {c.workspaceMode === "worktree" ? "独立 worktree" : "使用原目录"}
            </small>
            <strong className="path" title={c.executionCwd}>
              {c.executionCwd}
            </strong>
          </span>
        </div>
        <div>
          <span className={`avatar ${mine ? "self" : ""}`}>
            {mine ? "我" : "同"}
          </span>
          <span>
            <small>当前输入者</small>
            <strong>{closed ? "共享已关闭" : mine ? "我" : writerName}</strong>
          </span>
        </div>
        <div className="execution-sharing">
          <details className="sharing-inspector">
            <summary aria-label="查看共享状态详情" title={sharingDescription}>
              <span className={`share-state-icon ${c.sharing ? "active" : ""}`}>
                <Icon name="link" size={16} />
              </span>
              <span>
                <small>共享状态</small>
                <strong>{sharingState}</strong>
              </span>
              <Icon name="arrow" size={12} />
            </summary>
            <div className="sharing-inspector-popover">
              <div className="sharing-inspector-heading">
                <h3>共享状态</h3>
                <span className={`dot ${c.sharing ? "green-dot" : ""}`} />
              </div>
              <strong className="sharing-inspector-value">
                {sharingState}
              </strong>
              <p>{sharingDescription}</p>
              {owner && (c.sharing || c.sharingPreparing) ? (
                <button
                  className="text-link danger"
                  onClick={() => setModal("end")}
                >
                  {c.sharingPreparing ? "取消生成邀请" : "结束共享"}
                </button>
              ) : !owner && c.state !== "left" ? (
                <button
                  className="text-link"
                  disabled={!!busy}
                  onClick={() => void action("leave")}
                >
                  离开协作
                </button>
              ) : null}
            </div>
          </details>
        </div>
      </div>
      <section className="action-panel">
        <div className="action-copy">
          <span className={`status-orb ${c.online ? "active" : ""}`}>
            <Icon name={c.online ? "terminal" : "link"} size={24} />
          </span>
          <div>
            <h2>
              {closed
                ? c.state === "expired"
                  ? "这份邀请已到期"
                  : c.state === "left"
                    ? "你已离开协作"
                    : "这次共享已结束"
                : c.state === "error"
                  ? "协作需要处理"
                  : !c.online
                    ? owner
                      ? c.runtimeState === "releasing"
                        ? "正在释放协作会话"
                        : "恢复协作，继续上次的工作"
                      : "正在等待发起者连接"
                    : waiting
                      ? `${agentName} 需要你的回应`
                      : c.busy
                        ? `${agentName} 正在执行`
                        : mine
                          ? "准备好继续了"
                          : "同事正在掌握输入"}
            </h2>
            <p>
              {closed
                ? "会话与代码仍保留在发起者的 Mac 上。继续参与时，请向同事获取新邀请。"
                : !c.online
                  ? owner
                    ? c.runtimeState === "releasing"
                      ? "正在关闭这次协作的后台运行时，会话与代码继续保留。"
                      : "恢复同一个会话与目录，不会重新创建分支。"
                    : c.transport === "tailcat"
                      ? "确认双方网络可以访问 Tailcat DERP，并让发起者保持共享。"
                      : "确认两台 Mac 在同一局域网，并让发起者保持共享。"
                  : waiting
                    ? `请在当前 ${agentName} 客户端中查看并回应请求。`
                    : c.busy
                      ? `代码操作发生在 ${c.host}，可以在 ${agentName} 中补充或中断。`
                      : mine
                        ? c.connected
                          ? `${c.client || agentName} 已连接，继续在客户端中工作。`
                          : `打开 ${agentName} 客户端，接着已有上下文继续。`
                        : "先查看下方的共享上下文，需要操作时可以申请输入。"}
            </p>
          </div>
        </div>
        <div className="action-buttons">
          {closed ? (
            <a className="button primary" href="#/join">
              使用新邀请加入
              <Icon name="join" size={16} />
            </a>
          ) : c.state === "error" ? (
            <a className="button primary" href="#/settings">
              检查连接设置
            </a>
          ) : !c.online && owner && c.state === "ready" ? (
            <button
              className="button primary"
              disabled={!!busy || c.runtimeState === "releasing"}
              onClick={() => void action("start")}
            >
              {busy === "start"
                ? "正在恢复…"
                : c.runtimeState === "released"
                  ? `恢复并打开 ${agentName}`
                  : "恢复运行时"}
              <Icon name="refresh" size={16} />
            </button>
          ) : (
            <button
              className="button primary"
              onClick={() => setModal(mine ? "clients" : "assist")}
            >
              <Icon name={mine ? "terminal" : "people"} size={18} />
              {mine ? `打开 ${agentName}` : "用自己的客户端辅助"}
            </button>
          )}
          {canOpenPersonalCodex && (
            <button
              className="button"
              disabled={!!busy}
              onClick={() => void openPersonalCodex()}
              title="直接定位此协作，无需从 Desktop 列表中查找"
            >
              <Icon name="desktop" size={17} />
              {busy === "personal-desktop"
                ? "正在打开…"
                : canContinueInPersonalCodex
                  ? "在个人 Codex 中继续"
                  : "在个人 Codex 中打开"}
            </button>
          )}
          {owner && c.state !== "preparing" && (
            <button
              className="button"
              disabled={!!busy || c.sharingPreparing}
              onClick={() => setModal("invite")}
            >
              <Icon name="link" size={17} />
              {busy === "share"
                ? "正在生成…"
                : c.invitation
                  ? "邀请成员"
                  : "邀请成员"}
            </button>
          )}
        </div>
      </section>
      {personalOpenNote && (
        <p className="notice" role="status">
          {personalOpenNote}
        </p>
      )}
      <div className="detail-grid">
        <div className="detail-main">
          <ReadingTabs
            active={
              readingTab ??
              (c.materials?.some((m) => !m.withdrawnAt)
                ? "materials"
                : "context")
            }
            onChange={setReadingTab}
            materialCount={
              c.materials?.filter((m) => !m.withdrawnAt).length || 0
            }
            navigationKey={annotationLocation?.serial}
            materials={
              <Materials
                collaboration={c}
                reload={resource.reload}
                onAnnotate={(target, origin) =>
                  setAnnotationRequest({ target, origin, serial: Date.now() })
                }
                location={annotationLocation}
                onDiscuss={(annotationId, origin) =>
                  setAnnotationRequest({
                    annotationId,
                    origin,
                    serial: Date.now(),
                  })
                }
              />
            }
            context={
              <Context
                id={id}
                sessionId={c.sessionId}
                agentName={agentName}
                technical={
                  <TechnicalInformation
                    collaboration={c}
                    agentName={agentName}
                  />
                }
                canAnnotate={owner || (c.online && !closed)}
                onAnnotate={(target, origin) =>
                  setAnnotationRequest({ target, origin, serial: Date.now() })
                }
                location={
                  annotationLocation?.target?.kind === "material"
                    ? undefined
                    : annotationLocation
                }
                online={
                  c.online ||
                  (owner &&
                    c.state === "ready" &&
                    c.runtimeState !== "releasing" &&
                    c.runtimeState !== "starting")
                }
                annotations={c.annotations}
                onDiscuss={(annotationId, origin) =>
                  setAnnotationRequest({
                    annotationId,
                    origin,
                    serial: Date.now(),
                  })
                }
                sequence={c.sequence}
                closed={closed}
              />
            }
          />
        </div>
        <aside className="detail-aside">
          <Members
            collaboration={c}
            busy={!!busy}
            closed={closed}
            onAction={(value, memberId) =>
              void action(value, undefined, memberId)
            }
            onRemove={(memberId) => void manage("remove-member", { memberId })}
            onExecutionAccess={(memberId, allowed) =>
              void manage("execution-access", { memberId, allowed })
            }
            onAssist={() => setModal("assist")}
          />
          <Annotations
            key={id}
            id={id}
            annotations={c.annotations || []}
            materials={c.materials}
            onLocateMaterial={(ref) =>
              locateAnnotation({
                target: {
                  kind: "material",
                  materialId: ref.materialId,
                  version: ref.version,
                  turnId: ref.turnId,
                  quote: "",
                },
                serial: Date.now(),
              })
            }
            request={annotationRequest}
            disabled={!owner && (c.reachable === false || closed)}
            onSaved={(annotation) => {
              resource.setData((current) =>
                current
                  ? {
                      ...current,
                      annotations: current.annotations?.some(
                        (item) => item.id === annotation.id,
                      )
                        ? current.annotations.map((item) =>
                            item.id === annotation.id ? annotation : item,
                          )
                        : [...(current.annotations || []), annotation],
                    }
                  : current,
              );
              resource.reload();
            }}
            onLocate={(annotation) =>
              locateAnnotation({
                target: annotation.target,
                serial: Date.now(),
              })
            }
          />
        </aside>
      </div>
      {modal && (
        <Modal
          title={
            modal === "invite"
              ? "邀请成员"
              : modal === "end"
                ? c.sharingPreparing
                  ? "取消生成邀请？"
                  : "结束这次共享？"
                : modal === "assist"
                  ? "用个人客户端辅助"
                  : `用 ${agentName} 继续`
          }
          onClose={() => setModal(null)}
        >
          {modal === "invite" ? (
            <InvitationPanel
              collaboration={c}
              initialTransport={shareTransport}
              onUpdated={(value) => {
                resource.setData(value);
                resource.reload();
              }}
            />
          ) : modal === "end" ? (
            <>
              {c.sharingPreparing ? (
                <p>
                  将停止当前连接方式的准备过程。协作会话、执行目录和所有代码继续保留，之后可以重新选择连接方式。
                </p>
              ) : (
                <>
                  <p>
                    同事的直接连接和工具访问会关闭。协作会话、执行目录和所有代码继续保留。
                  </p>
                  <p>
                    当前执行和审批完成、专用客户端关闭后，会自动释放会话，之后可以从
                    Team Cross 恢复同一个会话。
                  </p>
                </>
              )}
              <div className="form-footer">
                <button className="button" onClick={() => setModal(null)}>
                  {c.sharingPreparing ? "继续等待" : "继续共享"}
                </button>
                <button
                  className="button danger-button"
                  disabled={!!busy}
                  onClick={() => void action("end")}
                >
                  {c.sharingPreparing ? "取消生成" : "结束共享"}
                </button>
              </div>
            </>
          ) : (
            <Clients
              collaboration={c}
              initial={modal === "assist" ? "assist" : "direct"}
            />
          )}
        </Modal>
      )}
    </>
  );
}
