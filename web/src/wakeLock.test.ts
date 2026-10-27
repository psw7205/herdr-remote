import { describe, expect, it } from 'vitest'
import { createScreenWakeLock, type PageVisibility, type WakeLockSentinelLike } from './wakeLock'

class FakeSentinel implements WakeLockSentinelLike {
  released = false
  releases = 0
  private listeners: Array<() => void> = []
  addEventListener(_type: 'release', listener: () => void) { this.listeners.push(listener) }
  async release() { this.releases++; this.revoke() }
  revoke() {
    if (this.released) return
    this.released = true
    for (const listener of this.listeners) listener()
  }
}

function fakeWakeLock() {
  const requests: Array<{ resolve: (sentinel: FakeSentinel) => void; reject: (error: Error) => void }> = []
  const sentinels: FakeSentinel[] = []
  return {
    requests,
    sentinels,
    api: {
      request(type: 'screen') {
        expect(type).toBe('screen')
        return new Promise<FakeSentinel>((resolve, reject) => { requests.push({ resolve, reject }) })
      },
    },
    grant(index = requests.length - 1) {
      const sentinel = new FakeSentinel()
      sentinels.push(sentinel)
      requests[index].resolve(sentinel)
      return sentinel
    },
  }
}

function fakePage(state: DocumentVisibilityState = 'visible') {
  const listeners = new Set<() => void>()
  const page: PageVisibility & { visibilityState: DocumentVisibilityState; listeners: Set<() => void>; show(next: DocumentVisibilityState): void } = {
    visibilityState: state,
    listeners,
    addEventListener: (_type, listener) => { listeners.add(listener) },
    removeEventListener: (_type, listener) => { listeners.delete(listener) },
    show(next) { page.visibilityState = next; for (const listener of [...listeners]) listener() },
  }
  return page
}

const settle = () => new Promise(done => setTimeout(done))

describe('createScreenWakeLock', () => {
  it('requests nothing until it is wanted', () => {
    const lock = fakeWakeLock()
    createScreenWakeLock(lock.api, fakePage())
    expect(lock.requests).toHaveLength(0)
  })

  it('holds one lock while wanted and releases it when no longer wanted', async () => {
    const lock = fakeWakeLock()
    const screen = createScreenWakeLock(lock.api, fakePage())
    screen.set(true)
    screen.set(true)
    expect(lock.requests).toHaveLength(1)
    const sentinel = lock.grant()
    await settle()
    screen.set(true)
    expect(lock.requests).toHaveLength(1)
    screen.set(false)
    expect(sentinel.releases).toBe(1)
  })

  it('releases a lock granted after it stopped being wanted', async () => {
    const lock = fakeWakeLock()
    const screen = createScreenWakeLock(lock.api, fakePage())
    screen.set(true)
    screen.set(false)
    const late = lock.grant()
    await settle()
    expect(late.releases).toBe(1)
  })

  it('releases a lock granted after the page was hidden', async () => {
    const lock = fakeWakeLock()
    const page = fakePage()
    const screen = createScreenWakeLock(lock.api, page)
    screen.set(true)
    page.show('hidden')
    const late = lock.grant()
    await settle()
    expect(late.releases).toBe(1)
  })

  it('takes the lock again when the page returns after the browser dropped it', async () => {
    const lock = fakeWakeLock()
    const page = fakePage()
    const screen = createScreenWakeLock(lock.api, page)
    screen.set(true)
    const first = lock.grant()
    await settle()
    first.revoke()
    page.show('hidden')
    expect(lock.requests).toHaveLength(1)
    page.show('visible')
    expect(lock.requests).toHaveLength(2)
    lock.grant()
    await settle()
    expect(lock.sentinels[1].released).toBe(false)
  })

  it('waits for the page to be visible before requesting', () => {
    const lock = fakeWakeLock()
    const page = fakePage('hidden')
    createScreenWakeLock(lock.api, page).set(true)
    expect(lock.requests).toHaveLength(0)
    page.show('visible')
    expect(lock.requests).toHaveLength(1)
  })

  it('swallows a refused request and tries again on the next change', async () => {
    const lock = fakeWakeLock()
    const page = fakePage()
    const screen = createScreenWakeLock(lock.api, page)
    screen.set(true)
    lock.requests[0].reject(new Error('NotAllowedError'))
    await settle()
    page.show('hidden')
    page.show('visible')
    expect(lock.requests).toHaveLength(2)
  })

  it('does nothing without the Wake Lock API', () => {
    const page = fakePage()
    const screen = createScreenWakeLock(undefined, page)
    expect(() => { screen.set(true); page.show('hidden'); page.show('visible'); screen.dispose() }).not.toThrow()
  })

  it('releases and stops listening on dispose', async () => {
    const lock = fakeWakeLock()
    const page = fakePage()
    const screen = createScreenWakeLock(lock.api, page)
    screen.set(true)
    const sentinel = lock.grant()
    await settle()
    screen.dispose()
    expect(sentinel.releases).toBe(1)
    expect(page.listeners.size).toBe(0)
    screen.set(true)
    expect(lock.requests).toHaveLength(1)
  })
})
