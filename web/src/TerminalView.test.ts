import { describe, expect, it } from 'vitest'
import { repaint } from './TerminalView'

describe('repaint', () => {
  it('carries the reset in the same write as the frame', () => {
    expect(repaint(null, 'one\r\ntwo')).toBe('\x1bcone\r\ntwo')
  })
  it('skips a frame that did not change', () => {
    expect(repaint('one', 'one')).toBeNull()
  })
  it('repaints a changed frame from a clean screen', () => {
    expect(repaint('one', 'two')).toBe('\x1bctwo')
    expect(repaint('one', '')).toBe('\x1bc')
  })
})
