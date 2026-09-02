import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { DiffViewer } from "../components/DiffViewer";
import type { Annotation } from "../types";

const patch = `diff --git a/src/retry.ts b/src/retry.ts
index 1111111..2222222 100644
--- a/src/retry.ts
+++ b/src/retry.ts
@@ -1,2 +1,2 @@
 const retries = 1;
-export const connected = false;
+export const connected = true;`;

const annotations: Annotation[] = [
  {
    id: "a1",
    author: "Ada",
    body: "Check this",
    file: "src/retry.ts",
    line: 2,
    createdAt: "2026-09-02T00:00:00Z",
  },
  {
    id: "a2",
    author: "Lin",
    body: "Agreed",
    file: "src/retry.ts",
    line: 2,
    createdAt: "2026-09-02T00:01:00Z",
  },
];

describe("DiffViewer", () => {
  it("locates annotations on diff lines and opens a line-targeted comment", async () => {
    const onAnnotate = vi.fn();
    const user = userEvent.setup();
    render(
      <DiffViewer
        annotations={annotations}
        onAnnotate={onAnnotate}
        patch={patch}
      />,
    );

    expect(screen.getByText("src/retry.ts")).toBeInTheDocument();
    const lineTwoButtons = screen.getAllByRole("button", {
      name: "Annotate src/retry.ts line 2",
    });
    expect(lineTwoButtons).toHaveLength(2);
    expect(lineTwoButtons[0]).toHaveTextContent("2");

    await user.click(lineTwoButtons[1]!);
    expect(onAnnotate).toHaveBeenCalledWith("src/retry.ts", 2);
  });

  it("can show the exact raw patch", async () => {
    const user = userEvent.setup();
    render(<DiffViewer annotations={[]} onAnnotate={vi.fn()} patch={patch} />);

    await user.click(screen.getByRole("button", { name: "Raw" }));
    expect(
      screen.getByText(
        (_, element) =>
          element?.tagName === "PRE" && element.textContent === patch,
      ),
    ).toHaveClass("raw-patch");
  });

  it("targets annotations at the file belonging to each unified diff hunk", async () => {
    const multiFilePatch = `diff --git a/src/first.ts b/src/first.ts
index 1111111..2222222 100644
--- a/src/first.ts
+++ b/src/first.ts
@@ -1 +1 @@
-export const first = false;
+export const first = true;
diff --git a/src/second.ts b/src/second.ts
index 3333333..4444444 100644
--- a/src/second.ts
+++ b/src/second.ts
@@ -10 +10 @@
-export const second = false;
+export const second = true;`;
    const onAnnotate = vi.fn();
    const user = userEvent.setup();
    render(
      <DiffViewer
        annotations={[
          {
            id: "second-comment",
            author: "Grace",
            body: "Only the second file",
            file: "src/second.ts",
            line: 10,
            createdAt: "2026-09-02T00:00:00Z",
          },
        ]}
        onAnnotate={onAnnotate}
        patch={multiFilePatch}
      />,
    );

    expect(screen.getByText("2 files")).toBeInTheDocument();
    expect(
      screen.getAllByRole("button", {
        name: "Annotate src/first.ts line 1",
      })[0],
    ).not.toHaveTextContent("1");
    const secondFileButtons = screen.getAllByRole("button", {
      name: "Annotate src/second.ts line 10",
    });
    expect(secondFileButtons).toHaveLength(2);
    expect(secondFileButtons[0]).toHaveTextContent("1");

    await user.click(secondFileButtons[1]!);
    expect(onAnnotate).toHaveBeenCalledWith("src/second.ts", 10);
  });

  it("does not expose binary payload rows as source lines and resumes with the next file", async () => {
    const binaryAndTextPatch = `diff --git a/assets/logo.bin b/assets/logo.bin
index 1111111..2222222 100644
GIT binary patch
literal 4
LcmeZIE&

diff --git a/src/after.ts b/src/after.ts
index 3333333..4444444 100644
--- a/src/after.ts
+++ b/src/after.ts
@@ -1 +1 @@
-export const after = false;
+export const after = true;`;
    const onAnnotate = vi.fn();
    const user = userEvent.setup();
    render(
      <DiffViewer
        annotations={[]}
        onAnnotate={onAnnotate}
        patch={binaryAndTextPatch}
      />,
    );

    expect(
      screen.queryByRole("button", { name: /assets\/logo\.bin/ }),
    ).not.toBeInTheDocument();
    const afterButtons = screen.getAllByRole("button", {
      name: "Annotate src/after.ts line 1",
    });
    expect(afterButtons).toHaveLength(2);
    await user.click(afterButtons[1]!);
    expect(onAnnotate).toHaveBeenCalledWith("src/after.ts", 1);
  });
});
