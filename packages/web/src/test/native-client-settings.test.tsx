import { useState } from "react";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { api } from "../api";
import { NativeClientSettings } from "../components/NativeClientSettings";
import { setLanguage } from "../i18n";
import { type Info } from "../types";

const oldCLI = "/Applications/ChatGPT.app/Contents/Resources/codex";
const newCLI =
  "/Applications/ChatGPT.app/Contents/Resources/codex-cli/CodexCLI.app/Contents/MacOS/codex";
const detected: Info = {
  host: "fixture",
  version: "fixture",
  binary: newCLI,
  codexVersion: "codex-cli 0.160.0",
  codexError: "",
  desktopApp: "/Applications/ChatGPT.app",
  dataDir: "/fixture",
  mcpCommand: "",
  mcpConfigured: false,
  claudeBinary: "/nvm/bin/claude",
  claudeVersion: "2.1.270 (Claude Code)",
  settings: { binary: "", desktopApp: "", claudeBinary: "" },
  codexInstallation: {
    binary: newCLI,
    source: "app",
    desktopApp: "/Applications/ChatGPT.app",
  },
};
let info: Info;
let writes: Info["settings"][];
let reads: string[];
let failure: boolean;
beforeEach(() => {
  setLanguage("zh-CN");
  info = structuredClone(detected);
  writes = [];
  reads = [];
  failure = false;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (init?.body) {
        const settings = JSON.parse(String(init.body));
        writes.push(settings);
        if (failure) throw new TypeError("lost response");
        info = { ...info, settings };
        return { ok: true, json: async () => ({ ok: true }) };
      }
      reads.push(url);
      return { ok: true, json: async () => structuredClone(info) };
    }),
  );
});
afterEach(() => {
  vi.unstubAllGlobals();
  setLanguage("zh-CN");
});

function Form() {
  const [current, setCurrent] = useState(info);
  return (
    <NativeClientSettings
      info={current}
      loading={false}
      disabled={false}
      reload={() => void api<Info>("info").then(setCurrent)}
      onChecked={setCurrent}
      onBusyChange={() => {}}
    />
  );
}

it("saving automatic discovery never pins detected Codex, Desktop or Claude paths", async () => {
  render(<Form />);
  expect(screen.getByRole("button", { name: "自动发现" })).toHaveAttribute(
    "aria-pressed",
    "true",
  );
  expect(screen.getByLabelText(/^Codex Desktop 应用/)).toHaveValue("");
  expect(screen.getByLabelText(/^Claude Code CLI 路径/)).toHaveValue("");
  await userEvent.click(screen.getByRole("button", { name: "保存设置" }));
  expect(writes).toEqual([{ binary: "", desktopApp: "", claudeBinary: "" }]);
  expect(await screen.findByText(/设置已保存/)).toBeVisible();
  expect(screen.getByText("CLI 可运行")).toBeVisible();
});

it("keeps the stale saved path visible and lets the user explicitly restore automatic discovery", async () => {
  info.settings = {
    binary: oldCLI,
    desktopApp: "/Applications/ChatGPT.app",
    claudeBinary: "/custom/claude",
  };
  info.codexInstallation = {
    ...info.codexInstallation!,
    recoveredFrom: oldCLI,
  };
  render(<Form />);
  expect(screen.getByLabelText("Codex CLI 路径")).toHaveValue(oldCLI);
  expect(screen.getByText(/已从同一应用重新发现/)).toBeVisible();
  await userEvent.click(screen.getByRole("button", { name: "恢复自动发现" }));
  expect(writes).toEqual([]);
  expect(screen.queryByLabelText("Codex CLI 路径")).not.toBeInTheDocument();
  expect(screen.getByText("设置有改动，保存后生效。")).toBeVisible();
  await userEvent.click(screen.getByRole("button", { name: "保存设置" }));
  expect(writes).toEqual([
    {
      binary: "",
      desktopApp: "/Applications/ChatGPT.app",
      claudeBinary: "/custom/claude",
    },
  ]);
});

it("shows an invalid custom path and recovery instead of replacing it with an empty field", async () => {
  info = {
    ...info,
    settings: { ...info.settings!, binary: "/missing/custom-codex" },
    binary: "",
    codexVersion: "",
    codexError: "Codex CLI 文件不存在",
    codexRecovery: "请修正保存的路径，或恢复自动发现",
    codexInstallation: { binary: "", source: "custom" },
  };
  render(<Form />);
  expect(screen.getByLabelText("Codex CLI 路径")).toHaveValue(
    "/missing/custom-codex",
  );
  expect(screen.getByText("Codex CLI 文件不存在")).toBeVisible();
  expect(screen.getByText("请修正保存的路径，或恢复自动发现")).toBeVisible();
  expect(writes).toEqual([]);
});

it("rechecks immediately without saving or overwriting an unsaved manual path", async () => {
  render(<Form />);
  await userEvent.click(screen.getByRole("button", { name: "手动指定" }));
  await userEvent.type(
    screen.getByLabelText("Codex CLI 路径"),
    "/new/custom-codex",
  );
  info = {
    ...info,
    binary: "/nvm/new/bin/codex",
    codexInstallation: { binary: "/nvm/new/bin/codex", source: "shell" },
  };
  await userEvent.click(screen.getByRole("button", { name: "重新检测" }));
  await waitFor(() =>
    expect(screen.getByText("/nvm/new/bin/codex")).toBeInTheDocument(),
  );
  expect(reads).toEqual(["/api/info?refreshClients=1"]);
  expect(writes).toEqual([]);
  expect(screen.getByLabelText("Codex CLI 路径")).toHaveValue(
    "/new/custom-codex",
  );
});

it("rejects an empty manual path and does not replay a save after losing its response", async () => {
  render(<Form />);
  await userEvent.click(screen.getByRole("button", { name: "手动指定" }));
  await userEvent.click(screen.getByRole("button", { name: "保存设置" }));
  expect(
    screen.getByText("请输入 Codex CLI 路径，或选择自动发现"),
  ).toBeVisible();
  expect(writes).toEqual([]);
  await userEvent.type(
    screen.getByLabelText("Codex CLI 路径"),
    "/custom/codex",
  );
  failure = true;
  await userEvent.click(screen.getByRole("button", { name: "保存设置" }));
  expect(await screen.findByText(/请重新连接后先查看操作结果/)).toBeVisible();
  await userEvent.click(screen.getByRole("button", { name: "重新检测" }));
  expect(writes).toHaveLength(1);
});

it("renders status and recovery in English", () => {
  setLanguage("en");
  info.codexError = "Codex CLI 文件不存在";
  info.codexRecovery = "请修正保存的路径，或恢复自动发现";
  render(<Form />);
  expect(screen.getByRole("button", { name: "Check again" })).toBeVisible();
  expect(
    screen.getByText("The Codex CLI file no longer exists."),
  ).toBeVisible();
});

it("does not overwrite saved paths when a stale Core omits settings", () => {
  info.settings = undefined;
  render(<Form />);
  expect(screen.getByRole("button", { name: "保存设置" })).toBeDisabled();
  expect(screen.getByText(/客户端设置尚未加载/)).toBeVisible();
  expect(writes).toEqual([]);
});
