import { act, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { createHash, webcrypto } from "node:crypto";
import { setTimeout as delay } from "node:timers/promises";
import {
  beginRefreshState,
  captureRefreshState,
  endRefreshState,
  hasRefreshDrafts,
  useRefreshGuard,
  useRefreshState,
} from "../refreshState";
import { watchPanelUpdates, validateBundle } from "../plugin/update";
import type { HostBridge } from "../plugin/bridge";
import { LeaveSpaceNote } from "../components/InFlow";
import { Annotations } from "../components/Annotations";
import { setAPITransport, APIError } from "../api";
import { setLanguageInfo } from "../i18n";

const style = "body{color:black}",
  script = "// new panel";
const revision = createHash("sha256")
  .update(style + "\0" + script)
  .digest("hex");
const bundle = { protocol: 1, revision, style, script };
let stop = () => {};
let pending = false;
let readBundle: () => Promise<typeof bundle>;
let replace = vi.fn(async (_bundle: unknown, _saved: unknown) => {});
const bridge = {
  get hasPendingWrites() {
    return pending;
  },
  call: vi.fn(async (_name: string, args: { path: string }) =>
    args.path === "plugin/panel-version"
      ? { protocol: 1, revision }
      : readBundle(),
  ),
} as unknown as HostBridge;
function Editor() {
  const [text, setText] = useRefreshState("test:text", "");
  useRefreshGuard("test", !!text);
  return (
    <textarea
      aria-label="draft"
      value={text}
      onChange={(e) => setText(e.target.value)}
    />
  );
}
beforeEach(() => {
  vi.useFakeTimers();
  vi.stubGlobal("crypto", webcrypto);
  setLanguageInfo({ mode: "zh-CN", resolved: "zh-CN" });
  const meta = document.createElement("meta");
  meta.name = "teamcross-panel-revision";
  meta.content = "0".repeat(64);
  document.head.append(meta);
  beginRefreshState("core-a");
  pending = false;
  readBundle = async () => bundle;
  replace = vi.fn(async () => {});
});
afterEach(() => {
  stop();
  endRefreshState();
  setAPITransport(undefined);
  document.querySelector('meta[name="teamcross-panel-revision"]')?.remove();
  vi.useRealTimers();
  vi.unstubAllGlobals();
  vi.clearAllMocks();
});
async function tick(ms = 1600) {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(ms);
  });
}
async function settle() {
  await act(async () => {
    await delay(20);
  });
}

