import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { startHold } from './holdRepeat'

const DELAY = 400
const INTERVAL = 120

function deferredSend() {
  const pending: Array<(accepted: boolean) => void> = []
  const send = vi.fn(() => new Promise<boolean>(resolve => pending.push(resolve)))
  const settle = (accepted = true) => pending.shift()?.(accepted)
  return { send, settle }
}

describe('startHold', () => {
  beforeEach(() => { vi.useFakeTimers() })
  afterEach(() => { vi.useRealTimers() })

  it('sends nothing on press and exactly once when a tap is released', async () => {
    const send = vi.fn(async () => true)
    const hold = startHold({ send, delay: DELAY, interval: INTERVAL })
    await vi.advanceTimersByTimeAsync(100)
    expect(send).not.toHaveBeenCalled()
    hold.release()
    await vi.advanceTimersByTimeAsync(0)
    expect(send).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(2000)
    expect(send).toHaveBeenCalledTimes(1)
  })

  it('sends nothing when cancelled before the delay', async () => {
    const send = vi.fn(async () => true)
    const hold = startHold({ send, delay: DELAY, interval: INTERVAL })
    await vi.advanceTimersByTimeAsync(100)
    hold.cancel()
    await vi.advanceTimersByTimeAsync(2000)
    expect(send).not.toHaveBeenCalled()
  })

  it('sends first at the delay and then at the interval until released', async () => {
    const send = vi.fn(async () => true)
    const hold = startHold({ send, delay: DELAY, interval: INTERVAL })
    await vi.advanceTimersByTimeAsync(DELAY - 1)
    expect(send).not.toHaveBeenCalled()
    await vi.advanceTimersByTimeAsync(1)
    expect(send).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(INTERVAL - 1)
    expect(send).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(1)
    expect(send).toHaveBeenCalledTimes(2)
    await vi.advanceTimersByTimeAsync(INTERVAL)
    expect(send).toHaveBeenCalledTimes(3)
    hold.release()
    await vi.advanceTimersByTimeAsync(2000)
    expect(send).toHaveBeenCalledTimes(3)
  })

  it('sends nothing more once released during a send in flight', async () => {
    const { send, settle } = deferredSend()
    const hold = startHold({ send, delay: DELAY, interval: INTERVAL })
    await vi.advanceTimersByTimeAsync(DELAY + INTERVAL)
    expect(send).toHaveBeenCalledTimes(1)
    hold.release()
    settle()
    await vi.advanceTimersByTimeAsync(2000)
    expect(send).toHaveBeenCalledTimes(1)
  })

  it('sends nothing more once cancelled during a send in flight', async () => {
    const { send, settle } = deferredSend()
    const hold = startHold({ send, delay: DELAY, interval: INTERVAL })
    await vi.advanceTimersByTimeAsync(DELAY + INTERVAL)
    expect(send).toHaveBeenCalledTimes(1)
    hold.cancel()
    settle()
    await vi.advanceTimersByTimeAsync(2000)
    expect(send).toHaveBeenCalledTimes(1)
  })

  it('waits for a slow send to settle before the next repeat', async () => {
    const { send, settle } = deferredSend()
    startHold({ send, delay: DELAY, interval: INTERVAL })
    await vi.advanceTimersByTimeAsync(DELAY + 1000)
    expect(send).toHaveBeenCalledTimes(1)
    settle()
    await vi.advanceTimersByTimeAsync(0)
    expect(send).toHaveBeenCalledTimes(2)
    await vi.advanceTimersByTimeAsync(500)
    expect(send).toHaveBeenCalledTimes(2)
    settle()
    await vi.advanceTimersByTimeAsync(0)
    expect(send).toHaveBeenCalledTimes(3)
  })

  it('still waits the full interval after a fast send', async () => {
    const { send, settle } = deferredSend()
    startHold({ send, delay: DELAY, interval: INTERVAL })
    await vi.advanceTimersByTimeAsync(DELAY)
    expect(send).toHaveBeenCalledTimes(1)
    settle()
    await vi.advanceTimersByTimeAsync(INTERVAL - 1)
    expect(send).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(1)
    expect(send).toHaveBeenCalledTimes(2)
  })

  it('ends when the first send is not accepted', async () => {
    const send = vi.fn(async () => false)
    startHold({ send, delay: DELAY, interval: INTERVAL })
    await vi.advanceTimersByTimeAsync(DELAY + 2000)
    expect(send).toHaveBeenCalledTimes(1)
  })

  it('ends when a repeat is not accepted', async () => {
    const send = vi.fn(async () => true).mockResolvedValueOnce(true).mockResolvedValueOnce(false)
    startHold({ send, delay: DELAY, interval: INTERVAL })
    await vi.advanceTimersByTimeAsync(DELAY + 2000)
    expect(send).toHaveBeenCalledTimes(2)
  })

  it('ends when a send fails', async () => {
    const send = vi.fn(async () => true).mockRejectedValueOnce(new Error('offline'))
    startHold({ send, delay: DELAY, interval: INTERVAL })
    await vi.advanceTimersByTimeAsync(DELAY + 2000)
    expect(send).toHaveBeenCalledTimes(1)
  })

  it('sends once when released twice', async () => {
    const send = vi.fn(async () => true)
    const immediate = startHold({ send, delay: DELAY, interval: INTERVAL })
    immediate.release()
    immediate.release()
    await vi.advanceTimersByTimeAsync(2000)
    expect(send).toHaveBeenCalledTimes(1)
    const later = startHold({ send, delay: DELAY, interval: INTERVAL })
    later.release()
    await vi.advanceTimersByTimeAsync(0)
    later.release()
    await vi.advanceTimersByTimeAsync(2000)
    expect(send).toHaveBeenCalledTimes(2)
  })

  it('sends nothing when a cancel is followed by a release', async () => {
    const send = vi.fn(async () => true)
    const hold = startHold({ send, delay: DELAY, interval: INTERVAL })
    await vi.advanceTimersByTimeAsync(100)
    hold.cancel()
    hold.release()
    await vi.advanceTimersByTimeAsync(2000)
    expect(send).not.toHaveBeenCalled()
  })
})
