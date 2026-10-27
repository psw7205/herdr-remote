import { describe, expect, it } from 'vitest'
import { quoteBlocked, repaint, stepAway } from './TerminalView'

describe('repaint', () => {
  it('carries the reset in the same write as the frame', () => {
    expect(repaint(null, 'one\r\ntwo')).toBe('\x1bcone\r\ntwo\r\x1b[?25l')
  })
  it('skips a frame that did not change', () => {
    expect(repaint('one', 'one')).toBeNull()
  })
  it('repaints a changed frame from a clean screen', () => {
    expect(repaint('one', 'two')).toBe('\x1bctwo\r\x1b[?25l')
    expect(repaint('one', '')).toBe('\x1bc\r\x1b[?25l')
  })
})

describe('stepAway', () => {
  it('moves the cursor to the row above', () => {
    expect(stepAway(39)).toBe('\x1b[39;1H')
    expect(stepAway(1)).toBe('\x1b[1;1H')
  })
  it('moves below on the first row', () => {
    expect(stepAway(0)).toBe('\x1b[2;1H')
  })
})

describe('quoteBlocked', () => {
  it('allows a quote into a Chat with no pending command', () => {
    expect(quoteBlocked({ chat: true }, false)).toBeNull()
  })
  it('blocks while a command waits on delivery, since the Chat would drop the draft', () => {
    expect(quoteBlocked({ chat: true }, true)).toMatch(/확인한 뒤/)
  })
  it('blocks a session without a Chat', () => {
    expect(quoteBlocked({ chat: false }, false)).toMatch(/Chat이 없어/)
  })
})
