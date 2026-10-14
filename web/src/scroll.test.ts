import { describe, expect, it } from 'vitest'
import { afterMessages, GESTURE_TAIL_MS, gestureEnd, gestureStart, initialFollower, isNearBottom, maySnap, nextPinned, scrolled, settled, type Follower } from './scroll'

describe('bottom detection', () => {
  it('treats the last few lines as the bottom', () => {
    expect(isNearBottom({ scrollTop: 900, scrollHeight: 1600, clientHeight: 700 })).toBe(true)
    expect(isNearBottom({ scrollTop: 840, scrollHeight: 1600, clientHeight: 700 })).toBe(true)
    expect(isNearBottom({ scrollTop: 700, scrollHeight: 1600, clientHeight: 700 })).toBe(false)
  })
  it('counts content shorter than the viewport as the bottom', () => {
    expect(isNearBottom({ scrollTop: 0, scrollHeight: 300, clientHeight: 700 })).toBe(true)
  })
})

describe('pinning', () => {
  const at = (scrollTop: number) => ({ scrollTop, scrollHeight: 3000, clientHeight: 700 })
  it('pins whenever the reader reaches the bottom', () => {
    expect(nextPinned(false, 1000, at(2250))).toBe(true)
  })
  it('unpins only when the reader scrolls up', () => {
    expect(nextPinned(true, 2300, at(1900))).toBe(false)
  })
  it('stays pinned while a scroll toward the bottom is still animating', () => {
    expect(nextPinned(true, 1200, at(1600))).toBe(true)
    expect(nextPinned(false, 1200, at(1600))).toBe(false)
  })
  it('keeps a reader pinned through a clamp inside the bottom band', () => {
    // The keyboard closing grows the scroller and the browser clamps scrollTop down.
    expect(nextPinned(true, 2300, at(2270))).toBe(true)
  })
  it('unpins at once when a gesture moves up inside the bottom band', () => {
    expect(nextPinned(true, 2300, at(2290), true)).toBe(false)
    expect(nextPinned(true, 2290, at(2300), true)).toBe(true)
  })
})

describe('follower', () => {
  const at = (scrollTop: number) => ({ scrollTop, scrollHeight: 3000, clientHeight: 700 })
  const atBottom: Follower = { ...initialFollower, previousTop: 2300 }

  it('does not snap while a finger is down, and unpins on the first upward move', () => {
    let state = gestureStart(atBottom)
    expect(maySnap(state, 100)).toBe(false)
    state = scrolled(state, 120, at(2290))
    expect(state.pinned).toBe(false)
    expect(maySnap(state, 130)).toBe(false)
  })

  it('keeps the fling as the gesture while scroll events keep coming', () => {
    let state = scrolled(gestureStart(atBottom), 100, at(2250))
    state = gestureEnd(state, 200)
    state = scrolled(state, 200 + GESTURE_TAIL_MS - 10, at(2100))
    state = scrolled(state, 200 + 2 * GESTURE_TAIL_MS - 20, at(1900))
    expect(state.pinned).toBe(false)
    expect(maySnap(state, 200 + 2 * GESTURE_TAIL_MS - 10)).toBe(false)
    expect(maySnap(settled(state, at(1900)), 200 + 3 * GESTURE_TAIL_MS)).toBe(false)
  })

  it('follows again when the finger lets go inside the bottom band', () => {
    let state = scrolled(gestureStart(atBottom), 100, at(2260))
    expect(state.pinned).toBe(false)
    state = gestureEnd(state, 200)
    expect(maySnap(state, 210)).toBe(false)
    state = settled(state, at(2260))
    expect(state.pinned).toBe(true)
    expect(maySnap(state, 200 + GESTURE_TAIL_MS + 1)).toBe(true)
  })

  it('stays away when the finger lets go above the band', () => {
    let state = scrolled(gestureStart(atBottom), 100, at(1800))
    state = settled(gestureEnd(state, 200), at(1800))
    expect(state.pinned).toBe(false)
  })

  it('treats movement without a gesture as before', () => {
    const clamped = scrolled(atBottom, 100, at(2270))
    expect(clamped.pinned).toBe(true)
    expect(maySnap(clamped, 101)).toBe(true)
    expect(scrolled(atBottom, 100, at(1900)).pinned).toBe(false)
  })

  it('re-pins when a gesture reaches the bottom and snaps once it settles', () => {
    const away: Follower = { pinned: false, previousTop: 1000, gestureUntil: -Infinity }
    let state = scrolled(gestureStart(away), 100, at(2250))
    expect(state.pinned).toBe(true)
    expect(maySnap(state, 110)).toBe(false)
    state = gestureEnd(state, 200)
    expect(maySnap(state, 200 + GESTURE_TAIL_MS + 1)).toBe(true)
  })
})

describe('new messages', () => {
  it('follows new messages only while the reader is at the bottom', () => {
    expect(afterMessages({ pinned: true, unseen: 0 }, 2)).toEqual({ follow: true, unseen: 0 })
    expect(afterMessages({ pinned: false, unseen: 1 }, 2)).toEqual({ follow: false, unseen: 3 })
  })
  it('does not count a replaced or shorter history as unseen', () => {
    expect(afterMessages({ pinned: false, unseen: 2 }, 0)).toEqual({ follow: false, unseen: 2 })
    expect(afterMessages({ pinned: false, unseen: 2 }, -5)).toEqual({ follow: false, unseen: 0 })
  })
})
