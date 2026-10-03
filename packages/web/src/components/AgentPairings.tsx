import { useEffect, useRef, useState } from "react";
import { api, errorText, useResource } from "../api";
import { formatDate, serviceText, t, tr } from "../i18n";
import { sameReference, useLibrary, type LibraryReference } from "../library";
import type { WorkbenchView } from "../workbench";
import { Copy, ErrorBox, Loading, Modal } from "./ui";

export type AgentPairing = {
  id: string;
  name: string;
  provider?: string;
  sessionId?: string;
  spaceId?: string;
  state: "waiting" | "verifying" | "paired" | "unsupported" | "expired";
  reason?: string;
  createdAt: string;
  expiresAt: string;
};
export type AgentRequest = {
  id: string;
  pairingId?: string;
  state:
    | "submitting"
    | "submitted"
    | "received"
    | "completed"
    | "failed"
    | "unknown"
    | "queued"
    | "cancelled";
  summary?: string;
  error?: string;
};
function pairingState(p: AgentPairing) {
  if (p.state === "paired") return p.reason ? t("暂不可发送") : t("已配对");
  if (p.state === "verifying") return t("正在核验接收能力");
  if (p.state === "unsupported") return t("暂不支持主动接收");
  if (p.state === "expired") return t("配对码已到期");
  return t("等待目标会话配对");
}
function pairingReason(p: AgentPairing) {
  return serviceText(p.reason, "接收会话暂不可用，请核对客户端与连接状态。");
}

