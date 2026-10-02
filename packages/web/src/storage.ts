// Browser storage stays unchanged in the WebGUI. Sandboxed hosts can supply
// private storage without changing page components or their behavior.
type Area = "local" | "session";
export type StoredValues = Record<Area, Record<string, string>>;
let hosted: StoredValues | undefined;
let changed: (() => void) | undefined;
const fallback: StoredValues = { local: {}, session: {} };
export function setHostedStorage(values?: StoredValues, onChange?: () => void) {
  hosted = values;
  changed = onChange;
}
function native(area: Area) {
  try {
    return window[area === "local" ? "localStorage" : "sessionStorage"];
  } catch {
    return undefined;
  }
}
function storage(area: Area) {
  return {
    getItem(key: string): string | null {
      if (!hosted) {
        try {
          return native(area)?.getItem(key) ?? fallback[area][key] ?? null;
        } catch {
          /* Storage can be denied by the embedding environment. */
        }
      }
      return (hosted || fallback)[area][key] ?? null;
    },
    setItem(key: string, value: string) {
      if (!hosted) {
        try {
          const store = native(area);
          if (store) {
            store.setItem(key, value);
            return;
          }
        } catch {
          /* Keep this window usable if storage is unavailable. */
        }
      }
      (hosted || fallback)[area][key] = value;
      changed?.();
    },
    removeItem(key: string) {
      if (!hosted) {
        try {
          native(area)?.removeItem(key);
        } catch {
          /* optional storage */
        }
      }
      delete (hosted || fallback)[area][key];
      changed?.();
    },
  };
}
export const localStorage = storage("local");
export const sessionStorage = storage("session");
