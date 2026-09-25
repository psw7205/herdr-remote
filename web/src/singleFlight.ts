// Runs at most one call of `task` at a time. A call made while one is still
// pending is dropped, so slow polls cannot pile up or finish out of order.
export function singleFlight(task: () => Promise<void>): () => Promise<void> {
  let running = false
  return async () => {
    if (running) return
    running = true
    try { await task() } finally { running = false }
  }
}
