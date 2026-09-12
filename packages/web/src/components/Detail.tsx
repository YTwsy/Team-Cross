import { invitationText } from "../types";
import { useEffect, useState } from "react";
import { api, errorText, useResource } from "../api";
import { type Collaboration, projectName } from "../types";
import { Clients } from "./Clients";
import { Context } from "./Context";
import { Annotations, type AnnotationRequest } from "./Annotations";
import {
  Badge,
  Copy,
  Empty,
  ErrorBox,
  Icon,
  Loading,
  Modal,
  PageHeading,
} from "./ui";
export function Detail({ id }: { id: string }) {
  const resource = useResource<Collaboration>(`collaborations/${id}`, 2500);
  const c = resource.data;
  const [modal, setModal] = useState<
    "clients" | "assist" | "invite" | "end" | null
  >(() => (sessionStorage.getItem(`teamcross.invite.${id}`) ? "invite" : null));
  const [busy, setBusy] = useState("");
  const [error, setError] = useState(
    () => sessionStorage.getItem(`teamcross.create.${id}`) || "",
  );
  useEffect(() => {
    sessionStorage.removeItem(`teamcross.create.${id}`);
    sessionStorage.removeItem(`teamcross.invite.${id}`);
  }, [id]);
  const [annotationRequest, setAnnotationRequest] =
    useState<AnnotationRequest>();
  const [annotationLocation, setAnnotationLocation] =
    useState<AnnotationRequest>();
  async function action(value: string) {
    if (!c) return;
    setBusy(value);
    setError("");
    try {
      await api(`collaborations/${id}/action`, {
        action: value,
        epoch: c.epoch,
      });
      resource.reload();
      if (value === "share") setModal("invite");
      if (value === "end") setModal(null);
      if (value === "start" && c.runtimeState === "released")
        setModal("clients");
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
  const agentName = c.provider === "claude" ? "Claude Code" : "Codex";
  const waiting = c.approvals > 0 || !!c.nativeWaiting;
  const owner = c.role === "owner";
  const mine = c.role === c.writer;
  const closed = ["ended", "left", "expired"].includes(c.state);
  return (
    <>
      <a href="#/" className="back-link">
        <Icon name="back" size={16} />
        协作空间
      </a>
      <PageHeading
        eyebrow={`${projectName(c.repo)} / ${owner ? "我发起的" : "我加入的"}`}
        title={c.title}
      >
        <Badge collaboration={c} />
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
            <strong>
              {closed ? "共享已关闭" : mine ? "我" : owner ? "同事" : "发起者"}
            </strong>
          </span>
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
          {owner && c.online && !c.participantJoined && (
            <button
              className="button"
              disabled={!!busy}
              onClick={() =>
                c.invitation ? setModal("invite") : void action("share")
              }
            >
              <Icon name="link" size={17} />
              {busy === "share" ? "正在生成…" : "邀请同事"}
            </button>
          )}
        </div>
      </section>
      <div className="detail-grid">
        <div className="detail-main">
          <Context
            id={id}
            sessionId={c.sessionId}
            agentName={agentName}
            canAnnotate={owner || (c.online && !closed)}
            onAnnotate={(target) =>
              setAnnotationRequest({ target, serial: Date.now() })
            }
            location={annotationLocation}
            online={
              c.online ||
              (owner &&
                c.state === "ready" &&
                c.runtimeState !== "releasing" &&
                c.runtimeState !== "starting")
            }
            sequence={c.sequence}
            closed={closed}
          />
        </div>
        <aside className="detail-aside">
          <Annotations
            key={id}
            id={id}
            annotations={c.annotations || []}
            request={annotationRequest}
            disabled={!owner && (!c.online || closed)}
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
              setAnnotationLocation({
                target: annotation.target,
                serial: Date.now(),
              })
            }
          />
          <section className="panel">
            <h3>参与协作</h3>
            <div className="participant">
              <span className="avatar self">{owner ? "我" : "A"}</span>
              <div>
                <strong>{owner ? "我" : "发起者"}</strong>
                <span>
                  执行主机 · {c.writer === "owner" ? "正在输入" : "可查看"}
                </span>
              </div>
            </div>
            <div className="participant">
              <span className="avatar">{owner ? "同" : "我"}</span>
              <div>
                <strong>{owner ? "同事" : "我"}</strong>
                <span>
                  {c.sharing
                    ? c.writer === "remote"
                      ? "已获输入权"
                      : c.participantOnline
                        ? "已加入 · 在线查看"
                        : c.participantJoined
                          ? "已加入 · 暂时离线"
                          : c.invitationState === "left"
                            ? "同事已离开"
                            : "等待同事加入"
                    : "共享未开启"}
                </span>
              </div>
            </div>
            {owner && c.inputRequested && (
              <div className="notice" role="status">
                同事正在申请输入，请在准备好后交接。
              </div>
            )}
            {owner && c.sharing && (
              <button
                className="button full-width"
                disabled={
                  !!busy || (mine && (c.busy || c.participantJoined === false))
                }
                onClick={() => void action(mine ? "handoff" : "reclaim")}
              >
                <Icon name="people" size={16} />
                {mine ? "将输入交给同事" : "接回输入"}
              </button>
            )}
            {!owner && c.sharing && !mine && (
              <button
                className="button full-width"
                disabled={!!busy || !c.online}
                onClick={() =>
                  void action(
                    c.inputRequested ? "cancel_input" : "request_input",
                  )
                }
              >
                {c.inputRequested ? "取消输入申请" : "申请输入"}
              </button>
            )}
            {!owner && c.sharing && mine && (
              <button
                className="button full-width"
                disabled={!!busy || c.busy}
                onClick={() => void action("return")}
              >
                <Icon name="people" size={16} />
                交还输入
              </button>
            )}
            {owner && c.sharing && c.busy && mine && (
              <p className="small-text muted">当前轮完成后可以交出输入。</p>
            )}
            <button
              className="text-link small-text"
              disabled={closed}
              onClick={() => setModal("assist")}
            >
              使用自己的客户端辅助 <Icon name="arrow" size={14} />
            </button>
          </section>
          <section className="panel">
            <div className="panel-heading">
              <h3>共享状态</h3>
              <span className={`dot ${c.sharing ? "green-dot" : ""}`} />
            </div>
            <p>{c.sharing ? "局域网共享中" : "共享已关闭"}</p>
            <p className="muted small-text">
              {c.participantJoined && c.sharing
                ? `${owner ? "同事" : "你"}已加入，访问持续有效，直到主动离开或结束共享。`
                : c.sharing && c.expiresAt
                  ? `首次加入期限：${new Date(c.expiresAt).toLocaleTimeString("zh-CN", { hour: "2-digit", minute: "2-digit" })}。加入后不受此期限影响。`
                  : c.sharing && c.invitationState === "expired"
                    ? "邀请尚未使用且已到期，可以重新邀请同事。"
                    : c.releasePending
                      ? "当前执行和审批完成、专用客户端关闭后，会自动释放会话。"
                      : "会话与代码保留在执行主机上，可随时重新打开。"}
            </p>
            {owner && c.sharing ? (
              <button
                className="text-link danger"
                onClick={() => setModal("end")}
              >
                结束共享
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
          </section>
          <details className="technical">
            <summary>技术信息</summary>
            <dl>
              <dt>原生客户端</dt>
              <dd>
                {agentName}
                {c.provider === "claude" ? " · 实验性" : ""}
              </dd>
              <dt>协作 ID</dt>
              <dd>{c.id}</dd>
              <dt>来源会话</dt>
              <dd>{c.sourceId}</dd>
              <dt>协作会话</dt>
              <dd>{c.sessionId}</dd>
              <dt>Git 分支</dt>
              <dd>{c.branch}</dd>
              <dt>基线提交</dt>
              <dd>{c.head}</dd>
              <dt>{c.online ? "当前模型" : "最近确认的模型"}</dt>
              <dd>{c.model || `等待 ${agentName} 确认`}</dd>
              <dt>推理强度</dt>
              <dd>{c.reasoningEffort || `${agentName} 默认`}</dd>
            </dl>
          </details>
        </aside>
      </div>
      {modal && (
        <Modal
          title={
            modal === "invite"
              ? "邀请同事加入"
              : modal === "end"
                ? "结束这次共享？"
                : modal === "assist"
                  ? "用个人客户端辅助"
                  : `用 ${agentName} 继续`
          }
          onClose={() => setModal(null)}
        >
          {modal === "invite" ? (
            <>
              <p className="muted">
                让同事在自己的 Mac 上打开 Team
                Cross，选择「加入协作」并粘贴邀请。
              </p>
              <div className="invite-card">
                <Icon name="link" size={24} />
                <strong>{c.title}</strong>
                <span className="muted">{c.host} · 局域网</span>
              </div>
              {c.invitation ? (
                <>
                  <Copy
                    text={invitationText(c.invitation)}
                    label="复制邀请"
                    className="primary full-width"
                  />
                  <details className="technical">
                    <summary>查看邀请内容</summary>
                    <textarea
                      readOnly
                      aria-label="邀请内容"
                      value={c.invitation}
                      rows={4}
                    />
                  </details>
                </>
              ) : c.participantJoined ? (
                <p className="notice">
                  同事已加入，访问持续有效，无需再次发送邀请。
                </p>
              ) : c.invitationState === "expired" ||
                c.invitationState === "left" ? (
                <button
                  className="button primary"
                  disabled={!!busy}
                  onClick={() => void action("share")}
                >
                  生成新邀请
                </button>
              ) : (
                <Loading text="正在读取邀请…" />
              )}
              <p className="small-text muted">
                邀请仅限一人首次加入，有效期一小时。加入后持续有效；你决定何时交出输入。发起者退出
                Team Cross 时会结束本次共享。
              </p>
            </>
          ) : modal === "end" ? (
            <>
              <p>
                同事的直接连接和工具访问会关闭。协作会话、执行目录和所有代码继续保留。
              </p>
              <p>
                当前执行和审批完成、专用客户端关闭后，会自动释放会话，之后可以从
                Team Cross 恢复同一个会话。
              </p>
              <div className="form-footer">
                <button className="button" onClick={() => setModal(null)}>
                  继续共享
                </button>
                <button
                  className="button danger-button"
                  disabled={!!busy}
                  onClick={() => void action("end")}
                >
                  结束共享
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
