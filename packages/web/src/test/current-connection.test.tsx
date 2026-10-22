import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { ConnectCurrentConversation } from "../components/AgentPairings";
import { setAPITransport } from "../api";
import {
  setCurrentConversation,
  type CurrentConnection,
} from "../currentConversation";
import { setLanguageInfo } from "../i18n";

let connection: CurrentConnection;
let connect: ReturnType<typeof vi.fn>;
let reset: ReturnType<typeof vi.fn>;
beforeEach(() => {
  setLanguageInfo({ mode: "zh-CN", resolved: "zh-CN" });
  connection = {
    known: true,
    linked: true,
    receiving: false,
    targetId: "target",
    name: "顺流感知设计",
  };
  connect = vi.fn(async () => {});
  reset = vi.fn();
  setCurrentConversation({
    status: () => undefined,
    send: vi.fn(),
    connect,
    allowConnectAgain: reset,
    connectionStatus: true,
  });
  setAPITransport(async () => structuredClone(connection));
});
afterEach(() => {
  setCurrentConversation();
  setAPITransport();
});

it("shows an existing association on reopening and explicitly retries only reception", async () => {
  render(<ConnectCurrentConversation spaceId="space" />);
  expect(
    await screen.findByRole("heading", { name: "当前会话已关联" }),
  ).toBeVisible();
  expect(screen.getByText("顺流感知设计")).toBeVisible();
  expect(
    screen.queryByRole("button", { name: "连接当前会话" }),
  ).not.toBeInTheDocument();
  expect(screen.queryByRole("textbox")).not.toBeInTheDocument();
  expect(connect).not.toHaveBeenCalled();
  await userEvent.click(
    screen.getByRole("button", { name: "重新检查接收连接" }),
  );
  expect(reset).toHaveBeenCalledExactlyOnceWith("space");
  expect(connect).toHaveBeenCalledExactlyOnceWith("space", "");
});

it("shows a receiving connection without a redundant connect action", async () => {
  connection.receiving = true;
  render(<ConnectCurrentConversation spaceId="space" />);
  expect(await screen.findByText("可以接收")).toBeVisible();
  expect(screen.queryByRole("button")).not.toBeInTheDocument();
  expect(connect).not.toHaveBeenCalled();
});

it("reconciles a submitted message against Core and observes removal", async () => {
  connection.linked = false;
  setCurrentConversation({
    status: () => "sent",
    send: vi.fn(),
    connect,
    connectionStatus: true,
  });
  render(<ConnectCurrentConversation spaceId="space" />);
  expect(
    await screen.findByText(
      "连接要求已带回当前对话，请查看身份与接收能力的确认结果。",
    ),
  ).toBeVisible();
  expect(screen.queryByText("当前会话已关联")).not.toBeInTheDocument();
  connection.linked = true;
  await waitFor(
    () => expect(screen.getByText("当前会话已关联")).toBeVisible(),
    { timeout: 3500 },
  );
  expect(
    screen.queryByText(
      "连接要求已带回当前对话，请查看身份与接收能力的确认结果。",
    ),
  ).not.toBeInTheDocument();
  connection.linked = false;
  await waitFor(
    () =>
      expect(
        screen.getByRole("heading", { name: "连接当前会话" }),
      ).toBeVisible(),
    { timeout: 3500 },
  );
  expect(connect).not.toHaveBeenCalled();
});

it("does not reuse an old space response while checking another space", async () => {
  let resolveSecond: (v: CurrentConnection) => void = () => {};
  setAPITransport(async (path) =>
    path.endsWith("=first")
      ? connection
      : new Promise<CurrentConnection>((resolve) => {
          resolveSecond = resolve;
        }),
  );
  const panel = render(<ConnectCurrentConversation spaceId="first" />);
  await screen.findByText("当前会话已关联");
  panel.rerender(<ConnectCurrentConversation spaceId="second" />);
  expect(screen.queryByText("当前会话已关联")).not.toBeInTheDocument();
  expect(
    screen.getByRole("button", { name: "正在核对连接状态…" }),
  ).toBeDisabled();
  resolveSecond({ known: true, linked: false, receiving: false });
  expect(
    await screen.findByRole("button", { name: "连接当前会话" }),
  ).toBeEnabled();
});

it("keeps unavailable identity distinct from a confirmed unlinked conversation", async () => {
  connection = { known: false, linked: false, receiving: false };
  render(<ConnectCurrentConversation spaceId="space" />);
  expect(
    await screen.findByText(
      "宿主未提供当前会话身份，请在对话中核对是否已关联。",
    ),
  ).toBeVisible();
  expect(screen.queryByText("当前会话已关联")).not.toBeInTheDocument();
  expect(connect).not.toHaveBeenCalled();
});
