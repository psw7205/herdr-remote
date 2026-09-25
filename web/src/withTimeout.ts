export class TimeoutError extends Error {
  constructor(ms: number) { super(`timed out after ${ms}ms`); this.name = 'TimeoutError' }
}

// Runs `task` with a signal that aborts after `ms` or when `parent` aborts.
// The returned promise settles on abort even if the task ignores the signal,
// so a late result from an abandoned call is never delivered.
export function withTimeout<T>(task: (signal: AbortSignal) => Promise<T>, ms: number, parent?: AbortSignal): Promise<T> {
  const controller = new AbortController()
  let timer: ReturnType<typeof setTimeout> | undefined
  let onParentAbort: (() => void) | undefined
  const aborted = new Promise<never>((_, reject) => {
    const fail = (reason: unknown) => { controller.abort(reason); reject(reason) }
    timer = setTimeout(() => fail(new TimeoutError(ms)), ms)
    onParentAbort = () => fail(parent?.reason ?? new DOMException('Aborted', 'AbortError'))
    if (parent?.aborted) onParentAbort()
    else parent?.addEventListener('abort', onParentAbort, { once: true })
  })
  return Promise.race([task(controller.signal), aborted]).finally(() => {
    clearTimeout(timer)
    if (onParentAbort) parent?.removeEventListener('abort', onParentAbort)
  })
}
