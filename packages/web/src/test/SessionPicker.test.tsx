import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { api } from "../api";
import { SessionPicker } from "../components/SessionPicker";
import { snapshot } from "./reviewFixtures";

vi.mock("../api", () => ({
  api: { storedSessions: vi.fn(), sessionPreview: vi.fn() },
}));
beforeEach(() => {
  vi.clearAllMocks();
  vi.mocked(api.storedSessions).mockResolvedValue([
    {
      id: "native-1",
      provider: "codex",
      title: "Fix parser",
      updatedAt: "2026-09-03T00:00:00Z",
    },
  ]);
  vi.mocked(api.sessionPreview).mockResolvedValue(snapshot);
});

describe("SessionPicker", () => {
  it("previews history without importing or running an Agent until explicit confirmation", async () => {
    const onImport = vi.fn().mockResolvedValue(undefined);
    const user = userEvent.setup();
    render(<SessionPicker onImport={onImport} />);
    expect(
      screen.getByRole("button", { name: "创建只读审阅 Thread" }),
    ).toBeDisabled();
    await user.click(await screen.findByRole("button", { name: /Fix parser/ }));
    expect(await screen.findByText("Visible request")).toBeInTheDocument();
    expect(api.sessionPreview).toHaveBeenCalledWith("codex", "native-1");
    expect(onImport).not.toHaveBeenCalled();
    await user.click(
      screen.getByRole("button", { name: "创建只读审阅 Thread" }),
    );
    await waitFor(() =>
      expect(onImport).toHaveBeenCalledWith("codex", "native-1", "Fix parser"),
    );
  });

  it("does not allow import when preview failed", async () => {
    vi.mocked(api.sessionPreview).mockRejectedValue(
      new Error("stored history missing"),
    );
    const onImport = vi.fn();
    const user = userEvent.setup();
    render(<SessionPicker onImport={onImport} />);
    await user.click(await screen.findByRole("button", { name: /Fix parser/ }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "stored history missing",
    );
    expect(
      screen.getByRole("button", { name: "创建只读审阅 Thread" }),
    ).toBeDisabled();
    expect(onImport).not.toHaveBeenCalled();
  });
});
