import {
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import App from "../App";
import { api } from "../api";
import { setLanguageInfo } from "../i18n";
import { initializeEnvironment } from "../plugin/environment";
import { HostBridge } from "../plugin/bridge";
import { currentConversation } from "../currentConversation";
import { localStorage as preferences } from "../storage";
import type { LibraryBundle, LibraryResource } from "../library";

const resource: LibraryResource = {
  key: "material-v1",
  reference: {
    spaceId: "space",
    kind: "material",
    materialId: "m1",
    version: 1,
  },
  title: "固定材料",
  spaceTitle: "现有空间",
  sessionId: "source",
  sessionTitle: "原生调查",
  updatedAt: "2026-10-03T00:00:00Z",
  favorite: false,
  selected: true,
  annotated: false,
  availability: "available",
  replyCount: 0,
};
const bundle: LibraryBundle = {
  code: "TC-ABCDEFGHJK",
  references: [resource.reference],
  expiresAt: "2030-10-09T00:00:00Z",
};
let messages: { method: string; params: any; id?: number }[];
let capabilities: { message?: object; updateModelContext?: object };
let handler: (message: any) => unknown;
let scope: string;
let widget: any;
function route(message: any): unknown {
  if (message.method === "ui/initialize")
    return { hostCapabilities: capabilities, hostContext: { theme: "light" } };
  if (message.method !== "tools/call") return {};
  const { path, body } = message.params.arguments;
  const value =
    path === "plugin/bootstrap"
      ? { scope }
      : path === "ui-language"
        ? { mode: "zh-CN", resolved: "zh-CN" }
        : path === "collaborations"
          ? []
          : path === "library"
            ? { resources: [resource], selection: [resource.key] }
            : path === "library/bundles"
              ? { ...bundle, references: body.references }
              : path === "agent-pairings" || path === "agent-requests"
                ? []
                : path === "sources?provider=codex"
                  ? { data: [] }
                  : {};
  return { structuredContent: value };
}
beforeEach(() => {
  vi.stubGlobal(
    "matchMedia",
    vi.fn(() => ({
      matches: false,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
    })),
  );
  messages = [];
  scope = "fixture";
  widget = undefined;
  capabilities = { message: {}, updateModelContext: {} };
  handler = route;
  location.hash = "";
  sessionStorage.clear();
  localStorage.clear();
  setLanguageInfo({ mode: "zh-CN", resolved: "zh-CN" });
  window.openai = {
    setWidgetState: (value) => {
      widget = (value as { privateContent: unknown }).privateContent;
      window.openai!.widgetState = value;
    },
  };
  vi.spyOn(window.parent, "postMessage").mockImplementation((message) => {
    messages.push(message);
    if (!message.id) return;
    queueMicrotask(() => {
      const result = handler(message);
      window.dispatchEvent(
        new MessageEvent("message", {
          source: window.parent,
          data:
            result instanceof Error
              ? {
                  jsonrpc: "2.0",
                  id: message.id,
                  error: { message: result.message },
                }
              : { jsonrpc: "2.0", id: message.id, result },
        }),
      );
    });
  });
  vi.stubGlobal(
    "fetch",
    vi.fn(() => {
      throw new Error("Plugin must use the host bridge");
    }),
  );
});
afterEach(() => {
  window.dispatchEvent(new Event("pagehide"));
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  delete window.openai;
});

it("connects only after an explicit host message and does not replay a lost acknowledgement", async () => {
  await initializeEnvironment();
  expect(messages.filter((m) => m.method === "ui/message")).toHaveLength(0);
  handler = (m) => (m.method === "ui/message" ? { isError: true } : route(m));
  await expect(
    currentConversation()!.connect!("space", "Current investigation"),
  ).rejects.toThrow();
  expect(currentConversation()!.status("connect:space")).toBe("unknown");
  await currentConversation()!.connect!("space", "Current investigation");
  expect(messages.filter((m) => m.method === "ui/message")).toHaveLength(1);
  const sent = messages.find((m) => m.method === "ui/message")!;
  expect(sent.params.content[0].text).toContain("connect_current_session");
  expect(sent.params.content[0].text).toContain('"spaceId":"space"');
  window.dispatchEvent(new Event("pagehide"));
  await initializeEnvironment();
  expect(currentConversation()!.status("connect:space")).toBe("unknown");
  currentConversation()!.allowConnectAgain!("space");
  expect(messages.filter((m) => m.method === "ui/message")).toHaveLength(1);
  expect(currentConversation()!.status("connect:space")).toBeUndefined();
});

it("mounts the entire existing App, including navigation, creation and pairing pages", async () => {
  await initializeEnvironment();
  render(<App />);
  expect(
    await screen.findByRole("heading", { name: "协作空间", level: 1 }),
  ).toBeVisible();
  const nav = screen.getByRole("navigation", { name: "主导航" });
  expect(within(nav).getByRole("link", { name: "资源库" })).toHaveAttribute(
    "href",
    "#/library",
  );
  expect(screen.getByRole("link", { name: /发起协作/ })).toHaveAttribute(
    "href",
    "#/create",
  );
  fireEvent.click(within(nav).getByRole("link", { name: "设置与连接" }));
  await screen.findByRole("heading", { name: "设置与连接", level: 1 });
  await waitFor(() =>
    expect(
      messages.some((m) => m.params?.arguments?.path === "agent-pairings"),
    ).toBe(true),
  );
  expect(fetch).not.toHaveBeenCalled();
  expect(messages.some((m) => m.method === "ui/message")).toBe(false);
});

it("uses the existing selection entry and sends only its pinned references on an explicit click", async () => {
  await initializeEnvironment();
  render(<App />);
  fireEvent.click(await screen.findByRole("button", { name: "生成读取入口" }));
  await screen.findByText(bundle.code);
  const staged = messages.find(
    (m) => m.params?.arguments?.path === "library/bundles",
  )!;
  expect(staged.params).toMatchObject({
    name: "teamcross_ui_write",
    arguments: { body: { references: [resource.reference] } },
  });
  expect(messages.some((m) => m.method === "ui/message")).toBe(false);
  fireEvent.click(screen.getByRole("button", { name: "带回当前对话" }));
  await screen.findByText("消息已交给当前会话，请在会话中查看处理结果。");
  expect(messages.filter((m) => m.method === "ui/message")).toHaveLength(1);
  expect(
    messages.find((m) => m.method === "ui/update-model-context")?.params
      .structuredContent.references,
  ).toEqual([resource.reference]);
  expect(widget.deliveries[bundle.code]).toBe("sent");
  expect(window.openai!.widgetState).toEqual({ privateContent: widget });
  expect(screen.getByRole("button", { name: "带回当前对话" })).toBeDisabled();
});

it("keeps the regular copy entry when the host does not support conversation messages", async () => {
  capabilities = {};
  await initializeEnvironment();
  render(<App />);
  fireEvent.click(await screen.findByRole("button", { name: "生成读取入口" }));
  await screen.findByText(bundle.code);
  expect(screen.getByRole("button", { name: "复制读取提示" })).toBeEnabled();
  expect(
    screen.queryByRole("button", { name: "带回当前对话" }),
  ).not.toBeInTheDocument();
});

it("restores private route and preferences without requiring iframe localStorage", async () => {
  vi.spyOn(window, "localStorage", "get").mockImplementation(() => {
    throw new DOMException("denied", "SecurityError");
  });
  await initializeEnvironment();
  preferences.setItem("teamcross.theme.v1", "dark");
  location.hash = "/settings";
  window.dispatchEvent(new Event("hashchange"));
  window.dispatchEvent(new Event("pagehide"));
  location.hash = "";
  await initializeEnvironment();
  expect(location.hash).toBe("#/settings");
  expect(preferences.getItem("teamcross.theme.v1")).toBe("dark");
  const view = render(<App />);
  await screen.findByRole("heading", { name: "设置与连接", level: 1 });
  expect(document.documentElement.dataset.theme).toBe("dark");
  view.unmount();
  window.dispatchEvent(new Event("pagehide"));
  scope = "another-core";
  location.hash = "";
  await initializeEnvironment();
  expect(preferences.getItem("teamcross.theme.v1")).toBeNull();
  expect(location.hash).toBe("");
});

it("retains an uncertain delivery across reopening and never automatically replays it", async () => {
  handler = (m) =>
    m.method === "ui/message" ? new Error("acknowledgement lost") : route(m);
  await initializeEnvironment();
  await expect(currentConversation()!.send(bundle, "核对原文")).rejects.toThrow(
    "acknowledgement lost",
  );
  expect(currentConversation()!.status(bundle.code)).toBe("unknown");
  window.dispatchEvent(new Event("pagehide"));
  await initializeEnvironment();
  await currentConversation()!.send(bundle, "核对原文");
  expect(currentConversation()!.status(bundle.code)).toBe("unknown");
  expect(messages.filter((m) => m.method === "ui/message")).toHaveLength(1);
});

it("permits an explicit retry when context update failed before a message was dispatched", async () => {
  handler = (m) =>
    m.method === "ui/update-model-context"
      ? new Error("context unavailable")
      : route(m);
  await initializeEnvironment();
  await expect(currentConversation()!.send(bundle, "核对原文")).rejects.toThrow(
    "context unavailable",
  );
  expect(currentConversation()!.status(bundle.code)).toBeUndefined();
  expect(messages.some((m) => m.method === "ui/message")).toBe(false);
  handler = route;
  await currentConversation()!.send(bundle, "核对原文");
  expect(currentConversation()!.status(bundle.code)).toBe("sent");
});

it("preserves read arguments, writes, Core errors and cancellation through the host", async () => {
  const bridge = new HostBridge();
  await bridge.initialize();
  await api("collaborations/space/read-material", { version: 1 });
  await api("collaborations/space/annotation-replies", {
    requestId: "fixed",
    text: "exact reply",
  });
  expect(
    messages.filter((m) => m.method === "tools/call").map((m) => m.params),
  ).toEqual([
    {
      name: "teamcross_ui_read",
      arguments: {
        path: "collaborations/space/read-material",
        body: { version: 1 },
      },
    },
    {
      name: "teamcross_ui_write",
      arguments: {
        path: "collaborations/space/annotation-replies",
        body: { requestId: "fixed", text: "exact reply" },
      },
    },
  ]);
  handler = () => ({
    isError: true,
    structuredContent: { error: "access revoked", code: "forbidden" },
  });
  await expect(api("library")).rejects.toMatchObject({ code: "forbidden" });
  const abort = new AbortController();
  abort.abort();
  const count = messages.length;
  await expect(api("library", undefined, abort.signal)).rejects.toMatchObject({
    name: "AbortError",
  });
  expect(messages.length).toBe(count);
  bridge.dispose();
});

it("honors negative message receipts instead of claiming success", async () => {
  handler = (m) => (m.method === "ui/message" ? { isError: true } : route(m));
  await initializeEnvironment();
  await expect(
    currentConversation()!.send(bundle, "核对原文"),
  ).rejects.toThrow();
  expect(currentConversation()!.status(bundle.code)).toBe("unknown");
});

it("submits the existing React form once in a restricted host and keeps native validation", async () => {
  await initializeEnvironment();
  const submit = vi.fn((event: React.FormEvent) => event.preventDefault());
  render(
    <form onSubmit={submit}>
      <input required aria-label="Required value" />
      <button type="submit">Save form</button>
    </form>,
  );
  fireEvent.click(screen.getByRole("button", { name: "Save form" }));
  expect(submit).not.toHaveBeenCalled();
  fireEvent.change(screen.getByRole("textbox"), { target: { value: "valid" } });
  fireEvent.click(screen.getByRole("button", { name: "Save form" }));
  expect(submit).toHaveBeenCalledTimes(1);
});
