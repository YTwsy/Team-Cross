import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { MarkdownText } from "../components/MarkdownText";
import { ReaderImages } from "../components/ReaderMedia";
import { ReaderMessage } from "../components/Reading";
import {
  joinMaterialSegments,
  mappedSelection,
  mergeMaterialSegments,
} from "../reading";

vi.mock("pele/lazy", () => ({
  detectType: () => "flowchart",
  load: async () => true,
  mount: (
    element: HTMLElement,
    source: string,
    options: { sanitize: (svg: string) => Node; onError: () => void },
  ) => {
    if (source.includes("broken")) options.onError();
    else
      element.replaceChildren(
        options.sanitize(
          '<svg xmlns="http://www.w3.org/2000/svg"><text>Start</text><script>alert(1)</script><a href="https://example.invalid"><text>Link</text></a></svg>',
        ),
      );
    return { destroy: () => element.replaceChildren() };
  },
}));
afterEach(() => {
  vi.unstubAllGlobals();
  vi.useRealTimers();
  window.getSelection()?.removeAllRanges();
});

it("retains images that arrive on later message segments without duplicating them", () => {
  const first = {
    turnId: "t",
    itemId: "i",
    type: "userMessage",
    text: "prefix ",
    startOffset: 0,
    endOffset: 7,
    length: 14,
  };
  const image = {
    id: "a",
    mime: "image/png",
    name: "a.png",
    hash: "frozen",
    startOffset: 7,
    endOffset: 14,
    quote: "[image]",
  };
  const second = {
    ...first,
    text: "[image]",
    startOffset: 7,
    endOffset: 14,
    attachments: [image],
  };
  expect(joinMaterialSegments([first, second])[0]!.attachments).toEqual([
    image,
  ]);
  const merged = mergeMaterialSegments([first], [second]);
  expect(merged[0]!.attachments).toEqual([image]);
  expect(merged[0]!.text).toBe("prefix [image]");
  expect(joinMaterialSegments(merged)[0]!.attachments).toEqual([image]);
  expect(first).not.toHaveProperty("attachments");
});

it("reveals an exact source match hidden by Markdown rendering", async () => {
  const source = "![diagram](https://example.invalid/private.png)";
  const quote = "https://example.invalid/private.png";
  const target = {
    kind: "material" as const,
    materialId: "m",
    version: 1,
    turnId: "t",
    itemId: "i",
    startOffset: 0,
    quote: "",
  };
  const start = source.indexOf(quote);
  const view = render(
    <ReaderMessage
      source={source}
      label="answer"
      target={target}
      activeTarget={{
        ...target,
        startOffset: start,
        endOffset: start + quote.length,
        quote,
      }}
      onAnnotate={vi.fn()}
    />,
  );
  await waitFor(() =>
    expect(
      view.container.querySelector(".reader-raw [data-annotation-highlight]"),
    ).toHaveTextContent(quote),
  );
});

describe("rich source-preserving reading", () => {
  it("keeps generating diagrams as source, then renders and supports source/block annotation", async () => {
    const source = "```mermaid\nflowchart LR\n A[Start] --> B[End]\n```";
    const annotate = vi.fn();
    const view = render(
      <MarkdownText source={source} streaming onAnnotate={annotate} />,
    );
    expect(screen.queryByRole("img")).not.toBeInTheDocument();
    view.rerender(<MarkdownText source={source} onAnnotate={annotate} />);
    await waitFor(() =>
      expect(view.container.querySelector("svg text")).toHaveTextContent(
        "Start",
      ),
    );
    expect(view.container.querySelector("svg script, svg a")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "批注此块" }));
    expect(annotate).toHaveBeenCalledWith(
      0,
      source.length,
      expect.any(HTMLElement),
    );
    fireEvent.click(screen.getByRole("button", { name: "显示源码" }));
    expect(screen.queryByRole("img")).not.toBeInTheDocument();
    expect(view.container.querySelector("pre")).toHaveTextContent(
      "flowchart LR",
    );
  });

  it("falls back to source on diagram errors and opens source for an exact search hit", async () => {
    const source = "```mermaid\nbroken diagram\n```";
    const view = render(<MarkdownText source={source} />);
    await screen.findByText("图形无法渲染，已保留源码。");
    expect(view.container.querySelector("pre")).toHaveTextContent(
      "broken diagram",
    );
    const valid = "```mermaid\nflowchart LR\nA-->B\n```";
    view.rerender(
      <MarkdownText
        source={valid}
        highlights={[{ start: 23, end: 24, active: true }]}
      />,
    );
    await waitFor(() =>
      expect(view.container.querySelector("pre")).toBeVisible(),
    );
  });

  it("keeps code selection offsets through wrapping and copies table formats", async () => {
    const write = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: { writeText: write },
    });
    const source =
      '```ts\nconst face = "🙂";\n```\n\n| Name | Value |\n| --- | --- |\n| a,b | **bold** |';
    const view = render(<MarkdownText source={source} />);
    fireEvent.click(screen.getAllByRole("button", { name: "自动换行" })[0]!);
    const span = Array.from(
      view.container.querySelectorAll<HTMLElement>("pre [data-source-start]"),
    ).find((el) => el.textContent?.includes("const face"))!;
    const range = document.createRange();
    range.setStart(span.firstChild!, 0);
    range.setEnd(span.firstChild!, 5);
    window.getSelection()?.addRange(range);
    expect(mappedSelection(view.container, source)).toEqual({
      startOffset: 6,
      endOffset: 11,
      quote: "const",
    });
    fireEvent.click(screen.getByRole("button", { name: "复制 CSV" }));
    await waitFor(() =>
      expect(write).toHaveBeenCalledWith('"Name","Value"\r\n"a,b","bold"'),
    );
    fireEvent.click(screen.getByRole("button", { name: "复制 Markdown" }));
    expect(write).toHaveBeenCalledWith(source.slice(source.indexOf("| Name")));
  });

  it("renders formulae and never loads Markdown image URLs", () => {
    const { container } = render(
      <MarkdownText
        source={
          "Inline $x^2$\n\n$$\n\\frac{a}{b}\n$$\n\n![remote](https://example.invalid/image.png)"
        }
      />,
    );
    expect(container.querySelectorAll("math")).toHaveLength(2);
    expect(container.querySelector("img")).toBeNull();
    expect(screen.getByRole("link", { name: "打开图片来源" })).toHaveAttribute(
      "href",
      "https://example.invalid/image.png",
    );
  });
});

