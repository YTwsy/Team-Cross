import { useCallback, useEffect, useRef, useState } from "react";
export class APIError extends Error {
  constructor(
    message: string,
    public code = "operation_failed",
    public recovery = "",
  ) {
    super(recovery ? `${message}。${recovery}` : message);
  }
}
export async function api<T>(
  path: string,
  body?: unknown,
  signal?: AbortSignal,
): Promise<T> {
  let res: Response;
  try {
    res = await fetch(`/api/${path}`, {
      method: body === undefined ? "GET" : "POST",
      headers:
        body === undefined ? undefined : { "Content-Type": "application/json" },
      body: body === undefined ? undefined : JSON.stringify(body),
      signal,
    });
  } catch (e) {
    if (signal?.aborted) throw e;
    throw new APIError(
      "本机服务连接已中断",
      "core_unreachable",
      body === undefined || path.includes("preview")
        ? "请从菜单栏或终端启动 Team Cross 后重新连接"
        : "请重新连接后先查看操作结果，避免重复提交",
    );
  }
  let value;
  try {
    value = await res.json();
  } catch {
    throw new Error("服务暂时不可用，请确认 Team Cross 正在运行后重试。");
  }
  if (!res.ok)
    throw new APIError(
      value.error || `请求失败 (${res.status})`,
      value.code,
      value.recovery,
    );
  return value as T;
}
export const errorText = (e: unknown) =>
  e instanceof Error ? e.message : "操作未完成，请重试。";
export function useResource<T>(
  path: string | null,
  interval = 0,
  refreshKey = 0,
) {
  const [data, setData] = useState<T>();
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  const [revision, setRevision] = useState(0);
  const reload = useCallback(() => setRevision((x) => x + 1), []);
  const lastPath = useRef(path);
  useEffect(() => {
    if (!path) {
      setLoading(false);
      return;
    }
    if (lastPath.current !== path) {
      setData(undefined);
      lastPath.current = path;
    }
    const abort = new AbortController();
    let timer: ReturnType<typeof setTimeout> | undefined;
    setLoading(true);
    async function load() {
      try {
        const next = await api<T>(path!, undefined, abort.signal);
        if (!abort.signal.aborted) {
          setData(next);
          setError("");
        }
      } catch (e) {
        if (!abort.signal.aborted) setError(errorText(e));
      } finally {
        if (!abort.signal.aborted) {
          setLoading(false);
          if (interval) timer = setTimeout(load, interval);
        }
      }
    }
    void load();
    return () => {
      abort.abort();
      clearTimeout(timer);
    };
  }, [path, interval, revision, refreshKey]);
  return { data, error, loading, reload, setData };
}
