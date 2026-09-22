import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { useState } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  captureReadingPosition,
  restoreReadingPosition,
} from "../reading-position";
import { ReadingTabs, type ReadingTab } from "../components/ReadingTabs";
import { Members } from "../components/Members";
import type { Collaboration } from "../types";

let scrollY = 0;
const rect = (top: number, height = 100) =>
  ({
    top,
    bottom: top + height,
    height,
    width: 700,
    left: 0,
    right: 700,
    x: 0,
    y: top,
    toJSON() {},
  }) as DOMRect;

beforeEach(() => {
  scrollY = 0;
  vi.spyOn(window, "scrollY", "get").mockImplementation(() => scrollY);
  vi.mocked(window.scrollTo).mockImplementation(
    (options: number | ScrollToOptions | undefined) => {
      if (typeof options === "object") scrollY = options.top || 0;
    },
  );
});
afterEach(() => {
  vi.restoreAllMocks();
  vi.mocked(window.scrollTo).mockReset();
});

describe("整页阅读位置", () => {
  it("keeps the same paragraph below a resized sticky toolbar when earlier content changes height", () => {
    const { container } = render(
      <section
        className="collaboration-reading"
        style={{ "--reading-sticky-top": "12px" } as React.CSSProperties}
      >
        <div className="reading-heading" />
        <div className="reading-pane">
          <div data-reading-key="turn:item:0">
            <div className="reader-markdown">
              <p>前一段</p>
              <p>正在阅读这一段</p>
            </div>
          </div>
        </div>
      </section>,
    );
    const root = container.querySelector<HTMLElement>(".reading-pane")!;
    const workspace = container.querySelector<HTMLElement>("section")!;
    const heading = container.querySelector<HTMLElement>(".reading-heading")!;
    const message = root.querySelector<HTMLElement>("[data-reading-key]")!;
    const paragraphs = root.querySelectorAll("p");
    let precedingHeight = 0,
      headerHeight = 100;
    scrollY = 900;
    vi.spyOn(workspace, "getBoundingClientRect").mockImplementation(() =>
      rect(300 - scrollY, 4000),
    );
    vi.spyOn(heading, "getBoundingClientRect").mockImplementation(() =>
      rect(12, headerHeight),
    );
    vi.spyOn(message, "getBoundingClientRect").mockImplementation(() =>
      rect(500 + precedingHeight - scrollY, 1600),
    );
    vi.spyOn(paragraphs[0]!, "getBoundingClientRect").mockImplementation(() =>
      rect(600 + precedingHeight - scrollY, 200),
    );
    vi.spyOn(paragraphs[1]!, "getBoundingClientRect").mockImplementation(() =>
      rect(1010 + precedingHeight - scrollY, 300),
    );
    const saved = captureReadingPosition(root);
    expect(saved?.key).toBe("turn:item:0");
    expect(saved?.block).toBe(1);
    precedingHeight = 260;
    headerHeight = 140;
    restoreReadingPosition(root, saved);
    expect(scrollY).toBe(1120);
    expect(
      paragraphs[1]!.getBoundingClientRect().top - (12 + headerHeight + 12),
    ).toBe(saved?.offset);
  });

  it("restores each tab in the document and lets annotation navigation own its destination", async () => {
    let navigate: (() => void) | undefined;
    function Harness() {
      const [active, setActive] = useState<ReadingTab>("materials");
      const [serial, setSerial] = useState(0);
      navigate = () => {
        setActive("materials");
        setSerial((n) => n + 1);
      };
      return (
        <ReadingTabs
          active={active}
          onChange={setActive}
          navigationKey={serial}
          materialCount={1}
          materials={
            <div data-reading-key="material">
              <p>材料正文</p>
            </div>
          }
          context={
            <div data-reading-key="context">
              <p>上下文正文</p>
            </div>
          }
        />
      );
    }
    vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockImplementation(
      function (this: HTMLElement) {
        if (this.classList.contains("collaboration-reading"))
          return rect(300 - scrollY, 4000);
        if (this.classList.contains("reading-heading"))
          return rect(Math.max(0, 300 - scrollY), 100);
        if (this.dataset.readingKey === "material")
          return rect(500 - scrollY, 3000);
        if (this.dataset.readingKey === "context")
          return rect(450 - scrollY, 3500);
        return rect(0, 0);
      },
    );
    render(<Harness />);
    scrollY = 900;
    fireEvent.click(screen.getByRole("tab", { name: "协作上下文" }));
    expect(scrollY).toBe(300);
    scrollY = 1300;
    fireEvent.click(screen.getByRole("tab", { name: /已发布会话材料/ }));
    expect(scrollY).toBe(900);
    fireEvent.click(screen.getByRole("tab", { name: "协作上下文" }));
    expect(scrollY).toBe(1300);
    scrollY = 1700;
    await act(async () => navigate?.());
    expect(scrollY).toBe(1700);
  });

  it("does not restore a hidden reader or turn a library preview into document scrolling", () => {
    const { container } = render(
      <div className="library-preview">
        <div className="reading-pane">
          <p>预览</p>
        </div>
      </div>,
    );
    const preview = container.querySelector<HTMLElement>(".library-preview")!;
    const pane = container.querySelector<HTMLElement>(".reading-pane")!;
    preview.scrollTop = 350;
    const position = captureReadingPosition(pane);
    preview.scrollTop = 50;
    restoreReadingPosition(pane, position);
    expect(preview.scrollTop).toBe(350);
    expect(scrollY).toBe(0);
    pane.hidden = true;
    restoreReadingPosition(pane, { scrollTop: 800 });
    expect(preview.scrollTop).toBe(350);
    expect(captureReadingPosition(pane)).toBeUndefined();
  });
});

