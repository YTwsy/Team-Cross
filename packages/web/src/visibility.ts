// An inactive window may still be visible. Only the DOM and native window
// visibility affect background reads; focus is an explicit refresh opportunity.
export function isWindowVisible() {
  return (
    !document.hidden &&
    (!document.querySelector('meta[name="teamcross-desktop"]') ||
      document.documentElement.dataset.teamcrossVisible === "true")
  );
}

export function watchVisibleRefresh(
  refresh: (signal: AbortSignal) => Promise<void>,
  interval: number,
  immediate = true,
) {
  let active = true;
  let visible = isWindowVisible();
  let request: AbortController | undefined;
  let timer: ReturnType<typeof setTimeout> | undefined;
  const stopRead = () => {
    clearTimeout(timer);
    request?.abort();
    request = undefined;
  };
  const run = async () => {
    if (!active || request || !isWindowVisible()) return;
    clearTimeout(timer);
    const current = new AbortController();
    request = current;
    try {
      await refresh(current.signal);
    } finally {
      if (request === current) {
        request = undefined;
        if (active && isWindowVisible() && interval)
          timer = setTimeout(() => void run(), interval);
      }
    }
  };
  const changed = () => {
    const next = isWindowVisible();
    if (next === visible) return;
    visible = next;
    if (next) void run();
    else stopRead();
  };
  const focus = () => void run();
  document.addEventListener("visibilitychange", changed);
  document.addEventListener("teamcross-visibility", changed);
  window.addEventListener("focus", focus);
  if (immediate) void run();
  else if (visible && interval) timer = setTimeout(() => void run(), interval);
  return () => {
    active = false;
    stopRead();
    document.removeEventListener("visibilitychange", changed);
    document.removeEventListener("teamcross-visibility", changed);
    window.removeEventListener("focus", focus);
  };
}
