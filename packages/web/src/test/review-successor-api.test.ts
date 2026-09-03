import { afterEach, describe, expect, it, vi } from "vitest";
import { api } from "../api";
import { executableThread } from "./reviewFixtures";

afterEach(() => vi.unstubAllGlobals());
describe("review successor API", () => {
  it("sends two explicit scoped requests with no hidden continue or Provider read", async () => {
    const input = {
      roundId: "round-1",
      repo: "/chosen/repo",
      untracked: ["notes.txt"],
      goal: "Next work",
      expectedRevision: 4,
    };
    const fetch = vi
      .fn()
      .mockResolvedValueOnce(
        new Response(
          JSON.stringify({
            previewHash: "confirmed-content",
            snapshotCount: 1,
            feedbackCount: 2,
            untracked: [],
          }),
          { headers: { "Content-Type": "application/json" } },
        ),
      )
      .mockResolvedValueOnce(
        new Response(JSON.stringify(executableThread), {
          headers: { "Content-Type": "application/json" },
        }),
      );
    vi.stubGlobal("fetch", fetch);
    const preview = await api.previewReviewSuccessor("source/id", input);
    expect(fetch).toHaveBeenNthCalledWith(
      1,
      "/api/v1/threads/source%2Fid/successor/preview",
      expect.objectContaining({ method: "POST", body: JSON.stringify(input) }),
    );
    const confirmed = {
      ...input,
      previewHash: preview.previewHash,
      confirmSeparateBaseline: true as const,
    };
    const result = await api.createReviewSuccessor("source/id", confirmed);
    expect(fetch).toHaveBeenNthCalledWith(
      2,
      "/api/v1/threads/source%2Fid/successors",
      expect.objectContaining({
        method: "POST",
        body: JSON.stringify(confirmed),
      }),
    );
    expect(result.id).toBe(executableThread.id);
    expect(fetch).toHaveBeenCalledTimes(2);
  });
});