describe("成员滚动折叠", () => {
  function mountMembers() {
    vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockImplementation(
      function (this: HTMLElement) {
        return this.classList.contains("detail-grid")
          ? rect(300 - scrollY, 3000)
          : rect(0, 100);
      },
    );
    const result = render(
      <div className="detail-grid" style={{ display: "grid" }}>
        <div className="detail-main" />
        <Members
          collaboration={
            {
              id: "space",
              role: "owner",
              writer: "owner",
              members: [],
              hasExecution: true,
            } as unknown as Collaboration
          }
          busy={false}
          closed={false}
          onAction={() => {}}
          onRemove={() => {}}
          onAssist={() => {}}
        />
      </div>,
    );
    return result;
  }
  it("starts fully expanded, permits a manual expansion during reading, and expands again at the overview", async () => {
    mountMembers();
    expect(screen.getByRole("button", { name: "收起成员" })).toHaveAttribute(
      "aria-expanded",
      "true",
    );
    scrollY = 500;
    fireEvent.scroll(window);
    await screen.findByRole("button", { name: "展开成员" });
    const body = document.querySelector(".participants-body");
    expect(body).not.toHaveAttribute("hidden");
    expect(body).toHaveAttribute("inert");
    expect(window.scrollTo).not.toHaveBeenCalled();
    expect(
      screen.queryByRole("button", { name: "使用自己的客户端辅助" }),
    ).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "展开成员" }));
    scrollY = 800;
    fireEvent.scroll(window);
    await waitFor(() =>
      expect(
        screen.getByRole("button", { name: "使用自己的客户端辅助" }),
      ).toBeVisible(),
    );
    scrollY = 0;
    fireEvent.scroll(window);
    await act(
      async () =>
        new Promise<void>((resolve) => requestAnimationFrame(() => resolve())),
    );
    scrollY = 500;
    fireEvent.scroll(window);
    await screen.findByRole("button", { name: "展开成员" });
    scrollY = 0;
    fireEvent.scroll(window);
    await screen.findByRole("button", { name: "收起成员" });
  });

  it("does not hide the focused member control while scrolling", async () => {
    mountMembers();
    screen.getByRole("button", { name: "使用自己的客户端辅助" }).focus();
    scrollY = 500;
    fireEvent.scroll(window);
    await act(
      async () =>
        new Promise<void>((resolve) => requestAnimationFrame(() => resolve())),
    );
    expect(screen.getByRole("button", { name: "收起成员" })).toBeVisible();
    (document.activeElement as HTMLElement).blur();
    await screen.findByRole("button", { name: "展开成员" });
  });

  it("does not flicker near the boundary and keeps the stacked mobile card expanded", async () => {
    const { container } = mountMembers();
    scrollY = 300;
    fireEvent.scroll(window);
    await screen.findByRole("button", { name: "展开成员" });
    scrollY = 275;
    fireEvent.scroll(window);
    await act(
      async () =>
        new Promise<void>((resolve) => requestAnimationFrame(() => resolve())),
    );
    expect(screen.getByRole("button", { name: "展开成员" })).toBeVisible();
    container.querySelector<HTMLElement>(".detail-grid")!.style.display =
      "flex";
    fireEvent.resize(window);
    await screen.findByRole("button", { name: "收起成员" });
  });
});
