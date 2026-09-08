import { useState } from "react";
import { api, errorText, useResource } from "../api";
import { type Collaboration } from "../types";
import { Clients } from "./Clients";
import { Badge, ErrorBox, Icon, PageHeading } from "./ui";
export function Join() {
  const [invitation, setInvitation] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [joined, setJoined] = useState<Collaboration>();
  const live = useResource<Collaboration>(
    joined ? `collaborations/${joined.id}` : null,
    2500,
  );
  async function join() {
    setBusy(true);
    setError("");
    try {
      setJoined(
        await api<Collaboration>("join", { invitation: invitation.trim() }),
      );
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
        subtitle="带上你熟悉的 Codex，接着同事的上下文继续。"
      />
      {!joined ? (
        <div className="join-layout">
          <form
            className="panel join-form"
            onSubmit={(e) => {
              e.preventDefault();
              void join();
            }}
          >
            <span className="entry-icon blue">
              <Icon name="link" size={25} />
            </span>
            <h2>粘贴协作邀请</h2>
            <p className="muted">邀请来自发起者的 Team Cross，无需账号。</p>
            <label className="field">
              邀请内容
              <textarea
                autoFocus
                rows={5}
                placeholder="tcx2.…"
                value={invitation}
                disabled={busy}
                onChange={(e) => setInvitation(e.target.value)}
                aria-invalid={!!error}
              />
            </label>
            <ErrorBox message={error} />
            {error && (
              <p className="small-text muted">
                请确认邀请仍有效、两台 Mac 在同一局域网，且发起者的 Team Cross
                正在共享；邀请到期时请同事重新生成。
              </p>
            )}
            <button
              className="button primary full-width"
              disabled={busy || !invitation.trim()}
            >
              {busy ? (
                <>
                  <span className="spinner" />
                  正在连接协作主机…
                </>
              ) : (
                <>
                  连接并查看协作 <Icon name="arrow" size={17} />
                </>
              )}
            </button>
          </form>
          <aside className="join-explainer">
            <span className="eyebrow">加入后，你可以</span>
            <div>
              <Icon name="terminal" size={22} />
              <h3>直接操作共享会话</h3>
              <p>
                用 TUI 或独立 Desktop 继续同一个协作会话，执行仍在同事的 Mac
                上。
              </p>
            </div>
            <div>
              <Icon name="people" size={22} />
              <h3>用自己的 Codex 辅助</h3>
              <p>
                在你自己的会话里分析，通过工具读取共享上下文、发送输入或留下批注。
              </p>
            </div>
            <p className="small-text muted">
              两种方式可以同时使用，并共享清楚的输入归属。
            </p>
          </aside>
        </div>
      ) : (
        <section className="panel joined-panel">
          <div className="joined-heading">
            <span className="entry-icon blue">
              <Icon name="check" size={25} />
            </span>
            <div>
              <span className="eyebrow">已连接到协作</span>
              <h2>{joined.title}</h2>
              <p className="muted">执行主机：{joined.host}</p>
            </div>
            <Badge collaboration={live.data || joined} />
          </div>
          <Clients collaboration={live.data || joined} />
          <div className="form-footer">
            <span className="muted">之后可以在详情中打开另一种入口。</span>
            <a className="button" href={`#/collaborations/${joined.id}`}>
              进入协作详情 <Icon name="arrow" size={16} />
            </a>
          </div>
        </section>
      )}
    </>
  );
}
