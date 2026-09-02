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
    expect(value.sessionSnapshots).toEqual([]);
    expect(value.participants).toEqual([]);
    expect(value.git.untracked).toEqual([]);
    expect(value.share?.transports).toEqual([]);
    expect(value.events[0]?.payload).toEqual({});
  });

  it("uses separate read-only preview/create/import APIs with no Agent execution fields", async () => {
    const fetchMock = vi
      .fn()
      .mockImplementation((path: string) =>
        Promise.resolve(
          new Response(
            JSON.stringify(
              path.endsWith("preview")
                ? { entries: null, warnings: null }
                : { id: "thread-1" },
            ),
            { headers: { "content-type": "application/json" } },
          ),
        ),
      );
    vi.stubGlobal("fetch", fetchMock);
    const preview = await api.sessionPreview("codex", "native-1");
    expect(preview.entries).toEqual([]);
    expect(preview.warnings).toEqual([]);
    await api.createThreadFromSession("codex", "native-1", "Review");
    await api.importSession("thread-1", "codex", "native-1");
    expect(fetchMock.mock.calls.map(([path]) => path)).toEqual([
      "/api/v1/sessions/preview",
      "/api/v1/threads/from-session",
      "/api/v1/threads/thread-1/sessions/import",
    ]);
    expect(JSON.parse(fetchMock.mock.calls[1]![1].body)).toEqual({
      provider: "codex",
      sessionId: "native-1",
      title: "Review",
    });
  });

  it("preserves annotation anchors and sends observer annotations with fencing metadata", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValue(
        new Response(JSON.stringify({ id: "thread-1" }), {
          headers: { "content-type": "application/json" },
        }),
      );
    vi.stubGlobal("fetch", fetchMock);
    await api.addAnnotation("thread-1", {
      body: "Review",
      target: { snapshotId: "snapshot-1", entryId: "entry-1" },
      expectedRevision: 7,
      leaseEpoch: 0,
    });
    const input = JSON.parse(fetchMock.mock.calls[0]![1].body);
    expect(input).toEqual({
      body: "Review",
      target: { snapshotId: "snapshot-1", entryId: "entry-1" },
      expectedRevision: 7,
      leaseEpoch: 0,
      commandId: expect.any(String),
    });
  });

  it("decodes vendor JSON bundles and keeps selection and copy confirmation explicit", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValue(
        new Response(JSON.stringify({ version: 1, objects: [] }), {
          headers: { "content-type": "application/vnd.teamcross.bundle+json" },
        }),
      );
    vi.stubGlobal("fetch", fetchMock);
    expect(
      await api.exportBundle("thread-1", {
        roundId: "round-1",
        evidenceIds: [],
        snapshotIds: [],
        confirmExport: true,
      }),
    ).toEqual({ version: 1, objects: [] });
    expect(JSON.parse(fetchMock.mock.calls[0]![1].body)).toEqual({
      roundId: "round-1",
      evidenceIds: [],
      snapshotIds: [],
      confirmExport: true,
    });
  });
});
