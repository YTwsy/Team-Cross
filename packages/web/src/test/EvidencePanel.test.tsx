import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { EvidencePanel } from "../components/EvidencePanel";

describe("EvidencePanel", () => {
  it("attaches UTF-8 text as base64 JSON and exposes evidence links", async () => {
    const onAttach = vi.fn().mockResolvedValue(undefined);
    const user = userEvent.setup();
    render(
      <EvidencePanel
        canManage
        evidence={[
          {
            id: "evidence/one",
            kind: "log",
            name: "build.log",
            size: 1025,
            createdAt: "2026-09-02T00:00:00Z",
          },
        ]}
        onAttach={onAttach}
        threadId="thread/one"
      />,
    );

    await user.type(screen.getByLabelText("Evidence name"), "note.txt");
    await user.type(screen.getByLabelText("Evidence content"), "交接说明");
    await user.click(screen.getByRole("button", { name: "Attach evidence" }));
    await waitFor(() => expect(onAttach).toHaveBeenCalledOnce());
    expect(onAttach).toHaveBeenCalledWith(
      expect.objectContaining({
        kind: "text",
        name: "note.txt",
        source: "webgui",
        mimeType: "text/plain;charset=utf-8",
        contentBase64: expect.any(String),
      }),
    );

    const view = screen.getByRole("link", { name: "View" });
    const download = screen.getByRole("link", { name: "Download" });
    expect(view).toHaveAttribute(
      "href",
      "/api/v1/threads/thread%2Fone/evidence/evidence%2Fone",
    );
    expect(download).toHaveAttribute("download", "build.log");
    expect(download).toHaveAttribute(
      "href",
      "/api/v1/threads/thread%2Fone/evidence/evidence%2Fone?download=1",
    );
  });

  it("keeps the attachment form host-only", () => {
    render(
      <EvidencePanel
        canManage={false}
        evidence={[]}
        onAttach={vi.fn()}
        threadId="thread-1"
      />,
    );
    expect(screen.queryByLabelText("Attach evidence")).not.toBeInTheDocument();
    expect(
      screen.getByText("The host has not attached any extra evidence."),
    ).toBeInTheDocument();
  });

  it("attaches a selected local file without exposing its local path", async () => {
    const onAttach = vi.fn().mockResolvedValue(undefined);
    const user = userEvent.setup();
    render(
      <EvidencePanel
        canManage
        evidence={[]}
        onAttach={onAttach}
        threadId="thread-1"
      />,
    );

    await user.click(screen.getByRole("button", { name: "Local file" }));
    await user.upload(
      screen.getByLabelText("Evidence file"),
      new File([new Uint8Array([0, 1, 2, 3])], "trace.bin", {
        type: "application/octet-stream",
      }),
    );
    await user.click(screen.getByRole("button", { name: "Attach evidence" }));
    await waitFor(() => expect(onAttach).toHaveBeenCalledOnce());
    expect(onAttach).toHaveBeenCalledWith({
      kind: "file",
      name: "trace.bin",
      source: "webgui",
      contentBase64: "AAECAw==",
      mimeType: "application/octet-stream",
    });
  });
});
