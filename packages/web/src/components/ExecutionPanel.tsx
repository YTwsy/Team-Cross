import { useState } from "react";
import { api, errorText } from "../api";
import { collaborationAddress } from "../execution";
import { serviceText, t, tr } from "../i18n";
import {
  runtimeModeName,
  runtimeModeDescription,
  type ClientPlan,
  type Collaboration,
  type MaterialReference,
} from "../types";
import { Annotations, type AnnotationRequest } from "./Annotations";
import { Clients } from "./Clients";
import { Context } from "./Context";
import { TechnicalInformation } from "./ExecutionTechnical";
import { Members } from "./Members";
import { Badge, Empty, ErrorBox, Icon, Modal } from "./ui";
import {
  SpaceCommentsButton,
  SpaceSessionComments,
  useSpaceWorkbench,
} from "./SpaceWorkbench";
import { SpaceAvatar } from "./SpaceIdentity";

// A visited pane remains mounted when selecting a sibling. Its comment drafts,
// history cursor, file path and reading state belong to this execution alone.
export function ExecutionPanel({
  space,
  session: c,
  reload,
  active,
  onLocateMaterial,
}: {
  space: Collaboration;
  session: Collaboration;
  reload: () => void;
  active: boolean;
  onLocateMaterial?: (reference: MaterialReference) => void;
}) {
  const [request, setRequest] = useState<AnnotationRequest>();
  const [location, setLocation] = useState<AnnotationRequest>();
  const [modal, setModal] = useState<"clients" | "assist" | "end" | null>(null);
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const [personalNote, setPersonalNote] = useState("");
  const workbench = useSpaceWorkbench();
  const [inputOpen, setInputOpen] = useState(false);
  const id = collaborationAddress({ ...c, spaceId: space.id });
  const owner = space.role === "owner";
  const closed = ["ended", "left", "expired"].includes(space.state);
  const accessible =
    c.hasExecution !== false &&
    (owner || (!closed && space.reachable !== false));
  const mine = c.writer === (space.selfId || "owner");
  const agentName = c.provider === "claude" ? "Claude Code" : "Codex";
  const writer =
    c.writer === "owner"
      ? t("发起者")
      : space.members?.find((m) => m.id === c.writer)?.name || t("同事");
  const view = {
    ...c,
    spaceId: space.id,
    role: space.role,
    selfId: space.selfId,
    reachable: space.reachable,
    materials: space.materials,
  };
  async function manage(path: string, body: object) {
    if (busy) return;
    setBusy(path);
    setError("");
    try {
      await api(`collaborations/${id}/${path}`, body);
      reload();
      return true;
    } catch (e) {
      setError(errorText(e));
      return false;
    } finally {
      setBusy("");
    }
  }
  async function action(action: string, memberId?: string) {
    return manage("action", { action, memberId, epoch: c.epoch });
  }
  async function openPersonal() {
    if (busy) return;
    setBusy("personal-desktop");
    setError("");
    setPersonalNote("");
    try {
      const result = await api<ClientPlan>(
        `collaborations/${id}/personal-desktop`,
        { launch: true },
      );
      setPersonalNote(
        serviceText(result.note) || t("已请求个人 Codex 打开此协作会话。"),
      );
    } catch (e) {
      setError(errorText(e));
    } finally {
      setBusy("");
    }
  }
  if (!accessible)
    return <Empty icon="link" title={t("连接恢复后可查看上下文")} />;
  return (
    <div className="space-material-reader execution-reader execution-pane">
      <div className="material-reader">
        <header className="space-reader-header execution-reader-header">
          <div className="space-material-header">
            <h2>{c.title}</h2>
            <div className="space-reader-header-actions">
              <SpaceCommentsButton />
              <details
                className="space-material-more"
                onKeyDown={(event) => {
                  if (event.key === "Escape") event.currentTarget.open = false;
                }}
              >
                <summary aria-label={t("会话操作")} title={t("会话操作")}>
                  <Icon name="more" size={19} />
                </summary>
                <div className="space-material-menu execution-menu">
                  <strong>
                    {runtimeModeName(c.runtimeMode)} {t("· 创建后固定")}
                  </strong>
                  <p className="small-text muted">
                    {runtimeModeDescription(c.runtimeMode)}
                  </p>
                  <p className="execution-directory">{c.executionCwd}</p>
                  {owner && c.online && !c.paused && (
                    <button
                      className="button"
                      disabled={!!busy}
                      onClick={() => void action("pause")}
                    >
                      {t("暂停会话")}
                    </button>
                  )}
                  {owner && c.sharing && !c.paused && (
                    <button
                      className="text-button"
                      disabled={!!busy}
                      onClick={() => setModal("end")}
                    >
                      {t("结束共同执行")}
                    </button>
                  )}
                  {owner &&
                    c.provider !== "claude" &&
                    c.sessionId &&
                    c.sessionId !== c.sourceId && (
                      <button
                        className="button"
                        disabled={!!busy}
                        onClick={() => void openPersonal()}
                        title={t("直接定位此协作，无需从 Desktop 列表中查找")}
                      >
                        {busy === "personal-desktop"
                          ? t("正在打开…")
                          : c.paused && c.runtimeState === "released"
                            ? t("在个人 Codex 中继续")
                            : t("在个人 Codex 中打开")}
                      </button>
                    )}
                </div>
              </details>
            </div>
          </div>
          <div className="space-reader-meta">
            <SpaceAvatar name={agentName} agent />
            <span>
              {agentName} · {t("协作会话")}
            </span>
            <Badge collaboration={view} />
          </div>
          <div className="execution-control-bar">
            <span className="execution-input-owner">
              <Icon name="people" size={14} />
              {tr`当前输入：${writer}`}
            </span>
            <button
              className="text-button"
              aria-expanded={inputOpen}
              onClick={() => setInputOpen(!inputOpen)}
            >
              {t("输入交接")}
              <Icon name="chevron" size={14} />
            </button>
            <div className="execution-primary-action">
              {owner && (!c.online || c.paused) && (
                <button
                  className="button small primary"
                  disabled={
                    !!busy ||
                    c.state !== "ready" ||
                    c.runtimeState === "releasing"
                  }
                  onClick={() => void action("start")}
                >
                  <Icon name="refresh" size={15} />
                  {t("恢复运行时")}
                </button>
              )}
              {c.online && !c.paused && (
                <button
                  className="button small primary"
                  disabled={!!busy}
                  onClick={() => setModal(mine ? "clients" : "assist")}
                >
                  <Icon name="terminal" size={15} />
                  {mine ? tr`打开 ${agentName}` : t("用自己的客户端辅助")}
                </button>
              )}
            </div>
          </div>
          <div hidden={!inputOpen} className="execution-input-management">
            <Members
              compact
              collaboration={view}
              busy={!!busy}
              closed={closed}
              onAction={(value, memberId) => void action(value, memberId)}
              onAssist={() => setModal("assist")}
            />
          </div>
          <ErrorBox message={error || c.error} />
          {(c.approvals > 0 || c.nativeWaiting) && (
            <div className="notice" role="status">
              <h3>{tr`${agentName} 需要你的回应`}</h3>
              <p>{tr`请在当前 ${agentName} 客户端中查看并回应请求。`}</p>
            </div>
          )}
          {personalNote && (
            <p className="notice" role="status">
              {personalNote}
            </p>
          )}
          {c.releasePending && (
            <p className="notice">
              {t("等待当前执行与审批完成后释放运行时。")}
            </p>
          )}
        </header>
        <Context
          conversation
          id={id}
          sessionId={c.sessionId}
          sequence={c.sequence}
          online={
            c.state === "ready" &&
            c.runtimeState !== "releasing" &&
            c.runtimeState !== "starting"
          }
          closed={false}
          canAnnotate={!closed || owner}
          agentName={agentName}
          technical={
            <TechnicalInformation collaboration={c} agentName={agentName} />
          }
          annotations={c.annotations}
          location={location}
          onAnnotate={(target, origin) => {
            workbench?.openComments();
            setRequest({
              target: { ...target, executionId: c.executionId },
              origin,
              serial: Date.now(),
            });
          }}
          onDiscuss={(annotationId, origin) => {
            workbench?.openComments();
            setRequest({ annotationId, origin, serial: Date.now() });
          }}
        />
      </div>
      <SpaceSessionComments active={active}>
        <p className="space-comment-scope">
          {c.title} · {t("全体成员可读")}
        </p>
        <Annotations
          id={id}
          annotations={c.annotations || []}
          materials={space.materials}
          request={request}
          disabled={closed && !owner}
          onSaved={reload}
          onLocateMaterial={onLocateMaterial}
          onLocate={(annotation) =>
            setLocation({ target: annotation.target, serial: Date.now() })
          }
        />
      </SpaceSessionComments>
      {modal && (
        <Modal
          title={
            modal === "end"
              ? t("结束这个会话的共同执行？")
              : modal === "assist"
                ? t("用个人客户端辅助")
                : tr`用 ${agentName} 继续`
          }
          onClose={() => setModal(null)}
        >
          {modal === "end" ? (
            <>
              <p>
                {t(
                  "接回此会话输入并暂停执行，等待已接收的工作完成后释放运行时。所有空间成员仍可阅读历史和讨论，其他会话继续运行。",
                )}
              </p>
              <button
                className="button danger-button"
                disabled={!!busy}
                onClick={() => {
                  void action("end").then((ok) => {
                    if (ok) setModal(null);
                  });
                }}
              >
                {t("结束共同执行")}
              </button>
            </>
          ) : (
            <Clients
              collaboration={view}
              initial={modal === "assist" ? "assist" : "direct"}
            />
          )}
        </Modal>
      )}
    </div>
  );
}
