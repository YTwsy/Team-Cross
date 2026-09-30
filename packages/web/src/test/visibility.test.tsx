import { act, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useResource } from "../api";
import { isWindowVisible, watchVisibleRefresh } from "../visibility";

function visibility(visible: boolean) {
  Object.defineProperty(document, "hidden", {
    configurable: true,
    value: !visible,
  });
  document.dispatchEvent(new Event("visibilitychange"));
}
function nativeVisibility(visible: boolean) {
  document.documentElement.dataset.teamcrossVisible = String(visible);
  document.dispatchEvent(new Event("teamcross-visibility"));
}
beforeEach(() => {
  vi.useFakeTimers();
  visibility(true);
});
afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
  document.querySelector('meta[name="teamcross-desktop"]')?.remove();
  delete document.documentElement.dataset.teamcrossVisible;
  Reflect.deleteProperty(document, "hidden");
});

describe("visible background reads", () => {
  it("pauses for native occlusion, keeps visible unfocused windows live, and refreshes once on return", async () => {
    const meta = document.createElement("meta");
    meta.name = "teamcross-desktop";
    document.head.append(meta);
    const refresh = vi.fn(async () => {});
    const stop = watchVisibleRefresh(refresh, 1000);
    expect(isWindowVisible()).toBe(false);
    await vi.advanceTimersByTimeAsync(3000);
    expect(refresh).not.toHaveBeenCalled();
    nativeVisibility(true);
    await vi.advanceTimersByTimeAsync(0);
    expect(refresh).toHaveBeenCalledTimes(1);
    window.dispatchEvent(new Event("blur"));
    await vi.advanceTimersByTimeAsync(1000);
    expect(refresh).toHaveBeenCalledTimes(2);
    nativeVisibility(false);
    await vi.advanceTimersByTimeAsync(3000);
    expect(refresh).toHaveBeenCalledTimes(2);
    nativeVisibility(true);
    nativeVisibility(true);
    window.dispatchEvent(new Event("focus"));
    await vi.advanceTimersByTimeAsync(0);
    expect(refresh).toHaveBeenCalledTimes(3);
    stop();
    await vi.advanceTimersByTimeAsync(2000);
    expect(refresh).toHaveBeenCalledTimes(3);
  });

  it("cancels a hidden read and never overlaps a live read or revives its stale timer", async () => {
    const pending: { signal: AbortSignal; done: () => void }[] = [];
    const refresh = vi.fn(
      (signal: AbortSignal) =>
        new Promise<void>((done) => pending.push({ signal, done })),
    );
    const stop = watchVisibleRefresh(refresh, 1000);
    window.dispatchEvent(new Event("focus"));
    await vi.advanceTimersByTimeAsync(3000);
    expect(refresh).toHaveBeenCalledTimes(1);
    visibility(false);
    expect(pending[0]!.signal.aborted).toBe(true);
    visibility(true);
    expect(refresh).toHaveBeenCalledTimes(2);
    pending[0]!.done();
    await vi.advanceTimersByTimeAsync(3000);
    expect(refresh).toHaveBeenCalledTimes(2);
    pending[1]!.done();
    await vi.advanceTimersByTimeAsync(1000);
    expect(refresh).toHaveBeenCalledTimes(3);
    stop();
    expect(pending[2]!.signal.aborted).toBe(true);
    pending[2]!.done();
  });

  it("retains the resource and draft while hidden and replaces only data after reopening", async () => {
    let version = 1;
    const fetch = vi.fn(async () => new Response(JSON.stringify({ version })));
    vi.stubGlobal("fetch", fetch);
    function Page() {
      const { data } = useResource<{ version: number }>("collaborations", 1000);
      return (
        <>
          <input aria-label="draft" />
          <span>{data?.version}</span>
        </>
      );
    }
    const page = render(<Page />);
    await act(() => vi.advanceTimersByTimeAsync(0));
    fireEvent.change(screen.getByLabelText("draft"), {
      target: { value: "保留草稿 🧪" },
    });
    expect(screen.getByText("1")).toBeInTheDocument();
    act(() => visibility(false));
    version = 2;
    await act(() => vi.advanceTimersByTimeAsync(4000));
    expect(fetch).toHaveBeenCalledTimes(1);
    expect(screen.getByLabelText("draft")).toHaveValue("保留草稿 🧪");
    act(() => visibility(true));
    await act(() => vi.advanceTimersByTimeAsync(0));
    expect(screen.getByText("2")).toBeInTheDocument();
    expect(screen.getByLabelText("draft")).toHaveValue("保留草稿 🧪");
    page.unmount();
  });
});
