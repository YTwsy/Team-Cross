import { afterEach, describe, expect, it, vi } from "vitest";
import { api, subscribeEvents } from "../api";

class FakeEventSource {
  static latest: FakeEventSource | undefined;

  readonly close = vi.fn();
  onopen: ((event: Event) => void) | null = null;
  onerror: ((event: Event) => void) | null = null;
  onmessage: ((event: MessageEvent<string>) => void) | null = null;

  constructor(readonly url: string) {
    FakeEventSource.latest = this;
  }
}

describe("subscribeEvents", () => {
  afterEach(() => {
    FakeEventSource.latest = undefined;
    vi.unstubAllGlobals();
  });

  it("resumes from the supplied cursor and closes cleanly", () => {
    vi.stubGlobal("EventSource", FakeEventSource);
    const onEvents = vi.fn();
    const onConnection = vi.fn();

    const unsubscribe = subscribeEvents("thread-1", 17, onEvents, onConnection);
    const source = FakeEventSource.latest!;
    expect(source.url).toBe("/api/v1/threads/thread-1/events?after=17");

    source.onopen?.(new Event("open"));
    source.onmessage?.(
      new MessageEvent("message", {
        data: JSON.stringify({
          seq: 18,
          type: "turn.completed",
          createdAt: "2026-09-02T00:00:00Z",
          payload: {},
        }),
      }),
    );

    expect(onConnection).toHaveBeenLastCalledWith(true);
    expect(onEvents).toHaveBeenCalledWith([
      expect.objectContaining({ seq: 18, type: "turn.completed" }),
    ]);
    unsubscribe();
    expect(source.close).toHaveBeenCalledOnce();
  });

  it("rejects malformed frames without throwing into the UI", () => {
    vi.stubGlobal("EventSource", FakeEventSource);
    const onEvents = vi.fn();
    const onConnection = vi.fn();
    subscribeEvents("thread-1", 0, onEvents, onConnection);

    expect(() => {
      FakeEventSource.latest?.onmessage?.(
        new MessageEvent("message", { data: "not-json" }),
      );
      FakeEventSource.latest?.onmessage?.(
        new MessageEvent("message", { data: JSON.stringify({ hello: true }) }),
      );
    }).not.toThrow();
    expect(onEvents).not.toHaveBeenCalled();
    expect(onConnection).toHaveBeenLastCalledWith(false);
  });

  it("normalizes nullable collections and event payloads from the Core", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response(
          JSON.stringify({
            id: "thread-1",
            git: { untracked: null },
            rounds: null,
            events: [
              {
                seq: 1,
                type: "thread.created",
                createdAt: "2026-09-02T00:00:00Z",
                payload: null,
              },
            ],
            annotations: null,
            evidence: null,
            participants: null,
            share: { transports: null },
          }),
          { headers: { "content-type": "application/json" } },
        ),
      ),
    );

    const value = await api.thread("thread-1");
    expect(value.rounds).toEqual([]);
    expect(value.annotations).toEqual([]);
    expect(value.evidence).toEqual([]);
    expect(value.participants).toEqual([]);
    expect(value.git.untracked).toEqual([]);
    expect(value.share?.transports).toEqual([]);
    expect(value.events[0]?.payload).toEqual({});
  });
});
