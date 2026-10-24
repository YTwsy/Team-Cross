import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { useRef, useState } from "react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { MemberActivity, ActivitySource } from "../components/MemberActivity";
import { MemberMentionInput } from "../components/MemberMentionInput";
import { Discussion } from "../components/Annotations";
import {
  editedMentions,
  type ActivityPage,
  type ActivityItem,
} from "../activity";
import type { Annotation, MemberMention } from "../types";

it.each(["message", "result"])(
  "reads an exact shadow %s source and clears unavailable content without marking human read",
  async (kind) => {
    let available = true;
    const reads: string[] = [];
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: string, init?: RequestInit) => {
        reads.push(String(url));
        expect(init?.body).toBeUndefined();
        return {
          ok: true,
          json: async () =>
            available
              ? kind === "message"
                ? {
                    message: {
                      text: "Original shadow answer",
                      actor: { name: "Fixture shadow" },
                    },
                  }
                : {
                    result: {
                      title: "Compatibility",
                      taskVersion: 1,
                      state: "completed",
                      text: "Original task evidence",
                      completionChecks: ["Verified original requirement"],
                    },
                  }
              : { unavailable: "shadow_result_unavailable" },
        };
      }),
    );
    const item: ActivityItem = {
      id: "fixed-shadow-source",
      seq: 0,
      kind: "source",
      subject: { kind: "brief", id: "space" },
      source: {
        spaceId: "space",
        shadowId: "shadow",
        ...(kind === "message"
          ? { shadowMessageId: "reply-original" }
          : { shadowResultId: "result-original" }),
      },
      actor: { kind: "shadow", name: "Fixture shadow" },
      createdAt: "",
      preview: "New version must not replace old evidence",
      reasons: [],
      unread: false,
      preserveReadState: true,
    };
    const rendered = render(<ActivitySource item={item} onClose={() => {}} />);
    const original =
      kind === "message" ? "Original shadow answer" : "Original task evidence";
    expect(await screen.findByText(original)).toBeVisible();
    expect(reads[0]).toContain(
      kind === "message"
        ? "messageId=reply-original"
        : "resultId=result-original",
    );
    if (kind === "result")
      expect(screen.getByText("Verified original requirement")).toBeVisible();
    available = false;
    rendered.rerender(
      <ActivitySource
        item={{ ...item, id: "read-again" }}
        onClose={() => {}}
      />,
    );
    expect(
      await screen.findByText("该分身消息或任务结果已不可用"),
    ).toBeVisible();
    expect(screen.queryByText(original)).not.toBeInTheDocument();
    expect(screen.queryByText(item.preview)).not.toBeInTheDocument();
    expect(reads.every((path) => path.includes("/shadows/source?"))).toBe(true);
  },
);

