import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { CLOSE_RETRY_MS, CONNECT_RETRY_MS, createReconnector } from './reconnect'

beforeEach(() => { vi.useFakeTimers() })
afterEach(() => { vi.useRealTimers() })

const deferred = () => {
  let resolve = () => {}
  let reject = (_: Error) => {}
  const promise = new Promise<void>((ok, fail) => { resolve = ok; reject = fail })
  return { promise, resolve, reject }
}

describe('reconnector', () => {
  it('connects at once on wake and lets the pending timer fire nothing more', async () => {
    const connect = vi.fn(async () => {})
    const reconnect = createReconnector(connect)
    reconnect.schedule(CLOSE_RETRY_MS)
    expect(reconnect.state()).toBe('scheduled')
    reconnect.wake()
    expect(connect).toHaveBeenCalledTimes(1)
    expect(reconnect.state()).toBe('connecting')
    await vi.advanceTimersByTimeAsync(CLOSE_RETRY_MS * 2)
    expect(connect).toHaveBeenCalledTimes(1)
  })

  it('fires a pending reconnect once when two wakes arrive in the same tick', () => {
    const connect = vi.fn(async () => {})
    const reconnect = createReconnector(connect)
    reconnect.schedule(CLOSE_RETRY_MS)
    reconnect.wake()
    reconnect.wake()
    expect(connect).toHaveBeenCalledTimes(1)
  })

  it('ignores wake and start while a connect is in flight', async () => {
    const attempt = deferred()
    const connect = vi.fn(() => attempt.promise)
    const reconnect = createReconnector(connect)
    const first = reconnect.start()
    reconnect.wake()
    await reconnect.start()
    expect(connect).toHaveBeenCalledTimes(1)
    attempt.resolve()
    await first
    reconnect.wake()
    expect(connect).toHaveBeenCalledTimes(1)
    expect(reconnect.state()).toBe('connecting')
  })

  it('ignores wake and start while connected', async () => {
    const connect = vi.fn(async () => {})
    const reconnect = createReconnector(connect)
    await reconnect.start()
    reconnect.connected()
    reconnect.wake()
    await reconnect.start()
    await vi.advanceTimersByTimeAsync(CONNECT_RETRY_MS * 2)
    expect(connect).toHaveBeenCalledTimes(1)
    expect(reconnect.state()).toBe('connected')
  })

  it('retries a failed connect after the longer delay', async () => {
    const connect = vi.fn(async () => { throw new Error('offline') })
    const reconnect = createReconnector(connect)
    await reconnect.start()
    expect(reconnect.state()).toBe('scheduled')
    await vi.advanceTimersByTimeAsync(CONNECT_RETRY_MS - 1)
    expect(connect).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(1)
    expect(connect).toHaveBeenCalledTimes(2)
  })

  it('replaces an earlier timer so one schedule fires once', async () => {
    const connect = vi.fn(async () => {})
    const reconnect = createReconnector(connect)
    reconnect.schedule(CLOSE_RETRY_MS)
    reconnect.schedule(0)
    await vi.advanceTimersByTimeAsync(0)
    expect(connect).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(CLOSE_RETRY_MS * 2)
    expect(connect).toHaveBeenCalledTimes(1)
  })

  it('stops all connects after cancel, including a retry from a failure in flight', async () => {
    const connect = vi.fn(async () => {})
    const reconnect = createReconnector(connect)
    reconnect.schedule(CLOSE_RETRY_MS)
    reconnect.cancel()
    await vi.advanceTimersByTimeAsync(CLOSE_RETRY_MS * 2)
    reconnect.wake()
    reconnect.schedule(0)
    await reconnect.start()
    await vi.advanceTimersByTimeAsync(0)
    expect(connect).not.toHaveBeenCalled()

    const attempt = deferred()
    const failing = vi.fn(() => attempt.promise)
    const other = createReconnector(failing)
    const running = other.start()
    other.cancel()
    attempt.reject(new Error('offline'))
    await running
    await vi.advanceTimersByTimeAsync(CONNECT_RETRY_MS * 2)
    expect(failing).toHaveBeenCalledTimes(1)
    expect(other.state()).toBe('cancelled')
  })

  it('uses injected timers', () => {
    const pending: Array<() => void> = []
    const timers = { set: vi.fn((run: () => void) => { pending.push(run); return 0 }), clear: vi.fn() }
    const connect = vi.fn(async () => {})
    const reconnect = createReconnector(connect, timers)
    reconnect.schedule(0)
    expect(timers.set).toHaveBeenCalledWith(expect.any(Function), 0)
    pending[0]()
    expect(connect).toHaveBeenCalledTimes(1)
  })
})
