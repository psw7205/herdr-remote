import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { singleFlight } from './singleFlight'
import { TimeoutError, withTimeout } from './withTimeout'

describe('withTimeout', () => {
  beforeEach(() => { vi.useFakeTimers() })
  afterEach(() => { vi.useRealTimers() })

  it('returns a result that arrives before the timeout and clears the timer', async () => {
    let seen: AbortSignal | undefined
    const result = withTimeout(async signal => { seen = signal; return 'frame' }, 6000)
    await expect(result).resolves.toBe('frame')
    expect(vi.getTimerCount()).toBe(0)
    expect(seen?.aborted).toBe(false)
  })

  it('aborts a stalled task and never delivers its late result', async () => {
    let seen: AbortSignal | undefined
    let finish: (value: string) => void = () => {}
    const result = withTimeout(signal => { seen = signal; return new Promise<string>(resolve => { finish = resolve }) }, 6000)
    const outcome = expect(result).rejects.toBeInstanceOf(TimeoutError)
    await vi.advanceTimersByTimeAsync(5999)
    expect(seen?.aborted).toBe(false)
    await vi.advanceTimersByTimeAsync(1)
    await outcome
    expect(seen?.aborted).toBe(true)
    finish('stale')
    await expect(result).rejects.toBeInstanceOf(TimeoutError)
  })

  it('aborts when the parent signal aborts', async () => {
    const parent = new AbortController()
    let seen: AbortSignal | undefined
    const result = withTimeout(signal => { seen = signal; return new Promise<never>(() => {}) }, 6000, parent.signal)
    parent.abort()
    await expect(result).rejects.toThrow()
    expect(seen?.aborted).toBe(true)
    expect(vi.getTimerCount()).toBe(0)
  })

  it('releases the single-flight guard after a timeout so the next poll runs', async () => {
    let calls = 0
    const poll = singleFlight(async () => {
      calls++
      await withTimeout(() => new Promise<never>(() => {}), 6000).catch(() => {})
    })
    const first = poll()
    await poll()
    expect(calls).toBe(1)
    await vi.advanceTimersByTimeAsync(6000)
    await first
    void poll()
    expect(calls).toBe(2)
  })
})
