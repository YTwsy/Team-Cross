import { useEffect, useState } from "react";
import { api, errorText } from "../api";
import { type Collaboration } from "../types";
import { ErrorBox, Icon, PageHeading } from "./ui";
type InvitationPreview = { title: string; host: string; expiresAt: string };
export function Join({ pendingId }: { pendingId?: string }) {
  const [invitation, setInvitation] = useState("");
  const [preview, setPreview] = useState<InvitationPreview>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  useEffect(() => {
    if (!pendingId) return;
    const abort = new AbortController();
    setBusy(true);
    api<InvitationPreview>("invitations/preview", { pendingId }, abort.signal)
      .then(setPreview)
      .catch((e) => {
        if (!abort.signal.aborted) setError(errorText(e));
      })
      .finally(() => {
        if (!abort.signal.aborted) setBusy(false);
      });
    return () => abort.abort();
  }, [pendingId]);
  async function next() {
    setBusy(true);
    setError("");
    try {
      const input = pendingId
        ? { pendingId }
        : { invitation: invitation.trim() };
      if (!preview)
        setPreview(await api<InvitationPreview>("invitations/preview", input));
      else {
        const joined = await api<Collaboration>("join", input);
        location.hash = `/collaborations/${joined.id}`;
      }
    } catch (e) {
      setError(errorText(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <>
      <a href="#/" className="back-link">
        <Icon name="back" size={16} />
        协作空间
      </a>
      <PageHeading
        title="加入协作"
        subtitle="先了解共享上下文，需要操作时再接入 Codex。"
      />
      <div className="join-layout">
        <form
          className="panel join-form"
          onSubmit={(e) => {
            e.preventDefault();
            void next();
          }}
        >
          <span className="entry-icon blue">
            <Icon name="link" size={25} />
          </span>
          <h2>{preview ? "确认这次协作" : "粘贴协作邀请"}</h2>
          {!pendingId && (
            <label className="field">
              邀请码或 App 链接
              <textarea
                autoFocus
                rows={5}
                placeholder="tcx2.… 或 teamcross://join?invite=…"
                value={invitation}
                disabled={busy}
                onChange={(e) => {
                  setInvitation(e.target.value);
                  setPreview(undefined);
                }}
                aria-invalid={!!error}
              />
            </label>
          )}
          {preview && (
            <div className="notice">
              <h3>{preview.title}</h3>
              <p>执行主机：{preview.host}</p>
              <p>
                请在 {new Date(preview.expiresAt).toLocaleString("zh-CN")}{" "}
                前首次加入。
              </p>
              <p>
                加入后持续有效，直到你主动离开或发起者结束共享。关闭客户端或暂时断线后可以重新连接。
              </p>
              <p>
                加入后可以读取这次协作的历史、文件与改动，并留下批注。代码和模型调用仍在发起者的
                Mac 上执行，输入需要发起者交接。
              </p>
              <small>以上为邀请声明的信息，连接时会核验主机指纹。</small>
            </div>
          )}
          <ErrorBox message={error} />
          <button
            className="button primary full-width"
            disabled={
              busy ||
              (!pendingId && !invitation.trim()) ||
              (!!pendingId && !preview)
            }
          >
            {busy
              ? "正在处理…"
              : preview
                ? "确认加入并查看上下文"
                : "查看邀请信息"}
            <Icon name="arrow" size={17} />
          </button>
          {pendingId && error && (
            <a className="text-link" href="#/join">
              重新粘贴邀请
            </a>
          )}
        </form>
        <aside className="join-explainer">
          <span className="eyebrow">从查看开始</span>
          <div>
            <Icon name="folder" size={22} />
            <h3>无需先配置 Codex</h3>
            <p>
              安装 Team Cross 后，即可查看共享上下文。参与者无需准备本地仓库。
            </p>
          </div>
          <div>
            <Icon name="people" size={22} />
            <h3>准备好后申请输入</h3>
            <p>
              发起者交接后，可以使用 TUI、专用 Desktop 或自己的 Codex 辅助协作。
            </p>
          </div>
          <p className="small-text muted">
            双方需在同一局域网。尚未安装时，请按邀请中的安装指引安装，再次打开链接。
          </p>
        </aside>
      </div>
    </>
  );
}
