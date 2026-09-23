import type { Collaboration } from "../types";
import { useEffect, useId, useRef, useState } from "react";
import { Icon } from "./ui";

export function Members({
  collaboration: c,
  busy,
  closed,
  onAction,
  onRemove,
  onAssist,
  onExecutionAccess,
}: {
  collaboration: Collaboration;
  busy: boolean;
  closed: boolean;
  onAction: (action: string, memberId?: string) => void;
  onRemove: (memberId: string) => void;
  onAssist: () => void;
  onExecutionAccess?: (memberId: string, allowed: boolean) => void;
}) {
  const panel = useRef<HTMLElement>(null);
  const body = useRef<HTMLDivElement>(null);
  const bodyId = useId();
  const [reading, setReading] = useState(false);
  const [manual, setManual] = useState<boolean>();
  const phase = useRef(false);
  const expanded = manual ?? !reading;
  useEffect(() => {
    const grid = panel.current?.closest<HTMLElement>(".detail-grid");
    if (!grid) return;
    let frame = 0;
    const update = () => {
      frame = 0;
      const top = grid.getBoundingClientRect().top;
      // Start folding as reading reaches the toolbar. A small hysteresis keeps
      // touchpad movement around that boundary from repeatedly toggling it.
      const next =
        getComputedStyle(grid).display === "grid" &&
        top < (phase.current ? 44 : 12);
      if (
        next === phase.current ||
        (next && body.current?.contains(document.activeElement))
      )
        return;
      phase.current = next;
      setReading(next);
      setManual(undefined);
    };
    const schedule = () => {
      if (!frame) frame = requestAnimationFrame(update);
    };
    update();
    window.addEventListener("scroll", schedule, { passive: true });
    window.addEventListener("resize", schedule);
    panel.current?.addEventListener("focusout", schedule);
    const current = panel.current;
    return () => {
      cancelAnimationFrame(frame);
      window.removeEventListener("scroll", schedule);
      window.removeEventListener("resize", schedule);
      current?.removeEventListener("focusout", schedule);
    };
  }, [c.id]);
  const owner = c.role === "owner";
  const selfId = c.selfId || c.role;
  const execution = c.hasExecution !== false;
  const mine = execution && c.writer === selfId;
  const members = c.members || [];
  const active = members.filter((m) => m.active);
  return (
    <section
      ref={panel}
      className={`panel participants-panel ${expanded ? "" : "is-collapsed"}`}
    >
      <div className="panel-heading">
        <h2>参与成员</h2>
        <div className="participants-heading-actions">
          <span className="participants-caption">{active.length + 1} 人</span>
          <button
            className="icon-button"
            aria-expanded={expanded}
            aria-controls={bodyId}
            aria-label={expanded ? "收起成员" : "展开成员"}
            onClick={() => setManual(!expanded)}
          >
            <Icon name="chevron" size={14} />
          </button>
        </div>
      </div>
      <div
        ref={body}
        id={bodyId}
        className="participants-body"
        aria-hidden={!expanded}
        inert={!expanded}
      >
        <div className="participants-body-inner">
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
                      {member.active &&
                        member.inputRequested &&
                        " · 正在申请输入"}
                    </span>
                    <small className="muted">
                      {member.executionAccess ? "可参与共同执行" : "阅读与讨论"}
                    </small>
                  </div>
                </div>
                {owner && member.active && c.sharing && (
                  <div className="member-actions">
                    {execution && onExecutionAccess && (
                      <button
                        className="text-link"
                        disabled={busy || c.state !== "ready"}
                        onClick={() =>
                          onExecutionAccess(member.id, !member.executionAccess)
                        }
                      >
                        {member.executionAccess
                          ? "收回执行访问"
                          : "开放执行访问"}
                      </button>
                    )}
                    {mine && member.executionAccess !== false && (
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
              <p className="small-text muted">
                复制空间邀请链接，同事可使用同一链接分别加入。
              </p>
            )}
          </div>
          {owner && execution && active.some((m) => !m.executionAccess) && (
            <p className="small-text muted">
              开放执行访问会共享这个协作会话的完整原生历史和工作目录；实际输入仍需单独交接。
            </p>
          )}
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
        </div>
      </div>
    </section>
  );
}