let page: ActivityPage;
let sourceAvailable: boolean;
let writes: { path: string; body: any }[];
const note: Annotation = {
  id: "activity-discussion",
  text: "Root",
  author: "Owner",
  createdAt: "2026-10-23T10:00:00Z",
  replies: [
    {
      id: "activity-reply",
      requestId: "existing",
      text: "A reply",
      author: "B",
      createdAt: "2026-10-23T10:01:00Z",
    },
  ],
};
beforeEach(() => {
  sessionStorage.clear();
  sourceAvailable = true;
  writes = [];
  const item = {
    id: "event-reply",
    seq: 2,
    kind: "reply",
    source: {
      spaceId: "activity-test",
      annotationId: note.id,
      replyId: "activity-reply",
    },
    subject: { kind: "discussion" as const, id: note.id },
    actor: { kind: "human", name: "B" },
    createdAt: "2026-10-23T10:01:00Z",
    preview: "A reply",
    reasons: ["direct_reply", "follow_discussion"],
    unread: true,
  };
  page = {
    spaceId: "activity-test",
    title: "Test space",
    selfId: "owner",
    groups: [
      { id: "group", latest: item, events: [item], count: 1, unread: 1 },
    ],
    unread: 1,
    snapshot: "snapshot-2",
    revision: 2,
    readRevision: 0,
    syncedAt: "2026-10-23T10:01:00Z",
  };
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      let value: any = {};
      if (init?.body) {
        const body = JSON.parse(String(init.body));
        writes.push({ path: String(url), body });
        if (String(url).endsWith("activity/read")) {
          page = {
            ...page,
            unread: 0,
            groups: page.groups.map((g) => ({
              ...g,
              unread: 0,
              latest: { ...g.latest, unread: false },
            })),
          };
          value = page;
        }
        if (String(url).endsWith("annotation-replies"))
          value = {
            ...note,
            replies: [
              ...note.replies!,
              {
                id: "new",
                author: "Owner",
                createdAt: new Date().toISOString(),
                ...body,
              },
            ],
          };
      } else if (String(url).includes("mention-members"))
        value = [
          { id: "owner", name: "Owner", online: true },
          { id: "member-b", name: "Bee", online: false },
        ];
      else if (String(url).includes("follows")) value = [];
      else if (String(url).includes("context?kind=annotations"))
        value = { annotations: sourceAvailable ? [note] : [] };
      else if (String(url).includes("/activity?")) value = page;
      return { ok: true, status: 200, json: async () => value };
    }),
  );
});
afterEach(() => vi.unstubAllGlobals());
it("keeps space activity inside the overview section and clears only an explicit snapshot", async () => {
  render(<MemberActivity spaceId="activity-test" />);
  expect(await screen.findByText("A reply")).toBeInTheDocument();
  expect(
    screen.queryByRole("link", { name: "空间动态" }),
  ).not.toBeInTheDocument();
  expect(writes).toHaveLength(0);
  fireEvent.click(screen.getByRole("button", { name: /查看全部动态/ }));
  fireEvent.click(screen.getByRole("button", { name: "本空间全部已读" }));
  await waitFor(() =>
    expect(writes[0]?.body).toEqual({ snapshot: "snapshot-2" }),
  );
  await waitFor(() =>
    expect(screen.queryByLabelText("未读")).not.toBeInTheDocument(),
  );
});
it("stores selected mentions with UTF-16 offsets and drops a mention when edited", async () => {
  function Composer() {
    const [value, setValue] = useState("");
    const [mentions, setMentions] = useState<MemberMention[]>([]);
    const editor = useRef<HTMLTextAreaElement>(null);
    return (
      <>
        <label htmlFor="mention-input">Message</label>
        <MemberMentionInput
          id="mention-input"
          spaceId="activity-test"
          editor={editor}
          value={value}
          mentions={mentions}
          onChange={(text, next) => {
            setValue(text);
            setMentions(next);
          }}
        />
        <output data-testid="mentions">{JSON.stringify(mentions)}</output>
      </>
    );
  }
  render(<Composer />);
  const editor = screen.getByRole("textbox");
  fireEvent.change(editor, { target: { value: "🙂 @Ow", selectionStart: 6 } });
  expect(screen.getByTestId("mentions")).toHaveTextContent("[]");
  fireEvent.click(await screen.findByRole("option", { name: /Owner/ }));
  expect(JSON.parse(screen.getByTestId("mentions").textContent!)).toEqual([
    { memberId: "owner", label: "Owner", startOffset: 3, endOffset: 9 },
  ]);
  fireEvent.change(editor, { target: { value: "🙂 @Someone " } });
  expect(screen.getByTestId("mentions")).toHaveTextContent("[]");
  expect(
    editedMentions("@Owner", "hello @Owner", [
      { memberId: "owner", label: "Owner", startOffset: 0, endOffset: 6 },
    ])[0]?.startOffset,
  ).toBe(6);
});
it("replies to the selected reply without guessing the last speaker", async () => {
  render(
    <Discussion
      id="activity-test"
      annotation={note}
      disabled={false}
      onSaved={() => {}}
      onLocate={() => {}}
    />,
  );
  fireEvent.click(screen.getByRole("button", { name: "回复这条回复" }));
  fireEvent.change(screen.getByRole("textbox"), {
    target: { value: "Reply to B" },
  });
  fireEvent.click(screen.getByRole("button", { name: "保存回复" }));
  await waitFor(() =>
    expect(writes.some((w) => w.path.endsWith("annotation-replies"))).toBe(
      true,
    ),
  );
  expect(
    writes.find((w) => w.path.endsWith("annotation-replies"))?.body,
  ).toMatchObject({
    annotationId: note.id,
    text: "Reply to B",
    replyTo: { kind: "reply", id: "activity-reply" },
    mentions: [],
  });
});

it("clears loaded source content when access revalidation fails", async () => {
  render(<MemberActivity spaceId="activity-test" />);
  fireEvent.click(await screen.findByRole("button", { name: /B 回复了你/ }));
  expect(await screen.findByText("Root")).toBeInTheDocument();
  sourceAvailable = false;
  await waitFor(
    () => expect(screen.queryByText("Root")).not.toBeInTheDocument(),
    { timeout: 5500 },
  );
  expect(screen.getByText("来源已不可用")).toBeInTheDocument();
});

it("reads the exact brief item version and clears unavailable historical content", async () => {
  let available = true;
  const reads: string[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      reads.push(String(url));
      expect(init?.body).toBeUndefined();
      return {
        ok: true,
        json: async () =>
          available
            ? {
                revision: 2,
                item: {
                  itemId: "decision",
                  text: "Historical brief evidence",
                  status: "confirmed",
                  sources: [],
                },
              }
            : { unavailable: "brief_revision_unavailable", revision: 2 },
      };
    }),
  );
  const item: ActivityItem = {
    id: "brief-source",
    seq: 0,
    source: {
      spaceId: "activity-test",
      briefRevision: 2,
      briefItemId: "decision",
    },
    subject: { kind: "brief", id: "activity-test" },
    kind: "brief_updated",
    actor: { kind: "shadow", name: "Shadow" },
    createdAt: "",
    preview: "Latest text must not replace history",
    reasons: [],
    unread: false,
    preserveReadState: true,
  };
  const rendered = render(<ActivitySource item={item} onClose={() => {}} />);
  expect(await screen.findByText("Historical brief evidence")).toBeVisible();
  expect(reads[0]).toContain("brief-read?revision=2&itemId=decision");
  expect(
    screen.queryByText("Latest text must not replace history"),
  ).not.toBeInTheDocument();
  available = false;
  rendered.rerender(
    <ActivitySource
      item={{ ...item, id: "brief-source-again" }}
      onClose={() => {}}
    />,
  );
  expect(await screen.findByText("该简报版本或条目已不可用")).toBeVisible();
  expect(
    screen.queryByText("Historical brief evidence"),
  ).not.toBeInTheDocument();
  expect(reads.every((path) => path.includes("brief-read?revision=2"))).toBe(
    true,
  );
});
