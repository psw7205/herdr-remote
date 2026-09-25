import { describe, expect, it } from 'vitest'
import { dividerLabel, relativeTime } from './time'

const zone = 'Asia/Seoul'
describe('relative time', () => {
  const now = Date.parse('2026-09-25T12:00:00+09:00')
  it('reads recent activity in minutes and hours', () => {
    expect(relativeTime('2026-09-25T11:59:40+09:00', now)).toBe('방금')
    expect(relativeTime('2026-09-25T11:57:00+09:00', now)).toBe('3분 전')
    expect(relativeTime('2026-09-25T09:00:00+09:00', now)).toBe('3시간 전')
  })
  it('switches to days and then to a date', () => {
    expect(relativeTime('2026-09-24T11:00:00+09:00', now)).toBe('어제')
    expect(relativeTime('2026-09-21T12:00:00+09:00', now)).toBe('4일 전')
    expect(relativeTime('2026-08-30T12:00:00+09:00', now, zone)).toBe('8월 30일')
  })
  it('never shows a future or unreadable time as a real one', () => {
    expect(relativeTime('2026-09-25T12:05:00+09:00', now)).toBe('방금')
    expect(relativeTime('not a time', now)).toBe('')
  })
})

describe('time divider', () => {
  it('marks the start of a conversation', () => {
    expect(dividerLabel(null, '2026-09-25T15:42:00+09:00', zone)).toBe('9월 25일 (금) 오후 3:42')
  })
  it('marks a pause of ten minutes or more on the same day with the time only', () => {
    expect(dividerLabel('2026-09-25T15:42:00+09:00', '2026-09-25T15:49:00+09:00', zone)).toBeNull()
    expect(dividerLabel('2026-09-25T15:42:00+09:00', '2026-09-25T15:52:00+09:00', zone)).toBe('오후 3:52')
  })
  it('marks a new day with the date', () => {
    expect(dividerLabel('2026-09-24T23:58:00+09:00', '2026-09-25T00:01:00+09:00', zone)).toBe('9월 25일 (금) 오전 12:01')
  })
})
