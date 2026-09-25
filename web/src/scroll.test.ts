import { describe, expect, it } from 'vitest'
import { afterMessages, isNearBottom, nextPinned } from './scroll'

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
