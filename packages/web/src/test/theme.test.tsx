import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { useTheme } from "../theme";

function Page() {
  const { theme, setTheme, working, error } = useTheme();
  return (
    <>
      <output>{theme}</output>
      <button disabled={working} onClick={() => void setTheme("dark")}>
        dark
      </button>
      <p role="alert">{error}</p>
    </>
  );
}
afterEach(() => {
  vi.unstubAllGlobals();
  localStorage.clear();
  delete document.documentElement.dataset.theme;
});
describe("shared theme preference", () => {
  it("reads Core without importing a browser cache and adopts another window's change", async () => {
    localStorage.setItem("teamcross.theme.v1", "dark");
    let mode = "light";
    const fetch = vi.fn(async () => new Response(JSON.stringify({ mode })));
    vi.stubGlobal("fetch", fetch);
    render(<Page />);
    await waitFor(() =>
      expect(screen.getByRole("status")).toHaveTextContent("light"),
    );
    expect(
      fetch.mock.calls.every(
        (call) =>
          (call as unknown as [string, RequestInit])[1].method === "GET",
      ),
    ).toBe(true);
    mode = "system";
    fireEvent.focus(window);
    await waitFor(() =>
      expect(screen.getByRole("status")).toHaveTextContent("system"),
    );
    expect(document.documentElement.dataset.theme).toBe("light");
  });
  it("applies a choice only after confirmation and never replays a lost response", async () => {
    let fail = false;
    let posts = 0;
    const fetch = vi.fn(async (_: string, init: RequestInit) => {
      if (init.method === "POST") {
        posts++;
        if (fail) throw new TypeError("response lost");
        return new Response('{"mode":"dark"}');
      }
      return new Response('{"mode":"light"}');
    });
    vi.stubGlobal("fetch", fetch);
    render(<Page />);
    await waitFor(() =>
      expect(screen.getByRole("status")).toHaveTextContent("light"),
    );
    fail = true;
    fireEvent.click(screen.getByRole("button"));
    await waitFor(() =>
      expect(screen.getByRole("alert")).not.toBeEmptyDOMElement(),
    );
    expect(screen.getByRole("status")).toHaveTextContent("light");
    expect(posts).toBe(1);
    fireEvent.focus(window);
    await act(async () => {});
    expect(posts).toBe(1);
    fail = false;
    fireEvent.click(screen.getByRole("button"));
    await waitFor(() =>
      expect(screen.getByRole("status")).toHaveTextContent("dark"),
    );
    expect(posts).toBe(2);
    await waitFor(() =>
      expect(document.documentElement.dataset.theme).toBe("dark"),
    );
  });
  it("does not let a stale read overwrite a newer confirmed write", async () => {
    let read: ((r: Response) => void) | undefined;
    vi.stubGlobal(
      "fetch",
      vi.fn((_path: string, init: RequestInit) =>
        init.method === "POST"
          ? Promise.resolve(new Response('{"mode":"dark"}'))
          : new Promise<Response>((resolve) => {
              read = resolve;
            }),
      ),
    );
    render(<Page />);
    fireEvent.click(screen.getByRole("button"));
    await waitFor(() =>
      expect(screen.getByRole("status")).toHaveTextContent("dark"),
    );
    await act(async () => read!(new Response('{"mode":"light"}')));
    expect(screen.getByRole("status")).toHaveTextContent("dark");
  });
});
