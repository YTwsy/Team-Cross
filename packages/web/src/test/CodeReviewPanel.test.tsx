import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { api } from "../api";
import { CodeReviewPanel } from "../components/CodeReviewPanel";
import type { SealedCodeReview } from "../types";
import { codeReview } from "./codeReviewFixtures";
import { executableThread } from "./reviewFixtures";

vi.mock("../api", () => ({ api: { roundCode: vi.fn() } }));
const props = {
  threadId: executableThread.id,
  rounds: executableThread.rounds,
  active: true,
  canManage: true,
  livePatch: "LIVE worktree content",
  annotations: [],
  onAnnotate: vi.fn(),
};
beforeEach(() => {
  vi.clearAllMocks();
  vi.mocked(api.roundCode).mockResolvedValue(codeReview);
});

describe("sealed CodeReviewPanel", () => {
  it("reads a sealed Round, keeps its selection across new Rounds, and separates live diff", async () => {
    const user = userEvent.setup();
    const { rerender } = render(<CodeReviewPanel {...props} />);
    expect(
      await screen.findByText("+export const connected = true;"),
    ).toBeInTheDocument();
    expect(api.roundCode).toHaveBeenCalledWith("thread-1", "round-1");
    expect(screen.queryByText("LIVE worktree content")).not.toBeInTheDocument();
    rerender(
      <CodeReviewPanel
        {...props}
        rounds={[
          ...props.rounds,
          { ...props.rounds[1]!, id: "round-2", sequence: 2 },
        ]}
      />,
    );
    expect(screen.getByLabelText("代码 Round")).toHaveValue("round-1");
    expect(api.roundCode).toHaveBeenCalledTimes(1);
    await user.click(
      screen.getByRole("button", { name: "Annotate src/retry.ts new line 2" }),
    );
    expect(props.onAnnotate).toHaveBeenCalledWith({
      file: "src/retry.ts",
      line: 2,
      target: { roundId: "round-1", side: "new" },
    });
    await user.click(
      screen.getByRole("button", { name: "查看实时 worktree（不能批注）" }),
    );
    expect(screen.getByText("LIVE worktree content")).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: /Annotate/ }),
    ).not.toBeInTheDocument();
  });

  it("does not fetch hidden tabs or offer a remote live view", () => {
    render(<CodeReviewPanel {...props} active={false} canManage={false} />);
    expect(api.roundCode).not.toHaveBeenCalled();
    expect(
      screen.queryByRole("button", { name: /实时 worktree/ }),
    ).not.toBeInTheDocument();
  });

  it("does not substitute another Round when exact code is unavailable", async () => {
    vi.mocked(api.roundCode).mockRejectedValue(new Error("Not authorized"));
    render(<CodeReviewPanel {...props} canManage={false} />);
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Not authorized",
    );
    expect(screen.queryByText("LIVE worktree content")).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: /Annotate/ }),
    ).not.toBeInTheDocument();
  });

  it("ignores a late response after selecting another Round", async () => {
    let complete: (value: SealedCodeReview) => void = () => {};
    vi.mocked(api.roundCode).mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          complete = resolve;
        }),
    );
    vi.mocked(api.roundCode).mockResolvedValueOnce({
      ...codeReview,
      roundId: "round-0",
      lines: [
        {
          kind: "add",
          text: "+OLD selected snapshot",
          newPath: "old.ts",
          newLine: 1,
        },
      ],
    });
    const user = userEvent.setup();
    render(<CodeReviewPanel {...props} />);
    await user.selectOptions(screen.getByLabelText("代码 Round"), "round-0");
    expect(
      await screen.findByText("+OLD selected snapshot"),
    ).toBeInTheDocument();
    await act(async () => {
      complete(codeReview);
    });
    expect(
      screen.queryByText("+export const connected = true;"),
    ).not.toBeInTheDocument();
    expect(screen.getByText("+OLD selected snapshot")).toBeInTheDocument();
  });

  it("ignores another Thread's late response", async () => {
    let complete: (value: SealedCodeReview) => void = () => {};
    vi.mocked(api.roundCode).mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          complete = resolve;
        }),
    );
    vi.mocked(api.roundCode).mockResolvedValueOnce({
      ...codeReview,
      lines: [
        {
          kind: "add",
          text: "+NEW Thread code",
          newPath: "new.ts",
          newLine: 1,
        },
      ],
    });
    const { rerender } = render(<CodeReviewPanel {...props} />);
    rerender(<CodeReviewPanel {...props} threadId="thread-2" />);
    expect(await screen.findByText("+NEW Thread code")).toBeInTheDocument();
    await act(async () => {
      complete(codeReview);
    });
    expect(
      screen.queryByText("+export const connected = true;"),
    ).not.toBeInTheDocument();
  });

  it("opens an exact old code reference and reports a missing line without relocating", async () => {
    const reference = {
      file: "src/retry.ts",
      line: 2,
      target: { roundId: "round-0", side: "old" as const },
    };
    vi.mocked(api.roundCode).mockResolvedValue({
      ...codeReview,
      roundId: "round-0",
    });
    const { rerender } = render(
      <CodeReviewPanel {...props} reference={reference} />,
    );
    await waitFor(() =>
      expect(
        screen
          .getByText("-export const connected = false;")
          .closest(".diff-line"),
      ).toHaveAttribute("aria-current", "location"),
    );
    expect(api.roundCode).toHaveBeenCalledWith("thread-1", "round-0");
    rerender(
      <CodeReviewPanel {...props} reference={{ ...reference, line: 999 }} />,
    );
    expect(await screen.findByRole("alert")).toHaveTextContent("未重新定位");
  });
});
