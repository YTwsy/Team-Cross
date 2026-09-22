import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { api, errorText } from "./api";
import type { AnnotationTarget } from "./types";
import { Icon } from "./components/ui";

export type LibraryReference = {
  spaceId: string;
  kind: "material" | "annotation" | "context";
  materialId?: string;
  version?: number;
  annotationId?: string;
  target?: AnnotationTarget;
};
export type LibraryResource = {
  key: string;
  reference: LibraryReference;
  title: string;
  spaceTitle: string;
  sessionId: string;
  sessionTitle: string;
  provider?: string;
  author?: string;
  summary?: string;
  updatedAt: string;
  openedAt?: string;
  favorite: boolean;
  selected: boolean;
  annotated: boolean;
  availability: "available" | "offline" | "withdrawn" | "unavailable";
  replyCount: number;
};
export type LibraryView = { resources: LibraryResource[]; selection: string[] };
export type LibraryBundle = {
  code: string;
  references: LibraryReference[];
  expiresAt: string;
};
type Change = {
  action: "visit" | "select" | "favorite" | "clear";
  reference?: LibraryReference;
  key?: string;
  enabled?: boolean;
};
type LibraryContext = {
  data?: LibraryView;
  error: string;
  working: boolean;
  refresh: (options?: { reorder?: boolean }) => void;
  change: (input: Change) => Promise<string | undefined>;
};
const Context = createContext<LibraryContext | undefined>(undefined);
export const useLibrary = () => useContext(Context);
export const libraryGroupKey = (resource: LibraryResource) =>
  `${resource.reference.spaceId}:${resource.sessionId}`;

function keepResourceOrder(
  previous: LibraryView | undefined,
  next: LibraryView,
) {
  if (!previous) return next;
  const groups = new Map<string, number>();
  const rows = new Map<string, number>();
  for (const resource of previous.resources) {
    const group = libraryGroupKey(resource);
    if (!groups.has(group)) groups.set(group, groups.size);
    rows.set(resource.key, rows.size);
  }
  const rank = (order: Map<string, number>, key: string) =>
    order.get(key) ?? Number.MAX_SAFE_INTEGER;
  return {
    ...next,
    // Apply current content and permissions while retaining the browsing order.
    resources: [...next.resources].sort(
      (a, b) =>
        rank(groups, libraryGroupKey(a)) - rank(groups, libraryGroupKey(b)) ||
        rank(rows, a.key) - rank(rows, b.key),
    ),
  };
}

const targetKey = (target?: AnnotationTarget) =>
  target
    ? JSON.stringify(
        Object.entries(target)
          .filter(
            ([, value]) => value !== undefined && value !== "" && value !== 0,
          )
          .sort(([a], [b]) => a.localeCompare(b)),
      )
    : "";
export const sameReference = (a: LibraryReference, b: LibraryReference) =>
  a.spaceId === b.spaceId &&
  a.kind === b.kind &&
  a.materialId === b.materialId &&
  a.version === b.version &&
  a.annotationId === b.annotationId &&
  targetKey(a.target) === targetKey(b.target);
export function LibraryProvider({ children }: { children: ReactNode }) {
  const [data, setData] = useState<LibraryView>();
  const [error, setError] = useState("");
  const [working, setWorking] = useState(false);
  const revision = useRef(0),
    pending = useRef(false);
  const refresh = useCallback((options?: { reorder?: boolean }) => {
    if (pending.current) return;
    const version = ++revision.current;
    void api<LibraryView>("library")
      .then((next) => {
        if (version !== revision.current) return;
        if (!Array.isArray(next.resources) || !Array.isArray(next.selection))
          throw new Error("资源库暂不可用，请更新本机服务后重试。");
        setData((previous) =>
          options?.reorder ? next : keepResourceOrder(previous, next),
        );
        setError("");
      })
      .catch((e) => {
        if (version === revision.current) setError(errorText(e));
      });
  }, []);
  useEffect(() => {
    const synchronize = () => refresh();
    synchronize();
    const timer = setInterval(synchronize, 5000);
    window.addEventListener("focus", synchronize);
    return () => {
      clearInterval(timer);
      window.removeEventListener("focus", synchronize);
      revision.current++;
    };
  }, [refresh]);
  const change = useCallback(async (input: Change) => {
    if (pending.current) return;
    pending.current = true;
    revision.current++;
    setWorking(true);
    setError("");
    try {
      const next = await api<LibraryView>("library/state", input);
      setData((previous) => keepResourceOrder(previous, next));
    } catch (e) {
      const message = errorText(e);
      setError(message);
      return message;
    } finally {
      pending.current = false;
      setWorking(false);
    }
  }, []);
  return (
    <Context.Provider value={{ data, error, working, refresh, change }}>
      {children}
    </Context.Provider>
  );
}
export function ResourceActions({
  reference,
  disabled = false,
  favorite = false,
}: {
  reference: LibraryReference;
  disabled?: boolean;
  favorite?: boolean;
}) {
  const lib = useLibrary();
  const [failure, setFailure] = useState("");
  useEffect(
    () => setFailure(""),
    [
      reference.spaceId,
      reference.materialId,
      reference.version,
      reference.annotationId,
    ],
  );
  if (!lib) return null;
  async function change(input: Change) {
    setFailure("");
    setFailure((await lib!.change(input)) || "");
  }
  const resource = lib.data?.resources.find((r) =>
    sameReference(r.reference, reference),
  );
  return (
    <span className="resource-actions">
      <button
        className={`button small ${resource?.selected ? "is-selected" : ""}`}
        disabled={lib.working || (disabled && !resource?.selected)}
        aria-pressed={!!resource?.selected}
        onClick={() =>
          void change({
            action: "select",
            reference,
            key: resource?.key,
            enabled: !resource?.selected,
          })
        }
      >
        <Icon name={resource?.selected ? "check" : "plus"} size={14} />
        {resource?.selected ? "已加入选择" : "加入选择"}
      </button>
      {favorite && (
        <button
          className={`button small library-star ${resource?.favorite ? "is-favorite" : ""}`}
          aria-label={resource?.favorite ? "取消收藏" : "收藏"}
          aria-pressed={!!resource?.favorite}
          disabled={lib.working || (disabled && !resource?.favorite)}
          onClick={() =>
            void change({
              action: "favorite",
              reference,
              key: resource?.key,
              enabled: !resource?.favorite,
            })
          }
        >
          <Icon name="star" size={16} />
          {resource?.favorite ? "已收藏" : "收藏"}
        </button>
      )}
      {failure && (
        <span className="resource-action-error" role="alert">
          {failure}
        </span>
      )}
    </span>
  );
}
export const availabilityLabel = (r: LibraryResource) =>
  ({
    available: "",
    offline: "暂时离线",
    withdrawn: "已撤回",
    unavailable: "访问已结束",
  })[r.availability];

// Only the native wrapper defines this narrow bridge; normal WebGUI uses links.
declare global {
  interface Window {
    webkit?: {
      messageHandlers?: {
        teamcross?: {
          postMessage: (body: { action: string; route?: string }) => void;
        };
      };
    };
  }
}
export function openFullLibrary(route = "/library") {
  const bridge = window.webkit?.messageHandlers?.teamcross;
  if (bridge) bridge.postMessage({ action: "open", route });
  else location.hash = route;
}
