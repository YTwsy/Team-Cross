import { useState } from "react";
import { t } from "../i18n";
import type { Collaboration } from "../types";
import { Icon, Modal } from "./ui";

export function SpaceAvatar({
  name,
  agent = false,
}: {
  name: string;
  agent?: boolean;
}) {
  const tone =
    Array.from(name).reduce((n, char) => n + char.codePointAt(0)!, 0) % 4;
  return (
    <span
      className={`space-avatar ${agent ? "space-agent-avatar" : `space-avatar-${tone}`}`}
      aria-hidden="true"
    >
      {agent ? <Icon name="terminal" size={17} /> : Array.from(name)[0]}
    </span>
  );
}

export function spaceMembers(c: Collaboration) {
  return [
    {
      id: "owner",
      name: c.host || t("发起者"),
      online: c.role === "owner" || c.reachable !== false,
    },
    ...(c.members || []).filter(
      (member) => member.active && member.id !== "owner",
    ),
  ];
}

export function SpacePeople({
  collaboration: c,
}: {
  collaboration: Collaboration;
}) {
  const [open, setOpen] = useState(false);
  const members = spaceMembers(c);
  return (
    <>
      <button
        className="space-people"
        aria-label={t("空间成员")}
        onClick={() => setOpen(true)}
      >
        {members.slice(0, 4).map((member) => (
          <SpaceAvatar key={member.id} name={member.name} />
        ))}
        {members.length > 4 && <span>+{members.length - 4}</span>}
      </button>
      {open && (
        <Modal title={t("空间成员")} onClose={() => setOpen(false)}>
          <div className="space-member-list">
            {members.map((member) => (
              <div className="space-member" key={member.id}>
                <SpaceAvatar name={member.name} />
                <div>
                  <strong>{member.name}</strong>
                  <small>
                    {member.id === "owner" ? `${t("发起者")} · ` : ""}
                    {member.online ? t("在线") : t("离线")}
                  </small>
                </div>
              </div>
            ))}
          </div>
        </Modal>
      )}
    </>
  );
}
