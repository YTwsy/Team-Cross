import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { PairConversation, SendToAgent } from "../components/AgentPairings";

const ref = {
  spaceId: "space",
  kind: "annotation" as const,
  annotationId: "note",
};
let calls: { path: string; body: any }[];
let failure: boolean;
let state: string;
beforeEach(() => {
  calls = [];
  failure = false;
  state = "submitted";
  localStorage.clear();
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const path = url.replace("/api/", ""),
        body = init?.body ? JSON.parse(String(init.body)) : undefined;
      calls.push({ path, body });
      if (path === "agent-requests" && body && failure)
        throw new TypeError("response lost");
      const result =
        path === "agent-pairings"
          ? body
            ? {
                pairing: {
                  id: "new",
                  name: body.name,
                  state: "waiting",
                  expiresAt: "2030-10-03T02:30:00+08:00",
                },
                code: "TCP-FIXTURE",
              }
            : [
                {
                  id: "target",
                  name: "方案讨论",
                  state: "paired",
                  provider: "codex",
                  spaceId: "space",
                },
              ]
          : path === "agent-requests"
            ? { ...body, id: body.requestId, state: "submitted" }
            : {
                id: path.split("/")[1],
                pairingId: "target",
                state,
                summary: state === "completed" ? "已核对原文" : undefined,
              };
      return { ok: true, json: async () => result };
    }),
  );
});
afterEach(() => vi.unstubAllGlobals());

it("pairs through a one-time prompt without treating creation as completion", async () => {
  const paired = vi.fn();
  render(<PairConversation onPaired={paired} />);
  await userEvent.type(
    screen.getByRole("textbox", { name: "接收会话名称" }),
    "方案讨论",
  );
  await userEvent.click(screen.getByRole("button", { name: "生成配对提示" }));
  expect(await screen.findByText("等待目标会话配对")).toBeVisible();
  expect(screen.getByRole("button", { name: "复制配对提示" })).toBeEnabled();
  expect(paired).not.toHaveBeenCalled();
  expect(calls.filter((c) => c.path === "agent-pairings" && c.body)).toEqual([
    { path: "agent-pairings", body: { name: "方案讨论" } },
  ]);
});

it("requires explicit target selection and preserves analyze-only intent", async () => {
  render(<SendToAgent references={[ref]} />);
  await screen.findByRole("option", { name: /方案讨论/ });
  expect(screen.getByRole("button", { name: "确认发送" })).toBeDisabled();
  await userEvent.selectOptions(
    screen.getByRole("combobox", { name: "接收会话" }),
    "target",
  );
  await userEvent.click(screen.getByRole("button", { name: "确认发送" }));
  expect(await screen.findByText("请求已提交，等待 Agent 读取")).toBeVisible();
  const posted = calls.filter((c) => c.path === "agent-requests" && c.body);
  expect(posted).toHaveLength(1);
  expect(posted[0]!.body).toMatchObject({
    pairingId: "target",
    references: [ref],
    intent: "analyze",
  });
  expect(
    screen.queryByRole("button", { name: "确认发送" }),
  ).not.toBeInTheDocument();
});

it("only reports completion after an Agent receipt and supports explicit annotation replies", async () => {
  state = "completed";
  render(<SendToAgent references={[ref]} />);
  await screen.findByRole("option", { name: /方案讨论/ });
  await userEvent.selectOptions(
    screen.getByRole("combobox", { name: "接收会话" }),
    "target",
  );
  await userEvent.selectOptions(
    screen.getByRole("combobox", { name: "处理方式" }),
    "analyze_reply",
  );
  await userEvent.click(screen.getByRole("button", { name: "确认发送" }));
  expect(await screen.findByText("Agent 已报告处理完成")).toBeVisible();
  expect(screen.getByText("已核对原文")).toBeVisible();
  expect(
    calls.find((c) => c.path === "agent-requests" && c.body)?.body.intent,
  ).toBe("analyze_reply");
});

it("queries the same request after a lost response without submitting again", async () => {
  failure = true;
  state = "received";
  render(<SendToAgent references={[ref]} />);
  await screen.findByRole("option", { name: /方案讨论/ });
  await userEvent.selectOptions(
    screen.getByRole("combobox", { name: "接收会话" }),
    "target",
  );
  await userEvent.click(screen.getByRole("button", { name: "确认发送" }));
  expect(
    await screen.findByText("Agent 已读取请求，尚未报告处理完成"),
  ).toBeVisible();
  const posted = calls.filter((c) => c.path === "agent-requests" && c.body);
  expect(posted).toHaveLength(1);
  await waitFor(() =>
    expect(
      calls.some(
        (c) => c.path === `agent-requests/${posted[0]!.body.requestId}`,
      ),
    ).toBe(true),
  );
  expect(
    screen.queryByRole("button", { name: "确认发送" }),
  ).not.toBeInTheDocument();
});
