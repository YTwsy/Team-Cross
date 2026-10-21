import type { LibraryBundle } from "./library";
export type DeliveryState = "sent" | "unknown";
export type CurrentConversation = {
  status: (code: string) => DeliveryState | undefined;
  send: (bundle: LibraryBundle, prompt: string) => Promise<void>;
  connect?: (spaceId: string, name: string) => Promise<void>;
  allowConnectAgain?: (spaceId: string) => void;
};
let current: CurrentConversation | undefined;
export function setCurrentConversation(value?: CurrentConversation) {
  current = value;
}
export function currentConversation() {
  return current;
}
