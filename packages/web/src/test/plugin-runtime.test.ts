import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { replacePanel, type PanelRuntime } from "../plugin/update";

const checkpoint = {
  scope: "fixture",
  values: { draft: { value: "keep me" } },
};
const bundle = {
  protocol: 1,
  revision: "a".repeat(64),
  style: "body{color:blue}",
  script: "// replacement",
};
let previous: PanelRuntime;
beforeEach(() => {
  vi.useFakeTimers();
  document.head.innerHTML =
    '<meta name="teamcross-panel-revision" content="old"><style id="teamcross-panel-style">body{color:red}</style>';
  document.body.innerHTML = '<div id="root">old editor</div>';
  previous = {
    dispose: vi.fn(),
    mount: vi.fn(async () => {
      window.__teamcrossRuntime = previous;
    }),
  };
  window.__teamcrossRuntime = previous;
});
afterEach(() => {
  vi.useRealTimers();
  delete window.__teamcrossRuntime;
  delete window.__teamcrossCheckpoint;
  delete window.__teamcrossFailedRevision;
  delete window.__teamcrossUpdateFailure;
  delete window.__teamcrossCancelMount;
  delete window.__teamcrossMountGeneration;
});
it("waits for the runtime when load fires before the asynchronous host handshake", async () => {
  let completed = false;
  const pending = replacePanel(bundle, checkpoint).then(() => {
    completed = true;
  });
  document
    .querySelector('script[type="module"]')!
    .dispatchEvent(new Event("load"));
  await vi.advanceTimersByTimeAsync(500);
  expect(completed).toBe(false);
  window.__teamcrossRuntime = {
    dispose: vi.fn(),
    mount: vi.fn(async () => {}),
  };
  await vi.advanceTimersByTimeAsync(50);
  await pending;
  expect(previous.mount).not.toHaveBeenCalled();
});
it("does not roll back a healthy replacement for unrelated window errors", async () => {
  const pending = replacePanel(bundle, checkpoint);
  window.dispatchEvent(
    new ErrorEvent("error", {
      message: "unrelated host error",
      filename: "host.js",
    }),
  );
  const replacement = { dispose: vi.fn(), mount: vi.fn(async () => {}) };
  window.__teamcrossRuntime = replacement;
  await vi.advanceTimersByTimeAsync(50);
  await pending;
  expect(window.__teamcrossRuntime).toBe(replacement);
  expect(previous.mount).not.toHaveBeenCalled();
});
it("cancels a stalled startup before restoring the editor and draft checkpoint", async () => {
  window.__teamcrossMountGeneration = 4;
  const cancel = vi.fn();
  window.__teamcrossCancelMount = cancel;
  const failed = replacePanel(bundle, checkpoint).catch((error) => error);
  await vi.advanceTimersByTimeAsync(120000);
  expect((await failed).message).toBe("Panel startup timed out");
  expect(cancel).toHaveBeenCalledTimes(1);
  expect(window.__teamcrossMountGeneration).toBe(5);
  expect(window.__teamcrossCheckpoint).toEqual(checkpoint);
  expect(window.__teamcrossFailedRevision).toBe(bundle.revision);
  expect(document.getElementById("teamcross-panel-style")?.textContent).toBe(
    "body{color:red}",
  );
  expect(previous.mount).toHaveBeenCalledTimes(1);
});
it("rolls back a startup failure immediately while retaining the concrete error", async () => {
  const failed = replacePanel(bundle, checkpoint).catch((error) => error);
  document
    .querySelector('script[type="module"]')!
    .dispatchEvent(
      new CustomEvent("error", { detail: new Error("bootstrap refused") }),
    );
  expect((await failed).message).toBe("bootstrap refused");
  expect(window.__teamcrossCheckpoint).toEqual(checkpoint);
  expect(previous.mount).toHaveBeenCalledTimes(1);
});
it("accepts explicit asynchronous startup without an inline module load event and stays updated past the old timeout", async () => {
  const replacement = { dispose: vi.fn(), mount: vi.fn(async () => {}) };
  const outcome = replacePanel(bundle, checkpoint).then(
    () => "ready",
    () => "rolled back",
  );
  await vi.advanceTimersByTimeAsync(500);
  window.__teamcrossRuntime = replacement;
  document
    .querySelector('script[type="module"]')!
    .dispatchEvent(new Event("teamcross-panel-ready"));
  await vi.advanceTimersByTimeAsync(125000);
  expect(await outcome).toBe("ready");
  expect(window.__teamcrossRuntime).toBe(replacement);
  expect(previous.mount).not.toHaveBeenCalled();
  expect(replacement.dispose).not.toHaveBeenCalled();
  expect(
    document.querySelector('meta[name="teamcross-panel-revision"]'),
  ).toHaveAttribute("content", bundle.revision);
});
