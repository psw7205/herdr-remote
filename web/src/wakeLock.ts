import { useEffect, useRef } from 'react'

export type WakeLockSentinelLike = {
  readonly released: boolean
  release(): Promise<void>
  addEventListener(type: 'release', listener: () => void): void
}
export type WakeLockLike = { request(type: 'screen'): Promise<WakeLockSentinelLike> }
export type PageVisibility = {
  readonly visibilityState: DocumentVisibilityState
  addEventListener(type: 'visibilitychange', listener: () => void): void
  removeEventListener(type: 'visibilitychange', listener: () => void): void
}
export type ScreenWakeLock = { set(wanted: boolean): void; dispose(): void }

// The browser drops a screen lock whenever the page is hidden, so it is taken
// again each time the page becomes visible while still wanted. A request that
// fails (no permission, battery saver) is left alone until the next change.
export function createScreenWakeLock(wakeLock: WakeLockLike | undefined, page: PageVisibility): ScreenWakeLock {
  let wanted = false
  let disposed = false
  let held: WakeLockSentinelLike | null = null
  let requesting = false

  const sync = () => {
    if (!wanted || disposed || page.visibilityState !== 'visible') {
      const sentinel = held
      held = null
      if (sentinel && !sentinel.released) sentinel.release().catch(() => {})
      return
    }
    if (!wakeLock || held || requesting) return
    requesting = true
    wakeLock.request('screen').then(sentinel => {
      requesting = false
      if (sentinel.released) return
      sentinel.addEventListener('release', () => { if (held === sentinel) held = null })
      held = sentinel
      sync()
    }, () => { requesting = false })
  }

  page.addEventListener('visibilitychange', sync)
  return {
    set(next) { wanted = next; sync() },
    dispose() { disposed = true; page.removeEventListener('visibilitychange', sync); sync() },
  }
}

function browserWakeLock(): WakeLockLike | undefined {
  try { return typeof navigator !== 'undefined' && 'wakeLock' in navigator ? navigator.wakeLock : undefined } catch { return undefined }
}

export function useScreenWakeLock(wanted: boolean) {
  const lock = useRef<ScreenWakeLock | null>(null)
  useEffect(() => {
    const created = createScreenWakeLock(browserWakeLock(), document)
    lock.current = created
    return () => { created.dispose(); if (lock.current === created) lock.current = null }
  }, [])
  useEffect(() => { lock.current?.set(wanted) }, [wanted])
}
