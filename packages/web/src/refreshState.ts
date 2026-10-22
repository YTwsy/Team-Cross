import {
  useEffect,
  useCallback,
  useRef,
  useState,
  type Dispatch,
  type SetStateAction,
} from "react";

export type RefreshSnapshot = {
  scope: string;
  scrollY?: number;
  values: Record<string, { value?: unknown }>;
};
let enabled = false;
let scope = "";
let restored: RefreshSnapshot["values"] = {};
const entries = new Map<
  string,
  { value: () => unknown; dirty: () => boolean }
>();

// A checkpoint is private to this panel and Core. It is not business data and
// is never sent to a model or used to submit a request automatically.
export function beginRefreshState(nextScope: string, saved?: RefreshSnapshot) {
  enabled = true;
  scope = nextScope;
  restored = saved?.scope === scope ? { ...saved.values } : {};
  entries.clear();
}
export function endRefreshState() {
  enabled = false;
  entries.clear();
  restored = {};
}
export function hasRefreshDrafts() {
  return (
    [...entries.values()].some((e) => e.dirty()) ||
    Object.entries(restored).some(
      ([key, item]) => key.startsWith("guard:") && item.value === true,
    )
  );
}
export function captureRefreshState(): RefreshSnapshot {
  const values = {
    ...restored,
    ...Object.fromEntries(
      [...entries].map(([key, e]) => [key, { value: e.value() }]),
    ),
  };
  const text = JSON.stringify({ scope, values, scrollY: window.scrollY });
  if (text.length > 4_000_000) throw new Error("Draft checkpoint is too large");
  return JSON.parse(text);
}
export function wasRefreshRestored(key: string) {
  return enabled && Object.hasOwn(restored, key);
}

export function useRefreshState<T>(
  key: string,
  initial: T | (() => T),
): [T, Dispatch<SetStateAction<T>>];
export function useRefreshState<T = undefined>(
  key: string,
): [T | undefined, Dispatch<SetStateAction<T | undefined>>];
export function useRefreshState<T>(key: string, initial?: T | (() => T)) {
  const initialValue = () =>
    wasRefreshRestored(key)
      ? (restored[key]!.value as T)
      : typeof initial === "function"
        ? (initial as () => T)()
        : initial;
  const [state, setState] = useState(() => ({ key, value: initialValue() }));
  if (state.key !== key) setState({ key, value: initialValue() });
  const setValue = useCallback<Dispatch<SetStateAction<T | undefined>>>(
    (value) =>
      setState((current) =>
        current.key !== key
          ? current
          : {
              key,
              value:
                typeof value === "function"
                  ? (value as (v: T | undefined) => T | undefined)(
                      current.value,
                    )
                  : value,
            },
      ),
    [key],
  );
  const latest = useRef(state.value);
  latest.current = state.value;
  useEffect(() => {
    if (!enabled) return;
    const entry = { value: () => latest.current, dirty: () => false };
    delete restored[key];
    entries.set(key, entry);
    return () => {
      if (entries.get(key) === entry) entries.delete(key);
    };
  }, [key]);
  return [state.value, setValue] as [
    T | undefined,
    Dispatch<SetStateAction<T | undefined>>,
  ];
}

export function useRefreshRef<T>(key: string, initial: T) {
  const load = () =>
    wasRefreshRestored(key) ? (restored[key]!.value as T) : initial;
  const slot = useRef({ key, ref: { current: load() } });
  if (slot.current.key !== key)
    slot.current = { key, ref: { current: load() } };
  const ref = slot.current.ref;
  useEffect(() => {
    if (!enabled) return;
    const entry = { value: () => ref.current, dirty: () => false };
    delete restored[key];
    entries.set(key, entry);
    return () => {
      if (entries.get(key) === entry) entries.delete(key);
    };
  }, [key]);
  return ref;
}

export function useRefreshGuard(key: string, dirty: boolean) {
  const latest = useRef(dirty);
  latest.current = dirty;
  useEffect(() => {
    if (!enabled) return;
    const entry = { value: () => latest.current, dirty: () => latest.current };
    delete restored[`guard:${key}`];
    entries.set(`guard:${key}`, entry);
    return () => {
      if (entries.get(`guard:${key}`) === entry) entries.delete(`guard:${key}`);
    };
  }, [key]);
}
