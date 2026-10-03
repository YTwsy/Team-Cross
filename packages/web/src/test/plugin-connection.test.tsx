import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import {
  ChatGPTPluginConnection,
  type PluginConnection,
} from "../components/ChatGPTPluginConnection";

let state: PluginConnection;
let writes: unknown[];
let failure = false;
beforeEach(() => {
  state = {
    state: "not_installed",
    available: true,
    autoUpdate: false,
    installed: false,
    pluginEnabled: false,
    reloadRequired: false,
    differentData: false,
  };
  writes = [];
  failure = false;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (_url: string, init?: RequestInit) => {
      if (init?.body) {
        const body = JSON.parse(String(init.body));
        writes.push(body);
        if (failure) throw new TypeError("lost response");
        state =
          body.action === "disconnect"
            ? {
                ...state,
                state: "not_installed",
                installed: false,
                autoUpdate: false,
                reloadRequired: false,
              }
            : {
                ...state,
                state: "reload_required",
                installed: true,
                autoUpdate: true,
                reloadRequired: true,
                differentData: false,
              };
      }
      return { ok: true, json: async () => ({ ...state }) };
    }),
  );
});
afterEach(() => vi.unstubAllGlobals());

it("connects only on click and separates installed files from a loaded interface", async () => {
  render(<ChatGPTPluginConnection />);
  const install = await screen.findByRole("button", { name: "安装到 ChatGPT" });
  expect(writes).toEqual([]);
  await userEvent.click(install);
  expect(await screen.findByText("等待打开新版插件")).toBeVisible();
  expect(writes).toEqual([{ action: "connect" }]);
  state = { ...state, state: "installed", reloadRequired: false };
  await userEvent.click(screen.getByRole("button", { name: "检查状态" }));
  expect(await screen.findByText("插件已安装")).toBeVisible();
  expect(
    screen.queryByText(/完全退出并重新打开 ChatGPT/),
  ).not.toBeInTheDocument();
  await userEvent.click(screen.getByRole("button", { name: "断开插件连接" }));
  expect(await screen.findByText("插件尚未安装")).toBeVisible();
  expect(writes).toEqual([{ action: "connect" }, { action: "disconnect" }]);
});

it("connects a legacy plugin to current data only after an explicit action", async () => {
  state = {
    ...state,
    installed: true,
    state: "update_available",
    root: "/legacy/plugin",
    dataDir: "/legacy/data",
    differentData: true,
  };
  render(<ChatGPTPluginConnection />);
  expect(
    await screen.findByText(/这个插件连接了另一个本机数据目录/),
  ).toBeVisible();
  const connect = await screen.findByRole("button", { name: "连接到当前数据" });
  expect(connect).toBeVisible();
  expect(writes).toEqual([]);
  await userEvent.click(connect);
  expect(await screen.findByText("等待打开新版插件")).toBeVisible();
  expect(writes).toEqual([{ action: "connect" }]);
  expect(
    screen.queryByText(/这个插件连接了另一个本机数据目录/),
  ).not.toBeInTheDocument();
});

it("does not replay a write after losing its response", async () => {
  failure = true;
  render(<ChatGPTPluginConnection />);
  await userEvent.click(
    await screen.findByRole("button", { name: "安装到 ChatGPT" }),
  );
  expect(await screen.findByText(/请重新连接后先查看操作结果/)).toBeVisible();
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "检查状态" })).toBeEnabled(),
  );
  await userEvent.click(screen.getByRole("button", { name: "检查状态" }));
  expect(writes).toHaveLength(1);
});

it("offers setup on the home page without writing or blocking other work", async () => {
  const { rerender } = render(<ChatGPTPluginConnection compact />);
  expect(
    await screen.findByRole("link", { name: "设置 ChatGPT 插件" }),
  ).toHaveAttribute("href", "#/settings");
  expect(writes).toEqual([]);
  state = { ...state, available: false, state: "unavailable" };
  rerender(<ChatGPTPluginConnection />);
  await userEvent.click(screen.getByRole("button", { name: "检查状态" }));
  expect(await screen.findByText("未找到 ChatGPT 桌面应用")).toBeVisible();
  expect(
    screen.queryByRole("button", { name: "安装到 ChatGPT" }),
  ).not.toBeInTheDocument();
});
