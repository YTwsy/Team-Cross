// Leave time for the Core's shortest (5s) cleanup RPC to receive a failure.
export const DEFAULT_CLOSE_TIMEOUT_MS = 4_000;

export function closeDeadline(timeoutMs = DEFAULT_CLOSE_TIMEOUT_MS): number {
  if (!Number.isFinite(timeoutMs) || timeoutMs <= 0) throw new Error("invalid close timeout");
  return Date.now() + timeoutMs;
}

export async function withinCloseDeadline<T>(pending: Promise<T>, deadline: number, waitingFor: string): Promise<T> {
  const remaining = deadline - Date.now();
  const timeoutError = () => new Error(`run close timed out waiting for ${waitingFor}; termination is unconfirmed`);
  let timer: ReturnType<typeof setTimeout> | undefined;
  try {
    return await Promise.race([
      new Promise<never>((_resolve, reject) => {
        if (remaining <= 0) reject(timeoutError());
        else timer = setTimeout(() => reject(timeoutError()), remaining);
      }),
      // Always observe the supplied promise, including when already expired.
      // Its later failure cannot turn a returned timeout into an unhandled rejection.
      pending,
    ]);
  } finally {
    if (timer) clearTimeout(timer);
  }
}
