import { describe, expect, it } from 'vitest'
import { repaint, stepAway } from './TerminalView'

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
