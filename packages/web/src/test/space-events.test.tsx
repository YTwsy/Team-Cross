import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, it, vi } from "vitest";
import { SpaceEvents } from "../components/SpaceEvents";

afterEach(() => vi.unstubAllGlobals());

it("does not create another credential after a lost response without an explicit check", async () => {
  const writes: { requestId: string }[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (_url: string, init?: RequestInit) => {
      if (init?.body) {
        writes.push(JSON.parse(String(init.body)));
        throw new TypeError("Response lost");
      }
      return {
        ok: true,
        json: async () => ({ connections: [], subscriptions: [], events: [] }),
      };
    }),
  );
  render(<SpaceEvents spaceId="space" disabled={false} />);
  await userEvent.click(screen.getByText("ChatGPT 云端通知"));
  const create = screen.getByRole("button", { name: "创建空间连接凭据" });
  await userEvent.click(create);
  await waitFor(() => expect(create).toBeDisabled());
  const checked = await screen.findByRole("button", {
    name: "已核对连接，准备创建新凭据",
  });
  await userEvent.click(create);
  expect(writes).toHaveLength(1);
  await userEvent.click(checked);
  expect(writes).toHaveLength(1);
  await userEvent.click(create);
  await waitFor(() => expect(writes).toHaveLength(2));
  expect(writes[1]!.requestId).not.toBe(writes[0]!.requestId);
});
