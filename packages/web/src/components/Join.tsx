import { formatDate, t, tr } from "../i18n";
import { useEffect, useState } from "react";
import { api, errorText } from "../api";
import {
  type Collaboration,
  type ShareTransport,
  type RuntimeMode,
  transportName,
  runtimeModeName,
  runtimeModeDescription,
} from "../types";
import { ErrorBox, Icon, PageHeading } from "./ui";
type InvitationPreview = {
  readOnly?: boolean;
  runtimeMode?: RuntimeMode;
  title: string;
  host: string;
  expiresAt?: string;
  transport: ShareTransport;
};
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
        {t("协作空间") + " "}
      </a>
      <PageHeading
        title={t("加入协作")}
        subtitle={t("先了解共享上下文，需要操作时再打开对应的原生客户端。")}
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
          <h2>{preview ? t("确认这次协作") : t("粘贴协作邀请")}</h2>
          {!pendingId && (
            <label className="field">
              {t("邀请码或 App 链接") + " "}
              <textarea
                autoFocus
                rows={5}
                placeholder={t("tcx3.… 或 teamcross://join?invite=…")}
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
              <p>
                {t("托管主机：")}
                {preview.host}
              </p>
              <p>
                {t("连接方式：")}
                {transportName(preview.transport)}
              </p>
              {preview.readOnly ? (
                <p>
                  {t(
                    "只读分享与讨论：可以查看已发布材料、发布自己的调查与回复；没有原生执行或目录访问权。",
                  ) + " "}
                </p>
              ) : (
                <>
                  <p>
                    {t("协作模式：")}
                    {runtimeModeName(preview.runtimeMode)}
                    {" " + t("· 创建后固定") + " "}
                  </p>
                  <p>{runtimeModeDescription(preview.runtimeMode)}</p>
                </>
              )}
              {preview.expiresAt && !preview.expiresAt.startsWith("0001-") ? (
                <p>{tr`请在 ${formatDate(preview.expiresAt)} 前首次加入。`}</p>
              ) : (
                <p>
                  {t(
                    "同一链接可供多人加入，有效至发起者关闭、重置链接或结束本次共享。",
                  ) + " "}
                </p>
              )}
              <p>
                {t(
                  "加入后持续有效，直到你主动离开或发起者结束共享。关闭客户端或暂时断线后可以重新连接。",
                ) + " "}
              </p>
              {!preview.readOnly && (
                <p>
                  {t(
                    "加入后可以读取这次协作的历史、文件与改动，并留下批注。代码和模型调用仍在发起者的 Mac 上执行，输入需要发起者交接。",
                  ) + " "}
                </p>
              )}
              <p>{t("新加入的成员也能读取空间中尚未撤回的已有材料。")}</p>
              <small>{t("以上为邀请声明的信息，连接时会核验主机指纹。")}</small>
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
              ? preview?.transport === "tailcat"
                ? t("正在通过 Tailcat 连接…")
                : t("正在处理…")
              : preview
                ? t("确认加入并查看上下文")
                : t("查看邀请信息")}
            <Icon name="arrow" size={17} />
          </button>
          {pendingId && error && (
            <a className="text-link" href="#/join">
              {t("重新粘贴邀请") + " "}
            </a>
          )}
        </form>
        <aside className="join-explainer">
          <span className="eyebrow">{t("从查看开始")}</span>
          <div>
            <Icon name="folder" size={22} />
            <h3>{t("无需先配置原生客户端")}</h3>
            <p>
              {t(
                "安装 Team Cross 后，即可查看共享上下文。参与者无需准备本地仓库。",
              ) + " "}
            </p>
          </div>
          <div>
            <Icon name="people" size={22} />
            <h3>{t("准备好后申请输入")}</h3>
            <p>
              {t(
                "发起者交接后，打开这次协作支持的原生客户端；也可以用自己的客户端辅助协作。",
              ) + " "}
            </p>
          </div>
          <p className="small-text muted">
            {preview?.transport === "tailcat"
              ? t(
                  "此邀请使用实验性 Tailcat 跨网络连接，无法直连时可能经第三方 DERP 中继。",
                )
              : preview
                ? t("此邀请要求双方位于同一局域网。")
                : t("邀请会声明使用局域网或实验性 Tailcat 跨网络连接。")}
            {t("尚未安装时，请按邀请中的安装指引安装，再次打开链接。") + " "}
          </p>
        </aside>
      </div>
    </>
  );
}
