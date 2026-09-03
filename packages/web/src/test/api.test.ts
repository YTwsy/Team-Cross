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

  it("loads exact sealed Round code and preserves code annotation side", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        new Response(
          JSON.stringify({
            roundId: "round-1",
            baseline: "base",
            patch: "",
            lines: null,
          }),
          { headers: { "content-type": "application/json" } },
        ),
      )
      .mockResolvedValueOnce(
        new Response(JSON.stringify({ id: "thread-1" }), {
          headers: { "content-type": "application/json" },
        }),
      );
    vi.stubGlobal("fetch", fetchMock);
    expect((await api.roundCode("thread-1", "round-1")).lines).toEqual([]);
    expect(fetchMock.mock.calls[0]![0]).toBe(
      "/api/v1/threads/thread-1/rounds/round-1/code",
    );
    await api.addAnnotation("thread-1", {
      body: "review",
      file: "src/a.ts",
      line: 4,
      target: { roundId: "round-1", side: "old" },
      expectedRevision: 5,
    });
    expect(JSON.parse(fetchMock.mock.calls[1]![1].body)).toMatchObject({
      file: "src/a.ts",
      line: 4,
      target: { roundId: "round-1", side: "old" },
    });
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
    expect(value.sessionFollows).toEqual([]);
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

  it("uses explicit read-only start and separate stop with no provider execution parameters", async () => {
    const fetchMock = vi.fn().mockImplementation(() =>
      Promise.resolve(
        new Response(JSON.stringify({ id: "follow/1", gaps: null }), {
          headers: { "content-type": "application/json" },
        }),
      ),
    );
    vi.stubGlobal("fetch", fetchMock);
    const follow = await api.startSessionFollow("thread/1", "snapshot-1");
    expect(follow.gaps).toEqual([]);
    expect(fetchMock).toHaveBeenNthCalledWith(
      1,
      "/api/v1/threads/thread%2F1/follows",
      expect.objectContaining({
        method: "POST",
        body: JSON.stringify({
          snapshotId: "snapshot-1",
          confirmReadOnly: true,
        }),
      }),
    );
    await api.stopSessionFollow("thread/1", "follow/1");
    expect(fetchMock).toHaveBeenNthCalledWith(
      2,
      "/api/v1/threads/thread%2F1/follows/follow%2F1",
      expect.objectContaining({ method: "DELETE" }),
    );
  });

  it("normalizes nullable Follow gaps in Thread snapshots", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response(
          JSON.stringify({
            id: "thread-1",
            sessionFollows: [{ id: "follow-1", gaps: null }],
          }),
          { headers: { "content-type": "application/json" } },
        ),
      ),
    );
    expect((await api.thread("thread-1")).sessionFollows?.[0]?.gaps).toEqual(
      [],
    );
  });

  it("preserves annotation anchors and sends observer annotations with fencing metadata", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
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

  it("reads an exact authorized snapshot and requests native Open without a deep link or Resume", async () => {
    const fetchMock = vi.fn().mockImplementation((path: string) =>
      Promise.resolve(
        new Response(
          JSON.stringify(
            path.endsWith("/open")
              ? {
                  status: "requested",
                  provider: "codex",
                  sessionId: "native-1",
                  target: "codex-desktop",
                  message: "Accepted",
                }
              : {
                  id: "snapshot/old",
                  threadId: "thread/1",
                  entries: null,
                  warnings: null,
                },
          ),
          {
            status: path.endsWith("/open") ? 202 : 200,
            headers: { "content-type": "application/json" },
          },
        ),
      ),
    );
    vi.stubGlobal("fetch", fetchMock);
    const snapshot = await api.sessionSnapshot("thread/1", "snapshot/old");
    expect(snapshot.entries).toEqual([]);
    expect(snapshot.warnings).toEqual([]);
    expect(fetchMock).toHaveBeenNthCalledWith(
      1,
      "/api/v1/threads/thread%2F1/sessions/snapshots/snapshot%2Fold",
      expect.not.objectContaining({ method: "POST" }),
    );
    expect(
      (await api.openNativeSession("thread/1", "snapshot/old")).status,
    ).toBe("requested");
    expect(fetchMock).toHaveBeenNthCalledWith(
      2,
      "/api/v1/threads/thread%2F1/sessions/open",
      expect.objectContaining({
        method: "POST",
        body: JSON.stringify({ snapshotId: "snapshot/old", confirmOpen: true }),
      }),
    );
  });

  it("preserves native-live consent separately from managed events and normalizes public live state", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(
        JSON.stringify({
          id: "thread-1",
          nativeLive: {
            followId: "follow-1",
            state: "limited",
            latestSnapshotId: "snapshot-2",
            entryKinds: null,
          },
        }),
        { headers: { "content-type": "application/json" } },
      ),
    );
    vi.stubGlobal("fetch", fetchMock);
    const nativeLive = {
      followId: "follow-1",
      expectedSnapshotId: "snapshot-1",
      entryKinds: ["message" as const],
      confirmCurrentAndFuture: true as const,
    };
    const value = await api.createShare("thread-1", {
      allowControl: false,
      allowDegraded: false,
      scope: { includeCode: false, includeEvents: false, nativeLive },
    });
    expect(JSON.parse(fetchMock.mock.calls[0]![1].body).scope).toEqual({
      includeCode: false,
      includeEvents: false,
      nativeLive,
    });
    expect(value.nativeLive?.entryKinds).toEqual([]);
    expect(value.sessionFollows).toEqual([]);
  });

  it("decodes vendor JSON bundles and keeps selection and copy confirmation explicit", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
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
