import { useState } from "react";
import { api, errorText } from "../api";
import { transportName, type Collaboration } from "../types";
import { Annotations, type AnnotationRequest } from "./Annotations";
import { Materials } from "./Materials";
import { Members } from "./Members";
import { Clients } from "./Clients";
import { InvitationPanel } from "./InvitationPanel";
import { Badge, ErrorBox, Modal, PageHeading } from "./ui";

export function ReadOnlySpace({
  collaboration: c,
  reload,
  update,
  showInvite = false,
}: {
  collaboration: Collaboration;
  reload: () => void;
  update: (value: Collaboration) => void;
  showInvite?: boolean;
}) {
  const [modal, setModal] = useState<"invite" | "end" | "assist" | null>(
    showInvite ? "invite" : null,
  );
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [request, setRequest] = useState<AnnotationRequest>();
  const [location, setLocation] = useState<AnnotationRequest>();
  const owner = c.role === "owner";
  const closed = ["ended", "left", "expired"].includes(c.state);
  const disabled = closed || (!owner && c.reachable === false);
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
          返回协作空间
        </a>
        <PageHeading title={c.title} subtitle={`托管主机：${c.host}`}>
          <Badge collaboration={c} />
        </PageHeading>
        <section className="panel readonly-intro">
          <h2>{c.state === "left" ? "你已离开这个空间" : "这次共享已结束"}</h2>
          <p>
            已发布材料与讨论仍保存在托管主机。继续参与时，请使用当前有效的空间邀请链接。
          </p>
          <a className="button primary" href="#/join">
            使用邀请链接加入
          </a>
        </section>
      </>
    );
  }
  return (
    <>
      <a href="#/" className="back-link">
        返回协作空间
      </a>
      <PageHeading
        title={c.title}
        subtitle={`托管主机：${c.host} · ${closed ? "空间已关闭" : !owner && c.reachable === false ? "连接中断，等待重连" : c.sharing ? `${transportName(c.transport)}共享中` : "尚未开放邀请"}`}
      >
        <Badge collaboration={c} />
      </PageHeading>
      <ErrorBox message={error || (!closed ? c.error : undefined)} />
      {closed && (
        <p className="notice">
          空间已关闭。材料和讨论保留，重新开放后可以继续邀请同事。
        </p>
      )}
      <section className="panel readonly-intro">
        <div>
          <h2>分享与审阅</h2>
          <p>
            空间保存已发布材料和讨论。每个人可以用自己的 Agent
            分析，再把新的调查附回来。
          </p>
        </div>
        <div className="material-actions">
          {owner ? (
            <>
              <button
                className="button primary"
                disabled={busy}
                onClick={() => setModal("invite")}
              >
                {closed ? "重新开放空间" : "邀请成员"}
              </button>
              {!closed && (
                <button
                  className="button"
                  disabled={busy}
                  onClick={() => setModal("end")}
                >
                  关闭空间
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
                离开空间
              </button>
            )
          )}
          <button
            className="button"
            disabled={disabled}
            onClick={() => setModal("assist")}
          >
            使用自己的 Agent 辅助
          </button>
        </div>
      </section>
      <div className="detail-grid">
        <div className="detail-main">
          <Materials
            collaboration={c}
            reload={reload}
            onAnnotate={(target) => setRequest({ target, serial: Date.now() })}
            location={location}
          />
          <section className="panel optional-execution">
            <h2>共同继续执行</h2>
            <p>
              {c.executionAvailable
                ? "发起者已启用共同执行。你可以继续阅读和讨论；获得执行访问后，此处会显示参与入口。"
                : "需要一起修改和运行时，由发起者在这个空间中启用共同执行。"}
            </p>
            {owner && !closed && (
              <a className="button" href={`#/execute/${c.id}`}>
                启用共同执行
              </a>
            )}
            <p className="small-text muted">
              启用时保留原邀请链接、成员、材料与讨论。发起者再向需要参与执行的成员开放原生历史与目录访问。
            </p>
          </section>
        </div>
        <aside className="detail-aside">
          <Members
            collaboration={c}
            busy={busy}
            closed={closed}
            onAction={() => {}}
            onRemove={(memberId) => void manage("remove-member", { memberId })}
            onAssist={() => setModal("assist")}
          />
          <Annotations
            id={c.id}
            annotations={c.annotations || []}
            materials={c.materials}
            request={request}
            disabled={disabled}
            onSaved={reload}
            onLocate={(annotation) =>
              setLocation({ target: annotation.target, serial: Date.now() })
            }
            onLocateMaterial={(ref) =>
              setLocation({
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
          />
        </aside>
      </div>
      {modal && (
        <Modal
          title={
            modal === "invite"
              ? "邀请成员"
              : modal === "end"
                ? "关闭这个空间？"
                : "使用个人 Agent 辅助"
          }
          onClose={() => setModal(null)}
        >
          {modal === "invite" ? (
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
                关闭这个协作空间和所有远端访问，保留已发布材料与讨论。之后可以重新开放。
              </p>
              <button
                className="button danger-button"
                disabled={busy}
                onClick={() => void manage("action", { action: "end" })}
              >
                确认关闭空间
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
