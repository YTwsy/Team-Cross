import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { SessionReview, SessionTranscript } from "../components/SessionReview";
import { snapshot } from "./reviewFixtures";

describe("SessionReview", () => {
  it("targets an immutable snapshot entry, not a visual row number", async () => {
    const onAnnotate = vi.fn();
    const user = userEvent.setup();
    render(
      <SessionReview
        snapshots={[snapshot]}
        annotations={[]}
        onAnnotate={onAnnotate}
      />,
    );
    await user.click(screen.getAllByRole("button", { name: "批注" })[1]!);
    expect(onAnnotate).toHaveBeenCalledWith({
      snapshotId: "snapshot-1",
      entryId: "entry-2",
    });
    expect(screen.getByText(/只读快照/)).toBeInTheDocument();
    expect(screen.getByText(/此快照不证明历史代码状态/)).toBeInTheDocument();
  });

  it("renders imported text without executing markup and shows truncation/capability boundaries", () => {
    render(
      <SessionTranscript
        preview={{
          ...snapshot,
          truncated: true,
          warnings: ["工具输出缺失"],
          entries: [
            {
              id: "unsafe",
              kind: "message",
              text: '<img src="x" onerror="alert(1)">',
            },
          ],
        }}
      />,
    );
    expect(
      screen.getByText('<img src="x" onerror="alert(1)">'),
    ).toBeInTheDocument();
    expect(screen.queryByRole("img")).not.toBeInTheDocument();
    expect(screen.getByText(/历史已截断/)).toBeInTheDocument();
    expect(screen.getByText("工具输出缺失")).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: /接管|Follow|Resume/ }),
    ).not.toBeInTheDocument();
    expect(
      screen.getByText("Native writer fencing is not verified"),
    ).toBeInTheDocument();
  });
});
