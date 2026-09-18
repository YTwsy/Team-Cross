import { useRef, useState } from "react";
import { api, errorText } from "../api";
import {
  invitationText,
  transportName,
  type Collaboration,
  type ShareTransport,
} from "../types";
import { Annotations, type AnnotationRequest } from "./Annotations";
import { Materials } from "./Materials";
import { Members } from "./Members";
import { Clients } from "./Clients";
import { Badge, Copy, ErrorBox, Modal, PageHeading } from "./ui";

export function ReadOnlySpace({
  collaboration: c,
  reload,
  showInvite = false,
}: {
  collaboration: Collaboration;
  reload: () => void;
  showInvite?: boolean;
}) {
  const [modal, setModal] = useState<"invite" | "end" | "assist" | null>(
    showInvite ? "invite" : null,
  );
  const [transport, setTransport] = useState<ShareTransport>(
    c.transport || "lan",
  );
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [token, setToken] = useState("");
  const [request, setRequest] = useState<AnnotationRequest>();
  const [location, setLocation] = useState<AnnotationRequest>();
  const inviteRequest = useRef(crypto.randomUUID());
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
      if (path === "invitations") {
        setToken(result.invitation || "");
        inviteRequest.current = crypto.randomUUID();
      }
      if (path === "action") {
        setToken("");
        setModal(null);
      }
      reload();
    } catch (e) {
      setError(errorText(e));
    } finally {
      setBusy(false);
    }
  }
  if (closed) {
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
            已发布材料与讨论仍保存在托管主机。继续参与时，请向同事获取新邀请。
          </p>
          <a className="button primary" href="#/join">
            使用新邀请加入
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
        subtitle={`托管主机：${c.host} · ${!owner && c.reachable === false ? "连接中断，等待重连" : c.sharing ? `${transportName(c.transport)}共享中` : "共享已关闭"}`}
      >
        <Badge collaboration={c} />
      </PageHeading>
      <ErrorBox message={error || (!closed ? c.error : undefined)} />
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
                邀请同事
              </button>
              {c.sharing && (
                <button
                  className="button"
                  disabled={busy}
                  onClick={() => setModal("end")}
                >
                  结束共享
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
              这个空间尚未关联可操作会话。需要一起修改和运行时，由托管主机选择本机原生来源、执行目录与运行权限。
            </p>
            {owner && (
              <a className="button" href={`#/execute/${c.id}`}>
                设置可操作的协作会话
              </a>
            )}
            <p className="small-text muted">
              启用后会额外共享原生历史与目录。现有只读邀请和成员资格将关闭，保留材料与讨论，再用新邀请加入。
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
              ? "邀请成员阅读与讨论"
              : modal === "end"
                ? "结束这次共享？"
                : "使用个人 Agent 辅助"
          }
          onClose={() => setModal(null)}
        >
          {modal === "invite" ? (
            <>
              <p>
                邀请成员可以读取本空间尚未撤回的材料、参与讨论、发布自己选择的会话。只读邀请不包含原生执行和目录访问。
              </p>
              <label className="field">
                连接方式
                <select
                  disabled={c.sharing || busy}
                  value={c.transport || transport}
                  onChange={(e) =>
                    setTransport(e.target.value as ShareTransport)
                  }
                >
                  <option value="lan">局域网</option>
                  <option value="tailcat">Tailcat 跨网络 · 实验性</option>
                </select>
              </label>
              <p className="small-text muted">
                每份邀请接纳一位成员。新增成员也能阅读空间已有材料。Tailcat
                无法直连时可能经第三方 DERP 中继。
              </p>
              {(token || c.invitation) && (
                <Copy
                  label="复制当前邀请"
                  text={invitationText(
                    token || c.invitation!,
                    c.transport || transport,
                  )}
                />
              )}
              <button
                className="button primary"
                disabled={busy}
                onClick={() =>
                  void manage("invitations", {
                    transport: c.transport || transport,
                    requestId: inviteRequest.current,
                  })
                }
              >
                {busy
                  ? "正在生成…"
                  : c.sharing
                    ? "为另一位成员生成邀请"
                    : "生成只读邀请"}
              </button>
              <div className="invitation-management">
                {c.invitations
                  ?.filter((i) => i.state === "pending")
                  .map((i) => (
                    <div className="invitation-row" key={i.id}>
                      <span>待用邀请 · {i.id.slice(0, 8)}</span>
                      <button
                        className="text-link"
                        disabled={busy}
                        onClick={() =>
                          void manage("invitations", { invitationId: i.id })
                        }
                      >
                        取回这份邀请
                      </button>
                      <button
                        className="text-link danger"
                        disabled={busy}
                        onClick={() =>
                          void manage("revoke-invitation", {
                            invitationId: i.id,
                          })
                        }
                      >
                        撤销
                      </button>
                    </div>
                  ))}
              </div>
              <ErrorBox message={error} />
            </>
          ) : modal === "end" ? (
            <>
              <p>
                关闭所有成员访问，保留已发布材料和讨论，之后可以重新生成邀请。
              </p>
              <button
                className="button danger-button"
                disabled={busy}
                onClick={() => void manage("action", { action: "end" })}
              >
                确认结束共享
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
