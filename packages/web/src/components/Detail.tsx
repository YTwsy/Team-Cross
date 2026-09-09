import { useState } from "react";
import { api, errorText, useResource } from "../api";
import {
  type Collaboration,
  type History,
  type Changes,
  projectName,
  relativeTime,
} from "../types";
import { Clients } from "./Clients";
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
function Context({
  id,
  sequence,
  online,
  closed,
}: {
  id: string;
  sequence: number;
  online: boolean;
  closed: boolean;
}) {
  const [tab, setTab] = useState("history");
  const [path, setPath] = useState("");
  const [file, setFile] = useState("");
  const context = useResource<History | Changes | { text: string }>(
    online && (tab !== "file" || file)
      ? `collaborations/${id}/context?kind=${tab}&path=${encodeURIComponent(file)}&revision=${sequence}`
      : null,
  );
  let body;
  if (context.data) {
    if (tab === "history" && "thread" in context.data) {
      const turns = context.data.thread.turns || [];
      body = turns.length ? (
        <div className="history">
          {turns.slice(-8).map((turn) => (
            <div key={turn.id} className="history-turn">
              {(turn.items || [])
                .filter(
                  (i) => i.type === "userMessage" || i.type === "agentMessage",
                )
                .map((item, index) => (
                  <div className={`history-message ${item.type}`} key={index}>
                    <span className="eyebrow">
                      {item.type === "userMessage" ? "用户" : "Codex"}
                    </span>
                    <p>
                      {item.text ||
                        item.content?.map((c) => c.text || "").join("\n")}
                    </p>
                  </div>
                ))}
            </div>
          ))}
        </div>
      ) : (
        <Empty icon="comment" title="还没有新的活动">
          <p>在 Codex 中继续，最新的上下文会出现在这里。</p>
        </Empty>
      );
    } else if (tab === "changes" && "diff" in context.data) {
      body =
        context.data.diff || context.data.status ? (
          <>
            <pre className="diff-summary">
              {context.data.status}
              {context.data.stat}
            </pre>
            <pre className="code-content">
              {context.data.diff ||
                "文件状态已列出；未跟踪文件可通过文件入口查看。"}
            </pre>
          </>
        ) : (
          <Empty icon="branch" title="当前没有代码改动" />
        );
    } else if ("text" in context.data)
      body = <pre className="code-content">{context.data.text}</pre>;
  }
  return (
    <section className="panel context-panel">
      <div className="panel-heading">
        <h2>协作上下文</h2>
        <button
          className="icon-button"
          aria-label="刷新上下文"
          disabled={!online}
          onClick={context.reload}
        >
          <Icon name="refresh" size={17} />
        </button>
      </div>
      <div className="tabs" role="tablist" aria-label="上下文类型">
        {[
          ["history", "最近对话"],
          ["changes", "代码改动"],
          ["file", "查看文件"],
        ].map(([value, label]) => (
          <button
            role="tab"
            key={value}
            aria-selected={tab === value}
            onClick={() => setTab(value!)}
          >
            {label}
          </button>
        ))}
      </div>
      {tab === "file" && (
        <form
          className="file-search"
          onSubmit={(e) => {
            e.preventDefault();
            setFile(path);
          }}
        >
          <input
            aria-label="相对执行目录的文件路径"
            placeholder="相对执行目录的文件路径，例如 src/main.ts"
            value={path}
            onChange={(e) => setPath(e.target.value)}
          />
          <button className="button small" disabled={!path || !online}>
            查看
          </button>
        </form>
      )}
      <ErrorBox message={context.error} retry={context.reload} />
      {!online ? (
        <Empty
          icon="link"
          title={
            closed ? "使用新邀请加入后可查看上下文" : "连接恢复后可查看上下文"
          }
        />
      ) : context.loading ? (
        <Loading />
      ) : (
        body || <Empty icon="folder" title="输入相对路径以查看文件" />
      )}
      <div className="panel-footnote">
        这里只保留轻量上下文，完整对话与执行交互请在 Codex 中查看。
      </div>
    </section>
  );
}
export function Detail({ id }: { id: string }) {
  const resource = useResource<Collaboration>(`collaborations/${id}`, 2500);
  const c = resource.data;
  const [modal, setModal] = useState<
    "clients" | "assist" | "invite" | "end" | null
  >(null);
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const [comment, setComment] = useState("");
  const [reference, setReference] = useState("");
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
    } catch (e) {
      setError(errorText(e));
    } finally {
      setBusy("");
    }
  }
  async function annotate() {
    setBusy("annotation");
    setError("");
    try {
      await api(`collaborations/${id}/annotations`, {
        text: comment,
        reference,
      });
      setComment("");
      setReference("");
      resource.reload();
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
                      ? "恢复协作，继续上次的工作"
                      : "正在等待发起者连接"
                    : c.approvals
                      ? "Codex 需要你的回应"
                      : c.busy
                        ? "Codex 正在执行"
                        : mine
                          ? "准备好继续了"
                          : "同事正在掌握输入"}
            </h2>
            <p>
              {closed
                ? "会话与代码仍保留在发起者的 Mac 上。继续参与时，请向同事获取新邀请。"
                : !c.online
                  ? owner
                    ? "恢复同一个会话与目录，不会重新创建分支。"
                    : "确认两台 Mac 在同一局域网，并让发起者保持共享。"
                  : c.approvals
                    ? "请在当前 Codex 客户端中查看并回应请求。"
                    : c.busy
                      ? `代码操作发生在 ${c.host}，可以在 Codex 中补充或中断。`
                      : mine
                        ? c.connected
                          ? `${c.client || "Codex"} 已连接，继续在客户端中工作。`
                          : "选择喜欢的 Codex 客户端，接着已有上下文继续。"
                        : "你可以先用自己的 Codex 读取上下文，或等待交接。"}
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
              disabled={!!busy}
              onClick={() => void action("start")}
            >
              {busy === "start" ? "正在恢复…" : "恢复运行时"}
              <Icon name="refresh" size={16} />
            </button>
          ) : (
            <button
              className="button primary"
              onClick={() => setModal(mine ? "clients" : "assist")}
            >
              <Icon name={mine ? "terminal" : "people"} size={18} />
              {mine ? "打开 Codex" : "用自己的 Codex 辅助"}
            </button>
          )}
          {owner && c.online && (
            <button
              className="button"
              disabled={!!busy}
              onClick={() =>
                c.sharing ? setModal("invite") : void action("share")
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
            online={c.online}
            sequence={c.sequence}
            closed={closed}
          />
          <section className="panel notes-panel">
            <div className="panel-heading">
              <h2>
                批注 <span className="count">{c.annotations?.length || 0}</span>
              </h2>
              <Icon name="comment" size={18} />
            </div>
            {c.annotations?.length ? (
              <div className="annotations">
                {c.annotations.map((a) => (
                  <article key={a.id}>
                    <div>
                      <span className="avatar small">{a.author[0]}</span>
                      <strong>{a.author}</strong>
                      <time>{relativeTime(a.createdAt)}</time>
                    </div>
                    {a.reference && <code>{a.reference}</code>}
                    <p>{a.text}</p>
                  </article>
                ))}
              </div>
            ) : (
              <p className="muted note-empty">
                留下一个想法，或为同事标记值得关注的地方。
              </p>
            )}
            <form
              className="annotation-form"
              onSubmit={(e) => {
                e.preventDefault();
                void annotate();
              }}
            >
              <label className="sr-only" htmlFor="annotation">
                添加批注
              </label>
              <textarea
                id="annotation"
                placeholder="添加批注…"
                rows={3}
                maxLength={4000}
                value={comment}
                onChange={(e) => setComment(e.target.value)}
              />
              <div>
                <input
                  aria-label="批注引用（可选）"
                  placeholder="引用文件或对话（可选）"
                  maxLength={1000}
                  value={reference}
                  onChange={(e) => setReference(e.target.value)}
                />
                <button
                  className="button small"
                  disabled={!!busy || !comment.trim() || (!owner && !c.online)}
                >
                  添加批注
                </button>
              </div>
            </form>
          </section>
        </div>
        <aside className="detail-aside">
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
                      : "可加入协作"
                    : "共享未开启"}
                </span>
              </div>
            </div>
            {owner && c.sharing && (
              <button
                className="button full-width"
                disabled={!!busy || (mine && c.busy)}
                onClick={() => void action(mine ? "handoff" : "reclaim")}
              >
                <Icon name="people" size={16} />
                {mine ? "将输入交给同事" : "接回输入"}
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
              使用自己的 Codex 辅助 <Icon name="arrow" size={14} />
            </button>
          </section>
          <section className="panel">
            <div className="panel-heading">
              <h3>共享状态</h3>
              <span className={`dot ${c.sharing ? "green-dot" : ""}`} />
            </div>
            <p>{c.sharing ? "局域网共享中" : "共享已关闭"}</p>
            <p className="muted small-text">
              {c.sharing && c.expiresAt
                ? `邀请有效至 ${new Date(c.expiresAt).toLocaleTimeString("zh-CN", { hour: "2-digit", minute: "2-digit" })}`
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
              <dd>{c.model || "等待 Codex 确认"}</dd>
              <dt>推理强度</dt>
              <dd>{c.reasoningEffort || "Codex 默认"}</dd>
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
                : "用 Codex 继续"
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
                    text={c.invitation}
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
              ) : (
                <Loading text="正在读取邀请…" />
              )}
              <p className="small-text muted">
                同事加入后可先查看；你决定何时交出输入。
              </p>
            </>
          ) : modal === "end" ? (
            <>
              <p>
                同事的直接连接和工具访问会关闭。协作会话、执行目录和所有代码继续保留。
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
