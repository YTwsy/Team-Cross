import type { Collaboration } from "../types";
import { Icon } from "./ui";

export function Members({
  collaboration: c,
  busy,
  closed,
  onAction,
  onRemove,
  onAssist,
}: {
  collaboration: Collaboration;
  busy: boolean;
  closed: boolean;
  onAction: (action: string, memberId?: string) => void;
  onRemove: (memberId: string) => void;
  onAssist: () => void;
}) {
  const owner = c.role === "owner";
  const selfId = c.selfId || c.role;
  const execution = c.hasExecution !== false;
  const mine = execution && c.writer === selfId;
  const members = c.members || [];
  const active = members.filter((m) => m.active);
  return (
    <section className="panel participants-panel">
      <div className="panel-heading">
        <h2>参与者与邀请</h2>
        <span className="participants-caption">{active.length + 1} 人</span>
      </div>
      <div className="aside-participant-list">
        <div className="participant">
          <span className="avatar self">{owner ? "我" : "A"}</span>
          <div>
            <strong>{owner ? "我" : "发起者"}</strong>
            <span>
              {execution ? "执行主机" : "托管主机"} ·{" "}
              {execution && c.writer === "owner" ? "正在输入" : "可查看"}
            </span>
          </div>
        </div>
        {members.map((member) => (
          <div className="member-entry" key={member.id}>
            <div className="participant">
              <span className="avatar">
                {member.id === selfId ? "我" : member.name.slice(0, 1)}
              </span>
              <div>
                <strong>
                  {member.name}
                  {member.id === selfId && "（我）"}
                </strong>
                <span>
                  {!member.active
                    ? "已离开或移除"
                    : c.writer === member.id
                      ? "已获输入权"
                      : member.online
                        ? "在线查看"
                        : "暂时离线"}
                  {member.active && member.inputRequested && " · 正在申请输入"}
                </span>
                <small className="muted">成员 {member.id.slice(0, 8)}</small>
              </div>
            </div>
            {owner && member.active && c.sharing && (
              <div className="member-actions">
                {mine && (
                  <button
                    className="button small"
                    disabled={busy || c.busy || !c.online}
                    onClick={() => onAction("handoff", member.id)}
                  >
                    将输入交给{member.name}
                  </button>
                )}
                <button
                  className="text-link danger small-text"
                  disabled={busy}
                  onClick={() => onRemove(member.id)}
                  aria-label={`移除成员 ${member.name}`}
                >
                  移除
                </button>
              </div>
            )}
          </div>
        ))}
        {!active.length && (
          <p className="small-text muted">等待同事通过各自的邀请加入。</p>
        )}
      </div>
      <div className="participants-actions">
        {execution && owner && c.sharing && !mine && (
          <button
            className="button small"
            disabled={busy}
            onClick={() => onAction("reclaim")}
          >
            <Icon name="people" size={14} />
            接回输入
          </button>
        )}
        {execution && !owner && c.sharing && !mine && (
          <button
            className="button small"
            disabled={busy || !c.online || closed}
            onClick={() =>
              onAction(c.inputRequested ? "cancel_input" : "request_input")
            }
          >
            {c.inputRequested ? "取消输入申请" : "申请输入"}
          </button>
        )}
        {!owner && c.sharing && mine && (
          <button
            className="button small"
            disabled={busy || c.busy || closed}
            onClick={() => onAction("return")}
          >
            交还输入
          </button>
        )}
        <button
          className="text-link small-text"
          disabled={closed}
          onClick={onAssist}
        >
          使用自己的客户端辅助 <Icon name="arrow" size={14} />
        </button>
      </div>
      {owner && c.sharing && c.busy && mine && (
        <p className="small-text muted participants-action-note">
          当前轮完成后可以交出输入。
        </p>
      )}
    </section>
  );
}
