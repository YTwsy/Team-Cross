import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { SharePanel } from "../components/SharePanel";
import { snapshot } from "./reviewFixtures";

describe("SharePanel scope", () => {
  it("requires explicit selection and sends only selected entries with no execution permissions", async () => {
    const onCreate = vi.fn().mockResolvedValue(undefined);
    const user = userEvent.setup();
    render(
      <SharePanel
        canManage
        snapshots={[snapshot]}
        participants={[]}
        evidence={[
          {
            id: "raw",
            kind: "agent_transcript",
            name: "raw-secret.json",
            size: 500,
            createdAt: snapshot.capturedAt,
          },
        ]}
        onCreate={onCreate}
        onRevoke={vi.fn()}
        onRevokeControl={vi.fn()}
      />,
    );
    expect(screen.getByRole("button", { name: "Create share" })).toBeDisabled();
    expect(screen.queryByText(/raw-secret/)).not.toBeInTheDocument();
    await user.selectOptions(
      screen.getByLabelText("分享的 Session 快照"),
      "snapshot-1",
    );
    await user.click(screen.getByLabelText(/1\. user/));
    await user.click(screen.getByRole("button", { name: "Create share" }));
    expect(onCreate).toHaveBeenCalledWith({
      allowDegraded: false,
      allowControl: false,
      scope: {
        snapshotId: "snapshot-1",
        entryIds: ["entry-1"],
        evidenceIds: [],
        includeCode: false,
        includeEvents: false,
      },
    });
  });

  it("requires code and explicit live events before enabling Managed control", async () => {
    const user = userEvent.setup();
    const onCreate = vi.fn().mockResolvedValue(undefined);
    render(
      <SharePanel
        canManage
        canControl
        canIncludeCode
        participants={[]}
        onCreate={onCreate}
        onRevoke={vi.fn()}
        onRevokeControl={vi.fn()}
      />,
    );
    const control = screen.getByLabelText("允许请求 Managed Agent 控制");
    expect(control).not.toBeChecked();
    expect(control).toBeDisabled();
    await user.click(screen.getByLabelText(/包含当前已封存代码/));
    await user.click(screen.getByLabelText(/实时分享 Managed Agent 事件/));
    expect(control).toBeEnabled();
    await user.click(control);
    await user.click(screen.getByLabelText(/实时分享 Managed Agent 事件/));
    expect(control).not.toBeChecked();
    expect(control).toBeDisabled();
  });
});