it("assembles authorized image chunks, exposes the whole-image anchor, and rejects changed hashes", async () => {
  const image = {
    id: "i",
    name: "fixture.png",
    mime: "image/png",
    hash: "frozen",
    startOffset: 3,
    endOffset: 12,
  };
  const annotate = vi.fn();
  const fetch = vi.fn(async (_url: string, init: RequestInit) => {
    const body = JSON.parse(String(init.body));
    expect(body.version).toBe(1);
    expect(body.attachmentId).toBe("i");
    return new Response(
      JSON.stringify({
        attachment: {
          ...image,
          data: body.attachmentOffset ? "BBBB" : "AAAA",
          nextOffset: body.attachmentOffset ? 0 : 4,
        },
      }),
    );
  });
  vi.stubGlobal("fetch", fetch);
  const view = render(
    <ReaderImages
      images={[image]}
      resource={{
        path: "collaborations/s/read-material",
        body: { materialId: "m", version: 1, turnId: "t", itemId: "item" },
      }}
      onAnnotate={annotate}
    />,
  );
  expect(
    await screen.findByRole("img", { name: "fixture.png" }),
  ).toHaveAttribute("src", "data:image/png;base64,AAAABBBB");
  expect(fetch).toHaveBeenCalledTimes(2);
  fireEvent.click(screen.getByRole("button", { name: "批注图片" }));
  expect(annotate).toHaveBeenCalledWith(image);
  fetch.mockImplementation(
    async () =>
      new Response(
        JSON.stringify({
          attachment: {
            ...image,
            hash: "different-version",
            data: "AAAA",
            nextOffset: 0,
          },
        }),
      ),
  );
  view.rerender(
    <ReaderImages
      images={[image]}
      resource={{
        path: "collaborations/s/read-material",
        body: { materialId: "m", version: 2, turnId: "t", itemId: "item" },
      }}
    />,
  );
  await waitFor(() =>
    expect(
      screen.queryByRole("img", { name: "fixture.png" }),
    ).not.toBeInTheDocument(),
  );
  expect(await screen.findByRole("button", { name: "重试" })).toBeVisible();
});

it("maps displayed formula source back to the original UTF-16 selection", () => {
  const source = "前言 🙂\n\n$$\n\\frac{a}{b}\n$$";
  const annotate = vi.fn();
  const view = render(<MarkdownText source={source} onAnnotate={annotate} />);
  fireEvent.click(screen.getByRole("button", { name: "显示源码" }));
  const span = Array.from(
    view.container.querySelectorAll("pre [data-source-start]"),
  ).find((el) => el.textContent?.startsWith("\\frac"))!;
  const range = document.createRange();
  range.selectNodeContents(span);
  window.getSelection()?.addRange(range);
  const selection = mappedSelection(view.container, source)!;
  expect(selection.quote.trim()).toBe("\\frac{a}{b}");
  expect(source.slice(selection.startOffset, selection.endOffset)).toBe(
    selection.quote,
  );
  expect(selection.startOffset).toBe(source.indexOf("\\frac"));
  fireEvent.click(screen.getByRole("button", { name: "批注此块" }));
  expect(annotate).toHaveBeenCalledWith(
    source.indexOf("$$"),
    source.length,
    expect.any(HTMLElement),
  );
});