export function PairConversation({
  onPaired,
}: {
  onPaired: (id: string) => void;
}) {
  const [name, setName] = useState("");
  const [created, setCreated] = useState<{
    pairing: AgentPairing;
    code: string;
  }>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const pairs = useResource<AgentPairing[]>(
    created ? "agent-pairings" : null,
    2000,
  );
  const current =
    pairs.data?.find((p) => p.id === created?.pairing.id) || created?.pairing;
  const delivered = useRef("");
  useEffect(() => {
    if (current?.state === "paired" && delivered.current !== current.id) {
      delivered.current = current.id;
      onPaired(current.id);
    }
  }, [current, onPaired]);
  async function create() {
    if (busy || !name.trim()) return;
    setBusy(true);
    setError("");
    try {
      if (created) await api(`agent-pairings/${created.pairing.id}/remove`, {});
      setCreated(await api("agent-pairings", { name: name.trim() }));
      pairs.reload();
    } catch (e) {
      setError(errorText(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <div className="agent-pair-flow">
      <p>
        {t(
          "首次使用时，将配对提示粘贴到要接收任务的那一个会话。以后可以直接从 Team Cross 发送。",
        )}
      </p>
      {!created ? (
        <form
          onSubmit={(e) => {
            e.preventDefault();
            void create();
          }}
        >
          <label>
            {t("接收会话名称")}
            <input
              autoFocus
              maxLength={80}
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder={t("例如：接口方案讨论")}
              disabled={busy}
            />
          </label>
          <button className="button primary" disabled={busy || !name.trim()}>
            {busy ? t("正在生成…") : t("生成配对提示")}
          </button>
        </form>
      ) : (
        <>
          <ol className="agent-pair-steps">
            <li>{t("复制下面的提示，粘贴到你要配对的会话。")}</li>
            <li>{t("让 Agent 调用配对工具，再回到这里查看接收核验结果。")}</li>
          </ol>
          <div className="agent-pair-copy">
            <strong>{created.pairing.name}</strong>
            <Copy
              label={t("复制配对提示")}
              text={tr`请使用 Team Cross 的 pair_current_session，以配对码 ${created.code} 配对当前会话。请核对返回的接收状态；只有 paired 才表示完成。`}
            />
          </div>
          <p className="small-text muted">{tr`提示有效至 ${formatDate(created.pairing.expiresAt, { dateStyle: "short", timeStyle: "short" })}，只用于这一次配对。新会话需要重新配对。`}</p>
          {current && (
            <div className="notice" role="status">
              <strong>{pairingState(current)}</strong>
              {current.reason && <p>{pairingReason(current)}</p>}
            </div>
          )}
          {current?.state !== "paired" && (
            <button
              className="button"
              disabled={busy}
              onClick={() => void create()}
            >
              {t("重新生成配对提示")}
            </button>
          )}
        </>
      )}
      <ErrorBox message={error || pairs.error} />
      <p className="small-text muted">
        {t(
          "目标会话需要已连接 Team Cross 工具。配对不会共享其他私人内容，也不会取得协作输入权。",
        )}
      </p>
    </div>
  );
}

export function AgentPairings() {
  const pairs = useResource<AgentPairing[]>("agent-pairings", 3000);
  const requests = useResource<AgentRequest[]>("agent-requests", 3000);
  const [pairing, setPairing] = useState(false);
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  async function remove(id: string) {
    setBusy(id);
    setError("");
    try {
      await api(`agent-pairings/${id}/remove`, {});
      pairs.reload();
    } catch (e) {
      setError(errorText(e));
    } finally {
      setBusy("");
    }
  }
  return (
    <section className="panel settings-section">
      <div className="panel-heading">
        <div>
          <h2>{t("接收会话")}</h2>
          <p>{t("在批注和选择清单中，把内容交给已经配对的 Agent 会话。")}</p>
        </div>
        <button className="button" onClick={() => setPairing(true)}>
          {t("配对会话")}
        </button>
      </div>
      <ErrorBox message={error || pairs.error} retry={pairs.reload} />
      {pairs.data?.length === 0 && (
        <p className="muted">{t("尚未配对接收会话。")}</p>
      )}
      <div className="agent-pair-list">
        {pairs.data?.map((p) => (
          <div className="agent-pair-row" key={p.id}>
            <div>
              <strong>{p.name}</strong>
              <span className="small-text">
                {pairingState(p)}
                {p.provider
                  ? ` · ${p.provider === "claude" ? "Claude Code" : "Codex"}`
                  : ""}
              </span>
              {p.reason && (
                <p className="small-text muted">{pairingReason(p)}</p>
              )}
            </div>
            <button
              className="button small"
              disabled={!!busy}
              aria-label={tr`移除配对 ${p.name}`}
              onClick={() => void remove(p.id)}
            >
              {t("移除配对")}
            </button>
          </div>
        ))}
      </div>
      <p className="small-text muted">
        {t("移除配对会停止后续投递，已经提交的请求不会因此撤回。")}
      </p>
      {!!requests.data?.length && (
        <details className="agent-recent-requests">
          <summary>{t("最近发送的请求")}</summary>
          {requests.data.map((r) => (
            <div key={r.id}>
              <p className="small-text">
                {pairs.data?.find((p) => p.id === r.pairingId)?.name ||
                  t("已移除的接收会话")}
              </p>
              <AgentRequestStatus request={r} />
            </div>
          ))}
        </details>
      )}
      {pairing && (
        <Modal title={t("配对会话")} onClose={() => setPairing(false)}>
          <PairConversation
            onPaired={() => {
              pairs.reload();
            }}
          />
        </Modal>
      )}
    </section>
  );
}

export function AgentRequestStatus({ request }: { request: AgentRequest }) {
  const text =
    request.state === "queued"
      ? t("等待接收端接手投递")
      : request.state === "cancelled"
        ? t("请求已取消，未启动新输入")
        : request.state === "completed"
          ? t("Agent 已报告处理完成")
          : request.state === "failed"
            ? request.summary
              ? t("Agent 已报告处理失败")
              : t("投递未完成，请查看原因")
            : request.state === "received"
              ? t("Agent 已读取请求，尚未报告处理完成")
              : request.state === "unknown"
                ? t("发送结果需要核对，界面不会自动重发")
                : t("请求已提交，等待 Agent 读取");
  return (
    <div className="notice agent-request-result" role="status">
      <strong>{text}</strong>
      {request.summary && <p>{request.summary}</p>}
      {request.error && (
        <p>{serviceText(request.error, "请打开接收会话核对请求状态。")}</p>
      )}
    </div>
  );
}

export function SendToAgent({
  references,
  spaceId,
  initialTargetId = "",
  parentRequestId,
  onSent,
}: {
  references: LibraryReference[];
  spaceId?: string;
  initialTargetId?: string;
  parentRequestId?: string;
  onSent?: () => void;
}) {
  const library = useLibrary();
  const pairs = useResource<AgentPairing[]>(
    spaceId ? null : "agent-pairings",
    2000,
  );
  const space = useResource<WorkbenchView>(
    spaceId ? `collaborations/${spaceId}/workbench/view` : null,
    2000,
  );
  const choices: AgentPairing[] | undefined = spaceId
    ? space.data?.targets
        ?.filter((p) => !p.removed)
        .map((p) => ({
          id: p.id,
          name: `${p.name} · ${p.member}`,
          state: "paired",
          reason:
            space.data?.assistant.targetId === p.id &&
            !["ready", "disabled"].includes(space.data.assistant.state)
              ? t("专用会话尚未接手或已暂停")
              : p.available
                ? ""
                : p.reason || t("接收端未连接"),
          createdAt: "",
          expiresAt: "",
        }))
    : pairs.data;
  const [selected, setSelected] = useState(
    () =>
      initialTargetId ||
      (spaceId ? "" : localStorage.getItem("teamcross.agent-target.v1") || ""),
  );
  const [pairing, setPairing] = useState(false);
  const [instruction, setInstruction] = useState(
    t("请读取所选内容，核对原文后分析。"),
  );
  const [intent, setIntent] = useState("analyze");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [request, setRequest] = useState<AgentRequest>();
  const [unknownID, setUnknownID] = useState("");
  const sending = useRef(false);
  const status = useResource<AgentRequest>(
    request || unknownID
      ? spaceId
        ? `collaborations/${spaceId}/workbench/request?requestId=${request?.id || unknownID}`
        : `agent-requests/${request?.id || unknownID}`
      : null,
    2000,
  );
  const target = choices?.find((p) => p.id === selected);
  const reason = !target
    ? t("请选择已配对的接收会话。")
    : target.state !== "paired"
      ? pairingState(target)
      : target.reason
        ? pairingReason(target)
        : target.spaceId && references.some((r) => r.spaceId !== target.spaceId)
          ? t("这个接收会话只能读取它所在空间的内容，请选择其他已配对会话。")
          : "";
  const locked = busy || !!request || !!unknownID;
  function choose(id: string) {
    setSelected(id);
    if (!spaceId) localStorage.setItem("teamcross.agent-target.v1", id);
  }
  async function paired(id: string) {
    if (!spaceId) {
      choose(id);
      setPairing(false);
      pairs.reload();
      return;
    }
    try {
      const target = await api<{ id: string }>(
        `collaborations/${spaceId}/workbench/register`,
        { pairingId: id },
      );
      choose(target.id);
      setPairing(false);
      space.reload();
    } catch (e) {
      setError(errorText(e));
    }
  }
  async function send() {
    if (sending.current || locked || reason || !instruction.trim()) return;
    sending.current = true;
    setBusy(true);
    setError("");
    const requestId = crypto.randomUUID();
    try {
      setRequest(
        await api<AgentRequest>(
          spaceId
            ? `collaborations/${spaceId}/workbench/send`
            : "agent-requests",
          {
            requestId,
            ...(spaceId
              ? {
                  targetId: selected,
                  ...(parentRequestId ? { parentRequestId } : {}),
                }
              : { pairingId: selected }),
            references,
            instruction: instruction.trim(),
            intent,
          },
        ),
      );
      onSent?.();
    } catch (e) {
      // Once the POST is in flight, even a lost response must not enable a
      // second click. Query the same request; creating another is explicit.
      setUnknownID(requestId);
      setError(errorText(e));
    } finally {
      setBusy(false);
      sending.current = false;
    }
  }
  return (
    <div className="send-to-agent">
      {spaceId && (
        <p className="notice">
          {t("请求、所选引用和结果摘要将对本空间成员可见。")}
        </p>
      )}
      <p>{tr`本次带入 ${references.length} 项明确引用。原文仍由 Agent 按当前权限读取。`}</p>
      <label>
        {t("接收会话")}
        <select
          value={selected}
          disabled={locked}
          onChange={(e) => choose(e.target.value)}
        >
          <option value="">{t("选择接收会话")}</option>
          {choices?.map((p) => (
            <option key={p.id} value={p.id}>
              {p.name} · {pairingState(p)}
            </option>
          ))}
        </select>
      </label>
      {!locked && (
        <button
          className="text-link"
          onClick={() => setPairing(!pairing)}
          aria-expanded={pairing}
        >
          {pairing ? t("收起配对步骤") : t("配对新会话")}
        </button>
      )}
      {pairing && !locked && (
        <div className="agent-pair-inline">
          <PairConversation onPaired={(id) => void paired(id)} />
        </div>
      )}
      <label>
        {t("处理方式")}
        <select
          value={intent}
          disabled={locked}
          onChange={(e) => setIntent(e.target.value)}
        >
          <option value="analyze">{t("分析后告诉我")}</option>
          {references.some((r) => r.kind === "annotation") && (
            <option value="analyze_reply">{t("分析并回复原批注")}</option>
          )}
        </select>
      </label>
      <label>
        {t("处理要求")}
        <textarea
          aria-label={t("Agent 处理要求")}
          rows={3}
          maxLength={4000}
          value={instruction}
          disabled={locked}
          onChange={(e) => setInstruction(e.target.value)}
        />
      </label>
      <details>
        <summary>{t("查看本次引用")}</summary>
        <ul>
          {references.map((ref, i) => {
            const resource = library?.data?.resources.find((r) =>
              sameReference(r.reference, ref),
            );
            const label =
              resource?.title ||
              (ref.kind === "material"
                ? t("已发布会话材料")
                : ref.kind === "annotation"
                  ? t("批注")
                  : t("上下文"));
            return (
              <li key={i}>
                {resource?.spaceTitle ? `${resource.spaceTitle} · ` : ""}
                {label}
                {ref.kind === "material" ? ` · ${tr`版本 ${ref.version}`}` : ""}
              </li>
            );
          })}
        </ul>
        {!references.length && (
          <p className="muted">
            {t("未附带材料引用；接收会话可按需读取空间简报。")}
          </p>
        )}
      </details>
      <ErrorBox message={error || pairs.error || space.error} />
      {!locked && reason && <p className="notice">{reason}</p>}
      {busy && <Loading text={t("正在提交，请勿重复发送…")} />}
      {(status.data || request) && (
        <AgentRequestStatus request={(status.data || request)!} />
      )}
      {unknownID && !status.data && (
        <p className="notice" role="status">
          {t("尚未查到请求回执，请在目标会话核对。不会自动重新发送。")}
        </p>
      )}
      <div className="modal-actions">
        {!locked && (
          <button
            className="button primary"
            disabled={!!reason || !instruction.trim()}
            onClick={() => void send()}
          >
            {t("确认发送")}
          </button>
        )}
      </div>
    </div>
  );
}
