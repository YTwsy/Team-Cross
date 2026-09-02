import { act, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { api } from "../api";
import { CaptureForm } from "../components/CaptureForm";
import type { CapturePreview, ThreadDetail } from "../types";

vi.mock("../api", () => ({
  api: {
    preview: vi.fn(),
    createThread: vi.fn(),
  },
}));

const preview: CapturePreview = {
  repo: "/repo",
  branch: "feature/handoff",
  head: "0123456789abcdef",
  unborn: false,
  status: " M src/index.ts\n?? notes.txt",
  untracked: [
    { path: "notes.txt", size: 1200 },
    { path: "debug.log", size: 2048 },
  ],
};

describe("CaptureForm", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.mocked(api.preview).mockResolvedValue(preview);
    vi.mocked(api.createThread).mockResolvedValue({
      id: "thread-1",
    } as ThreadDetail);
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.clearAllMocks();
  });

  it("previews Git state, lets the user exclude untracked files, and captures the handoff", async () => {
    const onCreated = vi.fn();
    render(
      <CaptureForm
        defaultRepo="/repo"
        onCancel={vi.fn()}
        onCreated={onCreated}
      />,
    );

    expect(
      screen.getByRole("button", { name: "Capture & create" }),
    ).toBeDisabled();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(250);
    });

    expect(api.preview).toHaveBeenCalledWith("/repo");
    expect(screen.getByText("feature/handoff")).toBeInTheDocument();
    expect(screen.getByText("2 changes")).toBeInTheDocument();

    const notes = screen.getByRole("checkbox", { name: /notes\.txt/ });
    const debug = screen.getByRole("checkbox", { name: /debug\.log/ });
    expect(notes).toBeChecked();
    expect(debug).toBeChecked();
    fireEvent.click(debug);

    fireEvent.change(screen.getByLabelText("Thread title"), {
      target: { value: "Investigate reconnect" },
    });
    fireEvent.change(screen.getByLabelText("Goal"), {
      target: { value: "Keep the event stream stable" },
    });
    fireEvent.change(screen.getByLabelText("Blocker"), {
      target: { value: "Reconnect loop" },
    });
    fireEvent.submit(
      screen.getByRole("button", { name: "Capture & create" }).closest("form")!,
    );

    await act(async () => {
      await Promise.resolve();
    });
    expect(api.createThread).toHaveBeenCalledWith(
      expect.objectContaining({
        repo: "/repo",
        title: "Investigate reconnect",
        goal: "Keep the event stream stable",
        blocker: "Reconnect loop",
        untracked: ["notes.txt"],
      }),
    );
    expect(onCreated).toHaveBeenCalledWith("thread-1");
  });

  it("invalidates a stale preview while a new repository is inspected", async () => {
    render(
      <CaptureForm
        defaultRepo="/repo"
        onCancel={vi.fn()}
        onCreated={vi.fn()}
      />,
    );
    await act(async () => {
      await vi.advanceTimersByTimeAsync(250);
    });
    expect(
      screen.getByRole("button", { name: "Capture & create" }),
    ).toBeEnabled();

    fireEvent.change(screen.getByLabelText("Repository"), {
      target: { value: "/other" },
    });

    expect(
      screen.getByRole("button", { name: "Capture & create" }),
    ).toBeDisabled();
    expect(screen.getByText("Inspecting…")).toBeInTheDocument();
  });
});
