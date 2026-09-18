import { useRef, useState } from "react";
import { api, errorText } from "../api";
import {
  invitationText,
  transportName,
  type Collaboration,
  type ShareTransport,
} from "../types";
import { Copy, ErrorBox, Loading } from "./ui";

export function InvitationPanel({
  collaboration: c,
  onUpdated,
  initialTransport = "lan",
}: {
  collaboration: Collaboration;
  onUpdated: (value: Collaboration) => void;
  initialTransport?: ShareTransport;
}) {
  const [transport, setTransport] = useState<ShareTransport>(
    c.transport || initialTransport,
  );
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const request = useRef(crypto.randomUUID());
  async function manage(path: string, body: object) {
    setBusy(true);
    setError("");
    try {
      const result = await api<Collaboration>(
        `collaborations/${c.id}/${path}`,
        body,
      );
      onUpdated(result);
      request.current = crypto.randomUUID();
    } catch (e) {
      setError(errorText(e));
    } finally {
      setBusy(false);
    }
  }
  const connection = c.transport || transport;
  const reset = () =>
    void manage("invitations", {
      transport: connection,
      reset: true,
      requestId: request.current,
    });
  return (
    <>
      <p>
        将同一个链接发给需要参与的同事。每个人分别加入，保留自己的材料、批注和输入身份。
      </p>
      <div className="invite-card">
        <strong>{c.title}</strong>
        <span className="muted">
          {c.host} · {transportName(connection)}
        </span>
      </div>
      <p className="small-text muted">
        {(c.invitationReadOnly ?? c.hasExecution === false)
          ? "通过此链接可以阅读已发布材料、参与讨论和发布调查。执行访问由发起者在成员列表另行开放。"
          : "通过此链接可以阅读已发布材料、协作原生历史和工作目录，并参与共同执行；实际输入由发起者交接。"}
      </p>
      {c.sharingPreparing ? (
        <>
          <Loading
            text={
              connection === "tailcat"
                ? "正在建立 Tailcat 跨网络通道…"
                : "正在生成邀请链接…"
            }
          />
          <button
            className="button"
            disabled={busy}
            onClick={() => void manage("action", { action: "end" })}
          >
            取消生成邀请
          </button>
        </>
      ) : c.invitation ? (
        <>
          <Copy
            text={invitationText(c.invitation, connection)}
            label="复制邀请链接"
            className="primary full-width"
          />
          <div className="material-actions invitation-management">
            <button className="button" disabled={busy} onClick={reset}>
              重置邀请链接
            </button>
            <button
              className="text-link danger"
              disabled={busy}
              onClick={() =>
                void manage("revoke-invitation", {
                  invitationId: c.invitationId,
                })
              }
            >
              关闭链接加入
            </button>
          </div>
          <p className="small-text muted">
            关闭链接会停止接纳新成员；重置后旧链接失效。已加入的成员继续参与。
          </p>
        </>
      ) : (
        <>
          <label className="field">
            连接方式
            <select
              value={connection}
              disabled={busy || c.sharing}
              onChange={(e) => setTransport(e.target.value as ShareTransport)}
            >
              <option value="lan">局域网</option>
              <option value="tailcat">Tailcat 跨网络 · 实验性</option>
            </select>
          </label>
          {c.sharing && (
            <p className="notice">链接加入已关闭。已加入的成员可以继续协作。</p>
          )}
          <button
            className="button primary full-width"
            disabled={busy}
            onClick={() =>
              c.sharing
                ? reset()
                : void manage("invitations", {
                    transport: connection,
                    requestId: request.current,
                  })
            }
          >
            {busy ? "正在生成…" : c.sharing ? "重新开放链接" : "生成邀请链接"}
          </button>
        </>
      )}
      <p className="small-text muted">
        链接可供多人使用，有效至关闭、重置或本次共享结束。新成员也能阅读尚未撤回的已有材料。
        {connection === "tailcat" &&
          " Tailcat 为实验性连接，无法直连时可能经第三方 DERP 中继。"}
      </p>
      <ErrorBox message={error} />
    </>
  );
}
