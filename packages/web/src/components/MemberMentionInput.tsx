import { useId, useState, type RefObject, type KeyboardEvent } from "react";
import { useResource } from "../api";
import { editedMentions } from "../activity";
import { t } from "../i18n";
import type { MemberMention } from "../types";

type Member = { id: string; name: string; online: boolean };
export function MemberMentionInput({
  spaceId,
  value,
  mentions,
  onChange,
  editor,
  id,
  disabled,
  placeholder,
  onKeyDown,
}: {
  spaceId: string;
  value: string;
  mentions: MemberMention[];
  onChange: (text: string, mentions: MemberMention[]) => void;
  editor: RefObject<HTMLTextAreaElement | null>;
  id: string;
  disabled?: boolean;
  placeholder?: string;
  onKeyDown?: (e: KeyboardEvent<HTMLTextAreaElement>) => void;
}) {
  const [query, setQuery] = useState<{
    start: number;
    end: number;
    text: string;
  }>();
  const [selected, setSelected] = useState(0);
  const listId = useId();
  const members = useResource<Member[]>(
    query ? `collaborations/${spaceId}/mention-members` : null,
  );
  const choices = (members.data || [])
    .filter((m) =>
      m.name
        .toLocaleLowerCase()
        .includes(query?.text.toLocaleLowerCase() || ""),
    )
    .slice(0, 20);
  function detect(text: string, caret: number) {
    const match = text.slice(0, caret).match(/(?:^|\s)@([^\s@]*)$/u);
    setQuery(
      match
        ? { start: caret - match[1]!.length - 1, end: caret, text: match[1]! }
        : undefined,
    );
    setSelected(0);
  }
  function choose(member: Member) {
    if (!query) return;
    const replacement = `@${member.name}`;
    const text =
      value.slice(0, query.start) + replacement + " " + value.slice(query.end);
    const next = [
      ...editedMentions(value, text, mentions),
      {
        memberId: member.id,
        label: member.name,
        startOffset: query.start,
        endOffset: query.start + replacement.length,
      },
    ].sort((a, b) => a.startOffset - b.startOffset);
    onChange(text, next);
    setQuery(undefined);
    requestAnimationFrame(() => {
      editor.current?.focus();
      editor.current?.setSelectionRange(
        query.start + replacement.length + 1,
        query.start + replacement.length + 1,
      );
    });
  }
  return (
    <div className="member-mention-input">
      <textarea
        ref={editor}
        id={id}
        rows={3}
        maxLength={4000}
        value={value}
        disabled={disabled}
        placeholder={placeholder}
        aria-autocomplete="list"
        aria-controls={query ? listId : undefined}
        aria-expanded={!!query}
        aria-activedescendant={
          query && choices[selected] ? `${listId}-${selected}` : undefined
        }
        onChange={(e) => {
          const text = e.target.value;
          onChange(text, editedMentions(value, text, mentions));
          if (!(e.nativeEvent as InputEvent).isComposing)
            detect(text, e.target.selectionStart);
        }}
        onCompositionEnd={(e) =>
          detect(e.currentTarget.value, e.currentTarget.selectionStart)
        }
        onKeyDown={(e) => {
          if (e.nativeEvent.isComposing) return;
          if (query) {
            if (e.key === "Escape") {
              e.preventDefault();
              setQuery(undefined);
              return;
            }
            if (["ArrowDown", "ArrowUp"].includes(e.key) && choices.length) {
              e.preventDefault();
              setSelected(
                (v) =>
                  (v + (e.key === "ArrowDown" ? 1 : choices.length - 1)) %
                  choices.length,
              );
              return;
            }
            if (
              e.key === "Enter" &&
              !e.ctrlKey &&
              !e.metaKey &&
              choices[selected]
            ) {
              e.preventDefault();
              choose(choices[selected]!);
              return;
            }
          }
          onKeyDown?.(e);
        }}
      />
      {query && (
        <div
          className="member-mention-menu"
          role="listbox"
          id={listId}
          aria-label={t("选择要 @ 的成员")}
        >
          {choices.map((member, index) => (
            <button
              type="button"
              role="option"
              aria-selected={index === selected}
              id={`${listId}-${index}`}
              key={member.id}
              onMouseDown={(e) => e.preventDefault()}
              onClick={() => choose(member)}
            >
              <strong>{member.name}</strong>
              <small>
                {member.id === "owner" ? t("发起者") : member.id.slice(0, 8)} ·{" "}
                {member.online ? t("在线") : t("离线")}
              </small>
            </button>
          ))}
          {!choices.length && (
            <p>
              {members.error ||
                (members.loading ? t("正在加载…") : t("没有匹配的成员"))}
            </p>
          )}
        </div>
      )}
    </div>
  );
}
