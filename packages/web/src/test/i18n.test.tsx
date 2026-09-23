import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { useState } from "react";
import { Settings } from "../components/Settings";
import {
  languageInfo,
  setLanguageInfo,
  t,
  tr,
  type LanguageInfo,
} from "../i18n";

afterEach(() => setLanguageInfo({ mode: "auto", resolved: "zh-CN" }));

function LanguageSettings() {
  const [choice, setChoice] = useState<LanguageInfo>(languageInfo);
  return (
    <Settings
      theme="light"
      setTheme={() => {}}
      uiLanguage={choice}
      onLanguageChange={(next) => {
        setLanguageInfo(next);
        setChoice(next);
      }}
    />
  );
}

describe("interface language", () => {
  it("uses a shared API choice and updates WebGUI immediately", async () => {
    const posts: unknown[] = [];
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: string, init?: RequestInit) => {
        if (url.endsWith("/api/ui-language")) {
          const body = JSON.parse(String(init?.body));
          posts.push(body);
          return {
            ok: true,
            json: async () => ({
              mode: body.mode,
              resolved: body.mode === "auto" ? "zh-CN" : body.mode,
            }),
          };
        }
        return { ok: true, json: async () => ({ mcpClients: {} }) };
      }),
    );
    const user = userEvent.setup();
    render(<LanguageSettings />);
    expect(screen.getByRole("heading", { name: "界面语言" })).toBeVisible();
    await user.click(screen.getByRole("button", { name: "English" }));
    expect(posts).toEqual([{ mode: "en" }]);
    expect(document.documentElement.lang).toBe("en");
    expect(
      screen.getByRole("heading", { name: "Interface language" }),
    ).toBeVisible();
    expect(tr`请在 ${"tomorrow"} 前首次加入。`).toBe(
      "Join for the first time by tomorrow.",
    );
    await user.click(
      screen.getByRole("button", { name: "Simplified Chinese" }),
    );
    expect(posts).toEqual([{ mode: "en" }, { mode: "zh-CN" }]);
    expect(document.documentElement.lang).toBe("zh-CN");
    expect(t("界面语言")).toBe("界面语言");
  });
});