it("automatically loads the actual new bundle when no drafts exist", async () => {
  render(<Editor />);
  stop = watchPanelUpdates(bridge, vi.fn(), replace);
  await tick();
  await settle();
  expect(replace).toHaveBeenCalledWith(bundle, {
    scope: "core-a",
    scrollY: 0,
    values: expect.any(Object),
  });
});
it("offers a deliberate refresh for drafts and restores text only in the same Core", async () => {
  const view = render(<Editor />);
  fireEvent.change(screen.getByRole("textbox"), {
    target: { value: "尚未保存的意见" },
  });
  stop = watchPanelUpdates(bridge, vi.fn(), replace);
  await tick();
  expect(replace).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: "保留草稿并刷新" }));
  await settle();
  expect(replace).toHaveBeenCalledTimes(1);
  const saved = captureRefreshState();
  view.unmount();
  endRefreshState();
  beginRefreshState("core-a", saved);
  const restored = render(<Editor />);
  expect(screen.getByRole("textbox")).toHaveValue("尚未保存的意见");
  restored.unmount();
  endRefreshState();
  beginRefreshState("core-b", saved);
  render(<Editor />);
  expect(screen.getByRole("textbox")).toHaveValue("");
});
it("does not replace a draft typed while downloading the bundle", async () => {
  let release!: (value: typeof bundle) => void;
  readBundle = () =>
    new Promise((resolve) => {
      release = resolve;
    });
  render(<Editor />);
  stop = watchPanelUpdates(bridge, vi.fn(), replace);
  await tick();
  fireEvent.change(screen.getByRole("textbox"), {
    target: { value: "下载期间输入" },
  });
  await act(async () => release(bundle));
  await settle();
  expect(replace).not.toHaveBeenCalled();
  expect(screen.getByRole("textbox")).toHaveValue("下载期间输入");
});
it("waits for pending writes and IME composition without sending any business operation", async () => {
  pending = true;
  render(<Editor />);
  stop = watchPanelUpdates(bridge, vi.fn(), replace);
  await tick();
  expect(replace).not.toHaveBeenCalled();
  fireEvent.compositionStart(screen.getByRole("textbox"));
  pending = false;
  await tick(1000);
  expect(replace).not.toHaveBeenCalled();
  fireEvent.compositionEnd(screen.getByRole("textbox"));
  await tick(1000);
  await settle();
  expect(replace).toHaveBeenCalledTimes(1);
  expect(
    vi
      .mocked(bridge.call)
      .mock.calls.every(([name]) => name === "teamcross_ui_read"),
  ).toBe(true);
});
it("preserves the editor and allows explicit retry after a failed update", async () => {
  render(<Editor />);
  readBundle = async () => {
    throw Error("Core restarting");
  };
  stop = watchPanelUpdates(bridge, vi.fn(), replace);
  await tick();
  expect(screen.getByText("刷新未完成，草稿已保留，请重试。")).toBeVisible();
  await tick(2000);
  expect(replace).not.toHaveBeenCalled();
  readBundle = async () => bundle;
  fireEvent.click(screen.getByRole("button", { name: "刷新到新版" }));
  await settle();
  expect(replace).toHaveBeenCalledTimes(1);
});
it("rejects truncated, changed or mismatched update code before execution", async () => {
  await expect(
    validateBundle({ ...bundle, script: "different" }, revision),
  ).rejects.toThrow();
  await expect(validateBundle(bundle, "0".repeat(64))).rejects.toThrow();
});
it("restores an open note with its unknown request identity and never resends it", async () => {
  const calls: { path: string; body: any }[] = [];
  setAPITransport(async (path, body) => {
    calls.push({ path, body });
    throw new APIError("connection lost");
  });
  const props = {
    spaceId: "space",
    targetId: "target",
    name: "Agent",
    enabled: true,
    disabled: false,
    notes: [],
    onChanged: vi.fn(),
  };
  const view = render(<LeaveSpaceNote {...props} />);
  fireEvent.click(screen.getByRole("button", { name: "留言" }));
  fireEvent.change(screen.getByRole("textbox", { name: "留言正文" }), {
    target: { value: "保留正文和原请求标识" },
  });
  await act(async () =>
    fireEvent.click(screen.getByRole("button", { name: "保存留言" })),
  );
  expect(calls).toHaveLength(1);
  const requestId = calls[0]!.body.requestId;
  const saved = captureRefreshState();
  view.unmount();
  endRefreshState();
  beginRefreshState("core-a", saved);
  render(<LeaveSpaceNote {...props} />);
  expect(screen.getByRole("textbox", { name: "留言正文" })).toHaveValue(
    "保留正文和原请求标识",
  );
  expect(calls).toHaveLength(1);
  await act(async () =>
    fireEvent.click(screen.getByRole("button", { name: "核对留言状态" })),
  );
  expect(calls[1]).toEqual({
    path: "collaborations/space/workbench/inflow-note-read",
    body: { requestId },
  });
});
it("retains comment drafts for independent sessions without cross-delivery", () => {
  const props = {
    annotations: [],
    disabled: false,
    onSaved: vi.fn(),
    onLocate: vi.fn(),
  };
  const view = render(
    <>
      <Annotations id="space/sessions/a" {...props} />
      <Annotations id="space/sessions/b" {...props} />
    </>,
  );
  const editors = screen.getAllByRole("textbox");
  fireEvent.change(editors[0]!, { target: { value: "会话 A 的草稿" } });
  fireEvent.change(editors[1]!, { target: { value: "会话 B 的草稿" } });
  expect(hasRefreshDrafts()).toBe(true);
  const saved = captureRefreshState();
  view.unmount();
  endRefreshState();
  beginRefreshState("core-a", saved);
  render(
    <>
      <Annotations id="space/sessions/b" {...props} />
      <Annotations id="space/sessions/a" {...props} />
    </>,
  );
  expect(
    screen.getAllByRole("textbox").map((e) => (e as HTMLTextAreaElement).value),
  ).toEqual(["会话 B 的草稿", "会话 A 的草稿"]);
});

it("does not carry a draft into a different scope when a component is reused", () => {
  function Scoped({ id }: { id: string }) {
    const [value, setValue] = useRefreshState(id, "");
    return <input value={value} onChange={(e) => setValue(e.target.value)} />;
  }
  const view = render(<Scoped id="first" />);
  fireEvent.change(screen.getByRole("textbox"), {
    target: { value: "only first" },
  });
  view.rerender(<Scoped id="second" />);
  expect(screen.getByRole("textbox")).toHaveValue("");
});
