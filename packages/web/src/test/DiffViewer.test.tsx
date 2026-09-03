import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { DiffViewer } from "../components/DiffViewer";
import type { Annotation } from "../types";
import { codeReview } from "./codeReviewFixtures";

describe("DiffViewer", () => {
  it("keeps identical old/new line numbers and other Round comments separate", async () => {
    const annotations: Annotation[] = [
      {
        id: "old",
        author: "Ada",
        body: "old",
        file: "src/retry.ts",
        line: 2,
        target: { roundId: "round-1", side: "old" },
        createdAt: "2026-09-03",
      },
      {
        id: "new",
        author: "Lin",
        body: "new",
        file: "src/retry.ts",
        line: 2,
        target: { roundId: "round-1", side: "new" },
        createdAt: "2026-09-03",
      },
      {
        id: "other",
        author: "Lin",
        body: "other Round",
        file: "src/retry.ts",
        line: 2,
        target: { roundId: "round-0", side: "new" },
        createdAt: "2026-09-03",
      },
      {
        id: "legacy",
        author: "Lin",
        body: "legacy",
        file: "src/retry.ts",
        line: 2,
        createdAt: "2026-09-03",
      },
    ];
    const onAnnotate = vi.fn();
    const user = userEvent.setup();
    render(
      <DiffViewer
        review={codeReview}
        patch={codeReview.patch}
        annotations={annotations}
        onAnnotate={onAnnotate}
      />,
    );
    const old = screen.getByRole("button", {
      name: "Annotate src/retry.ts old line 2",
    });
    const next = screen.getByRole("button", {
      name: "Annotate src/retry.ts new line 2",
    });
    expect(old).toHaveTextContent("1");
    expect(next).toHaveTextContent("1");
    await user.click(next);
    expect(onAnnotate).toHaveBeenCalledWith({
      file: "src/retry.ts",
      line: 2,
      target: { roundId: "round-1", side: "new" },
    });
    await user.click(old);
    expect(onAnnotate).toHaveBeenLastCalledWith({
      file: "src/retry.ts",
      line: 2,
      target: { roundId: "round-1", side: "old" },
    });
  });

  it("keeps live diff read-only even when a callback was passed", async () => {
    const onAnnotate = vi.fn();
    const user = userEvent.setup();
    render(
      <DiffViewer
        annotations={[]}
        onAnnotate={onAnnotate}
        patch={codeReview.patch}
      />,
    );
    expect(
      screen.queryByRole("button", { name: /Annotate/ }),
    ).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Raw" }));
    expect(
      screen.getByText(
        (_, element) =>
          element?.tagName === "PRE" &&
          element.textContent === codeReview.patch,
      ),
    ).toHaveClass("raw-patch");
    expect(onAnnotate).not.toHaveBeenCalled();
  });

  it("uses server paths per side and never treats binary metadata as source lines", async () => {
    const onAnnotate = vi.fn();
    const user = userEvent.setup();
    const review = {
      ...codeReview,
      lines: [
        {
          kind: "meta" as const,
          text: "GIT binary patch",
          newPath: "assets/logo.bin",
        },
        {
          kind: "delete" as const,
          text: "-old",
          oldPath: "旧 filename.ts",
          oldLine: 10,
        },
        {
          kind: "add" as const,
          text: "+new",
          newPath: "new filename.ts",
          newLine: 10,
        },
      ],
    };
    render(
      <DiffViewer
        review={review}
        patch={review.patch}
        annotations={[]}
        onAnnotate={onAnnotate}
      />,
    );
    expect(
      screen.queryByRole("button", { name: /assets\/logo/ }),
    ).not.toBeInTheDocument();
    await user.click(
      screen.getByRole("button", {
        name: "Annotate 旧 filename.ts old line 10",
      }),
    );
    expect(onAnnotate).toHaveBeenCalledWith({
      file: "旧 filename.ts",
      line: 10,
      target: { roundId: "round-1", side: "old" },
    });
  });

  it("highlights only the exact immutable reference", () => {
    render(
      <DiffViewer
        review={codeReview}
        patch={codeReview.patch}
        annotations={[]}
        reference={{
          file: "src/retry.ts",
          line: 2,
          target: { roundId: "round-1", side: "old" },
        }}
      />,
    );
    expect(
      screen
        .getByText("-export const connected = false;")
        .closest(".diff-line"),
    ).toHaveAttribute("aria-current", "location");
    expect(
      screen.getByText("+export const connected = true;").closest(".diff-line"),
    ).not.toHaveAttribute("aria-current");
  });
});
