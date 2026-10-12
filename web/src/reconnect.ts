export const CLOSE_RETRY_MS = 1200
export const CONNECT_RETRY_MS = 2000

export type ReconnectState = 'idle' | 'scheduled' | 'connecting' | 'connected' | 'cancelled'
type Handle = ReturnType<typeof setTimeout>
export type ReconnectTimers = { set: (run: () => void, ms: number) => Handle; clear: (handle: Handle) => void }

const globalTimers: ReconnectTimers = { set: (run, ms) => setTimeout(run, ms), clear: handle => clearTimeout(handle) }

// A connect stays in flight from start() until connected() or the next
// schedule(), so the socket handshake counts too. A rejected connect retries
// after CONNECT_RETRY_MS; the caller schedules the retry after a close.
export function createReconnector(connect: () => Promise<void>, timers: ReconnectTimers = globalTimers) {
  let state: ReconnectState = 'idle'
  let timer: Handle | undefined
  const clear = () => {
    if (timer === undefined) return
    timers.clear(timer)
    timer = undefined
  }
  const schedule = (ms: number) => {
    if (state === 'cancelled') return
    clear()
    state = 'scheduled'
    timer = timers.set(() => { timer = undefined; void start() }, ms)
  }
  const retry = () => { if (state === 'connecting') schedule(CONNECT_RETRY_MS) }
  const start = async () => {
    if (state === 'connecting' || state === 'connected' || state === 'cancelled') return
    clear()
    state = 'connecting'
    await connect().catch(retry)
  }
  return {
    start,
    schedule,
    wake: () => { if (state === 'scheduled') void start() },
    connected: () => {
      if (state === 'cancelled') return
      clear()
      state = 'connected'
    },
    cancel: () => {
      state = 'cancelled'
      clear()
    },
    state: () => state,
  }
}
