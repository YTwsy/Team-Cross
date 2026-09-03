import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { api } from "../api";
import { ReviewSuccessorPanel } from "../components/ReviewSuccessorPanel";
import { executableThread, reviewThread } from "./reviewFixtures";
import type { ReviewSuccessorPreview } from "../types";

vi.mock("../api", () => ({
  api: {
    preview: vi.fn(),
    previewReviewSuccessor: vi.fn(),
    createReviewSuccessor: vi.fn(),
    continueFromRound: vi.fn(),
    sessionPreview: vi.fn(),
  },
}));
const source = {
  ...reviewThread,
  rounds: [
    {
      id: "review-round",
      sequence: 0,
      summary: "Saved review",
      createdAt: reviewThread.updatedAt,
    },
  ],
};
const preview: ReviewSuccessorPreview = {
  roundId: "review-round",
  repo: "/chosen/repo",
  baseline: "baseline-1",
  previewHash: "exact-preview",
  expectedRevision: 4,
  snapshotCount: 1,
  feedbackCount: 2,
  untracked: [],
  warning: "当前代码不是历史 Session 的代码。",
};
beforeEach(() => {
  vi.clearAllMocks();
  vi.mocked(api.preview).mockResolvedValue({
    repo: "/chosen/repo",
    head: "baseline-1",
    branch: "main",
    unborn: false,
    status: "",
    untracked: [{ path: "notes.txt", size: 5 }],
  });
  vi.mocked(api.previewReviewSuccessor).mockResolvedValue(preview);
  vi.mocked(api.createReviewSuccessor).mockResolvedValue(executableThread);
});
async function prepare() {
  const user = userEvent.setup();
  await user.click(screen.getByText("为审阅内容选择代码基线"));
  await user.type(
    screen.getByLabelText("后续工作目标"),
    "Continue reviewed work",
  );
  await user.type(screen.getByLabelText("代码仓库路径"), "/chosen/repo");
  await user.click(screen.getByRole("button", { name: "检查代码仓库" }));
  await user.click(await screen.findByLabelText(/捕获 notes.txt/));
  await user.click(screen.getByRole("button", { name: "预览后继 Thread" }));
  await screen.findByText(preview.warning);
  return user;
}
describe("review successor", () => {
  it("requires exact separate-baseline consent and never starts or re-reads a Session", async () => {
    const onResult = vi.fn();
    render(<ReviewSuccessorPanel thread={source} onResult={onResult} />);
    expect(api.preview).not.toHaveBeenCalled();
    const user = await prepare();
    const create = screen.getByRole("button", {
      name: "创建后继 Thread，不启动 Agent",
    });
    expect(create).toBeDisabled();
    await user.click(screen.getByLabelText(/我确认这是另行选择的代码基线/));
    await user.click(create);
    await waitFor(() =>
      expect(api.createReviewSuccessor).toHaveBeenCalledWith("thread-1", {
        roundId: "review-round",
        repo: "/chosen/repo",
        goal: "Continue reviewed work",
        untracked: ["notes.txt"],
        expectedRevision: 4,
        previewHash: "exact-preview",
        confirmSeparateBaseline: true,
      }),
    );
    expect(onResult).toHaveBeenCalledWith(executableThread);
    expect(api.continueFromRound).not.toHaveBeenCalled();
    expect(api.sessionPreview).not.toHaveBeenCalled();
  });
  it("drops preview and consent when source revision changes", async () => {
    const { rerender } = render(
      <ReviewSuccessorPanel thread={source} onResult={vi.fn()} />,
    );
    const user = await prepare();
    await user.click(screen.getByLabelText(/我确认这是另行选择的代码基线/));
    rerender(
      <ReviewSuccessorPanel
        thread={{ ...source, revision: 5 }}
        onResult={vi.fn()}
      />,
    );
    expect(
      screen.queryByRole("button", { name: "创建后继 Thread，不启动 Agent" }),
    ).not.toBeInTheDocument();
    rerender(<ReviewSuccessorPanel thread={source} onResult={vi.fn()} />);
    expect(
      screen.queryByRole("button", { name: "创建后继 Thread，不启动 Agent" }),
    ).not.toBeInTheDocument();
    expect(api.createReviewSuccessor).not.toHaveBeenCalled();
  });
  it("changing selected code files invalidates the captured preview", async () => {
    render(<ReviewSuccessorPanel thread={source} onResult={vi.fn()} />);
    const user = await prepare();
    await user.click(screen.getByLabelText(/我确认这是另行选择的代码基线/));
    await user.click(screen.getByLabelText(/捕获 notes.txt/));
    expect(
      screen.queryByRole("button", { name: "创建后继 Thread，不启动 Agent" }),
    ).not.toBeInTheDocument();
    expect(api.createReviewSuccessor).not.toHaveBeenCalled();
  });
  it("keeps a delayed preview bound to the source revision it actually read", async () => {
    let resolve!: (value: ReviewSuccessorPreview) => void;
    vi.mocked(api.previewReviewSuccessor).mockReturnValue(
      new Promise((done) => {
        resolve = done;
      }),
    );
    const { rerender } = render(
      <ReviewSuccessorPanel thread={source} onResult={vi.fn()} />,
    );
    const user = userEvent.setup();
    await user.click(screen.getByText("为审阅内容选择代码基线"));
    await user.type(screen.getByLabelText("后续工作目标"), "Next");
    await user.type(screen.getByLabelText("代码仓库路径"), "/chosen/repo");
    await user.click(screen.getByRole("button", { name: "检查代码仓库" }));
    await user.click(
      await screen.findByRole("button", { name: "预览后继 Thread" }),
    );
    rerender(
      <ReviewSuccessorPanel
        thread={{ ...source, revision: 5 }}
        onResult={vi.fn()}
      />,
    );
    resolve(preview);
    await waitFor(() =>
      expect(
        screen.getByRole("button", { name: "检查代码仓库" }),
      ).not.toBeDisabled(),
    );
    expect(
      screen.queryByRole("button", { name: "创建后继 Thread，不启动 Agent" }),
    ).not.toBeInTheDocument();
  });
  it("blocks unborn code without entering a new Session", async () => {
    vi.mocked(api.preview).mockResolvedValue({
      repo: "/repo",
      head: "",
      branch: "main",
      unborn: true,
      status: "",
      untracked: [],
    });
    render(<ReviewSuccessorPanel thread={source} onResult={vi.fn()} />);
    const user = userEvent.setup();
    await user.click(screen.getByText("为审阅内容选择代码基线"));
    await user.type(screen.getByLabelText("后续工作目标"), "Next");
    await user.type(screen.getByLabelText("代码仓库路径"), "/repo");
    await user.click(screen.getByRole("button", { name: "检查代码仓库" }));
    expect(
      await screen.findByRole("button", { name: "预览后继 Thread" }),
    ).toBeDisabled();
    expect(api.previewReviewSuccessor).not.toHaveBeenCalled();
  });
});
